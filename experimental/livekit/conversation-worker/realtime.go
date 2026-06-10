// OpenAI Realtime API WebSocket client (GA schema).
//
// Realtime is a single WebSocket-based API that bundles STT + LLM + TTS
// into one streaming protocol. This file uses the GA (generally
// available) schema as of 2026-06:
//
//   session.update payload (audio nested under session.audio):
//     {
//       "type": "session.update",
//       "session": {
//         "type": "realtime",
//         "instructions": "...",
//         "audio": {
//           "input": {
//             "format": {"type": "audio/pcm", "rate": 24000},
//             "transcription": {"model": "whisper-1"},
//             "turn_detection": {"type": "server_vad", ...}
//           },
//           "output": {
//             "format": {"type": "audio/pcm", "rate": 24000},
//             "voice": "marin"
//           }
//         }
//       }
//     }
//
//   Audio events (GA names; beta names were different):
//     response.output_audio.delta            (was: response.audio.delta)
//     response.output_audio.done             (was: response.audio.done)
//     response.output_audio_transcript.delta (was: response.audio_transcript.delta)
//     response.output_text.delta             (was: response.text.delta)
//
//   Audio format: PCM16 24kHz mono little-endian. Only 24 kHz is
//   supported for PCM. Other formats (G.711 PCMU/PCMA) are 8 kHz.
//
//   Reference: https://developers.openai.com/api/reference/resources/realtime/client-events
package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"sync"

	"github.com/gorilla/websocket"
)

const realtimeWSSURL = "wss://api.openai.com/v1/realtime"

// realtimeAudioConfig is the GA session.audio object.
type realtimeAudioConfig struct {
	Input  *realtimeAudioInput  `json:"input,omitempty"`
	Output *realtimeAudioOutput `json:"output,omitempty"`
}

type realtimeAudioInput struct {
	Format         *realtimeAudioFormat  `json:"format,omitempty"`
	Transcription  *inputTranscription   `json:"transcription,omitempty"`
	TurnDetection  *turnDetection        `json:"turn_detection,omitempty"`
	NoiseReduction *realtimeNoiseReduction `json:"noise_reduction,omitempty"`
}

type realtimeAudioOutput struct {
	Format *realtimeAudioFormat `json:"format,omitempty"`
	Voice  string               `json:"voice,omitempty"`
	Speed  float64              `json:"speed,omitempty"`
}

// realtimeAudioFormat is GA's nested format object. For PCM the
// rate is fixed at 24000 and type is "audio/pcm".
type realtimeAudioFormat struct {
	Type string `json:"type"`
	Rate int    `json:"rate"`
}

// realtimeNoiseReduction is GA's noise reduction config. Disabled
// by passing nil. The default is null in the session.
type realtimeNoiseReduction struct {
	Type string `json:"type"`
}

type inputTranscription struct {
	Model string `json:"model"`
}

// turnDetection configures the server-side VAD. Type "server_vad" is
// the only one supported in the standard Realtime API. Higher
// threshold = less sensitive. Longer silence_duration_ms = the user
// has to be quiet longer before Realtime commits the buffer.
type turnDetection struct {
	Type              string  `json:"type"`
	Threshold         float64 `json:"threshold"`
	PrefixPaddingMs   int     `json:"prefix_padding_ms"`
	SilenceDurationMs int     `json:"silence_duration_ms"`
}

// realtimeSessionConfig is the body of a session.update event. The
// model is selected in the URL.
type realtimeSessionConfig struct {
	Type         string               `json:"type"`
	Instructions string               `json:"instructions,omitempty"`
	Audio        *realtimeAudioConfig `json:"audio,omitempty"`
}

// realtimeClient is a thin wrapper over the Realtime WebSocket. The
// caller registers callbacks for the events it cares about.
type realtimeClient struct {
	conn *websocket.Conn

	// OnAudio is called for each response.output_audio.delta event.
	// pcm is PCM16 24kHz mono. Must be non-blocking.
	OnAudio func(pcm []byte)
	// OnUserTranscript is called for
	// conversation.item.input_audio_transcription.completed.
	OnUserTranscript func(text string)
	// OnAssistantTranscript is called for
	// response.output_audio_transcript.delta (concatenated by caller).
	OnAssistantTranscript func(text string)
	// OnError is called for "error" events.
	OnError func(event map[string]interface{})

	mu     sync.Mutex
	closed bool
}

