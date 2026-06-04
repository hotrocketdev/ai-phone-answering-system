// Two-way LiveKit conversation spike (worker side).
//
// Flow:
//   1. Read LIVEKIT_* and CARTESIA_* from experimental/livekit/.env
//   2. Generate a LiveKit access token (room: voxlane-conv-spike)
//   3. Connect to the LiveKit room
//   4. Subscribe to all remote audio tracks (browser mic)
//   5. On first inbound Opus frame, trigger an outbound reply
//      (Cartesia HD → ffmpeg Opus → publish to room)
//   6. Browser hears Alex
//
// Stages (gated by REPLY_MODE env var):
//   "none"                   - subscribe + log only
//   "tone_on_first_frame"    - play 440 Hz test tone on first frame
//   "fixed_on_first_frame"   - Cartesia TTS reply on first frame
//
// Production runtime on VPS is untouched. This is a spike on
// feat/livekit-hd-spike, not production integration.
package main

import (
	"log"
	"os"
	"strconv"
	"time"

	"github.com/joho/godotenv"
)

const (
	defaultRoom     = "voxlane-conv-spike"
	defaultIdentity = "voxlane-conv-worker"
	cartesiaVersion = "2024-06-01"
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
	cartesiaKey := os.Getenv("CARTESIA_API_KEY")
	voiceID := os.Getenv("CARTESIA_VOICE_ID")
	modelID := os.Getenv("CARTESIA_MODEL")
	cartesiaRate := getenvInt("CARTESIA_RATE", 48000)
	cartesiaEncoding := getenv("CARTESIA_ENCODING", "pcm_f32le")
	filterChain := getenv("FILTER_CHAIN", "highpass=f=80,lowpass=f=12000,anlmdn=s=0.0001:p=0.004:r=0.012")
	bitrate := getenvInt("OPUS_BITRATE", 64000)
	opusApplication := getenv("OPUS_APPLICATION", "audio")
	replyMode := getenv("REPLY_MODE", "fixed_on_first_frame")
	replyText := getenv("REPLY_TEXT", "Hi there, this is Alex. Yes, I can hear you. How can I help today?")

	log.Printf("worker_config: room=%s identity=%s reply_mode=%s encoding=%s rate=%d voice=%s",
		roomName, identity, replyMode, cartesiaEncoding, cartesiaRate, voiceID)

	w := &worker{
		livekitURL:    livekitURL,
		apiKey:        apiKey,
		apiSecret:     apiSecret,
		roomName:      roomName,
		identity:      identity,
		cartesiaKey:   cartesiaKey,
		voiceID:       voiceID,
		modelID:       modelID,
		cartesiaRate:  cartesiaRate,
		cartesiaEnc:   cartesiaEncoding,
		filterChain:   filterChain,
		bitrate:       bitrate,
		opusApp:       opusApplication,
		replyMode:     replyMode,
		replyText:     replyText,
	}
	if err := w.run(); err != nil {
		log.Fatalf("worker: %v", err)
	}
}

// loadEnv loads LIVEKIT_* and CARTESIA_* from the spike's .env file.
// The .env file is gitignored (0600 perms on the VPS).
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
