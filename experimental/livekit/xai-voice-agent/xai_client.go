// xai_client.go — minimal WebSocket client for xAI Grok Voice Agent API.
//
// xAI's Voice Agent is OpenAI Realtime API compatible. Session config:
//   { "type": "session.update", "session": { "voice": "eve",
//     "instructions": "...", "turn_detection": { "type": "server_vad",
//     "threshold": 0.7, "prefix_padding_ms": 300, "silence_duration_ms": 1500 }}}
//
// Audio input events:
//   { "type": "input_audio_buffer.append", "audio": "<base64 PCM16 24kHz mono>" }
//   { "type": "input_audio_buffer.commit" }   // optional: force end-of-utterance
//   { "type": "response.create" }              // optional: force response
//
// Audio output events:
//   { "type": "response.output_audio.delta", "delta": "<base64 PCM16 24kHz mono>" }
//
// Endpoint: wss://api.x.ai/v1/realtime?model=grok-voice-latest
// Auth: Authorization: Bearer $XAI_API_KEY
package main

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	xaiRealtimeURL = "wss://api.x.ai/v1/realtime"
	// xAI Voice Agent sends audio in PCM16 24kHz mono (per OpenAI Realtime compat)
	xaiSampleRate      = 24000
	xaiBytesPerSample  = 2 // int16
	xaiChannels        = 1
	xaiFrameDurationMs = 100 // 100ms chunks at 24kHz = 4800 bytes PCM16
	xaiChunkBytes      = xaiSampleRate * xaiBytesPerSample * xaiChannels * xaiFrameDurationMs / 1000
)

// xaiEvent is a minimal subset of the Voice Agent event schema we need.
type xaiEvent struct {
	Type     string          `json:"type"`
	EventID  string          `json:"event_id,omitempty"`
	Session  *xaiSessionCfg  `json:"session,omitempty"`
	Item     json.RawMessage `json:"item,omitempty"`
	Delta    string          `json:"delta,omitempty"` // base64 audio OR text transcript delta
	Audio    string          `json:"audio,omitempty"` // base64 audio (input)
	Response json.RawMessage `json:"response,omitempty"`
	Error    *xaiError       `json:"error,omitempty"`
	// Server-vad status
	StartedMs  *int `json:"started_at_ms,omitempty"`
	EndedMs    *int `json:"ended_at_ms,omitempty"`
	ResponseID string `json:"response_id,omitempty"`
	// Transcript payload appears at the TOP LEVEL of the event
	// (not inside `item`) for response.output_audio_transcript.done.
	Transcript   string  `json:"transcript,omitempty"`
	ContentIndex int     `json:"content_index,omitempty"`
	Part         *xaiPart `json:"part,omitempty"`
}

type xaiPart struct {
	Type       string `json:"type"`
	Text       string `json:"text,omitempty"`
	Audio      string `json:"audio,omitempty"`
	Transcript string `json:"transcript,omitempty"`
}

type xaiSessionCfg struct {
	Voice         string      `json:"voice"`
	Instructions  string      `json:"instructions"`
	TurnDetection *xaiTurnDet `json:"turn_detection"`
	Tools         []xaiTool   `json:"tools,omitempty"`
}

type xaiTurnDet struct {
	Type            string  `json:"type"`
	Threshold       float64 `json:"threshold,omitempty"`
	PrefixPaddingMs int     `json:"prefix_padding_ms,omitempty"`
	SilenceDurationMs int   `json:"silence_duration_ms,omitempty"`
	CreateResponse  *bool   `json:"create_response,omitempty"`
	InterruptResponse *bool `json:"interrupt_response,omitempty"`
}

type xaiTool struct {
	Type     string          `json:"type"` // "web_search" | "x_search" | "file_search" | "function"
	Function json.RawMessage `json:"function,omitempty"`
}

type xaiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Type    string `json:"type"`
}

// xaiClient wraps the WebSocket connection to xAI's Voice Agent.
type xaiClient struct {
	cfg       *config
	conn      *websocket.Conn
	writeMu   sync.Mutex
	closeOnce sync.Once
	closed    chan struct{}

	// Callbacks set by the caller.
	OnAudioDelta      func(pcm []int16) // PCM16 24kHz mono chunk
	OnTranscript      func(role, text string) // user or assistant transcript (final)
	OnTranscriptDelta func(role, text string) // streaming transcript delta
	OnError           func(err error)
	OnSpeechStarted   func()
	OnSpeechEnded     func()
	OnResponseDone    func()
}

func newXaiClient(cfg *config) (*xaiClient, error) {
	url := fmt.Sprintf("%s?model=%s", xaiRealtimeURL, cfg.xaiModel)
	dialer := websocket.Dialer{
		HandshakeTimeout: 15 * time.Second,
		EnableCompression: false,
	}
	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+cfg.xaiAPIKey)
	conn, resp, err := dialer.Dial(url, headers)
	if err != nil {
		status := -1
		if resp != nil {
			status = resp.StatusCode
		}
		return nil, fmt.Errorf("xAI WSS dial failed (status=%d): %w", status, err)
	}
	c := &xaiClient{
		cfg:    cfg,
		conn:   conn,
		closed: make(chan struct{}),
	}
	// Configure session
	if err := c.sendSessionUpdate(); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return c, nil
}

func (c *xaiClient) sendSessionUpdate() error {
	turn := &xaiTurnDet{
		Type:              "server_vad",
		Threshold:         c.cfg.vadThreshold,
		PrefixPaddingMs:   c.cfg.vadPrefixMs,
		SilenceDurationMs: c.cfg.vadSilenceMs,
	}
	createResp := true
	turn.CreateResponse = &createResp
	turn.InterruptResponse = &createResp

	sess := &xaiSessionCfg{
		Voice:         c.cfg.xaiVoice,
		Instructions:  c.cfg.instructions,
		TurnDetection: turn,
	}
	ev := xaiEvent{Type: "session.update", Session: sess}
	return c.writeJSON(ev)
}

