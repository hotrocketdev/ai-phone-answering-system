// ffmpegopus.go — ffmpeg-backed Opus encoder for the LiveKit HD spike.
//
// Spawns `ffmpeg` as a child process. ffmpeg reads raw PCM (s16le, 48 kHz,
// mono) from stdin and writes Ogg Opus pages to stdout. We demux the
// Ogg stream with OggOpusReader and yield raw Opus packets to the
// OpusSampleProvider.
//
// PCM input can come from two sources:
//   1. The publisher generates a 440 Hz sine at 48 kHz and pipes it to
//      ffmpeg's stdin. This is the synthetic-tone path.
//   2. Cartesia HTTP TTS returns PCM. The publisher pipes it to ffmpeg's
//      stdin. This is the Cartesia-HD path.
//
// IMPORTANT: This spike path is NOT used in production. The production
// VoxLane runtime continues to use PCMU via Telnyx. Do NOT wire this
// file into production code.
package main

import (
	"bufio"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"math"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/pion/webrtc/v3/pkg/media"
)

// OpusFrameDuration is the cadence LiveKit expects for Opus audio
// (one frame per 20 ms). ffmpeg with `-application audio` produces
// 20 ms frames by default.
const OpusFrameDuration = 20 * time.Millisecond

// OpusSampleRate is the standard WebRTC/Opus clock rate.
const OpusSampleRate = 48000

// OpusChannels — we use mono for the spike (matches the PCMU spike).
const OpusChannels = 1

// OpusBitrate — 64 kbps is a good default for "HD voice". Opus supports
// 6 kbps to 510 kbps, but voice rarely benefits above 64 kbps.
const OpusBitrate = 64000

// OpusSampleProvider implements lksdk.SampleProvider for Opus output.
// It pulls raw Opus packets from an OggOpusReader and yields them as
// media.Sample at 20ms cadence.
type OpusSampleProvider struct {
	demuxer    *OggOpusReader
	audioLevel uint8
	mu         sync.Mutex
	stats      OpusStats
}

// OpusStats tracks the Opus publisher's runtime metrics.
type OpusStats struct {
	FramesSent int
	BytesSent  int
	StartTime  time.Time
	LastFrame  time.Time
}

// NewOpusSampleProvider wraps a demuxer (which reads ffmpeg's stdout).
func NewOpusSampleProvider(demuxer *OggOpusReader) *OpusSampleProvider {
	return &OpusSampleProvider{
		demuxer:    demuxer,
		audioLevel: 60,
		stats:      OpusStats{StartTime: time.Now()},
	}
}

func (p *OpusSampleProvider) NextSample() (media.Sample, error) {
	pkt, err := p.demuxer.NextOpusPacket()
	if err != nil {
		return media.Sample{}, err
	}
	p.mu.Lock()
	p.stats.FramesSent++
	p.stats.BytesSent += len(pkt)
	p.stats.LastFrame = time.Now()
	p.mu.Unlock()
	return media.Sample{Data: pkt, Duration: OpusFrameDuration}, nil
}

func (p *OpusSampleProvider) OnBind() error  { return nil }
func (p *OpusSampleProvider) OnUnbind() error { return nil }
func (p *OpusSampleProvider) Close() error    { return nil }

// CurrentAudioLevel satisfies lksdk.AudioSampleProvider.
func (p *OpusSampleProvider) CurrentAudioLevel() uint8 { return p.audioLevel }

// Stats returns a snapshot of the publisher's Opus statistics.
func (p *OpusSampleProvider) Stats() OpusStats {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.stats
}

// ffmpegProcess is a running ffmpeg child process.
type ffmpegProcess struct {
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	stdout  io.ReadCloser
	stderr  *bufio.Reader
	cancel  context.CancelFunc
}

