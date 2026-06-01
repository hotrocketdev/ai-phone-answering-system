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
	os.Unsetenv("OPENAI_MANUAL_TURN_FALLBACK")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Port != 8080 {
		t.Errorf("expected port 8080, got %d", cfg.Port)
	}
	if cfg.OpenAIManualTurnFallback {
		t.Errorf("expected manual turn fallback disabled by default")
	}
}

func TestLoad_ManualTurnFallbackOptIn(t *testing.T) {
	os.Setenv("OPENAI_API_KEY", "sk-test")
	os.Setenv("HMAC_SECRET", "test-secret")
	os.Setenv("OPENAI_MANUAL_TURN_FALLBACK", "true")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.OpenAIManualTurnFallback {
		t.Errorf("expected manual turn fallback enabled when explicitly configured")
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

func TestLoad_CartesiaRequiresAPIKey(t *testing.T) {
	os.Setenv("OPENAI_API_KEY", "sk-test")
	os.Setenv("HMAC_SECRET", "test-secret")
	os.Setenv("VOICE_RENDERER", "cartesia")
	os.Unsetenv("CARTESIA_API_KEY")
	os.Setenv("CARTESIA_VOICE_ID", "test-voice")
	os.Unsetenv("GATEWAY_PORT")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error: cartesia requires API key")
	}
}

func TestLoad_CartesiaRequiresVoiceID(t *testing.T) {
	os.Setenv("OPENAI_API_KEY", "sk-test")
	os.Setenv("HMAC_SECRET", "test-secret")
	os.Setenv("VOICE_RENDERER", "cartesia")
	os.Setenv("CARTESIA_API_KEY", "test-key")
	os.Unsetenv("CARTESIA_VOICE_ID")
	os.Unsetenv("GATEWAY_PORT")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error: cartesia requires voice ID")
	}
}

func TestLoad_CartesiaValidConfig(t *testing.T) {
	os.Setenv("OPENAI_API_KEY", "sk-test")
	os.Setenv("HMAC_SECRET", "test-secret")
	os.Setenv("VOICE_RENDERER", "cartesia")
	os.Setenv("CARTESIA_API_KEY", "test-key")
	os.Setenv("CARTESIA_VOICE_ID", "test-voice")
	os.Unsetenv("GATEWAY_PORT")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.CartesiaModel != "sonic-2" {
		t.Errorf("expected default model sonic-2, got %s", cfg.CartesiaModel)
	}
}

func TestLoad_OpenAIRendererSkipsCartesiaValidation(t *testing.T) {
	os.Setenv("OPENAI_API_KEY", "sk-test")
	os.Setenv("HMAC_SECRET", "test-secret")
	os.Setenv("VOICE_RENDERER", "openai")
	os.Unsetenv("CARTESIA_API_KEY")
	os.Unsetenv("CARTESIA_VOICE_ID")
	os.Unsetenv("GATEWAY_PORT")

	_, err := Load()
	if err != nil {
		t.Fatalf("openai renderer should not require cartesia config: %v", err)
	}
}
