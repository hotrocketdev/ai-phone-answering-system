// Per-utterance ffmpeg Opus decoder.
//
// The spike's VAD is time-based on inbound Opus frame cadence
// (browsers send 20ms frames at 48kHz reliably; silence = no
// frames). When the worker detects a 500ms silence gap, it
// snapshots the buffered Opus frames and runs them through ffmpeg
// once to get PCM for STT.
//
// Per-utterance ffmpeg (instead of one long-lived ffmpeg) keeps
// the design stateless and avoids ogg-stream-EOF edge cases. The
// ~50-100ms ffmpeg startup cost is small relative to a 2-3s turn.
package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"os/exec"
)

// opusDecodeSampleRate is the canonical rate for Whisper STT.
const opusDecodeSampleRate = 16000

// opusDecodeFrameBytes is one 20ms mono s16le frame at
// opusDecodeSampleRate: 16000 * 0.020 * 2 = 640 bytes.
const opusDecodeFrameBytes = 640

// decodeOpusUtteranceToPCM decodes a slice of raw Opus frames
// (one per OGG page) to a contiguous mono s16le PCM buffer at
// 16kHz, suitable for WAV-wrapping and OpenAI Whisper upload.
//
// It launches a single ffmpeg subprocess, writes OpusHead +
// OpusTags + each frame in OGG-page form, closes stdin, and reads
// all PCM from stdout.
//
// CRITICAL: stdout MUST be drained in a goroutine concurrently
// with the stdin write, otherwise the 64KB OS pipe buffer fills
// with PCM, ffmpeg blocks on stdout write, ffmpeg stops reading
// stdin, and the parent blocks on stdin write. Classic pipe
// deadlock. With >2-3s of audio this is reliably reproducible
// (30s ctx fires, ffmpeg killed, write returns EPIPE).
//
// ffmpeg is run with -ar 16000 -ac 1 -f s16le pipe:1.
func decodeOpusUtteranceToPCM(ctx context.Context, frames [][]byte) ([]byte, error) {
	if len(frames) == 0 {
		return nil, fmt.Errorf("opus-decode: no frames")
	}

	cmd := exec.CommandContext(ctx, "ffmpeg",
		"-hide_banner",
		"-loglevel", "error",
		"-f", "ogg",
		"-i", "pipe:0",
		// Inbound filter chain (mic -> STT). DC removal only;
		// anlmdn (RNNoise) on this audio was found to suppress
		// real speech along with noise, leaving Whisper with
		// silence. The browser's WebRTC NS already cleans
		// mic input; we don't double-denoise.
		"-af", "highpass=f=80",
		"-ar", fmt.Sprintf("%d", opusDecodeSampleRate),
		"-ac", "1",
		"-f", "s16le",
		"pipe:1",
	)
	cmd.Env = append([]string{"AV_LOG_FORCE_NOCOLOR=1"}, envFromCtx(ctx)...)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("opus-decode stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("opus-decode stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("opus-decode stderr pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("opus-decode start: %w", err)
	}

	// Drain stderr in a goroutine (so ffmpeg errors surface).
	go func() {
		buf := make([]byte, 4096)
		for {
			n, e := stderr.Read(buf)
			if n > 0 {
				log.Printf("opus-decode[stderr]: %s", string(buf[:n]))
			}
			if e != nil {
				return
			}
		}
	}()

	// Drain stdout in a goroutine BEFORE writing stdin. This
	// guarantees ffmpeg can always write to stdout (the 64KB OS
	// pipe buffer never fills, no deadlock) regardless of how
	// much PCM is produced. We accumulate into pcmBuf and signal
	// completion via stdoutDone.
	var pcmBuf bytes.Buffer
	stdoutDone := make(chan error, 1)
	go func() {
		buf := make([]byte, 8192)
		for {
			n, e := stdout.Read(buf)
			if n > 0 {
				pcmBuf.Write(buf[:n])
			}
			if e != nil {
				stdoutDone <- e
				return
			}
		}
	}()

	// Build the OGG Opus stream in a bytes.Buffer, then write it
	// to stdin. With the stdout drainer running, ffmpeg can pull
	// bytes as fast as we can write them.
	var oggBuf bytes.Buffer
	muxer := newOggMuxer(&oggBuf, 0xC0FFEE, 1, 48000)
	if err := muxer.writeOpusHead(); err != nil {
		_ = cmd.Process.Kill()
		return nil, err
	}
	if err := muxer.writeOpusTags(); err != nil {
		_ = cmd.Process.Kill()
		return nil, err
	}
	for _, f := range frames {
		if len(f) == 0 {
			continue
		}
		if err := muxer.writeOpusFrame(f, 960); err != nil { // 20ms at 48kHz
			_ = cmd.Process.Kill()
			return nil, err
		}
	}

	if _, err := stdin.Write(oggBuf.Bytes()); err != nil {
		_ = cmd.Process.Kill()
		return nil, fmt.Errorf("opus-decode write stdin: %w", err)
	}
	if err := stdin.Close(); err != nil {
		_ = cmd.Process.Kill()
		return nil, fmt.Errorf("opus-decode close stdin: %w", err)
	}

	// Wait for the stdout drainer to finish (ffmpeg closed
	// stdout after consuming all input and flushing output).
	if err := <-stdoutDone; err != nil && err != io.EOF {
		_ = cmd.Process.Kill()
		return nil, fmt.Errorf("opus-decode read stdout: %w", err)
	}

	if err := cmd.Wait(); err != nil {
		// ffmpeg can exit 0 or with a benign flush code; only fail
		// if we got no PCM at all.
		if pcmBuf.Len() == 0 {
			return nil, fmt.Errorf("opus-decode ffmpeg exit: %w", err)
		}
	}
	return pcmBuf.Bytes(), nil
}

// pcmS16LEToWAV wraps a mono s16le PCM buffer at the given sample
// rate in a minimal RIFF/WAVE header. Suitable for direct upload to
// OpenAI's `audio/transcriptions` endpoint.
func pcmS16LEToWAV(pcm []byte, sampleRate int) []byte {
	const (
		headerSize  = 44
		formatPCM   = 1
		channels    = 1
		bitsPerSamp = 16
	)
	dataSize := uint32(len(pcm))
	totalSize := uint32(headerSize-8) + dataSize
	out := make([]byte, headerSize+len(pcm))
	copy(out[0:4], "RIFF")
	binary.LittleEndian.PutUint32(out[4:8], totalSize)
	copy(out[8:12], "WAVE")
	copy(out[12:16], "fmt ")
	binary.LittleEndian.PutUint32(out[16:20], 16) // fmt chunk size
	binary.LittleEndian.PutUint16(out[20:22], formatPCM)
	binary.LittleEndian.PutUint16(out[22:24], channels)
	binary.LittleEndian.PutUint32(out[24:28], uint32(sampleRate))
	binary.LittleEndian.PutUint32(out[28:32], uint32(sampleRate*channels*bitsPerSamp/8))
	binary.LittleEndian.PutUint16(out[32:34], uint16(channels*bitsPerSamp/8))
	binary.LittleEndian.PutUint16(out[34:36], bitsPerSamp)
	copy(out[36:40], "data")
	binary.LittleEndian.PutUint32(out[40:44], dataSize)
	copy(out[44:], pcm)
	return out
}

// envFromCtx returns the process env (or empty slice if nil).
// Reserved for future use: passing per-call env overrides.
func envFromCtx(ctx context.Context) []string {
	return nil
}

// int16ToBytes converts a slice of int16 PCM samples to a
// little-endian byte buffer (for WAV wrapping or debug save).
func int16ToBytes(pcm []int16) []byte {
	buf := make([]byte, 2*len(pcm))
	for i, s := range pcm {
		buf[2*i] = byte(s)
		buf[2*i+1] = byte(s >> 8)
	}
	return buf
}
