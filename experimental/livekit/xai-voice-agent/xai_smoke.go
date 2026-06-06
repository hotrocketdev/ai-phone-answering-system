// xai_smoke.go — minimal text-only smoke test (no LiveKit).
//
// Use this for the first sanity check: does the xAI Voice Agent respond
// to a text message and stream audio out?
//
// Usage:
//
//   XAI_API_KEY=xai-... go run . --no-livekit
//
// Then type into stdin. Type :quit to exit.
package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// runSmokeTest is the non-LiveKit path. With cfg.autoMsg set, it sends
// one message and exits after response.done (or a 20-second timeout).
// Otherwise it reads text from stdin interactively.

func runSmokeTest(ctx context.Context, cfg *config) error {
	autoMsg := cfg.autoMsg

	xai, err := newXaiClient(cfg)
	if err != nil {
		return err
	}
	defer xai.Close()

	// Wire up callbacks: print transcripts, save audio to a single WAV file.
	out, err := os.Create("xai-smoke-output.wav")
	if err != nil {
		return fmt.Errorf("create wav: %w", err)
	}
	defer out.Close()
	wav := newWAVWriter(out, xaiSampleRate, 1, 16)
	defer wav.Close()

	var (
		audioBytes   int
		transcripts  []string
		responseDone = make(chan struct{}, 1)
	)

	xai.OnAudioDelta = func(pcm []int16) {
		wav.WriteSamples(pcm)
		audioBytes += len(pcm) * 2
	}
	xai.OnTranscript = func(role, text string) {
		log.Printf("xai transcript [%s]: %s", role, text)
		transcripts = append(transcripts, fmt.Sprintf("[%s] %s", role, text))
	}
	xai.OnTranscriptDelta = func(role, text string) {
		// Append the delta to the in-progress transcript for this role.
		// For simplicity, log incrementally. The full transcript will
		// be reported via OnTranscript when the response.done fires.
		// (No-op for the smoke test: the final transcript is enough.)
	}
	xai.OnError = func(err error) {
		log.Printf("xai error: %v", err)
	}
	xai.OnResponseDone = func() {
		log.Printf("xai response.done received; %d bytes of audio captured", audioBytes)
		select {
		case responseDone <- struct{}{}:
		default:
		}
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := xai.ReadEvents(ctx); err != nil && err != websocket.ErrCloseSent {
			log.Printf("xai read loop: %v", err)
		}
	}()

	if autoMsg != "" {
		log.Printf("auto mode: sending %q and waiting for response.done", autoMsg)
		if err := xai.SendUserText(autoMsg); err != nil {
			return fmt.Errorf("send: %w", err)
		}
		select {
		case <-responseDone:
			log.Printf("got response.done; transcripts=%d audio_bytes=%d", len(transcripts), audioBytes)
		case <-time.After(20 * time.Second):
			log.Printf("timeout waiting for response.done; transcripts=%d audio_bytes=%d", len(transcripts), audioBytes)
		}
		xai.CancelResponse()
		xai.Close()
		wg.Wait()
		log.Printf("smoke test done. Output saved to xai-smoke-output.wav")
		return nil
	}

	log.Printf("smoke test ready. Type a message and press Enter. Type :quit to exit.")
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for {
		fmt.Print("you> ")
		if !scanner.Scan() {
			break
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if line == ":quit" {
			break
		}
		if err := xai.SendUserText(line); err != nil {
			log.Printf("send error: %v", err)
		}
		// Best-effort: wait briefly for the response to finish so the
		// user sees a clean transcript before the next prompt.
		select {
		case <-responseDone:
		case <-time.After(15 * time.Second):
		}
	}
	xai.CancelResponse()
	xai.Close()
	wg.Wait()
	log.Printf("smoke test done. Output saved to xai-smoke-output.wav")
	return nil
}

// --- minimal WAV writer (PCM16 mono) ---

type wavWriter struct {
	f          *os.File
	dataBytes  uint32
	sampleRate int
	ch         int
	bps        int
	closed     bool
}

func newWAVWriter(f *os.File, sampleRate, ch, bps int) *wavWriter {
	w := &wavWriter{f: f, sampleRate: sampleRate, ch: ch, bps: bps}
	w.writeHeader()
	return w
}

func (w *wavWriter) writeHeader() {
	byteRate := w.sampleRate * w.ch * w.bps / 8
	blockAlign := w.ch * w.bps / 8
	hdr := make([]byte, 44)
	copy(hdr[0:4], "RIFF")
	binaryPutUint32LE(hdr[4:8], 0) // file size - 8, to be filled on close
	copy(hdr[8:12], "WAVE")
	copy(hdr[12:16], "fmt ")
	binaryPutUint32LE(hdr[16:20], 16) // fmt chunk size for PCM
	binaryPutUint16LE(hdr[20:22], 1)  // PCM format
	binaryPutUint16LE(hdr[22:24], uint16(w.ch))
	binaryPutUint32LE(hdr[24:28], uint32(w.sampleRate))
	binaryPutUint32LE(hdr[28:32], uint32(byteRate))
	binaryPutUint16LE(hdr[32:34], uint16(blockAlign))
	binaryPutUint16LE(hdr[34:36], uint16(w.bps))
	copy(hdr[36:40], "data")
	binaryPutUint32LE(hdr[40:44], 0) // data size, filled on close
	w.f.Write(hdr)
}

func (w *wavWriter) WriteSamples(pcm []int16) {
	if w.closed {
		return
	}
	buf := make([]byte, len(pcm)*2)
	for i, s := range pcm {
		binaryPutUint16LE(buf[i*2:i*2+2], uint16(s))
	}
	n, _ := w.f.Write(buf)
	w.dataBytes += uint32(n)
}

func (w *wavWriter) Close() {
	if w.closed {
		return
	}
	w.closed = true
	// Patch up the data size and total file size
	if _, err := w.f.Seek(40, 0); err == nil {
		var b [4]byte
		binaryPutUint32LE(b[:], w.dataBytes)
		w.f.Write(b[:])
	}
	if _, err := w.f.Seek(4, 0); err == nil {
		var b [4]byte
		binaryPutUint32LE(b[:], 36+w.dataBytes)
		w.f.Write(b[:])
	}
	w.f.Sync()
}

func binaryPutUint32LE(b []byte, v uint32) {
	b[0] = byte(v)
	b[1] = byte(v >> 8)
	b[2] = byte(v >> 16)
	b[3] = byte(v >> 24)
}

func binaryPutUint16LE(b []byte, v uint16) {
	b[0] = byte(v)
	b[1] = byte(v >> 8)
}
