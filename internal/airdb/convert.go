package airdb

import (
	"encoding/json"
	"os"
	"strings"

	"github.com/mixaill76/auto_ai_router/internal/config"
)

// convertCredential maps one air_credentials row to CredentialConfig. ok=false
// when the provider type is unknown (the row is skipped, mirroring the
// litellm_db loader's "unsupported provider" behaviour).
func convertCredential(r credentialRow) (config.CredentialConfig, bool) {
	cfg := config.CredentialConfig{
		Name:             r.Name,
		Type:             config.NormalizeProviderType(r.Type),
		BaseURL:          r.BaseURL,
		ProxyURL:         r.ProxyURL,
		RPM:              r.RPM,
		TPM:              r.TPM,
		Weight:           r.Weight,
		Priority:         r.Priority,
		FallbackPriority: r.FallbackPriority,
		IsFallback:       r.IsFallback,
		ReasoningOnly:    r.ReasoningOnly,
		Scopes:           r.Scopes,
		DeniedScopes:     r.DeniedScopes,
		ProjectID:        r.ProjectID,
		Location:         r.Location,
		CredentialsFile:  r.CredentialsFile,
		CredentialsJSON:  r.CredentialsJSON,
	}
	if !cfg.Type.IsValid() {
		return config.CredentialConfig{}, false
	}

	// API key: the jsonnet format stores "os.environ/KEY" references, never
	// values (see docs/air-db.md). A literal api_key column is meant for local
	// throwaway keys only and wins over the env reference when both are set.
	if r.APIKey != "" {
		cfg.APIKey = r.APIKey
	} else if r.APIKeyEnv != "" {
		cfg.APIKey = resolveEnvOrEmpty(r.APIKeyEnv)
	}
	return cfg, true
}

// convertModel maps one air_models row to ModelRPMConfig. The real provider
// model name defaults to the exposed name (same "alias != name" semantics as
// config.yaml models[].model).
func convertModel(r modelRow) config.ModelRPMConfig {
	out := config.ModelRPMConfig{
		Name:                 r.Name,
		Model:                r.Model,
		RPM:                  r.RPM,
		TPM:                  r.TPM,
		Weight:               r.Weight,
		Credential:           r.Credential,
		WebSocketResponses:   r.WebSocketResponses,
		PassthroughResponses: r.PassthroughResponses,
		PassthroughMessages:  r.PassthroughMessages,
	}
	if out.Model == "" || out.Model == out.Name {
		out.Model = ""
	}
	if len(r.DefaultParams) > 0 {
		var params map[string]any
		if err := json.Unmarshal(r.DefaultParams, &params); err == nil && len(params) > 0 {
			out.DefaultParams = params
		}
	}
	return out
}

// resolveEnvOrEmpty resolves "os.environ/NAME" references the same way the
// config loader does; anything else (including an unresolved reference) is
// returned unchanged so callers can rely on one rule.
func resolveEnvOrEmpty(value string) string {
	const prefix = "os.environ/"
	if strings.HasPrefix(value, prefix) {
		return os.Getenv(strings.TrimPrefix(value, prefix))
	}
	return value
}

// FilterStaticCredentials removes static (YAML) credentials that air_db also
// declares, implementing the "priority" semantics: the database entry replaces
// the static one instead of duplicating it. Returns the kept list and the
// names that were dropped because the database overrides them.
func FilterStaticCredentials(static []config.CredentialConfig, db []config.CredentialConfig) ([]config.CredentialConfig, map[string]bool) {
	dbNames := make(map[string]bool, len(db))
	for _, c := range db {
		dbNames[c.Name] = true
	}
	overridden := make(map[string]bool)
	kept := make([]config.CredentialConfig, 0, len(static))
	for _, c := range static {
		if dbNames[c.Name] {
			overridden[c.Name] = true
			continue
		}
		kept = append(kept, c)
	}
	return kept, overridden
}
