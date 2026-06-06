// xai-voice-agent — isolated harness for Plan D (xAI Voice Agent + Eve).
//
// This program is a minimal Go WebSocket client to xAI's Grok Voice
// Agent API, bridged to a LiveKit room. It is the spike harness for
// validating Plan D end-to-end in the VoxLane context.
//
// Flow (audio):
//
//   browser mic
//     -> LiveKit RTP Opus 48kHz
//     -> ffmpeg decode -> PCM16 24kHz mono
//     -> xai WSS input_audio_buffer.append
//     -> xAI server VAD, Grok 4.3 LLM, Eve TTS
//     -> xai WSS response.output_audio.delta (PCM16 24kHz mono)
//     -> ffmpeg encode -> Opus 48kHz
//     -> LiveKit RTP publish
//     -> browser speaker
//
// The harness is INTENTIONALLY a separate binary, not a mode in the
// existing conversation-worker. Per the manager's instructions: "If the
// official LiveKit integration is not practical in the current Go worker
// quickly: create an isolated harness under: experimental/livekit/xai-voice-agent/."
//
// What stays in the existing conversation-worker (not deleted):
//   - stitched mode (Stage 1, gpt-4o-mini + Cartesia, in prod)
//   - realtime-cartesia mode (Stage 1.5, current spike)
//   - All OGG/ffmpeg/audio plumbing
//
// Production .env, prod gateway, prod systemd, Telnyx prod webhook: untouched.
package main

import (
	"context"
	"flag"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"
)

const (
	defaultLiveKitURL    = "wss://ai-voice-assistant-314hy5b3.livekit.cloud"
	defaultRoom          = "voxlane-conv-spike"
	defaultIdentity      = "xai-voice-agent-harness"
	defaultXaiModel      = "grok-voice-latest"
	defaultXaiVoice      = "eve"
	defaultVadSilenceMs  = 1500
	defaultVadPrefixMs   = 300
	defaultVadThreshold  = 0.7
	defaultOpusBitrate   = 96000
)

func main() {
	envFile := flag.String("env", "", "path to .env file (default: ./xai-voice-agent.env)")
	roomName := flag.String("room", defaultRoom, "LiveKit room to join")
	identity := flag.String("identity", defaultIdentity, "participant identity")
	model := flag.String("model", defaultXaiModel, "xAI model (grok-voice-latest or grok-voice-think-fast-1.0)")
	voice := flag.String("voice", defaultXaiVoice, "xAI voice (ara, eve, leo, rex, sal)")
	silenceMs := flag.Int("vad-silence-ms", defaultVadSilenceMs, "xAI server VAD silence duration in ms (1500-2000 for production)")
	prefixMs := flag.Int("vad-prefix-ms", defaultVadPrefixMs, "xAI server VAD prefix padding in ms")
	threshold := flag.Float64("vad-threshold", defaultVadThreshold, "xAI server VAD threshold (0.0-1.0)")
	opusBitrate := flag.Int("opus-bitrate", defaultOpusBitrate, "outbound Opus bitrate to LiveKit (bps)")
	instructions := flag.String("instructions", defaultInstructions, "system prompt for the agent")
	toolsFile := flag.String("tools", "", "path to JSON file with xAI tools (function-calling schema)")
	noLiveKit := flag.Bool("no-livekit", false, "skip LiveKit; read audio from stdin, write audio to stdout (smoke test only)")
	autoMsg := flag.String("auto-msg", "", "smoke-test only: send this single message and exit after response.done")
	flag.Parse()

	if *envFile != "" {
		if err := godotenv.Load(*envFile); err != nil {
			log.Printf("note: could not load %s: %v", *envFile, err)
		}
	} else {
		_ = godotenv.Load("xai-voice-agent.env")
		_ = godotenv.Load("../conversation-worker/.env")
		_ = godotenv.Load("/opt/ai-voice-receptionist/experimental/livekit/.env")
	}

	cfg := &config{
		livekitURL:   getenv("LIVEKIT_URL", defaultLiveKitURL),
		apiKey:       os.Getenv("LIVEKIT_API_KEY"),
		apiSecret:    os.Getenv("LIVEKIT_API_SECRET"),
		roomName:     *roomName,
		identity:     *identity,
		xaiAPIKey:    os.Getenv("XAI_API_KEY"),
		xaiModel:     *model,
		xaiVoice:     *voice,
		vadSilenceMs: *silenceMs,
		vadPrefixMs:  *prefixMs,
		vadThreshold: *threshold,
		opusBitrate:  *opusBitrate,
		instructions: *instructions,
		noLiveKit:    *noLiveKit,
		autoMsg:      *autoMsg,
	}

	if *toolsFile != "" {
		tools, err := loadToolsFile(*toolsFile)
		if err != nil {
			log.Fatalf("load tools: %v", err)
		}
		cfg.tools = tools
		log.Printf("loaded %d tools from %s", len(tools), *toolsFile)
	}

	if cfg.xaiAPIKey == "" {
		log.Fatalf("XAI_API_KEY is not set. Add it to xai-voice-agent.env (or to the spike .env on the VPS).")
	}
	if !cfg.noLiveKit {
		if cfg.apiKey == "" || cfg.apiSecret == "" {
			log.Fatalf("LIVEKIT_API_KEY / LIVEKIT_API_SECRET not set. Cannot connect to LiveKit.")
		}
	}

	log.Printf("xai-voice-agent starting:")
	log.Printf("  model=%s voice=%s", cfg.xaiModel, cfg.xaiVoice)
	log.Printf("  VAD: silence=%dms prefix=%dms threshold=%.2f", cfg.vadSilenceMs, cfg.vadPrefixMs, cfg.vadThreshold)
	log.Printf("  LiveKit: url=%s room=%s identity=%s no_livekit=%v", cfg.livekitURL, cfg.roomName, cfg.identity, cfg.noLiveKit)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		log.Printf("shutdown signal received; cancelling")
		cancel()
	}()

	if cfg.noLiveKit {
		if err := runSmokeTest(ctx, cfg); err != nil {
			log.Fatalf("smoke test failed: %v", err)
		}
		return
	}

	if err := runLiveKitBridge(ctx, cfg); err != nil {
		log.Fatalf("livekit bridge failed: %v", err)
	}
	_ = time.Second // keep import
}

type config struct {
	livekitURL   string
	apiKey       string
	apiSecret    string
	roomName     string
	identity     string
	xaiAPIKey    string
	xaiModel     string
	xaiVoice     string
	vadSilenceMs int
	vadPrefixMs  int
	vadThreshold float64
	opusBitrate  int
	instructions string
	tools        []xaiTool
	noLiveKit    bool
	autoMsg      string
}

const defaultInstructions = "You are Alex, a warm, calm restaurant receptionist. Reply naturally and briefly. Ask one question at a time. Never invent restaurant facts. If you do not know, offer to take a message or arrange a callback."

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// loadToolsFile reads a JSON file with the xAI tools array. Expected format:
//
//	[
//	  { "type": "function", "function": { "name": "...", "description": "...",
//	    "parameters": { ... } } },
//	  ...
//	]
func loadToolsFile(path string) ([]xaiTool, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var tools []xaiTool
	if err := json.Unmarshal(b, &tools); err != nil {
		return nil, fmt.Errorf("parse tools JSON: %w", err)
	}
	return tools, nil
}
