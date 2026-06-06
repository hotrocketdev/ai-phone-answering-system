// xai_livekit.go — LiveKit <-> xAI Voice Agent audio bridge.
//
// Audio format chain (Plan D, post bridge-fix):
//
//   browser -> LiveKit inbound RTP Opus 48kHz stereo
//     -> samplebuilder (Opus depacketizer) -> raw Opus frames
//     -> oggMuxer (one Opus frame per OGG page) -> ffmpeg decode
//     -> PCM16 24kHz mono -> xai.AppendAudio (100ms chunks)
//     -> xAI server VAD / Grok / Eve
//     -> xai audio delta (PCM16 24kHz mono)
//     -> ffmpeg encode -> OGG Opus 48kHz mono (small pages)
//     -> oggOpusReader demux -> raw Opus frames
//     -> outboundProvider channel -> LocalSampleTrack -> LiveKit RTP
//     -> browser
//
// We use ffmpeg + OGG mux/demux (the same proven pattern as the main
// conversation-worker) instead of a CGo Opus library, because gcc is
// not installed on the build host and we want zero new dependencies
// in the spike harness.
package main

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/joho/godotenv"
	"github.com/livekit/protocol/auth"
	lksdk "github.com/livekit/server-sdk-go"
	"github.com/livekit/server-sdk-go/pkg/samplebuilder"
	"github.com/pion/rtp/codecs"
	"github.com/pion/webrtc/v3"
	"github.com/pion/webrtc/v3/pkg/media"
)

const (
	// ffmpeg: inbound decode, OGG Opus on stdin -> PCM16 24kHz mono
	inboundFFmpegArgs = "-hide_banner -loglevel error -f ogg -i pipe:0 -af aresample=24000 -ac 1 -ar 24000 -f s16le pipe:1"
	// ffmpeg: outbound encode, PCM16 24kHz mono -> OGG Opus 48kHz mono (small pages)
	outboundFFmpegArgs = "-hide_banner -loglevel error -f s16le -ar 24000 -ac 1 -i pipe:0 -c:a libopus -b:a 96k -flush_packets 1 -oggpagesize 256 -f ogg pipe:1"
	// Outbound (LiveKit) is Opus 48kHz mono at 96 kbps; a 20ms frame
	// at 48 kHz has 960 samples
	outboundFrameSamples = 960
)

type outboundProvider struct {
	ch chan media.Sample
}

func newOutboundProvider() *outboundProvider {
	return &outboundProvider{ch: make(chan media.Sample, 256)}
}

func (p *outboundProvider) push(s media.Sample) {
	select {
	case p.ch <- s:
	default:
	}
}

func (p *outboundProvider) pushSilence(d time.Duration) {
	frame := make([]byte, 6)
	n := int((d + 19*time.Millisecond) / (20 * time.Millisecond))
	if n < 1 {
		n = 1
	}
	for i := 0; i < n; i++ {
		p.push(media.Sample{Data: frame, Duration: 20 * time.Millisecond})
	}
}

func (p *outboundProvider) NextSample() (media.Sample, error) {
	s, ok := <-p.ch
	if !ok {
		return media.Sample{}, io.EOF
	}
	return s, nil
}
func (p *outboundProvider) OnBind() error           { return nil }
func (p *outboundProvider) OnUnbind() error         { return nil }
func (p *outboundProvider) Close() error            { return nil }
func (p *outboundProvider) CurrentAudioLevel() uint8 { return 60 }

