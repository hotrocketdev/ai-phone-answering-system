// One-way Cartesia → LiveKit HD audio spike (PCMU intermediate).
//
// Flow:
//   1. Read LIVEKIT_* and CARTESIA_* from experimental/livekit/.env
//   2. Generate a LiveKit access token (room: voxlane-hd-spike)
//   3. Connect to the LiveKit room
//   4. Synthesize greeting audio via Cartesia HTTP TTS (8 kHz mono PCM)
//      — falls back to a 5s 440 Hz test tone if CARTESIA_API_KEY is empty
//   5. Encode PCM into 20 ms G.711 µ-law (PCMU) frames
//   6. Publish a PCMU audio track
//   7. Stream audio until the buffer is exhausted, then exit
//
// Spike format: PCMU (G.711 µ-law, 8 kHz). This is the spike's
// intermediate format because it has no external dependencies. The
// HD (Opus, 48 kHz) follow-up requires libopus CGO bindings.
//
// Production PCMU runtime on VPS is untouched. This is a spike.
package main

import (
	"fmt"
	"log"
	"math"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	"github.com/livekit/protocol/auth"
	lksdk "github.com/livekit/server-sdk-go"
	"github.com/pion/webrtc/v3"
)

const (
	defaultRoom     = "voxlane-hd-spike"
	defaultIdentity = "voxlane-publisher"
	cartesiaVersion = "2024-06-01"
)

