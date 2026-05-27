// Package openai implements the OpenAI Realtime API WebSocket client.
// Manages session lifecycle, audio streaming, and tool call handling.
package openai

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
)

// ─── Configuration ───────────────────────────────────────────────────────

// Config holds the OpenAI Realtime session configuration.
type Config struct {
	APIKey       string
	Model        string
	Voice        string
	Tools        []Tool
	Instructions string
	Modalities   []string // "text", "audio" — defaults to both
}

// Tool defines a tool available to the AI.
type Tool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
}

var audioDeltaLogged bool

// ─── Session ─────────────────────────────────────────────────────────────

// Session manages a single OpenAI Realtime WebSocket connection.
type Session struct {
	conn   *websocket.Conn
	config Config

	mu     sync.Mutex
	sessID string // OpenAI session ID

	// Channels
	AudioOut chan []byte   // PCM16 24kHz audio from OpenAI (960 bytes per frame)
	Events   chan Event    // non-audio events
	Done     chan struct{} // closed when session ends
}

// Event represents a non-audio event from OpenAI.
type Event struct {
	Type string // session.created, session.updated, response.done, error, etc.
	Data json.RawMessage
}

// NewSession creates a new OpenAI Realtime session by connecting to the WebSocket API.
func NewSession(ctx context.Context, cfg Config) (*Session, error) {
	url := fmt.Sprintf("wss://api.openai.com/v1/realtime?model=%s", cfg.Model)

	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+cfg.APIKey)

	conn, _, err := websocket.DefaultDialer.DialContext(ctx, url, headers)
	if err != nil {
		return nil, fmt.Errorf("dial openai: %w", err)
	}

	s := &Session{
		conn:     conn,
		config:   cfg,
		AudioOut: make(chan []byte, 8),
		Events:   make(chan Event, 16),
		Done:     make(chan struct{}),
	}

	return s, nil
}

// ─── Session Lifecycle ───────────────────────────────────────────────────

// Start begins reading events and sends the initial session configuration.
// Must be called after NewSession. Runs synchronously until session.created is received.
func (s *Session) Start(ctx context.Context) error {
	// Read session.created
	_, raw, err := s.conn.ReadMessage()
	if err != nil {
		return fmt.Errorf("read session.created: %w", err)
	}

	var created struct {
		Type    string `json:"type"`
		Session struct {
			ID string `json:"id"`
		} `json:"session"`
	}
	if err := json.Unmarshal(raw, &created); err != nil {
		return fmt.Errorf("parse session.created: %w", err)
	}
	if created.Type != "session.created" {
		log.Printf("OpenAI unexpected response: %s", string(raw))
		return fmt.Errorf("expected session.created, got %s", created.Type)
	}

	s.mu.Lock()
	s.sessID = created.Session.ID
	s.mu.Unlock()

	audioDeltaLogged = false

	// Send session configuration
	log.Printf("OpenAI session.update payload (model=%s, voice=%s)", s.config.Model, s.config.Voice)
	if err := s.sendSessionUpdate(); err != nil {
		return fmt.Errorf("send session.update: %w", err)
	}

	// Read session.updated
	_, raw, err = s.conn.ReadMessage()
	if err != nil {
		return fmt.Errorf("read session.updated: %w", err)
	}

	log.Printf("OpenAI session.updated response: %s", string(raw))

	var updated struct {
		Type string `json:"type"`
	}
	json.Unmarshal(raw, &updated)

	return nil
}

// ReadLoop reads events from the OpenAI WebSocket in a loop.
// Should be run in its own goroutine after Start().
func (s *Session) ReadLoop() {
	defer func() {
		s.conn.Close()
		close(s.AudioOut)
		close(s.Events)
		close(s.Done)
	}()

	log.Printf("OpenAI ReadLoop started")

	for {
		_, raw, err := s.conn.ReadMessage()
		if err != nil {
			log.Printf("OpenAI ReadLoop ended: %v", err)
			return
		}
		log.Printf("OpenAI raw msg: %s", string(raw[:80]))

		s.handleMessage(raw)
	}
}

// ─── Event Handling ──────────────────────────────────────────────────────

func (s *Session) handleMessage(raw []byte) {
	var base struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(raw, &base); err != nil {
		return
	}

	// Log all non-audio events for diagnostics
	if base.Type == "response.output_audio.delta" {
		// Log first delta to verify field name
		if !audioDeltaLogged {
			log.Printf("OpenAI audio delta (first 200 chars): %s", string(raw[:200]))
			audioDeltaLogged = true
		}
	} else {
		log.Printf("OpenAI event: %s", base.Type)
		if base.Type == "response.done" || base.Type == "error" {
			log.Printf("OpenAI event detail: %s", string(raw))
		}
	}

	switch {
	case base.Type == "response.output_audio.delta":
		s.handleAudioDelta(raw)

	case base.Type == "response.output_audio.done":
		s.Events <- Event{Type: "audio.done", Data: raw}

	case base.Type == "response.text.delta":
		s.Events <- Event{Type: "text.delta", Data: raw}

	case base.Type == "response.text.done":
		s.Events <- Event{Type: "text.done", Data: raw}

	case base.Type == "response.function_call_arguments.done":
		s.Events <- Event{Type: "function_call.done", Data: raw}

	case base.Type == "response.done":
		s.Events <- Event{Type: "response.done", Data: raw}

	case base.Type == "input_audio_buffer.speech_started":
		s.Events <- Event{Type: "speech_started"}

	case base.Type == "input_audio_buffer.speech_stopped":
		s.Events <- Event{Type: "speech_stopped"}

	case base.Type == "error":
		s.Events <- Event{Type: "error", Data: raw}

	default:
		s.Events <- Event{Type: base.Type, Data: raw}
	}
}

