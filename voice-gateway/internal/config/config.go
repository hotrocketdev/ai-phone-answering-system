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
	TelnyxAPIKey       string
	TelnyxConnectionID string
	TelnyxPublicKey    string
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

	// Environment
	LogLevel string
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
		SignalWireProjectID: os.Getenv("SIGNALWIRE_PROJECT_ID"),
		SignalWireToken:     os.Getenv("SIGNALWIRE_TOKEN"),
		SignalWireSpaceURL:  getEnv("SIGNALWIRE_SPACE_URL", "example.signalwire.com"),
		RedisAddr:           getEnv("REDIS_ADDR", "localhost:6379"),
		RedisPassword:       os.Getenv("REDIS_PASSWORD"),
		NestJSUrl:           getEnv("NESTJS_URL", "http://localhost:3000"),
		HMACSecret:          os.Getenv("HMAC_SECRET"),
		LogLevel:            getEnv("LOG_LEVEL", "info"),
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
