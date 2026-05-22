// Package twilio handles Twilio Media Streams WebSocket connections.
// Parses inbound JSON events and sends outbound media frames.
package twilio

import (
	"encoding/base64"
	"encoding/json"
	"fmt"

	"github.com/gorilla/websocket"
)

// ─── Event Types ─────────────────────────────────────────────────────────

// MediaEvent represents a Twilio Media Streams WebSocket message.
type MediaEvent struct {
	Event          string       `json:"event"`
	SequenceNumber string       `json:"sequenceNumber,omitempty"`
	StreamSid      string       `json:"streamSid,omitempty"`
	Media          *MediaPayload `json:"media,omitempty"`
	Mark           *MarkPayload  `json:"mark,omitempty"`
	Stop           *StopPayload  `json:"stop,omitempty"`
}

// MediaPayload contains audio data from/to Twilio.
type MediaPayload struct {
	Track     string `json:"track"`     // "inbound" or "outbound"
	Chunk     string `json:"chunk"`
	Timestamp string `json:"timestamp"`
	Payload   string `json:"payload"` // base64-encoded u-law audio
	StreamSid string `json:"streamSid,omitempty"`
}

// MarkPayload is sent when a marked event is reached.
type MarkPayload struct {
	Name string `json:"name"`
}

// StopPayload indicates the stream has ended.
type StopPayload struct {
	AccountSid string `json:"accountSid"`
	CallSid    string `json:"callSid"`
}

// ─── Event Type Constants ────────────────────────────────────────────────

const (
	EventConnected = "connected"
	EventStart     = "start"
	EventMedia     = "media"
	EventStop      = "stop"
	EventMark      = "mark"
)

// ─── Handler ─────────────────────────────────────────────────────────────

// Handler manages a Twilio Media Streams WebSocket connection.
// It reads inbound events and writes outbound media frames.
type Handler struct {
	conn      *websocket.Conn
	streamSid string
	callSid   string

	// Channels
	AudioIn  chan []byte // raw u-law audio bytes from Twilio (160 bytes per frame)
	Events   chan HandlerEvent
}

// HandlerEvent represents a non-media event from the Twilio stream.
type HandlerEvent struct {
	Type  string // "connected", "stopped", "disconnected", "mark"
	Error error
	Label string // for mark events
}

// NewHandler creates a new Twilio stream handler.
func NewHandler(conn *websocket.Conn, callSid string) *Handler {
	return &Handler{
		conn:     conn,
		callSid:  callSid,
		AudioIn:  make(chan []byte, 8),
		Events:   make(chan HandlerEvent, 8),
	}
}

// ReadLoop reads messages from the Twilio WebSocket in a loop.
// It should be run in its own goroutine.
// Blocks until the connection closes or an error occurs.
func (h *Handler) ReadLoop() {
	defer func() {
		h.conn.Close()
		close(h.AudioIn)
		close(h.Events)
	}()

	for {
		_, rawMsg, err := h.conn.ReadMessage()
		if err != nil {
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				h.Events <- HandlerEvent{Type: "disconnected", Error: nil}
			} else {
				h.Events <- HandlerEvent{Type: "disconnected", Error: err}
			}
			return
		}

		h.handleMessage(rawMsg)
	}
}

// handleMessage processes a single raw WebSocket message.
func (h *Handler) handleMessage(raw []byte) {
	var event MediaEvent
	if err := json.Unmarshal(raw, &event); err != nil {
		// Unknown message format — log and ignore
		return
	}

	switch event.Event {
	case EventConnected:
		h.handleConnected(event)

	case EventStart:
		h.handleStart(event)

	case EventMedia:
		h.handleMedia(event)

	case EventStop:
		h.handleStop(event)

	case EventMark:
		h.handleMark(event)
	}
}

// ─── Event Handlers ──────────────────────────────────────────────────────

func (h *Handler) handleConnected(event MediaEvent) {
	h.Events <- HandlerEvent{Type: "connected"}
}

func (h *Handler) handleStart(event MediaEvent) {
	h.streamSid = event.StreamSid
	h.Events <- HandlerEvent{Type: "started"}
}

func (h *Handler) handleMedia(event MediaEvent) {
	if event.Media == nil {
		return
	}

	// Only process inbound audio (caller speaking)
	if event.Media.Track != "inbound" {
		return
	}

	// Decode base64 u-law audio
	audio, err := base64.StdEncoding.DecodeString(event.Media.Payload)
	if err != nil {
		return
	}

	// Send to audio pipeline (non-blocking send)
	select {
	case h.AudioIn <- audio:
	default:
		// Drop frame if channel is full — audio is realtime, can't buffer
	}
}

func (h *Handler) handleStop(event MediaEvent) {
	h.Events <- HandlerEvent{Type: "stopped"}
}

func (h *Handler) handleMark(event MediaEvent) {
	label := ""
	if event.Mark != nil {
		label = event.Mark.Name
	}
	h.Events <- HandlerEvent{Type: "mark", Label: label}
}

// ─── Outbound Methods ────────────────────────────────────────────────────

// SendAudio sends a u-law audio frame to Twilio as an outbound media event.
func (h *Handler) SendAudio(mulaw []byte) error {
	msg := MediaEvent{
		Event:     EventMedia,
		StreamSid: h.streamSid,
		Media: &MediaPayload{
			Track:   "outbound",
			Payload: base64.StdEncoding.EncodeToString(mulaw),
		},
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal media event: %w", err)
	}

	return h.conn.WriteMessage(websocket.TextMessage, data)
}

// SendMark sends a mark event to Twilio (used for synchronization).
func (h *Handler) SendMark(name string) error {
	msg := MediaEvent{
		Event:     EventMark,
		StreamSid: h.streamSid,
		Mark:      &MarkPayload{Name: name},
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal mark event: %w", err)
	}

	return h.conn.WriteMessage(websocket.TextMessage, data)
}

// Close gracefully closes the WebSocket connection.
func (h *Handler) Close() error {
	return h.conn.WriteMessage(
		websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseNormalClosure, "call ended"),
	)
}

// StreamSid returns the media stream identifier.
func (h *Handler) GetStreamSid() string {
	return h.streamSid
}

// CallSid returns the call identifier.
func (h *Handler) GetCallSid() string {
	return h.callSid
}