func (s *Session) handleAudioDelta(raw []byte) {
	var delta struct {
		Delta string `json:"delta"`
	}
	if err := json.Unmarshal(raw, &delta); err != nil {
		return
	}

	audio, err := base64.StdEncoding.DecodeString(delta.Delta)
	if err != nil {
		return
	}

	select {
	case s.AudioOut <- audio:
	default:
		// Drop frame if consumer is slow
	}
}

// ─── Outbound Methods ────────────────────────────────────────────────────

// SendAudio appends audio data to OpenAI's input buffer.
// audio must be base64-encoded PCM16 24kHz.
func (s *Session) SendAudio(b64Audio string) error {
	msg := map[string]interface{}{
		"type":  "input_audio_buffer.append",
		"audio": b64Audio,
	}
	return s.writeJSON(msg)
}

// SendAudioBytes sends raw PCM16 24kHz bytes (auto base64-encodes).
func (s *Session) SendAudioBytes(pcm24k []byte) error {
	b64 := base64.StdEncoding.EncodeToString(pcm24k)
	return s.SendAudio(b64)
}

// CancelResponse cancels the current AI response (barge-in).
func (s *Session) CancelResponse() error {
	msg := map[string]interface{}{
		"type": "response.cancel",
	}
	return s.writeJSON(msg)
}

// FeedToolResult sends a tool call result back to OpenAI.
func (s *Session) FeedToolResult(callID string, output string) error {
	msg := map[string]interface{}{
		"type": "conversation.item.create",
		"item": map[string]interface{}{
			"type":    "function_call_output",
			"call_id": callID,
			"output":  output,
		},
	}
	return s.writeJSON(msg)
}

// ClearAudio clears the input audio buffer.
func (s *Session) ClearAudio() error {
	msg := map[string]interface{}{
		"type": "input_audio_buffer.clear",
	}
	return s.writeJSON(msg)
}

// CreateResponse triggers the AI to generate a new response.
func (s *Session) CreateResponse() error {
	msg := map[string]interface{}{
		"type": "response.create",
	}
	return s.writeJSON(msg)
}

// ─── Internal ────────────────────────────────────────────────────────────

func (s *Session) sendSessionUpdate() error {
	sessionCfg := map[string]interface{}{
		"type":         "realtime",
		"model":        s.config.Model,
		"instructions": s.config.Instructions,
		"voice":        s.config.Voice,
		"input_audio_format":  "pcm16",
		"output_audio_format": "pcm16",
		"tools": s.config.Tools,
	}

	msg := map[string]interface{}{
		"type":    "session.update",
		"session": sessionCfg,
	}

	return s.writeJSON(msg)
}

// WriteRaw sends an arbitrary JSON message to OpenAI. Used for conversation control.
func (s *Session) WriteRaw(v interface{}) error {
	return s.writeJSON(v)
}

func (s *Session) getModalities() []string {
	if len(s.config.Modalities) > 0 {
		return s.config.Modalities
	}
	return []string{"text", "audio"} // default
}

func (s *Session) writeJSON(v interface{}) error {
	return s.conn.WriteJSON(v)
}

// ─── Helpers ─────────────────────────────────────────────────────────────

// ParseFunctionCall extracts function call details from a function_call_arguments.done event.
func ParseFunctionCall(raw json.RawMessage) (callID, name string, args json.RawMessage, err error) {
	var event struct {
		CallID string `json:"call_id"`
		Name   string `json:"name"`
		Arguments string `json:"arguments"`
	}
	if err := json.Unmarshal(raw, &event); err != nil {
		return "", "", nil, err
	}

	// Arguments is a JSON string that needs to be parsed
	args = json.RawMessage(event.Arguments)
	return event.CallID, event.Name, args, nil
}

// ParseResponseDone extracts the response status from a response.done event.
func ParseResponseDone(raw json.RawMessage) (status string, err error) {
	var event struct {
		Response struct {
			Status string `json:"status"`
		} `json:"response"`
	}
	if err := json.Unmarshal(raw, &event); err != nil {
		return "", err
	}
	return event.Response.Status, nil
}

// SessionID returns the OpenAI session identifier.
func (s *Session) SessionID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sessID
}

// Close gracefully closes the WebSocket connection.
func (s *Session) Close() error {
	return s.conn.WriteMessage(
		websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseNormalClosure, "session end"),
	)
}

// IsClosed returns true if the session's Done channel is closed.
func (s *Session) IsClosed() bool {
	select {
	case <-s.Done:
		return true
	default:
		return false
	}
}

// ─── Prompt Builder ──────────────────────────────────────────────────────

// BuildGreetingPrompt returns the system prompt for the initial greeting state.
func BuildGreetingPrompt(restaurantName string) string {
	return fmt.Sprintf(`You are the friendly AI receptionist for %s, a premium restaurant.
You have just answered the phone.

CRITICAL RULES:
- Be warm, natural, and conversational. Sound like a real person, not a robot.
- Never confirm a booking until the create_booking tool returns success.
- Never transition conversation state yourself. Only tool results change state.
- If the caller asks to speak to a human, transfer immediately — do not negotiate.
- Do not apologize excessively. Be warm but concise.
- Keep responses brief and natural.

When the caller speaks:
1. Greet them warmly using the restaurant name.
2. Detect their intent: booking, modification, cancellation, FAQ, or speak-to-human.
3. Do not ask for details until you understand what they want.`, restaurantName)
}
