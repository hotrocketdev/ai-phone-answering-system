// Two-way LiveKit conversation spike (worker side).
//
// Stage 1 (current): per-utterance VAD + STT (OpenAI Whisper) +
// LLM (gpt-4o-mini) + TTS (Cartesia Sonic 3.5 + Julia) +
// ffmpeg Opus -> LiveKit. Multi-turn.
//
// Production runtime on VPS is untouched. This is a spike on
// feat/livekit-hd-spike, not production integration.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/joho/godotenv"
)

const (
	defaultRoom          = "voxlane-conv-spike"
	defaultIdentity      = "voxlane-conv-worker"
	cartesiaVersion      = "2024-06-01"
	defaultSilenceMs     = 500
	openAIVerifyTimeout  = 10 * time.Second
)

// spikeStartTime is the wall-clock reference for latencyLog and the
// first-inbound-frame timestamp reported by the worker.
var spikeStartTime = time.Now()

// latencyLog logs a milestone with elapsed time since spikeStartTime.
func latencyLog(label string) {
	elapsed := time.Since(spikeStartTime)
	log.Printf("[+%5dms] %s", elapsed.Milliseconds(), label)
}

func main() {
	spikeStartTime = time.Now()
	latencyLog("worker_start")
	if err := loadEnv(); err != nil {
		log.Fatalf("env: %v", err)
	}

	livekitURL := mustEnv("LIVEKIT_URL")
	apiKey := mustEnv("LIVEKIT_API_KEY")
	apiSecret := mustEnv("LIVEKIT_API_SECRET")

	roomName := getenv("WORKER_ROOM", defaultRoom)
	identity := getenv("WORKER_IDENTITY", defaultIdentity)
	mode := getenv("LIVEKIT_WORKER_MODE", "stitched")
	if mode != "stitched" && mode != "realtime" && mode != "realtime-cartesia" {
		log.Fatalf("invalid LIVEKIT_WORKER_MODE=%q (must be 'stitched', 'realtime', or 'realtime-cartesia')", mode)
	}
	log.Printf("worker_mode: %s", mode)

	cartesiaKey := os.Getenv("CARTESIA_API_KEY")
	voiceID := os.Getenv("CARTESIA_VOICE_ID")
	modelID := os.Getenv("CARTESIA_MODEL")
	cartesiaRate := getenvInt("CARTESIA_RATE", 48000)
	cartesiaEncoding := getenv("CARTESIA_ENCODING", "pcm_f32le")
	// Outbound filter chain (TTS PCM -> Opus). TTS audio is already
	// clean — no denoiser, no compressor (the previous
	// acompressor was found to make the reply harder to
	// understand). Just a gentle DC-removal highpass in case the
	// source has any sub-audible bias.
	filterChain := getenv("FILTER_CHAIN", "highpass=f=60")
	bitrate := getenvInt("OPUS_BITRATE", 96000)
	opusApplication := getenv("OPUS_APPLICATION", "audio")
	openaiKey := os.Getenv("OPENAI_API_KEY")
	silenceMs := getenvInt("VAD_SILENCE_TIMEOUT_MS", defaultSilenceMs)

	// Stage 1.5: Realtime mode config. Read here so missing
	// REALTIME_MODEL surfaces as a clear error before we connect.
	rtModel := getenv("REALTIME_MODEL", "gpt-realtime-mini")
	rtVoice := getenv("REALTIME_VOICE", "marin")
	rtInstructions := getenv("REALTIME_INSTRUCTIONS",
		"You are Alex, a calm restaurant receptionist. Reply briefly and naturally. Do not over-explain. Ask one question at a time.")
	rtVadThreshold := getenvFloat("REALTIME_VAD_THRESHOLD", 0.75)
	rtVadSilenceMs := getenvInt("REALTIME_VAD_SILENCE_MS", 600)
	rtVadPrefixMs := getenvInt("REALTIME_VAD_PREFIX_MS", 200)
	rtOpusBitrate := getenvInt("REALTIME_OPUS_BITRATE", 96000)
	rtInboundFilter := getenv("REALTIME_INBOUND_FILTER", "highpass=f=80")

	log.Printf("worker_config: room=%s identity=%s mode=%s encoding=%s rate=%d voice=%s silence_ms=%d openai_key_set=%t",
		roomName, identity, mode, cartesiaEncoding, cartesiaRate, voiceID, silenceMs, openaiKey != "")
	if mode == "realtime" || mode == "realtime-cartesia" {
		log.Printf("realtime_config: model=%s voice=%s vad_threshold=%.2f vad_silence_ms=%d vad_prefix_ms=%d opus_bitrate=%d",
			rtModel, rtVoice, rtVadThreshold, rtVadSilenceMs, rtVadPrefixMs, rtOpusBitrate)
		if mode == "realtime-cartesia" {
			log.Printf("realtime_cartesia_config: voice_id=%s model=%s rate=%d encoding=%s",
				voiceID, modelID, cartesiaRate, cartesiaEncoding)
		}
	}

	if openaiKey == "" {
		log.Printf("WARN: OPENAI_API_KEY not set — STT/LLM/Realtime will fail")
	} else {
		if err := verifyOpenAIKey(openaiKey); err != nil {
			log.Printf("WARN: OPENAI_API_KEY verify failed: %v", err)
		}
	}
	latencyLog("config_loaded")

	w := &worker{
		livekitURL:     livekitURL,
		apiKey:         apiKey,
		apiSecret:      apiSecret,
		roomName:       roomName,
		identity:       identity,
		mode:           mode,

		// Stage 1 (stitched mode) config
		cartesiaKey:    cartesiaKey,
		voiceID:        voiceID,
		modelID:        modelID,
		cartesiaRate:   cartesiaRate,
		cartesiaEnc:    cartesiaEncoding,
		filterChain:    filterChain,
		bitrate:        bitrate,
		opusApp:        opusApplication,
		openaiKey:      openaiKey,
		silenceTimeout: time.Duration(silenceMs) * time.Millisecond,

		// Stage 1.5 (realtime mode) config
		rtModel:         rtModel,
		rtVoice:         rtVoice,
		rtInstructions:  rtInstructions,
		rtVadThreshold:  rtVadThreshold,
		rtVadSilenceMs:  rtVadSilenceMs,
		rtVadPrefixMs:   rtVadPrefixMs,
		rtOpusBitrate:   rtOpusBitrate,
		rtInboundFilter: rtInboundFilter,
	}
	if err := w.run(); err != nil {
		log.Fatalf("worker: %v", err)
	}
}

