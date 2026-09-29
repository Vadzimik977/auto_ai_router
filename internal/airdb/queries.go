package airdb

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// credentialRow mirrors one air_credentials row (see docs/air-db.md).
type credentialRow struct {
	Name             string
	Type             string
	APIKeyEnv        string
	APIKey           string
	BaseURL          string
	ProxyURL         string
	Weight           int
	Priority         int
	FallbackPriority int // 0 = unset
	IsFallback       bool
	RPM              int
	TPM              int
	Scopes           []string
	DeniedScopes     []string
	ReasoningOnly    bool
	ProjectID        string
	Location         string
	CredentialsFile  string
	CredentialsJSON  string
}

// modelRow mirrors one air_models row.
type modelRow struct {
	Name                 string
	Credential           string
	Model                string
	RPM                  int
	TPM                  int
	Weight               int
	PassthroughResponses *bool
	WebSocketResponses   bool
	PassthroughMessages  *bool
	DefaultParams        []byte // jsonb
}

const queryCredentials = `
SELECT name, type,
       COALESCE(api_key_env, ''),
       COALESCE(api_key, ''),
       COALESCE(base_url, ''),
       COALESCE(proxy_url, ''),
       COALESCE(weight, 1),
       COALESCE(priority, 0),
       COALESCE(fallback_priority, 0),
       COALESCE(is_fallback, false),
       COALESCE(NULLIF(rpm, 0), -1),
       COALESCE(NULLIF(tpm, 0), -1),
       COALESCE(scopes, '{}'),
       COALESCE(denied_scopes, '{}'),
       COALESCE(reasoning_only, false),
       COALESCE(project_id, ''),
       COALESCE(location, ''),
       COALESCE(credentials_file, ''),
       COALESCE(credentials_json, '')
FROM air_credentials`

const queryModels = `
SELECT name, credential,
       COALESCE(model, name),
       COALESCE(NULLIF(rpm, 0), -1),
       COALESCE(NULLIF(tpm, 0), -1),
       COALESCE(weight, 0),
       passthrough_responses,
       COALESCE(websocket_responses, false),
       passthrough_messages,
       default_params
FROM air_models`

func fetchCredentials(ctx context.Context, tx pgx.Tx) ([]credentialRow, error) {
	rows, err := tx.Query(ctx, queryCredentials)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []credentialRow
	for rows.Next() {
		var r credentialRow
		if err := rows.Scan(
			&r.Name, &r.Type,
			&r.APIKeyEnv, &r.APIKey,
			&r.BaseURL, &r.ProxyURL,
			&r.Weight, &r.Priority, &r.FallbackPriority,
			&r.IsFallback, &r.RPM, &r.TPM,
			&r.Scopes, &r.DeniedScopes, &r.ReasoningOnly,
			&r.ProjectID, &r.Location,
			&r.CredentialsFile, &r.CredentialsJSON,
		); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func fetchModels(ctx context.Context, tx pgx.Tx) ([]modelRow, error) {
	rows, err := tx.Query(ctx, queryModels)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []modelRow
	for rows.Next() {
		var r modelRow
		if err := rows.Scan(
			&r.Name, &r.Credential,
			&r.Model, &r.RPM, &r.TPM, &r.Weight,
			&r.PassthroughResponses, &r.WebSocketResponses,
			&r.PassthroughMessages, &r.DefaultParams,
		); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
