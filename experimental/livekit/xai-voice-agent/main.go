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
	defaultVadThreshold  = 0.6
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

const defaultInstructions = "You are Alex, the warm, calm receptionist at Vox Lane Bistro, a small neighbourhood restaurant in Bristol, UK. You handle phone calls, take bookings, and answer simple questions.\n" +
	"\n" +
	"Opening rule (CRITICAL — always do this on the first turn):\n" +
	"  Greet the caller warmly and ask how you can help before any other action. Example: \"Hi, you've reached Vox Lane Bistro, this is Alex. How can I help you today?\"\n" +
	"  If the caller has already stated their request in their first utterance, acknowledge it briefly and then ask the one piece of info you need to proceed.\n" +
	"\n" +
	"Behaviour rules:\n" +
	"  1. Lead the conversation — ask one question at a time, but keep the caller moving forward.\n" +
	"  2. Use normal human date and time language. If the caller says 'tomorrow', 'today', 'tonight', 'Friday', or 'this evening', resolve it naturally using the current date. Do not treat these as guesses. Only ask again when the date, time, party size, name, or phone number is genuinely missing or ambiguous. Never invent a value the caller did not say.\n" +
	"  3. If the caller has already provided a date in natural language, do not ask for the date again. Move to the next missing detail.\n" +
	"  4. If the caller says 'tomorrow at seven for four people', treat that as date=tomorrow, time=19:00, party_size=4.\n" +
	"  5. If enough details are present for availability, call availability.check. Do not keep asking the same question.\n" +
	"  6. NEVER invent restaurant facts (menu prices, opening hours, parking, dietary details). If unsure, offer to check with the manager via a callback.\n" +
	"  7. Use British English spelling and phrasing (e.g. \"table for 4\", \"half past seven\", \"Brilliant, thanks\").\n" +
	"  8. For booking requests, gather: date, time, party size, name, phone. Use availability.check first, then booking.create.\n" +
	"  9. Repeat back phone numbers digit by digit to confirm (e.g. \"zero seven nine one seven, seven one five seven three four\").\n" +
	"  10. If the caller changes their mind mid-booking, acknowledge warmly and start over — do not get flustered.\n" +
	"  11. If you cannot hear something clearly, ask the caller to repeat, never guess.\n" +
	"  12. After a successful booking, summarise and offer a friendly closing.\n" +
	"  13. If the caller is rude or upset, stay calm and offer the manager's callback via manager.escalate.\n" +
	"  14. Replies should be conversational and warm, not robotic. Brief, but human."

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