// verifyOpenAIKey hits /v1/models and reports a one-line status.
// A 200 means the key is valid; a 401 means the key is bad or
// revoked; a 429 means the org is out of quota.
func verifyOpenAIKey(apiKey string) error {
	ctx, cancel := context.WithTimeout(context.Background(), openAIVerifyTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", "https://api.openai.com/v1/models", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == 200 {
		// Find the gpt-4o-mini model id from the list so the log
		// confirms our model choice.
		var out struct {
			Data []struct {
				ID string `json:"id"`
			} `json:"data"`
		}
		_ = json.Unmarshal(body, &out)
		var gpt4mini string
		for _, m := range out.Data {
			if m.ID == "gpt-4o-mini" {
				gpt4mini = m.ID
				break
			}
		}
		log.Printf("openai_verify: OK (status=200, gpt-4o-mini_available=%t)", gpt4mini != "")
		return nil
	}
	return fmt.Errorf("status=%d body=%s", resp.StatusCode, string(body))
}

// loadEnv loads LIVEKIT_*, CARTESIA_*, OPENAI_* from the spike's
// .env file. The .env file is gitignored (0600 perms on the VPS).
func loadEnv() error {
	candidates := []string{
		"experimental/livekit/.env",
		"/opt/ai-voice-receptionist/experimental/livekit/.env",
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			log.Printf("loading env from %s", p)
			return godotenv.Load(p)
		}
	}
	log.Printf("no .env file found in candidates — relying on process env")
	return nil
}

func mustEnv(name string) string {
	v := os.Getenv(name)
	if v == "" {
		log.Fatalf("missing required env var: %s", name)
	}
	return v
}

func getenv(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}

func getenvInt(name string, fallback int) int {
	if v := os.Getenv(name); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

func getenvFloat(name string, fallback float64) float64 {
	if v := os.Getenv(name); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return fallback
}
