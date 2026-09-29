// Package airdb loads credentials and models from a native Postgres source
// whose tables mirror the services jsonnet format (air_credentials /
// air_models). Unlike litellm_db, API keys are stored only as "os.environ/KEY"
// references and all balancing fields (weight, priority, fallback_priority,
// is_fallback) round-trip without loss. See docs/air-db.md.
package airdb

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mixaill76/auto_ai_router/internal/config"
)

// Config holds pool and module settings for the air_db source.
type Config struct {
	DatabaseURL    string
	MaxConns       int32
	MinConns       int32
	ConnectTimeout time.Duration
	Logger         *slog.Logger
}

// Manager is the air_db module surface used by cmd/server.
type Manager interface {
	IsEnabled() bool
	IsHealthy() bool
	// Fetch loads all credentials and models in one snapshot. It returns
	// zero-length slices (never nil) on success so callers can diff freely.
	Fetch(ctx context.Context) ([]config.CredentialConfig, []config.ModelRPMConfig, error)
	Shutdown(ctx context.Context) error
}

// NoopManager is returned when air_db is disabled.
type NoopManager struct{}

func (n NoopManager) IsEnabled() bool                { return false }
func (n NoopManager) IsHealthy() bool                { return false }
func (n NoopManager) Shutdown(context.Context) error { return nil }
func (n NoopManager) Fetch(context.Context) ([]config.CredentialConfig, []config.ModelRPMConfig, error) {
	return nil, nil, nil
}

// DefaultManager is the real implementation backed by a pgx connection pool.
type DefaultManager struct {
	pool   *pgxpool.Pool
	config *Config
	logger *slog.Logger
}

// New creates a DefaultManager and establishes the connection pool. A failed
// initial ping is NOT fatal here: the sync loop treats a broken database as a
// transient condition and retries on the next tick (matching litellm_db).
func New(cfg *Config) (*DefaultManager, error) {
	ctx, cancel := context.WithTimeout(context.Background(), cfg.ConnectTimeout)
	defer cancel()

	poolCfg, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}
	if cfg.MaxConns > 0 {
		poolCfg.MaxConns = cfg.MaxConns
	}
	if cfg.MinConns > 0 {
		poolCfg.MinConns = cfg.MinConns
	}
	poolCfg.MaxConnLifetime = 30 * time.Minute
	poolCfg.MaxConnIdleTime = 5 * time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return &DefaultManager{pool: pool, config: cfg, logger: cfg.Logger}, nil
}

// IsEnabled always reports true for a constructed DefaultManager.
func (m *DefaultManager) IsEnabled() bool { return true }

// IsHealthy pings the database (used by the health checker).
func (m *DefaultManager) IsHealthy() bool {
	ctx, cancel := context.WithTimeout(context.Background(), m.config.ConnectTimeout)
	defer cancel()
	return m.pool.Ping(ctx) == nil
}

// Fetch reads air_credentials and air_models inside one READ COMMITTED
// transaction so the returned snapshot corresponds to one point in time.
func (m *DefaultManager) Fetch(ctx context.Context) ([]config.CredentialConfig, []config.ModelRPMConfig, error) {
	conn, err := m.pool.Acquire(ctx)
	if err != nil {
		return nil, nil, err
	}
	defer conn.Release()

	tx, err := conn.Begin(ctx)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	creds, err := fetchCredentials(ctx, tx)
	if err != nil {
		return nil, nil, err
	}
	models, err := fetchModels(ctx, tx)
	if err != nil {
		return nil, nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, nil, err
	}

	airCreds := make([]config.CredentialConfig, 0, len(creds))
	for i := range creds {
		cfg, ok := convertCredential(creds[i])
		if !ok {
			m.logger.Warn("air_db: skipping credential with unsupported provider",
				"credential", creds[i].Name)
			continue
		}
		airCreds = append(airCreds, cfg)
	}

	airModels := make([]config.ModelRPMConfig, 0, len(models))
	for i := range models {
		airModels = append(airModels, convertModel(models[i]))
	}

	m.logger.Debug("air_db fetch completed", "credentials", len(airCreds), "models", len(airModels))
	return airCreds, airModels, nil
}

// Shutdown closes the connection pool.
func (m *DefaultManager) Shutdown(ctx context.Context) error {
	m.pool.Close()
	return nil
}
