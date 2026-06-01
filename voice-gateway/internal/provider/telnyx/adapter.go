// Package telnyx implements the provider.Adapter for Telnyx Media Streaming.
//
// Telnyx uses bidirectional RTP streaming over WebSocket:
// - Outbound: JSON media event with a base64 encoded RTP payload (raw audio, no RTP header)
// - Inbound: JSON media event with base64 encoded RTP payload (raw audio, no RTP header)
//
// Reference: https://developers.telnyx.com/docs/voice/programmable-voice/media-streaming
package telnyx

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"sync"

	"github.com/gorilla/websocket"

	"github.com/voxlane/voice-gateway/internal/provider"
)

// ─── Adapter ─────────────────────────────────────────────────────────────

type Adapter struct {
	conn     *websocket.Conn
	cfg      provider.TelnyxConfig
	callID   string
	streamID string

	writeMu sync.Mutex

	Frames chan provider.AudioFrame
	Events chan provider.Event

	outPacketCount int
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

// ReadLoop reads Telnyx JSON media events from the WebSocket.
func (a *Adapter) ReadLoop() {
	defer func() {
		a.conn.Close()
		close(a.Frames)
		close(a.Events)
	}()

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

		if msgType == websocket.TextMessage {
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
			continue
		}

		if msgType == websocket.BinaryMessage {
			if len(raw) == 0 {
				continue
			}
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

func (a *Adapter) ParseMediaEvent(raw []byte) (*provider.AudioFrame, *provider.Event) {
	var msg struct {
		Event          string `json:"event"`
		StreamID       string `json:"stream_id,omitempty"`
		SequenceNumber string `json:"sequence_number,omitempty"`
		Start          *struct {
			MediaFormat *struct {
				Encoding   string `json:"encoding"`
				SampleRate int    `json:"sample_rate"`
				Channels   int    `json:"channels"`
			} `json:"media_format,omitempty"`
		} `json:"start,omitempty"`
		Media *struct {
			Track     string `json:"track"`
			Chunk     string `json:"chunk"`
			Timestamp string `json:"timestamp"`
			Payload   string `json:"payload"`
		} `json:"media,omitempty"`
		Mark *struct {
			Name string `json:"name"`
		} `json:"mark,omitempty"`
		Payload *struct {
			Code   int    `json:"code"`
			Title  string `json:"title"`
			Detail string `json:"detail"`
		} `json:"payload,omitempty"`
	}
	if err := json.Unmarshal(raw, &msg); err != nil {
		return nil, &provider.Event{Type: provider.EventError, Error: err}
	}

	if msg.StreamID != "" {
		a.streamID = msg.StreamID
	}

	switch msg.Event {
	case "connected":
		return nil, &provider.Event{Type: provider.EventConnected}
	case "start":
		if msg.Start != nil && msg.Start.MediaFormat != nil {
			log.Printf("[telnyx] inbound media format encoding=%s sample_rate=%d channels=%d",
				msg.Start.MediaFormat.Encoding, msg.Start.MediaFormat.SampleRate, msg.Start.MediaFormat.Channels)
		}
		return nil, &provider.Event{Type: provider.EventStarted}
	case "media":
		if msg.Media == nil || msg.Media.Payload == "" {
			return nil, nil
		}
		track := msg.Media.Track
		if track == "" {
			track = "inbound"
		}
		audio, err := base64.StdEncoding.DecodeString(msg.Media.Payload)
		if err != nil {
			return nil, &provider.Event{Type: provider.EventError, Error: err}
		}
		return &provider.AudioFrame{
			Codec:      "pcmu",
			SampleRate: 8000,
			Payload:    audio,
			Timestamp:  msg.Media.Timestamp,
			Direction:  track,
			CallID:     a.callID,
			StreamID:   a.streamID,
		}, nil
	case "stop":
		return nil, &provider.Event{Type: provider.EventStopped}
	case "mark":
		label := ""
		if msg.Mark != nil {
			label = msg.Mark.Name
		}
		return nil, &provider.Event{Type: provider.EventMark, Label: label}
	case "error":
		err := fmt.Errorf("telnyx stream error")
		if msg.Payload != nil {
			err = fmt.Errorf("telnyx stream error %d %s: %s", msg.Payload.Code, msg.Payload.Title, msg.Payload.Detail)
		}
		return nil, &provider.Event{Type: provider.EventError, Error: err}
	}
	return nil, nil
}

// EncodeAudio returns raw PCMU RTP payload bytes. Telnyx wraps these bytes in
// a JSON media envelope in WriteRaw; no 12-byte RTP header is sent.
func (a *Adapter) EncodeAudio(frame provider.AudioFrame) ([]byte, error) {
	pcmu := frame.Payload
	a.outPacketCount++
	if a.outPacketCount <= 5 {
		log.Printf("[telnyx] RTP payload out packet=%d payload_len=%d", a.outPacketCount, len(pcmu))
	}
	return pcmu, nil
}

func (a *Adapter) EncodeMark(label string) ([]byte, error) {
	msg := map[string]interface{}{
		"event": "mark",
		"mark":  map[string]string{"name": label},
	}
	return json.Marshal(msg)
}

// WriteRaw sends a Telnyx media event containing base64 encoded RTP payload data.
func (a *Adapter) WriteRaw(data []byte) error {
	a.writeMu.Lock()
	defer a.writeMu.Unlock()
	msg, err := encodeOutboundMedia(data)
	if err != nil {
		return err
	}
	return a.conn.WriteMessage(websocket.TextMessage, msg)
}

func (a *Adapter) CloseMessage() []byte { return nil }
func (a *Adapter) CallID() string       { return a.callID }
func (a *Adapter) StreamID() string     { return a.streamID }
func (a *Adapter) Close() error         { return a.conn.Close() }

func encodeOutboundMedia(payload []byte) ([]byte, error) {
	msg := map[string]interface{}{
		"event": "media",
		"media": map[string]string{
			"payload": base64.StdEncoding.EncodeToString(payload),
		},
	}
	return json.Marshal(msg)
}
