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
	"context"
	"fmt"
	"log"
	"math"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
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

	// 3. Audio source — codec selection
	// SPIKE_AUDIO_CODEC selects between PCMU (default, original spike)
	// and Opus (HD follow-up, ffmpeg-backed). The two paths share
	// nothing at the SampleProvider level: PCMU is encoded in pure Go,
	// Opus is encoded by an ffmpeg child process.
	spikeCodec := strings.ToLower(getenv("SPIKE_AUDIO_CODEC", "pcmu"))
	switch spikeCodec {
	case "pcmu", "opus":
		// ok
	default:
		log.Fatalf("invalid SPIKE_AUDIO_CODEC=%q (want pcmu or opus)", spikeCodec)
	}
	log.Printf("spike_audio_codec=%s", spikeCodec)

	// 4. Build the SampleProvider + track for the chosen codec.
	var provider lksdk.SampleProvider
	switch spikeCodec {
	case "pcmu":
		// PCMU path (original spike): 8 kHz mono PCM → G.711 µ-law.
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
		pcmuProv, err := NewPCMSampleProvider(pcm, sampleRate)
		if err != nil {
			log.Fatalf("encoder: %v", err)
		}
		provider = pcmuProv
	case "opus":
		// Opus path (HD follow-up): PCM (s16le, mono) → ffmpeg → Ogg Opus
		// → demux → raw Opus packets → LiveKit.
		//
		// PCM source:
		//   - If CARTESIA_API_KEY and SPIKE_GREETING_TEXT are set, use
		//     Cartesia HTTP TTS to synthesize HD PCM (24 kHz s16le mono
		//     for natural voice), pipe into ffmpeg stdin. ffmpeg
		//     resamples to 48 kHz and encodes to Opus.
		//   - Otherwise fall back to a synthetic 440 Hz tone at 48 kHz
		//     for codec-only verification.
		if cartesiaKey != "" && greeting != "" {
			// Cartesia HD path — Step 5 of the spike plan.
			// 24 kHz mono s16le is Cartesia's natural voice rate and
			// well-suited to Opus (ffmpeg resamples 24k -> 48k).
			const cartesiaRate = 24000
			log.Printf("synthesizing via Cartesia HD: voice=%s model=%s rate=%d greeting=%q",
				voiceID, modelID, cartesiaRate, greeting)
			cartesiaPCM, cerr := Synthesize(cartesiaKey, greeting, voiceID, modelID, cartesiaRate)
			if cerr != nil {
				log.Fatalf("cartesia: %v", cerr)
			}
			log.Printf("cartesia_pcm samples=%d duration=%.2fs rate=%d channels=1",
				len(cartesiaPCM), float64(len(cartesiaPCM))/float64(cartesiaRate), cartesiaRate)
			ff, err := startFfmpegOpus(context.Background(), cartesiaRate)
			if err != nil {
				log.Fatalf("ffmpeg start: %v", err)
			}
			log.Printf("ffmpeg_started=true pid=%d input_rate=%d", ff.cmd.Process.Pid, cartesiaRate)
			if err := streamCartesiaPCM(ff, cartesiaPCM); err != nil {
				ff.kill()
				log.Fatalf("stream cartesia PCM: %v", err)
			}
			// The opus provider + ffmpeg process are wired up below.
			demuxer := NewOggOpusReader(ff.stdout)
			opusProv := NewOpusSampleProvider(demuxer)
			provider = opusProv
			// Parse OpusHead + OpusTags inline so we fail fast on
			// a bad ffmpeg output.
			headPkt, err := demuxer.NextOpusPacket()
			if err != nil {
				ff.kill()
				log.Fatalf("ogg: read OpusHead: %v", err)
			}
			head, err := ParseOpusHead(headPkt)
			if err != nil {
				ff.kill()
				log.Fatalf("ogg: parse OpusHead: %v", err)
			}
			log.Printf("opus_header version=%d channels=%d input_rate=%d pre_skip=%d gain=%d mapping_family=%d",
				head.Version, head.ChannelCount, head.InputSampleRate, head.PreSkip, head.OutputGain, head.MappingFamily)
			if _, err := demuxer.NextOpusPacket(); err != nil {
				ff.kill()
				log.Fatalf("ogg: read OpusTags: %v", err)
			}
			log.Printf("ogg_demuxer_ready (OpusHead + OpusTags consumed, cartesia_path=true)")
			// Provider is set; skip the synthetic-tone branch below.
			goto opusProviderReady
		}
		// Synthetic-tone fallback (no Cartesia key).
		ff, err := startFfmpegOpus(context.Background(), OpusSampleRate)
		if err != nil {
			log.Fatalf("ffmpeg start: %v", err)
		}
		log.Printf("ffmpeg_started=true pid=%d input_rate=%d (synthetic tone)", ff.cmd.Process.Pid, OpusSampleRate)
		if err := streamSyntheticTone(ff, 440.0, 5.0, 0.3); err != nil {
			ff.kill()
			log.Fatalf("stream synthetic tone: %v", err)
		}
		demuxer := NewOggOpusReader(ff.stdout)
		// First packet is OpusHead — consume and validate.
		headPkt, err := demuxer.NextOpusPacket()
		if err != nil {
			ff.kill()
			log.Fatalf("ogg: read OpusHead: %v", err)
		}
		head, err := ParseOpusHead(headPkt)
		if err != nil {
			ff.kill()
			log.Fatalf("ogg: parse OpusHead: %v", err)
		}
		log.Printf("opus_header version=%d channels=%d input_rate=%d pre_skip=%d gain=%d mapping_family=%d",
			head.Version, head.ChannelCount, head.InputSampleRate, head.PreSkip, head.OutputGain, head.MappingFamily)
		// Second packet is OpusTags — consume and skip.
		if _, err := demuxer.NextOpusPacket(); err != nil {
			ff.kill()
			log.Fatalf("ogg: read OpusTags: %v", err)
		}
		log.Printf("ogg_demuxer_ready (OpusHead + OpusTags consumed)")
		opusProv := NewOpusSampleProvider(demuxer)
		provider = opusProv
		// Note: we keep `ff` alive for the lifetime of the provider.
		// When the demuxer hits io.EOF, the ffmpeg stream is done.
		// If the publisher is interrupted, ff is leaked but Go's
		// process group cleanup will reap it.
	}
opusProviderReady:

	// 5. Track
	var trackMimeType string
	var trackClockRate uint32
	switch spikeCodec {
	case "pcmu":
		trackMimeType = webrtc.MimeTypePCMU
		trackClockRate = 8000
	case "opus":
		trackMimeType = webrtc.MimeTypeOpus
		trackClockRate = OpusSampleRate
	}
	track, err := lksdk.NewLocalSampleTrack(webrtc.RTPCodecCapability{
		MimeType:  trackMimeType,
		ClockRate: trackClockRate,
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
		log.Printf("spike complete in %s", elapsed)
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
