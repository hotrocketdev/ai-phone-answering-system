// ffmpeg subprocess wrapper for outbound Opus encoding.
//
// The spike pattern: write Cartesia HD PCM (or test-tone PCM) to
// ffmpeg's stdin, ask ffmpeg to encode it to Opus on stdout, then
// demux the Ogg Opus into raw Opus frames and hand each to the
// outbound LiveKit track.
//
// Same shape as the spike publisher's ffmpegopus.go but with a
// simpler streamPCM (single buffer, no streaming).
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
	"strings"
	"time"
)

const (
	opusSampleRate    = 48000
	opusChannels      = 1
	opusFrameDuration = 20 * time.Millisecond
)

// ffmpegProcess is a running ffmpeg child process.
type ffmpegProcess struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	stderr *bufio.Reader
	cancel context.CancelFunc
}

// startFfmpegOpus launches ffmpeg with PCM-on-stdin, Opus-on-stdout.
//
// Parameters mirror the publisher's spike:
//   - ctx: cancellation context.
//   - inputSampleRate: rate of the PCM arriving on stdin (e.g. 48000
//     for Cartesia at 48 kHz or the synthetic tone).
//   - inputFormat: ffmpeg `-f` value matching the byte layout.
//     "s16le" (int16) or "f32le" (float32). The Cartesia-style
//     "pcm_s16le" / "pcm_f32le" prefix is stripped.
//   - filterChain: full `-af` argument. Pass "none" to skip filters.
//   - bitrate: Opus target bitrate in bps.
//   - application: Opus application mode ("audio" or "voip").
func startFfmpegOpus(ctx context.Context, inputSampleRate int, inputFormat, filterChain string, bitrate int, application string) (*ffmpegProcess, error) {
	normalized := strings.TrimPrefix(inputFormat, "pcm_")
	args := []string{
		"-hide_banner",
		"-loglevel", "error",
		"-f", normalized,
		"-ar", fmt.Sprintf("%d", inputSampleRate),
		"-ac", fmt.Sprintf("%d", opusChannels),
		"-i", "pipe:0",
	}
	if filterChain != "" && filterChain != "none" {
		args = append(args, "-af", filterChain)
	}
	args = append(args,
		"-c:a", "libopus",
		"-application", application,
		"-b:a", fmt.Sprintf("%d", bitrate),
		"-vbr", "on",
		"-compression_level", "10",
		"-f", "opus",
		"pipe:1",
	)
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
	go func() {
		s := bufio.NewScanner(stderr)
		for s.Scan() {
			line := s.Text()
			if line != "" {
				log.Printf("ffmpeg[stderr]: %s", line)
			}
		}
	}()
	return &ffmpegProcess{cmd: cmd, stdin: stdin, stdout: stdout, stderr: bufio.NewReader(stderr), cancel: cancel}, nil
}

// streamPCM writes a single PCM buffer to ffmpeg's stdin in the
// requested byte format and closes stdin so ffmpeg can finalize.
func streamPCM(f *ffmpegProcess, pcm []int16, format string) error {
	normalized := strings.TrimPrefix(format, "pcm_")
	switch normalized {
	case "s16le", "":
		if err := writePCMInt16(f.stdin, pcm); err != nil {
			return fmt.Errorf("write PCM s16le: %w", err)
		}
	case "f32le":
		if err := writePCMFloat32(f.stdin, pcm); err != nil {
			return fmt.Errorf("write PCM f32le: %w", err)
		}
	default:
		return fmt.Errorf("unsupported PCM format %q (use s16le or f32le)", format)
	}
	if err := f.stdin.Close(); err != nil {
		return fmt.Errorf("close ffmpeg stdin: %w", err)
	}
	return nil
}

func writePCMInt16(w io.Writer, pcm []int16) error {
	buf := make([]byte, 2*len(pcm))
	for i, s := range pcm {
		binary.LittleEndian.PutUint16(buf[2*i:2*i+2], uint16(s))
	}
	_, err := w.Write(buf)
	return err
}

func writePCMFloat32(w io.Writer, pcm []int16) error {
	buf := make([]byte, 4*len(pcm))
	for i, s := range pcm {
		// Normalize int16 to [-1, 1] float32.
		binary.LittleEndian.PutUint32(buf[4*i:4*i+4], math.Float32bits(float32(s)/32768.0))
	}
	_, err := w.Write(buf)
	return err
}

func (f *ffmpegProcess) kill() {
	if f == nil {
		return
	}
	if f.cmd != nil && f.cmd.Process != nil {
		_ = f.cmd.Process.Kill()
	}
	if f.cancel != nil {
		f.cancel()
	}
}