func main() {
	if err := loadEnv(); err != nil {
		log.Fatalf("env: %v", err)
	}

	livekitURL := mustEnv("LIVEKIT_URL")
	apiKey := mustEnv("LIVEKIT_API_KEY")
	apiSecret := mustEnv("LIVEKIT_API_SECRET")

	roomName := getenv("SPIKE_ROOM", defaultRoom)
	identity := getenv("SPIKE_IDENTITY", defaultIdentity)
	cartesiaKey := os.Getenv("CARTESIA_API_KEY")
	voiceID := os.Getenv("CARTESIA_VOICE_ID")
	modelID := os.Getenv("CARTESIA_MODEL")
	greeting := os.Getenv("SPIKE_GREETING_TEXT")

	// 1. Token
	token, err := mintToken(apiKey, apiSecret, roomName, identity)
	if err != nil {
		log.Fatalf("token: %v", err)
	}
	log.Printf("token generated (room=%s identity=%s ttl=1h)", roomName, identity)

	// 2. Room
	waitForSubscriber := os.Getenv("SPIKE_WAIT_FOR_SUBSCRIBER") == "true"
	subscribed := make(chan struct{}, 1)
	cb := lksdk.NewRoomCallback()
	cb.OnDisconnected = func() { log.Println("disconnected") }
	cb.OnParticipantConnected = func(p *lksdk.RemoteParticipant) {
		log.Printf("participant connected: %s (sid=%s)", p.Identity(), p.SID())
		// Signal the first non-self participant so wait-for-subscriber
		// mode can start publishing.
		if p.Identity() != identity {
			select {
			case subscribed <- struct{}{}:
			default:
			}
		}
	}
	cb.OnParticipantDisconnected = func(p *lksdk.RemoteParticipant) {
		log.Printf("participant disconnected: %s (sid=%s)", p.Identity(), p.SID())
	}

	room, err := lksdk.ConnectToRoomWithToken(livekitURL, token, cb, lksdk.WithAutoSubscribe(false))
	if err != nil {
		log.Fatalf("connect: %v", err)
	}
	defer room.Disconnect()
	log.Printf("connected to room=%s as %s", room.SID(), identity)

	if waitForSubscriber {
		// If a subscriber is already in the room (e.g. browser connected
		// first), don't wait. Otherwise, block until the first remote
		// participant joins.
		if len(room.GetParticipants()) == 0 {
			log.Println("SPIKE_WAIT_FOR_SUBSCRIBER=true — waiting for a listener to join…")
			select {
			case <-subscribed:
				log.Println("listener detected — proceeding with publish")
			case <-time.After(60 * time.Second):
				log.Fatal("no listener joined within 60s — aborting")
			}
		} else {
			log.Printf("listener already in room (%d remote) — proceeding with publish", len(room.GetParticipants()))
		}
	}

	// 3. Audio source
	// PCMU is the spike's intermediate codec (see pcmsampleprovider.go
	// for the rationale and Opus follow-up plan). Cartesia is asked for
	// 8 kHz mono to avoid a resampling step.
	const sampleRate = 8000
	var pcm []int16
	if cartesiaKey != "" && greeting != "" {
		log.Printf("synthesizing via Cartesia: voice=%s model=%s rate=%d", voiceID, modelID, sampleRate)
		pcm, err = Synthesize(cartesiaKey, greeting, voiceID, modelID, sampleRate)
		if err != nil {
			log.Fatalf("cartesia: %v", err)
		}
		log.Printf("got %d PCM samples (%.2fs @ %d Hz)", len(pcm), float64(len(pcm))/float64(sampleRate), sampleRate)
	} else {
		log.Println("no CARTESIA_API_KEY or SPIKE_GREETING_TEXT — falling back to 5s 440 Hz test tone at 8 kHz")
		pcm = generateTone(sampleRate, 1, 440.0, 5.0)
	}
	if len(pcm) == 0 {
		log.Fatal("no audio to publish")
	}

	// 4. Sample provider (PCMU)
	provider, err := NewPCMSampleProvider(pcm, sampleRate)
	if err != nil {
		log.Fatalf("encoder: %v", err)
	}

	// 5. Track
	track, err := lksdk.NewLocalSampleTrack(webrtc.RTPCodecCapability{
		MimeType:  webrtc.MimeTypePCMU,
		ClockRate: uint32(sampleRate),
		Channels:  1,
	})
	if err != nil {
		log.Fatalf("track: %v", err)
	}

	pub, err := room.LocalParticipant.PublishTrack(track, &lksdk.TrackPublicationOptions{})
	if err != nil {
		log.Fatalf("publish: %v", err)
	}
	log.Printf("track published: id=%s name=%s mime=%s", pub.SID(), pub.Name(), pub.MimeType())

	// 6. Stream
	done := make(chan struct{})
	if err := track.StartWrite(provider, func() {
		log.Println("audio playback complete")
		close(done)
	}); err != nil {
		log.Fatalf("start write: %v", err)
	}

	// 7. Wait for completion or signal
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

	startWait := time.Now()
	select {
	case <-done:
		elapsed := time.Since(startWait)
		log.Printf("spike complete in %s (audio: %.2fs)", elapsed, float64(len(pcm))/float64(sampleRate))
	case s := <-sig:
		log.Printf("signal %v — shutting down", s)
	case <-time.After(60 * time.Second):
		log.Println("timeout (60s) — shutting down")
	}
}

func mintToken(apiKey, apiSecret, room, identity string) (string, error) {
	at := auth.NewAccessToken(apiKey, apiSecret)
	at.AddGrant(&auth.VideoGrant{
		RoomJoin:       true,
		Room:           room,
		CanPublish:     &[]bool{true}[0],
		CanSubscribe:   &[]bool{false}[0],
		CanPublishData: &[]bool{true}[0],
	})
	at.SetIdentity(identity)
	at.SetValidFor(time.Hour)
	return at.ToJWT()
}

func generateTone(sampleRate, channels int, freq, durSec float64) []int16 {
	n := int(float64(sampleRate) * durSec)
	out := make([]int16, n)
	for i := 0; i < n; i++ {
		s := math.Sin(2 * math.Pi * freq * float64(i) / float64(sampleRate))
		out[i] = int16(0.3 * 32767 * s)
	}
	return out
}

func loadEnv() error {
	candidates := []string{
		".env",
		filepath.Join("..", ".env"),
		filepath.Join("..", "..", ".env"),
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return godotenv.Load(p)
		}
	}
	return nil
}

func mustEnv(name string) string {
	v := os.Getenv(name)
	if v == "" {
		log.Fatalf("missing required env: %s", name)
	}
	return v
}

func getenv(name, def string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return def
}

// ensure fmt import is used
var _ = fmt.Sprintf
