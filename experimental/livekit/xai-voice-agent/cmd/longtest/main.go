// cmd/longtest/main.go — 30-minute production-style xAI Voice Agent validation.
//
// Automated fallback for the manager-mandated 30-min test. Sends a sequence
// of realistic restaurant scenarios as user text messages via the WSS-only
// path (no LiveKit, no audio I/O) and logs structured METRIC events.
//
// Usage:
//
//	go build -o xai-longtest.exe ./cmd/longtest
//	./xai-longtest --env xai-voice-agent.env --tools ../tools-booking.json \
//	    --scenarios scenarios-30min.json --duration 30m --log longtest.log
//
//	# Single-shot mode:
//	./xai-longtest --env xai-voice-agent.env --auto-msg "Book 4 at 7pm tomorrow"
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

const (
	xaiRealtimeURL  = "wss://api.x.ai/v1/realtime"
	defaultInstr    = "You are Alex, a warm, calm restaurant receptionist. Reply naturally and briefly. Ask one question at a time. Never invent restaurant facts. If you do not know, offer to take a message or arrange a callback."
)

// _ ensures strings package is referenced (used in main() flag init).
var _ = strings.TrimSpace

type scenario struct {
	Category string `json:"category"`
	User     string `json:"user"`
}

type testConfig struct {
	APIKey       string
	Model        string
	Voice        string
	Tools        json.RawMessage
	Instructions string
	Scenarios    []scenario
	Duration     time.Duration
	PerTurnWait  time.Duration
	PauseMs      int
}

func main() {
	var (
		envFile     = flag.String("env", "xai-voice-agent.env", "path to .env file")
		tools       = flag.String("tools", "", "path to tools JSON file")
		sceneF      = flag.String("scenarios", "cmd/longtest/scenarios-30min.json", "path to scenarios JSON file (default: ./cmd/longtest/scenarios-30min.json)")
		dur         = flag.Duration("duration", 30*time.Minute, "total test duration")
		perWait     = flag.Duration("per-turn-wait", 30*time.Second, "max wait per turn")
		logFile     = flag.String("log", "", "log file (default: stdout)")
		model       = flag.String("model", "grok-voice-latest", "xAI model")
		voice       = flag.String("voice", "eve", "xAI voice")
		autoMsg     = flag.String("auto-msg", "", "single-shot mode: send this text and exit on response.done")
		instructions = flag.String("instructions", "", "path to system instructions file (or string starting with @)")
		pauseMs     = flag.Int("pause-ms", 800, "ms to pause between scenarios")
	)
	flag.Parse()

	if *logFile != "" {
		f, err := os.OpenFile(*logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			log.Fatalf("open log: %v", err)
		}
		log.SetOutput(f)
	}

	cfg := testConfig{
		APIKey:       loadAPIKey(*envFile),
		Model:        *model,
		Voice:        *voice,
		Duration:     *dur,
		PerTurnWait:  *perWait,
		PauseMs:      *pauseMs,
		Instructions: *instructions,
	}

	if cfg.APIKey == "" {
		log.Fatalf("XAI_API_KEY not set; add it to %s", *envFile)
	}

	if *tools != "" {
		b, err := os.ReadFile(*tools)
		if err != nil {
			log.Fatalf("read tools: %v", err)
		}
		cfg.Tools = b
	}

	if cfg.Instructions != "" {
		if strings.HasPrefix(cfg.Instructions, "@") {
			b, err := os.ReadFile(strings.TrimPrefix(cfg.Instructions, "@"))
			if err != nil {
				log.Fatalf("read instructions: %v", err)
			}
			cfg.Instructions = strings.TrimSpace(string(b))
		}
	} else {
		cfg.Instructions = defaultInstr
	}

	if *sceneF != "" {
		if _, err := os.Stat(*sceneF); err == nil {
			b, err := os.ReadFile(*sceneF)
			if err != nil {
				log.Fatalf("read scenarios: %v", err)
			}
			if err := json.Unmarshal(b, &cfg.Scenarios); err != nil {
				log.Fatalf("parse scenarios: %v", err)
			}
		}
	}

	v := newValidator(cfg)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	go func() { <-sigCh; log.Printf("METRIC signal=interrupt"); cancel() }()

	if err := v.connect(); err != nil {
		log.Fatalf("connect: %v", err)
	}
	defer v.Close()

	if *autoMsg != "" {
		if err := v.sendUserText(ctx, *autoMsg); err != nil {
			log.Fatalf("send: %v", err)
		}
		log.Printf("DONE single shot")
		return
	}

	if len(cfg.Scenarios) == 0 {
		log.Fatalf("no scenarios: pass --scenarios scenarios-30min.json (or --auto-msg for single shot)")
	}

	v.runLoop(ctx)
}