// startFfmpegOpus launches ffmpeg with PCM-on-stdin, Opus-on-stdout.
// `inputSampleRate` is the rate of the PCM arriving on stdin (e.g. 24000
// for Cartesia HD or 48000 for synthetic tone). ffmpeg resamples to
// the Opus native rate of 48000 internally.
// Reads stderr in a goroutine and logs non-empty lines.
func startFfmpegOpus(ctx context.Context, inputSampleRate int) (*ffmpegProcess, error) {
	args := []string{
		"-hide_banner",
		"-loglevel", "error",
		"-f", "s16le",
		"-ar", fmt.Sprintf("%d", inputSampleRate),
		"-ac", fmt.Sprintf("%d", OpusChannels),
		"-i", "pipe:0", // PCM from stdin
		// Highpass + anlmdn (non-local means). The highpass removes
		// low-frequency rumble; anlmdn is ffmpeg's best no-model
		// denoiser and is often more effective than afftdn for
		// broadband noise from TTS sources. Strength 0.0001 is
		// 10x the default — aggressive but still preserves speech.
		"-af", "highpass=f=80,anlmdn=s=0.0001:p=0.004:r=0.012",
		"-c:a", "libopus",
		"-application", "audio",
		"-b:a", fmt.Sprintf("%d", OpusBitrate),
		"-vbr", "on",
		"-compression_level", "10",
		"-f", "opus", // Ogg Opus on stdout
		"pipe:1",
	}
	cctx, cancel := context.WithCancel(ctx)
	cmd := exec.CommandContext(cctx, "ffmpeg", args...)
	cmd.Env = append(os.Environ(), "AV_LOG_FORCE_NOCOLOR=1")

	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("ffmpeg stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("ffmpeg stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("ffmpeg stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("ffmpeg start: %w", err)
	}

	// drain stderr so ffmpeg doesn't block
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := stderr.Read(buf)
			if n > 0 {
				log.Printf("ffmpeg[stderr]: %s", buf[:n])
			}
			if err != nil {
				return
			}
		}
	}()

	return &ffmpegProcess{
		cmd:    cmd,
		stdin:  stdin,
		stdout: stdout,
		cancel: cancel,
	}, nil
}

// writePCM writes a PCM int16 buffer (48 kHz mono) to ffmpeg's stdin.
func (f *ffmpegProcess) writePCM(pcm []int16) error {
	buf := make([]byte, len(pcm)*2)
	for i, s := range pcm {
		// little-endian int16
		buf[2*i] = byte(s & 0xFF)
		buf[2*i+1] = byte((s >> 8) & 0xFF)
	}
	_, err := f.stdin.Write(buf)
	return err
}

// closeStdin closes ffmpeg's stdin to signal EOF (so it can finish
// encoding and exit cleanly).
func (f *ffmpegProcess) closeStdin() error {
	return f.stdin.Close()
}

// wait waits for ffmpeg to exit and returns its error (if any).
func (f *ffmpegProcess) wait() error {
	return f.cmd.Wait()
}

// kill terminates the ffmpeg process.
func (f *ffmpegProcess) kill() {
	f.cancel()
	if f.cmd.Process != nil {
		_ = f.cmd.Process.Kill()
	}
}

// generateSinePCM48k generates a mono 48 kHz PCM int16 buffer for a
// pure sine wave at the given frequency and duration.
func generateSinePCM48k(freq float64, durSec float64, amplitude float64) []int16 {
	n := int(OpusSampleRate * durSec)
	out := make([]int16, n)
	for i := 0; i < n; i++ {
		s := math.Sin(2 * math.Pi * freq * float64(i) / float64(OpusSampleRate))
		out[i] = int16(amplitude * 32767 * s)
	}
	return out
}

