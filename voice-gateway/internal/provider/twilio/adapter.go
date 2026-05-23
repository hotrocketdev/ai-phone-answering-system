// Package twilio implements the provider.Adapter for Twilio Media Streams.
// Wraps the existing twilio.Handler with provider-neutral types.
package twilio

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/gorilla/websocket"

	"github.com/voxlane/voice-gateway/internal/provider"
)

// ─── Adapter ─────────────────────────────────────────────────────────────

// Adapter implements provider.Adapter for Twilio by wrapping a raw WebSocket.
type Adapter struct {
	conn     *websocket.Conn
	cfg      provider.TwilioConfig
	callID   string
	streamID string

	// Channels for the caller to consume
	Frames chan provider.AudioFrame
	Events chan provider.Event
}

// New creates a Twilio adapter from a gorilla WebSocket connection.
func New(conn *websocket.Conn, callID string, cfg provider.TwilioConfig) *Adapter {
	return &Adapter{
		conn:   conn,
		cfg:    cfg,
		callID: callID,
		Frames: make(chan provider.AudioFrame, 8),
		Events: make(chan provider.Event, 16),
	}
}

// ─── Read Loop ───────────────────────────────────────────────────────────

// ReadLoop reads messages from the Twilio WebSocket and parses them
// into provider-neutral frames and events. Runs in its own goroutine.
func (a *Adapter) ReadLoop() {
	defer func() {
		a.conn.Close()
		close(a.Frames)
		close(a.Events)
	}()

	for {
		_, raw, err := a.conn.ReadMessage()
		if err != nil {
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				a.Events <- provider.Event{Type: provider.EventDisconnected}
			} else {
				a.Events <- provider.Event{Type: provider.EventDisconnected, Error: err}
			}
			return
		}

		frame, event := a.ParseMediaEvent(raw)
		if frame != nil {
			select {
			case a.Frames <- *frame:
			default:
			}
		}
		if event != nil {
			a.Events <- *event
		}
	}
}

// ─── Interface Implementation ────────────────────────────────────────────

func (a *Adapter) Type() provider.Type { return provider.TypeTwilio }

func (a *Adapter) ValidateRequest(_ context.Context, _ map[string]string, _ []byte) (string, error) {
	return a.callID, nil
}

func (a *Adapter) GenerateCallControl(_ string, ctrl provider.CallControlResponse) ([]byte, string, error) {
	twiml := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<Response>
  <Connect>
    <Stream url="%s">
      <Parameter name="callerId" value="%s"/>
    </Stream>
  </Connect>
  <Say>%s</Say>
</Response>`, ctrl.StreamURL, ctrl.CallerID, ctrl.Fallback)
	return []byte(twiml), "text/xml", nil
}

func (a *Adapter) ParseMediaEvent(raw []byte) (*provider.AudioFrame, *provider.Event) {
	var msg struct {
		Event     string `json:"event"`
		StreamSid string `json:"streamSid,omitempty"`
		Media     *struct {
			Track     string `json:"track"`
			Chunk     string `json:"chunk"`
			Timestamp string `json:"timestamp"`
			Payload   string `json:"payload"`
		} `json:"media,omitempty"`
		Mark *struct {
			Name string `json:"name"`
		} `json:"mark,omitempty"`
	}
	if err := json.Unmarshal(raw, &msg); err != nil {
		return nil, &provider.Event{Type: provider.EventError, Error: err}
	}

	if msg.StreamSid != "" {
		a.streamID = msg.StreamSid
	}

	switch msg.Event {
	case "media":
		if msg.Media == nil || msg.Media.Track != "inbound" {
			return nil, nil
		}
		audio, _ := base64.StdEncoding.DecodeString(msg.Media.Payload)
		return &provider.AudioFrame{
			Codec: "ulaw", SampleRate: 8000, Payload: audio,
			Timestamp: msg.Media.Timestamp, Direction: "inbound",
			CallID: a.callID, StreamID: a.streamID,
		}, nil

	case "connected":
		return nil, &provider.Event{Type: provider.EventConnected}
	case "start":
		return nil, &provider.Event{Type: provider.EventStarted}
	case "stop":
		return nil, &provider.Event{Type: provider.EventStopped}
	case "mark":
		label := ""
		if msg.Mark != nil {
			label = msg.Mark.Name
		}
		return nil, &provider.Event{Type: provider.EventMark, Label: label}
	}
	return nil, nil
}

func (a *Adapter) EncodeAudio(frame provider.AudioFrame) ([]byte, error) {
	msg := map[string]interface{}{
		"event":     "media",
		"streamSid": a.streamID,
		"media": map[string]interface{}{
			"track":   "outbound",
			"payload": base64.StdEncoding.EncodeToString(frame.Payload),
		},
	}
	return json.Marshal(msg)
}

func (a *Adapter) EncodeMark(label string) ([]byte, error) {
	msg := map[string]interface{}{
		"event":     "mark",
		"streamSid": a.streamID,
		"mark":      map[string]string{"name": label},
	}
	return json.Marshal(msg)
}

func (a *Adapter) CloseMessage() []byte { return nil }
func (a *Adapter) CallID() string       { return a.callID }
func (a *Adapter) StreamID() string     { return a.streamID }

// WriteRaw sends raw bytes directly on the WebSocket.
func (a *Adapter) WriteRaw(data []byte) error {
	return a.conn.WriteMessage(websocket.TextMessage, data)
}

func (a *Adapter) Close() error {
	return a.conn.WriteMessage(websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseNormalClosure, "call ended"))
}

// ValidateInboundRequest validates an incoming Twilio HTTP webhook.
func ValidateInboundRequest(r *http.Request, authToken string) (string, error) {
	if err := r.ParseForm(); err != nil {
		return "", fmt.Errorf("parse form: %w", err)
	}
	callSid := r.FormValue("CallSid")
	if callSid == "" {
		return "", fmt.Errorf("missing CallSid")
	}
	return callSid, nil
}