func loadAPIKey(envFile string) string {
	if envFile == "" {
		envFile = "xai-voice-agent.env"
	}
	if k, ok := readKeyFromFile(envFile, "XAI_API_KEY"); ok {
		return k
	}
	for _, p := range []string{".env", "../.env", "../../.env"} {
		if k, ok := readKeyFromFile(p, "XAI_API_KEY"); ok {
			return k
		}
	}
	if k := os.Getenv("XAI_API_KEY"); k != "" {
		return k
	}
	return ""
}

func readKeyFromFile(path, key string) (string, bool) {
	f, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		kv := strings.SplitN(line, "=", 2)
		if len(kv) == 2 && strings.TrimSpace(kv[0]) == key {
			v := strings.TrimSpace(kv[1])
			v = strings.Trim(v, `"'`)
			return v, true
		}
	}
	return "", false
}

type validator struct {
	cfg testConfig
	ws  *websocket.Conn
	wMu sync.Mutex

	turnCounter     int64
	functionCallCnt int64
	transcriptCnt   int64
	errorCnt        int64
	audioBytes      int64

	turnStartMs   int64
	turnAssistant string
	turnFunctions []string
	turnResponse  chan struct{}
}

func newValidator(cfg testConfig) *validator {
	return &validator{cfg: cfg, turnResponse: make(chan struct{}, 1)}
}

func (v *validator) connect() error {
	url := fmt.Sprintf("%s?model=%s", xaiRealtimeURL, v.cfg.Model)
	dialer := websocket.Dialer{HandshakeTimeout: 15 * time.Second, EnableCompression: false}
	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+v.cfg.APIKey)
	conn, resp, err := dialer.Dial(url, headers)
	if err != nil {
		status := -1
		if resp != nil {
			status = resp.StatusCode
		}
		return fmt.Errorf("WSS dial failed (status=%d): %w", status, err)
	}
	v.ws = conn

	tools := []json.RawMessage{}
	if len(v.cfg.Tools) > 0 {
		tools = append(tools, v.cfg.Tools)
	}
	sess := map[string]any{
		"type": "session.update",
		"session": map[string]any{
			"voice":          v.cfg.Voice,
			"turn_detection": map[string]any{"type": "server_vad", "threshold": 0.7, "prefix_padding_ms": 300, "silence_duration_ms": 1500},
			"tools":          tools,
			"tool_choice":    "auto",
			"instructions":   v.cfg.Instructions,
		},
	}
	if err := v.writeJSON(sess); err != nil {
		return fmt.Errorf("session.update: %w", err)
	}
	// Wait for session.updated to ensure the server has applied our config
	// before we send the first user message.
	if err := v.waitForSessionUpdated(8 * time.Second); err != nil {
		return fmt.Errorf("waiting for session.updated: %w", err)
	}
	log.Printf("METRIC session_connect url=%s model=%s voice=%s", xaiRealtimeURL, v.cfg.Model, v.cfg.Voice)
	return nil
}

func (v *validator) waitForSessionUpdated(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		_ = v.ws.SetReadDeadline(time.Now().Add(timeout))
		var raw json.RawMessage
		if err := v.ws.ReadJSON(&raw); err != nil {
			return err
		}
		var ev map[string]json.RawMessage
		if err := json.Unmarshal(raw, &ev); err != nil {
			continue
		}
		var typ string
		_ = json.Unmarshal(ev["type"], &typ)
		if typ == "session.updated" {
			return nil
		}
	}
	return fmt.Errorf("session.updated not received within %s", timeout)
}

func (v *validator) writeJSON(m any) error {
	v.wMu.Lock()
	defer v.wMu.Unlock()
	return v.ws.WriteJSON(m)
}

