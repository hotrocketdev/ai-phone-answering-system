package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	// Gateway
	Port           int
	WSURL          string
	MaxConcurrentCalls  int
	MaxCallDurationSecs int
	SilencePromptSecs   int
	SilenceHangupSecs   int

	// OpenAI
	OpenAIAPIKey       string
	OpenAIRealtimeModel string

	// Twilio
	TwilioAccountSID  string
	TwilioAuthToken   string

	// Voice Provider
	VoiceProvider string // "twilio" | "telnyx" | "signalwire"
	TelnyxAPIKey            string
	TelnyxConnectionID      string
	TelnyxPublicKey         string
	TelnyxStreamCodec       string // "PCMU" (default) or "L16"
	TelnyxBidirectionalCodec string // "PCMU" (default) or "L16"
	SignalWireProjectID string
	SignalWireToken     string
	SignalWireSpaceURL  string

	// Redis
	RedisAddr     string
	RedisPassword string

	// NestJS
	NestJSUrl string

	// HMAC
	HMACSecret string

	// LLM Provider
	LLMProvider string // "openai" | "grok"

	// Voice Renderer
	VoiceRenderer   string // "openai" | "cartesia" | "elevenlabs"
	CartesiaAPIKey  string
	CartesiaVoiceID  string
	CartesiaModel    string
	CartesiaLanguage string
	CartesiaSpeed    float64
	CartesiaVolume   float64
	CartesiaEmotion  string
	ElevenLabsAPIKey string

	// Voice Runtime
	VoiceRuntime        string // "custom" | "deepgram_agent"
	DeepgramAPIKey      string
	DeepgramListenModel string
	DeepgramListenLang  string
	DeepgramTTSModel    string
	DeepgramThinkProvider string
	DeepgramThinkModel  string

	// Environment
	LogLevel     string
	BusinessName       string // platform name (VoxLane) — used for logs, admin, dashboard
	TenantBusinessName string // tenant name override — used for caller-facing greetings/prompts
}

