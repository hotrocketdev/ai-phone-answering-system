// Package provider defines the abstraction layer for telephony/voice providers.
// Adapters for Twilio, Telnyx, and SignalWire implement this interface.
package provider

import (
	"context"
	"time"
)

// ─── Provider Type ───────────────────────────────────────────────────────

// Type identifies the telephony provider.
type Type string

const (
	TypeTwilio     Type = "twilio"
	TypeTelnyx     Type = "telnyx"
	TypeSignalWire Type = "signalwire"
)

// Valid returns true if the provider type is recognised.
func (t Type) Valid() bool {
	switch t {
	case TypeTwilio, TypeTelnyx, TypeSignalWire:
		return true
	}
	return false
}

// ─── Audio Frame ─────────────────────────────────────────────────────────

// AudioFrame represents a single media frame from any provider.
// It is the provider-neutral format used throughout the audio pipeline.
type AudioFrame struct {
	Codec      string // "ulaw" (G.711), "pcm16", or "pcmu"
	SampleRate int    // 8000 or 16000
	Payload    []byte // raw audio bytes
	Timestamp  string // provider timestamp
	Direction  string // "inbound" (caller) or "outbound" (AI)
	SeqNumber  int    // sequence number if available
	CallID     string // provider call SID/ID
	StreamID   string // provider stream SID/ID
}

// ─── Events ──────────────────────────────────────────────────────────────

// EventType categorises non-audio events from the voice provider.
type EventType string

const (
	EventConnected    EventType = "connected"
	EventStarted      EventType = "started"
	EventStopped      EventType = "stopped"
	EventDisconnected EventType = "disconnected"
	EventMark         EventType = "mark"
	EventError        EventType = "error"
)

// Event represents a non-audio event from the provider stream.
type Event struct {
	Type  EventType
	Label string // for mark events
	Error error
}

// ─── Call Control ────────────────────────────────────────────────────────

// CallControlResponse is the provider-neutral response to an inbound call.
// Each adapter translates this into the provider-specific format
// (TwiML for Twilio, JSON for Telnyx, etc.).
type CallControlResponse struct {
	StreamURL string // WebSocket URL the provider should connect to for media
	CallerID  string // caller phone number template variable
	Fallback  string // message to play/say if stream fails
}

// ─── Adapter Interface ───────────────────────────────────────────────────

// Adapter is the interface all voice providers must implement.
type Adapter interface {
	// Type returns the provider type identifier.
	Type() Type

	// ValidateRequest authenticates an inbound call webhook request.
	// Returns the call identifier and any error.
	ValidateRequest(ctx context.Context, headers map[string]string, body []byte) (callID string, err error)

	// GenerateCallControl produces the provider-specific call control response.
	GenerateCallControl(callID string, ctrl CallControlResponse) ([]byte, string, error)
	// Returns: (responseBody, contentType, error)

	// ParseMediaEvent parses a raw WebSocket message into an AudioFrame or Event.
	// Returns the frame (non-nil for audio), event (non-nil for control events), or error.
	ParseMediaEvent(raw []byte) (frame *AudioFrame, event *Event)

	// EncodeAudio encodes a provider-neutral AudioFrame into a provider-specific
	// outbound WebSocket message ready to send.
	EncodeAudio(frame AudioFrame) ([]byte, error)

	// EncodeMark creates a provider-specific mark/sync message.
	EncodeMark(label string) ([]byte, error)

	// CloseMessage returns the WebSocket close frame for graceful disconnect.
	CloseMessage() []byte

	// CallID returns the provider's call identifier for this stream.
	CallID() string

	// StreamID returns the provider's stream identifier.
	StreamID() string

	// Close releases any adapter resources.
	Close() error
}

// ─── Factory ─────────────────────────────────────────────────────────────

// Config holds provider-specific configuration.
type Config struct {
	ProviderType Type
	Twilio       TwilioConfig
	Telnyx       TelnyxConfig
	SignalWire   SignalWireConfig
}

// TwilioConfig holds Twilio-specific credentials.
type TwilioConfig struct {
	AccountSID string
	AuthToken  string
}

// TelnyxConfig holds Telnyx-specific credentials.
type TelnyxConfig struct {
	APIKey             string
	ConnectionID       string
	PublicKey          string
	StreamCodec        string // "PCMU", "PCMA", "G722", "OPUS", "AMR-WB", or "L16"
	BidirectionalCodec string // "PCMU", "PCMA", "G722", "OPUS", "AMR-WB", or "L16"
}

// SignalWireConfig holds SignalWire-specific credentials.
type SignalWireConfig struct {
	ProjectID string
	Token     string
	SpaceURL  string
}

// StreamHandler is the raw WebSocket connection interface passed to adapters.
type StreamHandler interface {
	ReadMessage() (messageType int, p []byte, err error)
	WriteMessage(messageType int, data []byte) error
	Close() error
}

// NewAdapter creates the appropriate adapter for the configured provider.
// stream is the raw WebSocket connection from the provider.
func NewAdapter(cfg Config, stream StreamHandler, callID string) (Adapter, error) {
	// Provider-specific adapters are registered via init() in their packages.
	// This function is here for documentation; actual construction happens
	// in the provider-specific packages imported by the caller.
	return nil, nil // placeholder — real creation in provider packages
}

// ─── Timing ──────────────────────────────────────────────────────────────

const (
	// Default stream timeout values
	StreamReadTimeout  = 30 * time.Second
	StreamWriteTimeout = 5 * time.Second
	StreamPingInterval = 20 * time.Second
)
