// Package deepgram implements the runtime.Session interface for Deepgram Voice Agent.
// STATUS: Experimental — not yet tested with real calls.
//
// Deepgram provides full-duplex STT + LLM + British TTS in one WebSocket connection.
package deepgram

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"

	"github.com/voxlane/voice-gateway/internal/runtime"
)

// ─── Agent ───────────────────────────────────────────────────────────────

type Agent struct {
	cfg      runtime.Config
	conn     *websocket.Conn
	audioOut chan []byte
	done     chan struct{}
	mu       sync.Mutex
	ctx      context.Context
	cancel   context.CancelFunc
}

// New creates a Deepgram Voice Agent session.
func New(ctx context.Context, cfg runtime.Config) (*Agent, error) {
	headers := http.Header{}
	headers.Set("Authorization", "Token "+cfg.DeepgramAPIKey)

	conn, _, err := websocket.DefaultDialer.DialContext(ctx,
		"wss://agent.deepgram.com/v1/agent/converse", headers)
	if err != nil {
		return nil, fmt.Errorf("deepgram connect: %w", err)
	}

	a := &Agent{
		cfg:      cfg,
		conn:     conn,
		audioOut: make(chan []byte, 8),
		done:     make(chan struct{}),
	}
	a.ctx, a.cancel = context.WithCancel(ctx)

	return a, nil
}

// ─── Interface ───────────────────────────────────────────────────────────

func (a *Agent) Provider() runtime.Provider { return runtime.ProviderDeepgramAgent }

func (a *Agent) Start(ctx context.Context) error {
	// Send Settings payload
	settings := map[string]interface{}{
		"type": "Settings",
		"agent": map[string]interface{}{
			"listen": map[string]interface{}{
				"model":    a.cfg.DeepgramListenModel,
				"language": a.cfg.DeepgramListenLang,
			},
			"think": map[string]interface{}{
				"provider": map[string]interface{}{
					"type": a.cfg.DeepgramThinkProvider,
				},
				"model": a.cfg.DeepgramThinkModel,
			},
			"speak": map[string]interface{}{
				"model": a.cfg.DeepgramTTSModel,
			},
		},
	}

	if a.cfg.DeepgramThinkProvider == "open_ai" && a.cfg.OpenAIAPIKey != "" {
		settings["agent"].(map[string]interface{})["think"].(map[string]interface{})["provider"].(map[string]interface{})["endpoint"] = map[string]interface{}{
			"url": "https://api.openai.com/v1",
			"headers": map[string]interface{}{
				"Authorization": "Bearer " + a.cfg.OpenAIAPIKey,
			},
		}
	}

	if err := a.conn.WriteJSON(settings); err != nil {
		return fmt.Errorf("deepgram settings: %w", err)
	}

	log.Printf("[deepgram] settings sent (listen=%s, speak=%s, think=%s/%s)",
		a.cfg.DeepgramListenModel, a.cfg.DeepgramTTSModel,
		a.cfg.DeepgramThinkProvider, a.cfg.DeepgramThinkModel)

	// Start read loop
	go a.readLoop(ctx)
	return nil
}

func (a *Agent) SendAudio(payload []byte) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.conn == nil {
		return fmt.Errorf("deepgram: not connected")
	}
	return a.conn.WriteMessage(websocket.BinaryMessage, payload)
}

func (a *Agent) AudioOut() chan []byte { return a.audioOut }
func (a *Agent) Done() chan struct{}   { return a.done }

func (a *Agent) Close() error {
	a.cancel()
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.conn != nil {
		a.conn.Close()
	}
	close(a.done)
	return nil
}

// ─── Read Loop ───────────────────────────────────────────────────────────

func (a *Agent) readLoop(ctx context.Context) {
	defer func() {
		a.Close()
		close(a.audioOut)
	}()

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		msgType, data, err := a.conn.ReadMessage()
		if err != nil {
			return
		}

		// Binary = audio output
		if msgType == websocket.BinaryMessage {
			select {
			case a.audioOut <- data:
			case <-ctx.Done():
				return
			}
			continue
		}

		// Text = control events
		var base struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(data, &base); err != nil {
			continue
		}

		switch base.Type {
		case "UserStartedSpeaking":
			log.Printf("[deepgram] barge-in detected")
			// Flush queued audio
			for len(a.audioOut) > 0 {
				<-a.audioOut
			}

		case "AgentAudioDone":
			log.Printf("[deepgram] agent audio done")

		case "ConversationText":
			var msg struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			}
			json.Unmarshal(data, &msg)
			log.Printf("[deepgram] transcript: [%s] %s", msg.Role, msg.Content)

		case "Error":
			log.Printf("[deepgram] error: %s", string(data))
		}
	}
}
