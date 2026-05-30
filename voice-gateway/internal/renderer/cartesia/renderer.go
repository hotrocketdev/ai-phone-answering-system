// Package cartesia implements the renderer.Renderer interface for Cartesia Sonic TTS.
// STATUS: Implemented against documented API — untested (no API key).
//
// Cartesia provides low-latency streaming British voice synthesis.
// Audio output: PCM16 8kHz → converted to u-law for Twilio compatibility.
package cartesia

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/voxlane/voice-gateway/internal/renderer"
)

// ─── Config ──────────────────────────────────────────────────────────────

type Config struct {
	APIKey   string
	VoiceID  string // Cartesia voice ID for British female
	ModelID  string // "sonic-2" for streaming
	Language string // "en"
	Speed    float64
	Volume   float64
	Emotion  string
}

// ─── Renderer ────────────────────────────────────────────────────────────

type Renderer struct {
	cfg    Config
	conn   *websocket.Conn
	mu     sync.Mutex
	ctx    context.Context
	cancel context.CancelFunc
}

func New(cfg Config) *Renderer {
	return &Renderer{cfg: cfg}
}

func (r *Renderer) Provider() renderer.Provider { return renderer.ProviderCartesia }

// Render collects all audio chunks into a single byte slice.
func (r *Renderer) Render(ctx context.Context, text string) ([]byte, error) {
	ch, err := r.RenderStream(ctx, text)
	if err != nil {
		return nil, err
	}
	var result []byte
	for chunk := range ch {
		result = append(result, chunk...)
	}
	return result, nil
}

// RenderStream connects to Cartesia, sends text, and returns PCM16 8kHz audio chunks.
func (r *Renderer) RenderStream(ctx context.Context, text string) (<-chan []byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.ctx, r.cancel = context.WithCancel(ctx)

	headers := http.Header{}
	headers.Set("X-API-Key", r.cfg.APIKey)
	headers.Set("Cartesia-Version", "2024-06-01")

	conn, _, err := websocket.DefaultDialer.DialContext(r.ctx,
		"wss://api.cartesia.ai/tts/websocket", headers)
	if err != nil {
		r.cancel()
		return nil, fmt.Errorf("cartesia connect: %w", err)
	}
	r.conn = conn

	// Send streaming synthesis request
	req := map[string]interface{}{
		"model_id":   r.cfg.ModelID,
		"transcript": text,
		"context_id": fmt.Sprintf("voxlane-%d", time.Now().UnixNano()),
		"continue":   false,
		"voice":      map[string]interface{}{"mode": "id", "id": r.cfg.VoiceID},
		"output_format": map[string]interface{}{
			"container":   "raw",
			"encoding":    "pcm_mulaw",
			"sample_rate": 8000,
		},
		"language": r.cfg.Language,
		"generation_config": map[string]interface{}{
			"speed":   r.cfg.Speed,
			"volume":  r.cfg.Volume,
			"emotion": r.cfg.Emotion,
		},
	}

	if err := conn.WriteJSON(req); err != nil {
		conn.Close()
		r.cancel()
		return nil, fmt.Errorf("cartesia write: %w", err)
	}

	log.Printf("[cartesia] streaming: voice=%s model=%s", r.cfg.VoiceID, r.cfg.ModelID)

	ch := make(chan []byte, 8)
	go r.readLoop(ch)
	return ch, nil
}

// readLoop reads binary PCM16 chunks from Cartesia WebSocket.
func (r *Renderer) readLoop(ch chan<- []byte) {
	defer func() {
		if r.conn != nil { r.conn.Close() }
		close(ch)
	}()

	for {
		select {
		case <-r.ctx.Done():
			return
		default:
		}

		msgType, data, err := r.conn.ReadMessage()
		if err != nil {
			return
		}

		if msgType == websocket.BinaryMessage {
			select {
			case ch <- data:
			case <-r.ctx.Done():
				return
			}
			continue
		}

		// Text message — check for done signal
		var msg struct {
			Type string `json:"type"`
			Done bool   `json:"done"`
		}
		json.Unmarshal(data, &msg)
		if msg.Done || msg.Type == "done" {
			return
		}
	}
}

// Cancel stops the current synthesis stream.
func (r *Renderer) Cancel() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cancel != nil { r.cancel() }
	if r.conn != nil { r.conn.Close() }
}

func (r *Renderer) Close() error {
	r.Cancel()
	return nil
}

// ─── Audio Conversion ────────────────────────────────────────────────────

// ConvertPCM16ToMulaw converts PCM16 little-endian bytes to u-law for Twilio.
func ConvertPCM16ToMulaw(pcm16 []byte) []byte {
	mulaw := make([]byte, len(pcm16)/2)
	for i := 0; i < len(pcm16); i += 2 {
		sample := int16(binary.LittleEndian.Uint16(pcm16[i:]))
		mulaw[i/2] = pcm16ToUlauTable[uint16(sample)]
	}
	return mulaw
}

var pcm16ToUlauTable [65536]byte

func init() {
	for i := int(-32768); i <= 32767; i++ {
		pcm16ToUlauTable[uint16(i)] = encodeUlau(int16(i))
	}
}

func encodeUlau(sample int16) byte {
	sign := byte(0)
	if sample < 0 { sign = 0x80; sample = -sample }
	mag := int(sample) + 132
	exp := 7
	for e := 0; e < 8; e++ {
		if mag < (256 << e) { exp = e; break }
	}
	mantissa := byte((mag >> (exp + 3)) & 0x0F)
	ulaw := ^byte((byte(exp) << 4) | mantissa)
	if sign != 0 { ulaw &^= 0x80 }
	return ulaw
}

// Recommended British voice placeholder. Replace with real Cartesia voice ID.
const DefaultBritishVoiceID = "REPLACE_WITH_CARTESIA_VOICE_ID"
const DefaultModel = "sonic-2"
