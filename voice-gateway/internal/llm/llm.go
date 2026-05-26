// Package llm defines the abstraction for realtime conversation intelligence providers.
// Implementations: OpenAI, Grok.
package llm

import (
	"context"
	"encoding/json"
)

// ─── Provider Type ───────────────────────────────────────────────────────

type Provider string

const (
	ProviderOpenAI Provider = "openai"
	ProviderGrok   Provider = "grok"
)

// ─── Events ──────────────────────────────────────────────────────────────

type EventType string

const (
	EventAudioDelta        EventType = "audio_delta"
	EventAudioDone         EventType = "audio_done"
	EventSpeechStarted     EventType = "speech_started"
	EventSpeechStopped     EventType = "speech_stopped"
	EventFunctionCallDone  EventType = "function_call_done"
	EventResponseDone      EventType = "response_done"
	EventError             EventType = "error"
)

type Event struct {
	Type EventType
	Data json.RawMessage
}

// ─── Tool ────────────────────────────────────────────────────────────────

type Tool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
}

// ─── Config ──────────────────────────────────────────────────────────────

type Config struct {
	Provider     Provider
	Model        string
	Voice        string
	Instructions string
	Tools        []Tool
	APIKey       string
}

// ─── Session Interface ───────────────────────────────────────────────────

// Session represents an active realtime conversation session.
type Session interface {
	// Start initialises the session (connect, send config, receive acknowledgement).
	Start(ctx context.Context) error

	// ReadLoop reads events from the provider in a loop. Blocking.
	ReadLoop()

	// SendAudio sends audio bytes to the provider.
	// The format depends on the provider config (PCM16 24kHz or g711_ulaw).
	SendAudio(b64 string) error

	// CancelResponse cancels the current AI response (barge-in).
	CancelResponse() error

	// FeedToolResult sends a tool call result back.
	FeedToolResult(callID, output string) error

	// CreateResponse triggers the AI to generate a new response.
	CreateResponse() error

	// Close gracefully closes the connection.
	Close() error

	// IsClosed returns true if the session's Done channel is closed.
	IsClosed() bool

	// AudioOut returns the channel for outbound audio frames.
	AudioOut() chan []byte

	// Events returns the channel for non-audio events.
	Events() chan Event

	// Done returns a channel that's closed when the session ends.
	Done() chan struct{}

	// Provider returns the provider type.
	Provider() Provider
}

// ─── Factory ─────────────────────────────────────────────────────────────

// NewSession creates a new LLM session for the configured provider.
// Provider-specific constructors are in their respective packages.
func NewSession(ctx context.Context, cfg Config) (Session, error) {
	// Placeholder — real construction in provider packages
	return nil, nil
}
