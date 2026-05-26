// Package runtime defines the voice runtime provider abstraction.
// Runtimes handle the full realtime call flow: audio in/out, STT, LLM, TTS.
package runtime

import "context"

// ─── Provider Type ───────────────────────────────────────────────────────

type Provider string

const (
	ProviderCustom        Provider = "custom"         // existing OpenAI/Cartesia pipeline
	ProviderDeepgramAgent Provider = "deepgram_agent" // Deepgram full-duplex agent
)

// ─── Session ─────────────────────────────────────────────────────────────

// Session represents an active voice runtime session.
// The runtime handles bidirectional audio and conversation orchestration.
type Session interface {
	// Start begins the runtime session.
	Start(ctx context.Context) error

	// SendAudio sends an inbound audio frame to the runtime.
	SendAudio(payload []byte) error

	// Close gracefully ends the session.
	Close() error

	// AudioOut returns a channel of outbound audio frames ready for Twilio.
	AudioOut() chan []byte

	// Done returns a channel that's closed when the session ends.
	Done() chan struct{}

	// Provider returns the runtime provider type.
	Provider() Provider
}

// ─── Config ──────────────────────────────────────────────────────────────

type Config struct {
	Provider Provider

	// Deepgram
	DeepgramAPIKey       string
	DeepgramListenModel  string
	DeepgramListenLang   string
	DeepgramTTSModel     string
	DeepgramThinkProvider string
	DeepgramThinkModel   string

	// Shared
	OpenAIAPIKey string
}