// SyntheticToneStreamer feeds a synthetic 440 Hz tone to ffmpeg in
// real-time. It is a thin wrapper that writes the sine wave to ffmpeg's
// stdin at the correct cadence (1× real-time so the audio plays for
// `durSec` seconds).
func streamSyntheticTone(f *ffmpegProcess, freq float64, durSec float64, amplitude float64) error {
	// write the full buffer; ffmpeg will block reading until we close
	// stdin OR ffmpeg's internal buffer fills. We close stdin when
	// done so ffmpeg can finalize the stream.
	pcm := generateSinePCM48k(freq, durSec, amplitude)
	if err := f.writePCM(pcm); err != nil {
		return fmt.Errorf("write synthetic PCM: %w", err)
	}
	if err := f.closeStdin(); err != nil {
		return fmt.Errorf("close ffmpeg stdin: %w", err)
	}
	return nil
}

// streamCartesiaPCM writes a Cartesia HD PCM buffer (s16le, mono, at
// the rate Cartesia returned) to ffmpeg's stdin and closes stdin so
// ffmpeg can finalize the stream. The input sample rate MUST match
// the rate passed to startFfmpegOpus (inputSampleRate).
//
// This is the Step 5 path: Cartesia HD PCM -> ffmpeg Opus -> LiveKit
// Opus -> browser. The user hears Cartesia's natural voice through
// the HD/WebRTC path, bypassing PSTN's 3.4 kHz ceiling.
func streamCartesiaPCM(f *ffmpegProcess, pcm []int16) error {
	if len(pcm) == 0 {
		return fmt.Errorf("cartesia: empty PCM buffer")
	}
	if err := f.writePCM(pcm); err != nil {
		return fmt.Errorf("write cartesia PCM: %w", err)
	}
	if err := f.closeStdin(); err != nil {
		return fmt.Errorf("close ffmpeg stdin: %w", err)
	}
	return nil
}

// savePCMAsWAV writes a mono int16 PCM buffer to a 16-bit PCM WAV
// file. Used for diagnostic purposes: saving the raw Cartesia PCM
// so the user can listen to it directly (bypassing Opus, ffmpeg,
// LiveKit, and the browser). Set SPIKE_SAVE_PCM=/path/to/file.wav
// in the spike env to enable.
//
// Format: RIFF/WAVE, fmt chunk (PCM, 1 channel, sample rate,
// 16-bit), data chunk.
func savePCMAsWAV(pcm []int16, sampleRate int, path string) error {
	const bitsPerSample = 16
	const numChannels = 1
	byteRate := sampleRate * numChannels * bitsPerSample / 8
	blockAlign := numChannels * bitsPerSample / 8
	dataSize := len(pcm) * 2
	riffSize := 36 + dataSize

	buf := make([]byte, 44+dataSize)
	// RIFF header
	copy(buf[0:4], "RIFF")
	binary.LittleEndian.PutUint32(buf[4:8], uint32(riffSize))
	copy(buf[8:12], "WAVE")
	// fmt chunk
	copy(buf[12:16], "fmt ")
	binary.LittleEndian.PutUint32(buf[16:20], 16) // fmt chunk size
	binary.LittleEndian.PutUint16(buf[20:22], 1)  // PCM format
	binary.LittleEndian.PutUint16(buf[22:24], uint16(numChannels))
	binary.LittleEndian.PutUint32(buf[24:28], uint32(sampleRate))
	binary.LittleEndian.PutUint32(buf[28:32], uint32(byteRate))
	binary.LittleEndian.PutUint16(buf[32:34], uint16(blockAlign))
	binary.LittleEndian.PutUint16(buf[34:36], uint16(bitsPerSample))
	// data chunk
	copy(buf[36:40], "data")
	binary.LittleEndian.PutUint32(buf[40:44], uint32(dataSize))
	// PCM samples
	for i, s := range pcm {
		binary.LittleEndian.PutUint16(buf[44+2*i:46+2*i], uint16(s))
	}
	return os.WriteFile(path, buf, 0644)
}

// mediaSample alias removed — we use github.com/pion/webrtc/v3/pkg/media.Sample directly.