func Load() (*Config, error) {
	cfg := &Config{
		Port:           getEnvInt("GATEWAY_PORT", 8080),
		WSURL:          getEnv("GATEWAY_WS_URL", "wss://voice.voxlane.com/stream"),
		MaxConcurrentCalls:  getEnvInt("GATEWAY_MAX_CONCURRENT_CALLS", 100),
		MaxCallDurationSecs: getEnvInt("GATEWAY_MAX_CALL_DURATION_SECONDS", 1800),
	SilencePromptSecs:   getEnvInt("GATEWAY_SILENCE_TIMEOUT_PROMPT_SECONDS", 10),
	SilenceHangupSecs:   getEnvInt("GATEWAY_SILENCE_TIMEOUT_HANGUP_SECONDS", 20),
		OpenAIAPIKey:        os.Getenv("OPENAI_API_KEY"),
		OpenAIRealtimeModel: getEnv("OPENAI_REALTIME_MODEL", "gpt-realtime-mini"),
		TwilioAccountSID:    os.Getenv("TWILIO_ACCOUNT_SID"),
		TwilioAuthToken:     os.Getenv("TWILIO_AUTH_TOKEN"),
		VoiceProvider:       getEnv("VOICE_PROVIDER", "twilio"),
		TelnyxAPIKey:        os.Getenv("TELNYX_API_KEY"),
		TelnyxConnectionID:  os.Getenv("TELNYX_CONNECTION_ID"),
		TelnyxPublicKey:     os.Getenv("TELNYX_PUBLIC_KEY"),
		TelnyxStreamCodec:   getEnv("TELNYX_STREAM_CODEC", "PCMU"),
		TelnyxBidirectionalCodec: getEnv("TELNYX_BIDIRECTIONAL_CODEC", "PCMU"),
		SignalWireProjectID: os.Getenv("SIGNALWIRE_PROJECT_ID"),
		SignalWireToken:     os.Getenv("SIGNALWIRE_TOKEN"),
		SignalWireSpaceURL:  getEnv("SIGNALWIRE_SPACE_URL", "example.signalwire.com"),
		RedisAddr:           getEnv("REDIS_ADDR", "localhost:6379"),
		RedisPassword:       os.Getenv("REDIS_PASSWORD"),
		NestJSUrl:           getEnv("NESTJS_URL", "http://localhost:3000"),
		HMACSecret:          os.Getenv("HMAC_SECRET"),
		LLMProvider:         getEnv("LLM_PROVIDER", "openai"),
		VoiceRenderer:       getEnv("VOICE_RENDERER", "openai"),
		CartesiaAPIKey:      os.Getenv("CARTESIA_API_KEY"),
		CartesiaVoiceID:     os.Getenv("CARTESIA_VOICE_ID"),
		CartesiaModel:       getEnv("CARTESIA_MODEL", "sonic-2"),
		CartesiaLanguage:     getEnv("CARTESIA_LANGUAGE", "en"),
		CartesiaSpeed:        getEnvFloat("CARTESIA_SPEED", 0.95),
		CartesiaVolume:       getEnvFloat("CARTESIA_VOLUME", 0.9),
		CartesiaEmotion:      getEnv("CARTESIA_EMOTION", "content"),
		ElevenLabsAPIKey:    os.Getenv("ELEVENLABS_API_KEY"),
		VoiceRuntime:        getEnv("VOICE_RUNTIME", "custom"),
		DeepgramAPIKey:      os.Getenv("DEEPGRAM_API_KEY"),
		DeepgramListenModel: getEnv("DEEPGRAM_LISTEN_MODEL", "flux-general-en"),
		DeepgramListenLang:  getEnv("DEEPGRAM_LISTEN_LANGUAGE", "en"),
		DeepgramTTSModel:    getEnv("DEEPGRAM_TTS_MODEL", "aura-2-pandora-en"),
		DeepgramThinkProvider: getEnv("DEEPGRAM_THINK_PROVIDER", "open_ai"),
		DeepgramThinkModel:  getEnv("DEEPGRAM_THINK_MODEL", "gpt-4o-mini"),
		LogLevel:            getEnv("LOG_LEVEL", "info"),
		BusinessName:        getEnv("BUSINESS_NAME", "VoxLane"),
		TenantBusinessName:  getEnv("TENANT_BUSINESS_NAME", ""),
	}

	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (c *Config) validate() error {
	if c.OpenAIAPIKey == "" {
		return fmt.Errorf("OPENAI_API_KEY is required")
	}
	if c.HMACSecret == "" {
		return fmt.Errorf("HMAC_SECRET is required")
	}
	if c.Port < 1 || c.Port > 65535 {
		return fmt.Errorf("GATEWAY_PORT must be between 1 and 65535")
	}

	// Provider validation
	switch c.VoiceProvider {
	case "twilio":
		// Twilio creds optional (can use ngrok tunnel without them)
	case "telnyx":
		if c.TelnyxAPIKey == "" {
			return fmt.Errorf("VOICE_PROVIDER=telnyx requires TELNYX_API_KEY")
		}
	case "signalwire":
		return fmt.Errorf("VOICE_PROVIDER=signalwire is not yet implemented")
	default:
		return fmt.Errorf("unknown VOICE_PROVIDER: %s (valid: twilio, telnyx, signalwire)", c.VoiceProvider)
	}

	// Voice renderer validation
	switch c.VoiceRenderer {
	case "openai":
	case "cartesia":
		if c.CartesiaAPIKey == "" {
			return fmt.Errorf("VOICE_RENDERER=cartesia requires CARTESIA_API_KEY")
		}
		if c.CartesiaVoiceID == "" {
			return fmt.Errorf("VOICE_RENDERER=cartesia requires CARTESIA_VOICE_ID")
		}
	case "elevenlabs":
		return fmt.Errorf("VOICE_RENDERER=elevenlabs is not yet implemented")
	default:
		return fmt.Errorf("unknown VOICE_RENDERER: %s (valid: openai, cartesia, elevenlabs)", c.VoiceRenderer)
	}

	// Voice runtime validation
	switch c.VoiceRuntime {
	case "custom":
		// existing pipeline — no additional validation
	case "deepgram_agent":
		if c.DeepgramAPIKey == "" {
			return fmt.Errorf("VOICE_RUNTIME=deepgram_agent requires DEEPGRAM_API_KEY")
		}
	default:
		return fmt.Errorf("unknown VOICE_RUNTIME: %s (valid: custom, deepgram_agent)", c.VoiceRuntime)
	}

	return nil
}

func (c *Config) MaxCallDuration() time.Duration {
	return time.Duration(c.MaxCallDurationSecs) * time.Second
}

func (c *Config) SilencePromptDuration() time.Duration {
	return time.Duration(c.SilencePromptSecs) * time.Second
}

func (c *Config) SilenceHangupDuration() time.Duration {
	return time.Duration(c.SilenceHangupSecs) * time.Second
}

// CustomerName returns the tenant-facing name for greetings and prompts.
// If TENANT_BUSINESS_NAME is set, that is used for customer calls.
// Otherwise falls back to BUSINESS_NAME (platform name).
func (c *Config) CustomerName() string {
	if c.TenantBusinessName != "" {
		return c.TenantBusinessName
	}
	return c.BusinessName
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		n, err := strconv.Atoi(v)
		if err == nil {
			return n
		}
	}
	return fallback
}

func getEnvFloat(key string, fallback float64) float64 {
	if v := os.Getenv(key); v != "" {
		f, err := strconv.ParseFloat(v, 64)
		if err == nil {
			return f
		}
	}
	return fallback
}