func (v *validator) Close() {
	if v.ws != nil {
		_ = v.ws.WriteMessage(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
		_ = v.ws.Close()
	}
}

func (v *validator) runLoop(ctx context.Context) {
	deadline := time.Now().Add(v.cfg.Duration)
	log.Printf("METRIC loop_start duration=%s scenarios=%d", v.cfg.Duration, len(v.cfg.Scenarios))

	scenarioIdx := 0
	for {
		if ctx.Err() != nil {
			break
		}
		if time.Now().After(deadline) {
			log.Printf("METRIC deadline_reached")
			break
		}
		sc := v.cfg.Scenarios[scenarioIdx%len(v.cfg.Scenarios)]
		turnID := atomic.AddInt64(&v.turnCounter, 1)
		scenarioIdx++

		log.Printf("METRIC turn_start turn_id=%d scenario_idx=%d category=%s", turnID, scenarioIdx-1, sc.Category)
		turnCtx, turnCancel := context.WithTimeout(ctx, v.cfg.PerTurnWait)
		if err := v.sendUserText(turnCtx, sc.User); err != nil {
			log.Printf("METRIC turn_error turn_id=%d err=%q", turnID, err.Error())
		}
		turnCancel()

		select {
		case <-time.After(time.Duration(v.cfg.PauseMs) * time.Millisecond):
		case <-ctx.Done():
		}
	}
	log.Printf("METRIC loop_end turns=%d function_calls=%d transcripts=%d errors=%d",
		atomic.LoadInt64(&v.turnCounter),
		atomic.LoadInt64(&v.functionCallCnt),
		atomic.LoadInt64(&v.transcriptCnt),
		atomic.LoadInt64(&v.errorCnt),
	)
}

func (v *validator) sendUserText(ctx context.Context, text string) error {
	atomic.StoreInt64(&v.turnStartMs, time.Now().UnixMilli())
	v.turnAssistant = ""
	v.turnFunctions = nil
	v.turnResponse = make(chan struct{}, 1)

	item := map[string]any{
		"type": "conversation.item.create",
		"item": map[string]any{
			"type": "user",
			"content": []map[string]any{
				{"type": "input_text", "text": text},
			},
		},
	}
	if err := v.writeJSON(item); err != nil {
		return err
	}
	if err := v.writeJSON(map[string]any{"type": "response.create"}); err != nil {
		return err
	}
	return v.readUntilDone(ctx)
}

func (v *validator) readUntilDone(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		_ = v.ws.SetReadDeadline(time.Now().Add(60 * time.Second))
		var raw json.RawMessage
		if err := v.ws.ReadJSON(&raw); err != nil {
			return err
		}
		var ev map[string]json.RawMessage
		if err := json.Unmarshal(raw, &ev); err != nil {
			continue
		}
		var typ string
		_ = json.Unmarshal(ev["type"], &typ)
		switch typ {
		case "response.done":
			latencyMs := time.Now().UnixMilli() - atomic.LoadInt64(&v.turnStartMs)
			log.Printf("METRIC turn_end latency_ms=%d audio_bytes=%d assistant_chars=%d functions=%q assistant=%q",
				latencyMs, atomic.LoadInt64(&v.audioBytes), len(v.turnAssistant),
				v.turnFunctions, truncate(v.turnAssistant, 200))
			atomic.StoreInt64(&v.audioBytes, 0)
			select {
			case v.turnResponse <- struct{}{}:
			default:
			}
			return nil
		case "response.output_audio_transcript.done":
			var s string
			_ = json.Unmarshal(ev["transcript"], &s)
			if s != "" {
				atomic.AddInt64(&v.transcriptCnt, 1)
				v.turnAssistant = s
			}
		case "response.function_call_arguments.done":
			var name, args string
			_ = json.Unmarshal(ev["name"], &name)
			_ = json.Unmarshal(ev["arguments"], &args)
			atomic.AddInt64(&v.functionCallCnt, 1)
			v.turnFunctions = append(v.turnFunctions, name)
			log.Printf("METRIC function_call name=%s args=%s", name, args)
		case "response.output_audio.delta":
			var d string
			_ = json.Unmarshal(ev["delta"], &d)
			atomic.AddInt64(&v.audioBytes, int64(len(d)*3/4))
		case "error":
			var msg string
			_ = json.Unmarshal(ev["message"], &msg)
			if msg == "" {
				continue
			}
			atomic.AddInt64(&v.errorCnt, 1)
			log.Printf("METRIC error msg=%q", msg)
		}
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
