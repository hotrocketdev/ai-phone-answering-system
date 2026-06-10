// Self-test for the OGG Opus muxer + ffmpeg decode path.
//
// Builds a synthetic 1-second OGG Opus stream (50 frames of fake
// "Opus data"), writes it to a file, runs `ffmpeg -f ogg -i
// file.ogg -f s16le -ar 16000 -ac 1 out.pcm`, and reports PCM
// bytes. Run with:
//
//   go test -run TestOggMuxerDecode -v
package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestOggMuxerDecode(t *testing.T) {
	if os.Getenv("SKIP_FFMPEG") == "1" {
		t.Skip("SKIP_FFMPEG=1")
	}

	// Build a synthetic OGG Opus stream: OpusHead + OpusTags + 50
	// fake Opus frames, each ~80 bytes (typical for 20ms at 64kbps).
	var buf bytes.Buffer
	muxer := newOggMuxer(&buf, 0xC0FFEE, 1, 48000)
	if err := muxer.writeOpusHead(); err != nil {
		t.Fatalf("writeOpusHead: %v", err)
	}
	if err := muxer.writeOpusTags(); err != nil {
		t.Fatalf("writeOpusTags: %v", err)
	}
	for i := 0; i < 50; i++ {
		frame := make([]byte, 80)
		// Fill with non-zero so the data looks like real Opus.
		for j := range frame {
			frame[j] = byte((i + j) & 0xFF)
		}
		if err := muxer.writeOpusFrame(frame, 960); err != nil {
			t.Fatalf("writeOpusFrame[%d]: %v", i, err)
		}
	}

	oggBytes := buf.Bytes()
	t.Logf("OGG stream: %d bytes (OpusHead + OpusTags + 50 frames)", len(oggBytes))

	// Write to a temp file.
	dir, err := os.MkdirTemp("", "oggtest-")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	defer os.RemoveAll(dir)
	oggPath := filepath.Join(dir, "test.ogg")
	if err := os.WriteFile(oggPath, oggBytes, 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Run ffmpeg to decode.
	ctx, cancel := context.WithTimeout(context.Background(), 10*1e9)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ffmpeg",
		"-hide_banner",
		"-loglevel", "info",
		"-f", "ogg",
		"-i", oggPath,
		"-ar", "16000",
		"-ac", "1",
		"-f", "s16le",
		"-",
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("StdoutPipe: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("ffmpeg start: %v", err)
	}
	pcmBytes, _ := io.ReadAll(stdout)
	if err := cmd.Wait(); err != nil {
		t.Logf("ffmpeg stderr:\n%s", stderr.String())
		t.Fatalf("ffmpeg exit: %v", err)
	}
	t.Logf("ffmpeg stderr:\n%s", stderr.String())
	t.Logf("PCM out: %d bytes (%.2fs @ 16kHz mono s16le)", len(pcmBytes), float64(len(pcmBytes))/32000.0)
	if len(pcmBytes) == 0 {
		t.Fatalf("got 0 PCM bytes — decode failed silently")
	}
	if len(pcmBytes) < 30000 {
		t.Fatalf("got %d PCM bytes, expected ~32000 (1s of 16kHz mono s16le)", len(pcmBytes))
	}
	t.Logf("PASS: 1s synthetic utterance decoded to %d PCM bytes", len(pcmBytes))
}
