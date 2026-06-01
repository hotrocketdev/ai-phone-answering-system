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
	os.Unsetenv("FAST_STATIC_GREETING")
	os.Unsetenv("TELNYX_ECHO_SUPPRESSION_TAIL_MS")
	os.Unsetenv("TELNYX_STREAM_BIDIRECTIONAL_CODEC")
	os.Unsetenv("TELNYX_BIDIRECTIONAL_CODEC")
	os.Unsetenv("CARTESIA_OUTPUT_ENCODING")
	os.Unsetenv("CARTESIA_OUTPUT_SAMPLE_RATE")
	os.Unsetenv("AUDIO_TRANSCODE_OUTBOUND_TO")

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
	if cfg.TelnyxEchoSuppressionTailMs != 300 {
		t.Errorf("expected telnyx echo suppression tail 300ms, got %d", cfg.TelnyxEchoSuppressionTailMs)
	}
	if cfg.FastStaticGreeting {
		t.Errorf("expected fast static greeting disabled by default")
	}
	if cfg.TelnyxBidirectionalCodec != "PCMU" {
		t.Errorf("expected default bidirectional codec PCMU, got %s", cfg.TelnyxBidirectionalCodec)
	}
	if cfg.CartesiaOutputEncoding != "pcm_mulaw" {
		t.Errorf("expected default Cartesia output pcm_mulaw, got %s", cfg.CartesiaOutputEncoding)
	}
	if cfg.CartesiaOutputSampleRate != 8000 {
		t.Errorf("expected default Cartesia sample rate 8000, got %d", cfg.CartesiaOutputSampleRate)
	}
	if cfg.AudioTranscodeOutboundTo != "none" {
		t.Errorf("expected default outbound transcode none, got %s", cfg.AudioTranscodeOutboundTo)
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

func TestLoad_FastStaticGreetingOptIn(t *testing.T) {
	os.Setenv("OPENAI_API_KEY", "sk-test")
	os.Setenv("HMAC_SECRET", "test-secret")
	os.Setenv("FAST_STATIC_GREETING", "true")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.FastStaticGreeting {
		t.Errorf("expected fast static greeting enabled when explicitly configured")
	}
}

func TestLoad_TelnyxEchoSuppressionTailOverride(t *testing.T) {
	os.Setenv("OPENAI_API_KEY", "sk-test")
	os.Setenv("HMAC_SECRET", "test-secret")
	os.Setenv("TELNYX_ECHO_SUPPRESSION_TAIL_MS", "150")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.TelnyxEchoSuppressionTailMs != 150 {
		t.Errorf("expected telnyx echo suppression tail 150ms, got %d", cfg.TelnyxEchoSuppressionTailMs)
	}
}

func TestLoad_G722OutboundConfig(t *testing.T) {
	os.Setenv("OPENAI_API_KEY", "sk-test")
	os.Setenv("HMAC_SECRET", "test-secret")
	os.Setenv("TELNYX_STREAM_BIDIRECTIONAL_CODEC", "G722")
	os.Setenv("CARTESIA_OUTPUT_ENCODING", "pcm_s16le")
	os.Setenv("CARTESIA_OUTPUT_SAMPLE_RATE", "16000")
	os.Setenv("AUDIO_TRANSCODE_OUTBOUND_TO", "g722")
	defer os.Unsetenv("TELNYX_STREAM_BIDIRECTIONAL_CODEC")
	defer os.Unsetenv("CARTESIA_OUTPUT_ENCODING")
	defer os.Unsetenv("CARTESIA_OUTPUT_SAMPLE_RATE")
	defer os.Unsetenv("AUDIO_TRANSCODE_OUTBOUND_TO")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.TelnyxBidirectionalCodec != "G722" {
		t.Fatalf("bidirectional codec = %s, want G722", cfg.TelnyxBidirectionalCodec)
	}
	if cfg.CartesiaOutputEncoding != "pcm_s16le" {
		t.Fatalf("cartesia output = %s, want pcm_s16le", cfg.CartesiaOutputEncoding)
	}
	if cfg.CartesiaOutputSampleRate != 16000 {
		t.Fatalf("cartesia sample rate = %d, want 16000", cfg.CartesiaOutputSampleRate)
	}
	if cfg.AudioTranscodeOutboundTo != "g722" {
		t.Fatalf("transcode target = %s, want g722", cfg.AudioTranscodeOutboundTo)
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
