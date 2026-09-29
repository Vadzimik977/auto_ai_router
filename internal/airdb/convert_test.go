package airdb

import (
	"testing"

	"github.com/mixaill76/auto_ai_router/internal/config"
)

func TestConvertCredential(t *testing.T) {
	t.Setenv("KEY_ALIBABA", "sk-live-secret")

	cfg, ok := convertCredential(credentialRow{
		Name:             "alibabacloud-ru-global",
		Type:             "openai",
		APIKeyEnv:        "os.environ/KEY_ALIBABA",
		BaseURL:          "https://ali-dashscope.vsellm.ru/compatible-mode",
		Weight:           20,
		Priority:         50,
		FallbackPriority: 3,
		IsFallback:       true,
		RPM:              -1,
		TPM:              250000,
		Scopes:           []string{"s1", "s2"},
		DeniedScopes:     []string{"s3"},
		ReasoningOnly:    true,
	})
	if !ok {
		t.Fatal("expected credential to convert")
	}
	if cfg.APIKey != "sk-live-secret" {
		t.Errorf("env reference not resolved: %q", cfg.APIKey)
	}
	if cfg.Weight != 20 || cfg.Priority != 50 || cfg.FallbackPriority != 3 || !cfg.IsFallback {
		t.Errorf("balancing fields lost: %+v", cfg)
	}
	if cfg.RPM != -1 || cfg.TPM != 250000 {
		t.Errorf("limits lost: rpm=%d tpm=%d", cfg.RPM, cfg.TPM)
	}
	if len(cfg.Scopes) != 2 || len(cfg.DeniedScopes) != 1 {
		t.Errorf("scopes lost: %+v", cfg)
	}
	if !cfg.ReasoningOnly {
		t.Error("reasoning_only lost")
	}
	if cfg.Type != config.ProviderTypeOpenAI {
		t.Errorf("type: %q", cfg.Type)
	}
}

func TestConvertCredentialLiteralKeyWins(t *testing.T) {
	cfg, ok := convertCredential(credentialRow{
		Name:      "local-test",
		Type:      "openai",
		APIKey:    "literal",
		APIKeyEnv: "os.environ/MISSING_ENV_VAR",
	})
	if !ok {
		t.Fatal("expected credential to convert")
	}
	if cfg.APIKey != "literal" {
		t.Errorf("literal api_key should win over env reference, got %q", cfg.APIKey)
	}
}

func TestConvertCredentialUnsupportedTypeSkipped(t *testing.T) {
	_, ok := convertCredential(credentialRow{Name: "weird", Type: "some-unknown-provider"})
	if ok {
		t.Error("unknown provider type must be skipped")
	}
}

func TestConvertCredentialMissingEnvKeepsReference(t *testing.T) {
	cfg, ok := convertCredential(credentialRow{
		Name:      "no-env",
		Type:      "proxy",
		APIKeyEnv: "os.environ/NEVER_SET",
	})
	if !ok {
		t.Fatal("expected proxy credential to convert")
	}
	if cfg.APIKey != "" {
		t.Errorf("unresolved env reference should yield empty key, got %q", cfg.APIKey)
	}
	if !cfg.Type.IsProxyLike() {
		t.Errorf("proxy type must be proxy-like: %q", cfg.Type)
	}
}

func TestConvertModel(t *testing.T) {
	m := convertModel(modelRow{
		Name:          "qwen3-max",
		Credential:    "alibaba",
		Model:         "qwen-max",
		RPM:           600,
		TPM:           1000000,
		Weight:        2,
		DefaultParams: []byte(`{"temperature":0.7}`),
	})
	if m.Name != "qwen3-max" || m.Model != "qwen-max" || m.Credential != "alibaba" {
		t.Errorf("identity fields: %+v", m)
	}
	if m.RPM != 600 || m.TPM != 1000000 || m.Weight != 2 {
		t.Errorf("limits/weight: %+v", m)
	}
	if m.DefaultParams["temperature"] != 0.7 {
		t.Errorf("default_params: %+v", m.DefaultParams)
	}
}

func TestConvertModelDefaultsToName(t *testing.T) {
	m := convertModel(modelRow{Name: "plain", Credential: "c"})
	if m.Model != "" {
		t.Errorf("same-name model should keep Model empty, got %q", m.Model)
	}
}

func TestFilterStaticCredentials(t *testing.T) {
	static := []config.CredentialConfig{
		{Name: "static-only"},
		{Name: "shared"},
	}
	db := []config.CredentialConfig{{Name: "shared"}}

	kept, overridden := FilterStaticCredentials(static, db)
	if len(kept) != 1 || kept[0].Name != "static-only" {
		t.Errorf("kept: %+v", kept)
	}
	if !overridden["shared"] || len(overridden) != 1 {
		t.Errorf("overridden: %+v", overridden)
	}
}