func runLiveKitBridge(ctx context.Context, cfg *config) error {
	token, err := buildLiveKitToken(cfg)
	if err != nil {
		return fmt.Errorf("build token: %w", err)
	}
	log.Printf("joining LiveKit room %q as %q", cfg.roomName, cfg.identity)
	room, err := lksdk.ConnectToRoomWithToken(cfg.livekitURL, token, &lksdk.RoomCallback{
		ParticipantCallback: lksdk.ParticipantCallback{
			OnTrackSubscribed: func(track *webrtc.TrackRemote, pub *lksdk.RemoteTrackPublication, rp *lksdk.RemoteParticipant) {
				if track.Kind() == webrtc.RTPCodecTypeAudio {
					go handleInboundTrack(ctx, cfg, track, rp)
				}
			},
		},
	}, lksdk.WithAutoSubscribe(true))
	if err != nil {
		return fmt.Errorf("connect to room: %w", err)
	}
	defer room.Disconnect()

	outbound, err := lksdk.NewLocalSampleTrack(webrtc.RTPCodecCapability{
		MimeType:  webrtc.MimeTypeOpus,
		ClockRate: 48000,
		Channels:  1,
	})
	if err != nil {
		return fmt.Errorf("create outbound track: %w", err)
	}
	provider := newOutboundProvider()
	pub, err := room.LocalParticipant.PublishTrack(outbound, &lksdk.TrackPublicationOptions{Name: "xai-voice"})
	if err != nil {
		return fmt.Errorf("publish track: %w", err)
	}
	log.Printf("published outbound Opus track 'xai-voice' (id=%s mime=%s)", pub.SID(), pub.MimeType())
	provider.pushSilence(500 * time.Millisecond)
	if err := outbound.StartWrite(provider, func() {
		log.Println("outbound playback complete (unexpected; channel is never closed)")
	}); err != nil {
		return fmt.Errorf("start write: %w", err)
	}

	xai, err := newXaiClient(cfg)
	if err != nil {
		return fmt.Errorf("xai connect: %w", err)
	}
	defer xai.Close()

	inboundFFmpeg, err := startInboundFFmpeg(ctx)
	if err != nil {
		return fmt.Errorf("start inbound ffmpeg: %w", err)
	}
	defer inboundFFmpeg.Close()

	outboundFFmpeg, err := startOutboundFFmpeg(ctx)
	if err != nil {
		return fmt.Errorf("start outbound ffmpeg: %w", err)
	}
	defer outboundFFmpeg.Close()

	var bridgeWg sync.WaitGroup

	// Pipe xAI audio deltas into outbound ffmpeg as PCM16 24kHz mono
	xai.OnAudioDelta = func(pcm []int16) {
		b := make([]byte, len(pcm)*2)
		for i, s := range pcm {
			binary.LittleEndian.PutUint16(b[i*2:], uint16(s))
		}
		if _, err := outboundFFmpeg.stdin.Write(b); err != nil {
			log.Printf("outbound ffmpeg write error: %v", err)
		}
	}
	xai.OnTranscript = func(role, text string) {
		log.Printf("xai transcript [%s]: %s", role, text)
	}
	xai.OnError = func(err error) {
		log.Printf("xai error: %v", err)
	}

	// Pipe inbound ffmpeg PCM output into xAI as 100ms chunks
	bridgeWg.Add(1)
	go func() {
		defer bridgeWg.Done()
		buf := make([]byte, xaiChunkBytes)
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}
			n, err := io.ReadFull(inboundFFmpeg.stdout, buf)
			if err != nil {
				if err == io.EOF || err == io.ErrUnexpectedEOF {
					return
				}
				log.Printf("inbound ffmpeg read error: %v", err)
				return
			}
			if err := xai.AppendAudio(buf[:n]); err != nil {
				log.Printf("xai AppendAudio error: %v", err)
				return
			}
		}
	}()

	// Pipe outbound ffmpeg OGG Opus output through the demuxer, then
	// push raw Opus frames to the outboundProvider.
	bridgeWg.Add(1)
	go func() {
		defer bridgeWg.Done()
		oggReader := newOggOpusReader(outboundFFmpeg.stdout)
		// Discard OpusHead and OpusTags (first two packets)
		for i := 0; i < 2; i++ {
			if _, err := oggReader.NextOpusPacket(); err != nil {
				if err != io.EOF {
					log.Printf("ogg demux: skip header packet %d: %v", i, err)
				}
				return
			}
		}
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}
			pkt, err := oggReader.NextOpusPacket()
			if err != nil {
				if err == io.EOF {
					return
				}
				log.Printf("ogg demux error: %v", err)
				return
			}
			provider.push(media.Sample{Data: pkt, Duration: 20 * time.Millisecond})
		}
	}()

	bridgeWg.Add(1)
	go func() {
		defer bridgeWg.Done()
		if err := xai.ReadEvents(ctx); err != nil {
			log.Printf("xai read loop ended: %v", err)
		}
	}()

	log.Printf("xai-voice-agent LIVE. Press Ctrl+C to stop.")
	<-ctx.Done()
	log.Printf("xai-voice-agent shutting down")
	bridgeWg.Wait()
	return nil
}

// handleInboundTrack is called for each subscribed audio track from a
// remote participant. We assemble Opus frames via samplebuilder, wrap
// each in an OGG page, and forward to the inbound ffmpeg decoder.
func handleInboundTrack(ctx context.Context, cfg *config, track *webrtc.TrackRemote, rp *lksdk.RemoteParticipant) {
	log.Printf("subscribed to track %s from %s (codec=%s)", track.ID(), rp.Identity(), track.Codec().MimeType)
	// Wait for the inbound ffmpeg to be initialized; the OnTrackSubscribed
	// callback can fire before startInboundFFmpeg completes.
	select {
	case <-inboundFFmpegReady:
	case <-ctx.Done():
		return
	case <-time.After(5 * time.Second):
		log.Printf("inbound ffmpeg not ready after 5s; dropping track %s", track.ID())
		return
	}
	inbound := getInboundFFmpegStdin()
	if inbound == nil {
		log.Printf("no inbound ffmpeg available; dropping track")
		return
	}
	ogg := newOggMuxer(inbound, 0xDEADBEEF, 2 /* LiveKit emits stereo Opus */, 48000)
	if err := ogg.writeOpusHead(); err != nil {
		log.Printf("ogg writeOpusHead: %v", err)
		return
	}
	if err := ogg.writeOpusTags(); err != nil {
		log.Printf("ogg writeOpusTags: %v", err)
		return
	}
	sb := samplebuilder.New(20, &codecs.OpusPacket{}, 48000)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		rtpPacket, _, err := track.ReadRTP()
		if err != nil {
			log.Printf("ReadRTP error on track %s: %v", track.ID(), err)
			return
		}
		sb.Push(rtpPacket)
		for {
			sample := sb.Pop()
			if sample == nil {
				break
			}
			// A 20ms Opus frame at 48kHz has 960 samples (mono);
			// for stereo it's still 960 samples per channel
			// (20ms of 48kHz audio). The granule is per-channel.
			if err := ogg.writeOpusFrame(sample.Data, uint64(outboundFrameSamples)); err != nil {
				log.Printf("ogg writeOpusFrame: %v", err)
				return
			}
		}
	}
}