// AppendAudio sends a base64-encoded PCM16 24kHz mono chunk to xAI.
// Call this continuously with ~100ms chunks (xaiChunkBytes bytes).
func (c *xaiClient) AppendAudio(pcm []byte) error {
	if len(pcm) == 0 {
		return nil
	}
	ev := xaiEvent{Type: "input_audio_buffer.append", Audio: base64.StdEncoding.EncodeToString(pcm)}
	return c.writeJSON(ev)
}

// CommitAudio forces end-of-utterance (optional; server VAD will do this automatically).
func (c *xaiClient) CommitAudio() error {
	return c.writeJSON(xaiEvent{Type: "input_audio_buffer.commit"})
}

// CreateResponse forces the model to respond (optional; server VAD will do this automatically).
func (c *xaiClient) CreateResponse() error {
	return c.writeJSON(xaiEvent{Type: "response.create"})
}

// CancelResponse interrupts the current response.
func (c *xaiClient) CancelResponse() error {
	return c.writeJSON(xaiEvent{Type: "response.cancel"})
}

// SendUserText injects a text message as if the user said it (useful for smoke tests).
func (c *xaiClient) SendUserText(text string) error {
	item := map[string]any{
		"type": "message",
		"role": "user",
		"content": []map[string]any{
			{"type": "input_text", "text": text},
		},
	}
	itemJSON, _ := json.Marshal(item)
	ev := xaiEvent{Type: "conversation.item.create", Item: itemJSON}
	if err := c.writeJSON(ev); err != nil {
		return err
	}
	return c.CreateResponse()
}

// ReadEvents pumps the WebSocket read loop. Returns when ctx is cancelled or the connection drops.
func (c *xaiClient) ReadEvents(ctx context.Context) error {
	defer c.Close()
	for {
		_, data, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				return nil
			}
			if c.OnError != nil {
				c.OnError(err)
			}
			return err
		}
		var ev xaiEvent
		if err := json.Unmarshal(data, &ev); err != nil {
			log.Printf("xai: invalid JSON event: %v (raw=%s)", err, truncate(string(data), 200))
			continue
		}
		// Verbose dump of all events; enable with XAI_DEBUG_EVENTS=1
		if os.Getenv("XAI_DEBUG_EVENTS") != "" {
			log.Printf("xai event [%s] raw=%s", ev.Type, truncate(string(data), 400))
		}
		c.handleEvent(&ev)
	}
}

func (c *xaiClient) handleEvent(ev *xaiEvent) {
	switch ev.Type {
	case "response.output_audio.delta":
		if ev.Delta == "" {
			return
		}
		b, err := base64.StdEncoding.DecodeString(ev.Delta)
		if err != nil {
			log.Printf("xai: bad base64 in audio delta: %v", err)
			return
		}
		if len(b)%2 != 0 {
			log.Printf("xai: audio delta has odd byte count (%d); truncating", len(b))
			b = b[:len(b)-1]
		}
		pcm := make([]int16, len(b)/2)
		for i := range pcm {
			pcm[i] = int16(binary.LittleEndian.Uint16(b[i*2:]))
		}
		if c.OnAudioDelta != nil {
			c.OnAudioDelta(pcm)
		}
	case "response.output_audio_transcript.delta":
		// xAI streams the transcript as plain text deltas in the
		// top-level `delta` field. Stream them via OnTranscriptDelta.
		if ev.Delta != "" && c.OnTranscriptDelta != nil {
			c.OnTranscriptDelta("assistant", ev.Delta)
		}
	case "response.output_audio_transcript.done":
		// Final transcript is in the top-level `transcript` field.
		if ev.Transcript != "" && c.OnTranscript != nil {
			c.OnTranscript("assistant", ev.Transcript)
		}
	case "response.output_text.done":
		// For text-only responses (no audio), capture the text.
		var item struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal(ev.Item, &item); err == nil && item.Text != "" && c.OnTranscript != nil {
			c.OnTranscript("assistant", item.Text)
		}
	case "conversation.item.input_audio_transcription.completed":
		var item struct {
			Transcript string `json:"transcript"`
		}
		if err := json.Unmarshal(ev.Item, &item); err == nil && c.OnTranscript != nil {
			c.OnTranscript("user", item.Transcript)
		}
	case "input_audio_buffer.speech_started":
		if c.OnSpeechStarted != nil {
			c.OnSpeechStarted()
		}
	case "input_audio_buffer.speech_stopped":
		if c.OnSpeechEnded != nil {
			c.OnSpeechEnded()
		}
	case "response.done":
		if c.OnResponseDone != nil {
			c.OnResponseDone()
		}
	case "error":
		if ev.Error != nil {
			log.Printf("xai error: %s %s — %s", ev.Error.Type, ev.Error.Code, ev.Error.Message)
			if c.OnError != nil {
				c.OnError(errors.New(ev.Error.Message))
			}
		}
	default:
		// Quietly log unknown events at debug level
		log.Printf("xai: unhandled event %s", ev.Type)
	}
}

func (c *xaiClient) writeJSON(v any) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if err := c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil {
		return err
	}
	return c.conn.WriteJSON(v)
}

func (c *xaiClient) Close() {
	c.closeOnce.Do(func() {
		close(c.closed)
		if c.conn != nil {
			_ = c.conn.WriteMessage(websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
			_ = c.conn.Close()
		}
	})
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