// newRealtimeClient dials the Realtime WSS endpoint and configures
// the session. It returns once the session.update has been written.
// The read loop starts in a background goroutine.
func newRealtimeClient(apiKey, model string, cfg realtimeSessionConfig) (*realtimeClient, error) {
	url := fmt.Sprintf("%s?model=%s", realtimeWSSURL, model)
	headers := map[string][]string{
		"Authorization": {"Bearer " + apiKey},
	}
	conn, resp, err := websocket.DefaultDialer.Dial(url, headers)
	if err != nil {
		status := -1
		if resp != nil {
			status = resp.StatusCode
		}
		return nil, fmt.Errorf("realtime: dial %s status=%d: %w", url, status, err)
	}

	rc := &realtimeClient{conn: conn}
	if err := rc.sendSessionUpdate(cfg); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("realtime: session.update: %w", err)
	}
	log.Printf("realtime: connected model=%s voice=%s vad=%v",
		model, voiceFromCfg(cfg), cfg.Audio != nil && cfg.Audio.Input != nil && cfg.Audio.Input.TurnDetection != nil)

	go rc.readLoop()
	return rc, nil
}

func voiceFromCfg(cfg realtimeSessionConfig) string {
	if cfg.Audio != nil && cfg.Audio.Output != nil {
		return cfg.Audio.Output.Voice
	}
	return ""
}

func (rc *realtimeClient) sendSessionUpdate(cfg realtimeSessionConfig) error {
	payload := map[string]interface{}{
		"type":    "session.update",
		"session": cfg,
	}
	return rc.conn.WriteJSON(payload)
}

// sendAudio appends a PCM16 24kHz mono chunk to the input buffer.
// Safe to call from any goroutine.
func (rc *realtimeClient) sendAudio(pcm []byte) error {
	payload := map[string]interface{}{
		"type":  "input_audio_buffer.append",
		"audio": base64.StdEncoding.EncodeToString(pcm),
	}
	return rc.conn.WriteJSON(payload)
}

// createResponse forces the model to produce a reply without waiting
// for server VAD. Used for the initial greeting.
func (rc *realtimeClient) createResponse(instructions string) error {
	payload := map[string]interface{}{
		"type": "response.create",
	}
	if instructions != "" {
		payload["response"] = map[string]interface{}{
			"instructions": instructions,
		}
	}
	return rc.conn.WriteJSON(payload)
}

func (rc *realtimeClient) readLoop() {
	for {
		_, raw, err := rc.conn.ReadMessage()
		if err != nil {
			rc.mu.Lock()
			closed := rc.closed
			rc.mu.Unlock()
			if !closed {
				log.Printf("realtime: read error (closed=%t): %v", closed, err)
			}
			return
		}
		var msg map[string]interface{}
		if err := json.Unmarshal(raw, &msg); err != nil {
			log.Printf("realtime: bad json: %v body=%s", err, string(raw))
			continue
		}
		eventType, _ := msg["type"].(string)
		switch eventType {
		case "response.output_audio.delta":
			if delta, ok := msg["delta"].(string); ok {
				if pcm, err := base64.StdEncoding.DecodeString(delta); err == nil {
					if rc.OnAudio != nil {
						rc.OnAudio(pcm)
					}
				} else {
					log.Printf("realtime: output_audio.delta base64: %v", err)
				}
			}
		case "response.audio.delta":
			// Legacy event name (some server versions).
			if delta, ok := msg["delta"].(string); ok {
				if pcm, err := base64.StdEncoding.DecodeString(delta); err == nil {
					if rc.OnAudio != nil {
						rc.OnAudio(pcm)
					}
				}
			}
		case "conversation.item.input_audio_transcription.completed":
			if transcript, ok := msg["transcript"].(string); ok && rc.OnUserTranscript != nil {
				rc.OnUserTranscript(transcript)
			}
		case "response.output_audio_transcript.delta":
			if delta, ok := msg["delta"].(string); ok && rc.OnAssistantTranscript != nil {
				rc.OnAssistantTranscript(delta)
			}
		case "response.output_text.delta":
			if delta, ok := msg["delta"].(string); ok && rc.OnAssistantTranscript != nil {
				rc.OnAssistantTranscript(delta)
			}
		case "error":
			body, _ := json.Marshal(msg)
			log.Printf("realtime: ERROR event: %s", string(body))
			if rc.OnError != nil {
				rc.OnError(msg)
			}
		case "session.created", "session.updated":
			log.Printf("realtime: %s", eventType)
		case "input_audio_buffer.speech_started", "input_audio_buffer.speech_stopped":
			log.Printf("realtime: %s", eventType)
		case "response.done":
			log.Printf("realtime: response.done")
		default:
			// Log unknown events for debugging. For most events
			// we log the full body (events are small, except for
			// audio deltas which we never get here). For
			// content_part.added/output_item.added we want the
			// full body to see what content type the model
			// produced.
			log.Printf("realtime: UNHANDLED event_type=%q body=%s", eventType, string(raw))
		}
	}
}

func (rc *realtimeClient) close() {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	if rc.closed {
		return
	}
	rc.closed = true
	_ = rc.conn.WriteMessage(websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
	_ = rc.conn.Close()
}

// itoa is a small helper to format an int as a string without
// pulling in strconv for the unused default branch. (Above import
// is used here.)
func itoa(n int) string { return strconv.Itoa(n) }