// --- ffmpeg plumbing ---

type inboundFFmpegHandle struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
}
type outboundFFmpegHandle struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
}

var (
	inboundFFmpegOnce   sync.Once
	inboundFFmpegInst   *inboundFFmpegHandle
	inboundFFmpegReady  = make(chan struct{})
	outboundFFmpegOnce  sync.Once
	outboundFFmpegInst  *outboundFFmpegHandle
)

func startInboundFFmpeg(ctx context.Context) (*inboundFFmpegHandle, error) {
	var err error
	inboundFFmpegOnce.Do(func() {
		cmd := exec.CommandContext(ctx, "ffmpeg", splitArgs(inboundFFmpegArgs)...)
		stdin, e1 := cmd.StdinPipe()
		stdout, e2 := cmd.StdoutPipe()
		if e1 != nil || e2 != nil {
			err = fmt.Errorf("stdin/stdout pipes: %v %v", e1, e2)
			return
		}
		cmd.Stderr = os.Stderr
		if e := cmd.Start(); e != nil {
			err = fmt.Errorf("start ffmpeg: %w", e)
			return
		}
		inboundFFmpegInst = &inboundFFmpegHandle{cmd: cmd, stdin: stdin, stdout: stdout}
		log.Printf("inbound ffmpeg started (pid=%d)", cmd.Process.Pid)
		close(inboundFFmpegReady)
	})
	return inboundFFmpegInst, err
}

func startOutboundFFmpeg(ctx context.Context) (*outboundFFmpegHandle, error) {
	var err error
	outboundFFmpegOnce.Do(func() {
		cmd := exec.CommandContext(ctx, "ffmpeg", splitArgs(outboundFFmpegArgs)...)
		stdin, e1 := cmd.StdinPipe()
		stdout, e2 := cmd.StdoutPipe()
		if e1 != nil || e2 != nil {
			err = fmt.Errorf("stdin/stdout pipes: %v %v", e1, e2)
			return
		}
		cmd.Stderr = os.Stderr
		if e := cmd.Start(); e != nil {
			err = fmt.Errorf("start ffmpeg: %w", e)
			return
		}
		outboundFFmpegInst = &outboundFFmpegHandle{cmd: cmd, stdin: stdin, stdout: stdout}
		log.Printf("outbound ffmpeg started (pid=%d)", cmd.Process.Pid)
	})
	return outboundFFmpegInst, err
}

func (h *inboundFFmpegHandle) Close() error {
	if h == nil {
		return nil
	}
	_ = h.stdin.Close()
	_ = h.stdout.Close()
	if h.cmd.Process != nil {
		return h.cmd.Process.Kill()
	}
	return nil
}
func (h *outboundFFmpegHandle) Close() error {
	if h == nil {
		return nil
	}
	_ = h.stdin.Close()
	_ = h.stdout.Close()
	if h.cmd.Process != nil {
		return h.cmd.Process.Kill()
	}
	return nil
}

func getInboundFFmpegStdin() io.WriteCloser {
	if inboundFFmpegInst == nil {
		return nil
	}
	return inboundFFmpegInst.stdin
}

func splitArgs(s string) []string {
	var args []string
	cur := ""
	for _, r := range s {
		if r == ' ' {
			if cur != "" {
				args = append(args, cur)
				cur = ""
			}
		} else {
			cur += string(r)
		}
	}
	if cur != "" {
		args = append(args, cur)
	}
	return args
}

func buildLiveKitToken(cfg *config) (string, error) {
	_ = godotenv.Load()
	at := auth.NewAccessToken(cfg.apiKey, cfg.apiSecret)
	yes := true
	at.AddGrant(&auth.VideoGrant{
		RoomJoin:       true,
		Room:           cfg.roomName,
		CanPublish:     &yes,
		CanSubscribe:   &yes,
		CanPublishData: &yes,
	})
	at.SetIdentity(cfg.identity)
	at.SetValidFor(2 * time.Hour)
	return at.ToJWT()
}
