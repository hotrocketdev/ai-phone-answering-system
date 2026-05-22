package config

import (
	"os"
	"testing"
)

func TestLoad_RequiresOpenAIKey(t *testing.T) {
	os.Unsetenv("OPENAI_API_KEY")
	os.Setenv("HMAC_SECRET", "test-secret")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for missing OPENAI_API_KEY")
	}
}

func TestLoad_RequiresHMACSecret(t *testing.T) {
	os.Setenv("OPENAI_API_KEY", "sk-test")
	os.Unsetenv("HMAC_SECRET")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for missing HMAC_SECRET")
	}
}

func TestLoad_ValidConfig(t *testing.T) {
	os.Setenv("OPENAI_API_KEY", "sk-test")
	os.Setenv("HMAC_SECRET", "test-secret")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Port != 8080 {
		t.Errorf("expected port 8080, got %d", cfg.Port)
	}
}

func TestLoad_CustomPort(t *testing.T) {
	os.Setenv("OPENAI_API_KEY", "sk-test")
	os.Setenv("HMAC_SECRET", "test-secret")
	os.Setenv("GATEWAY_PORT", "9090")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Port != 9090 {
		t.Errorf("expected port 9090, got %d", cfg.Port)
	}
}

func TestLoad_InvalidPort(t *testing.T) {
	os.Setenv("OPENAI_API_KEY", "sk-test")
	os.Setenv("HMAC_SECRET", "test-secret")
	os.Setenv("GATEWAY_PORT", "99999")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for invalid port")
	}
}
