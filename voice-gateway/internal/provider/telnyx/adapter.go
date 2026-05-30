// Package telnyx implements the provider.Adapter for Telnyx Media Streaming.
//
// Telnyx sends/receives raw PCMU bytes over WebSocket — no JSON envelope.
// Unlike Twilio, there is no base64 wrapping, no streamSid, and no control events
// on the media WS (hangup is detected by connection close).
//
// Reference: https://developers.telnyx.com/docs/voice/voice-ai/media-streams
package telnyx

import (
	"context"
	"fmt"
	"sync"

	"github.com/gorilla/websocket"

	"github.com/voxlane/voice-gateway/internal/provider"
)

// Adapter implements provider.Adapter for Telnyx.
type Adapter struct {
	conn   *websocket.Conn
	cfg    provider.TelnyxConfig
	callID string

	writeMu sync.Mutex

	Frames chan provider.AudioFrame
	Events chan provider.Event
}

// New creates a Telnyx adapter from a gorilla WebSocket connection.
func New(conn *websocket.Conn, callID string, cfg provider.TelnyxConfig) *Adapter {
	return &Adapter{
		conn:   conn,
		cfg:    cfg,
		callID: callID,
		Frames: make(chan provider.AudioFrame, 8),
		Events: make(chan provider.Event, 16),
	}
}

// ReadLoop reads raw PCMU binary frames from the Telnyx WebSocket.
func (a *Adapter) ReadLoop() {
	defer func() {
		a.conn.Close()
		close(a.Frames)
		close(a.Events)
	}()

	// Telnyx WS is ready immediately — signal connected
	a.Events <- provider.Event{Type: provider.EventConnected}

	for {
		msgType, raw, err := a.conn.ReadMessage()
		if err != nil {
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				a.Events <- provider.Event{Type: provider.EventDisconnected}
			} else {
				a.Events <- provider.Event{Type: provider.EventDisconnected, Error: err}
			}
			return
		}

		if msgType == websocket.BinaryMessage {
			select {
			case a.Frames <- provider.AudioFrame{
				Codec:      "pcmu",
				SampleRate: 8000,
				Payload:    raw,
				Direction:  "inbound",
				CallID:     a.callID,
			}:
			default:
			}
		}
	}
}

func (a *Adapter) Type() provider.Type { return provider.TypeTelnyx }

func (a *Adapter) ValidateRequest(_ context.Context, _ map[string]string, _ []byte) (string, error) {
	return a.callID, nil
}

func (a *Adapter) GenerateCallControl(_ string, ctrl provider.CallControlResponse) ([]byte, string, error) {
	body := fmt.Sprintf(`{"stream_url":"%s","stream_track":"both_tracks","client_state":"%s"}`,
		ctrl.StreamURL, a.callID)
	return []byte(body), "application/json", nil
}

func (a *Adapter) ParseMediaEvent(_ []byte) (*provider.AudioFrame, *provider.Event) {
	return nil, nil
}

// EncodeAudio returns raw bytes — no JSON wrapper.
func (a *Adapter) EncodeAudio(frame provider.AudioFrame) ([]byte, error) {
	return frame.Payload, nil
}

func (a *Adapter) EncodeMark(_ string) ([]byte, error) { return nil, nil }

// WriteRaw sends binary data directly on the WebSocket.
func (a *Adapter) WriteRaw(data []byte) error {
	a.writeMu.Lock()
	defer a.writeMu.Unlock()
	return a.conn.WriteMessage(websocket.BinaryMessage, data)
}

func (a *Adapter) CloseMessage() []byte { return nil }
func (a *Adapter) CallID() string      { return a.callID }
func (a *Adapter) StreamID() string     { return a.callID }
func (a *Adapter) Close() error         { return a.conn.Close() }
