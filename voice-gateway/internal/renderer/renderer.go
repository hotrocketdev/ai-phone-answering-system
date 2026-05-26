// Package renderer defines the abstraction for outbound voice rendering providers.
// Implementations: OpenAI (native), Cartesia, ElevenLabs.
package renderer

import "context"

// ─── Provider Type ───────────────────────────────────────────────────────

type Provider string

const (
	ProviderOpenAI    Provider = "openai"
	ProviderCartesia  Provider = "cartesia"
	ProviderElevenLabs Provider = "elevenlabs"
)

// ─── Config ──────────────────────────────────────────────────────────────

type Config struct {
	Provider Provider
	Voice    string
	Language string // "en-GB", "en-US", etc.
	APIKey   string
	Model    string
}

// ─── Renderer Interface ──────────────────────────────────────────────────

// Renderer converts text or audio chunks into telephony-compatible audio.
type Renderer interface {
	// Render converts text to audio bytes (PCM16 or u-law depending on config).
	Render(ctx context.Context, text string) ([]byte, error)

	// RenderStream returns a channel that streams audio chunks as they're generated.
	RenderStream(ctx context.Context, text string) (<-chan []byte, error)

	// Provider returns the renderer provider type.
	Provider() Provider

	// Close releases any resources.
	Close() error
}

// ─── Factory ─────────────────────────────────────────────────────────────

func NewRenderer(cfg Config) (Renderer, error) {
	// Placeholder — real construction in provider packages
	return nil, nil
}
