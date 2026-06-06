// xai_pcmu_test: simulate the production PCMU/G.711 8kHz path and feed the
// roundtripped audio to xAI Voice Agent to check whether the internal STT
// degrades vs clean 24 kHz input.
//
// Pipeline:
//   input.wav (24kHz PCM16 mono)
//     ffmpeg -> 8 kHz PCM16 mono (pcm16_8k.wav)
//     ffmpeg -> 8 kHz PCMU (pcmu_8k.wav)
//     ffmpeg -> 8 kHz PCM16 (pcm16_8k_roundtrip.wav)
//     ffmpeg -> 24 kHz PCM16 (pcm16_24k_roundtrip.wav)
//   feed pcm16_24k_roundtrip.wav to xAI Voice Agent as user audio
//   capture user transcript from conversation.item.input_audio_transcription.completed
//
// This validates the production path: customer PCM over Telnyx is
// typically 8 kHz PCMU; if xAI Voice Agent's internal STT can't handle
// the 8 kHz-derived audio, we need a resampler in the production worker.
package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/joho/godotenv"
)

func main() {
	for _, f := range []string{".env", "../.env", "../../.env", "xai-voice-agent.env"} {
		if _, err := os.Stat(f); err == nil {
			_ = godotenv.Load(f)
		}
	}
	apiKey := os.Getenv("XAI_API_KEY")
	if apiKey == "" {
		log.Fatal("XAI_API_KEY not set")
	}
	if len(os.Args) < 2 {
		log.Fatal("usage: xai-pcmu-test <input-24k-wav> [expected-text]")
	}
	inPath, _ := filepath.Abs(os.Args[1])
	expected := ""
	if len(os.Args) > 2 {
		expected = os.Args[2]
	}

	fmt.Println("=== xAI PCMU roundtrip test ===")
	fmt.Printf("time:        %s\n", time.Now().UTC().Format(time.RFC3339))
	fmt.Printf("input:       %s\n", inPath)
	fmt.Printf("expected:    %s\n", expected)

	tmpDir, err := os.MkdirTemp("", "xai-pcmu-*")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)
	fmt.Printf("scratch dir: %s\n", tmpDir)

	pcm8k := filepath.Join(tmpDir, "pcm16_8k.wav")
	pcmu := filepath.Join(tmpDir, "pcmu_8k.wav")
	pcm8kRT := filepath.Join(tmpDir, "pcm16_8k_roundtrip.wav")
	pcm24kRT := filepath.Join(tmpDir, "pcm16_24k_roundtrip.wav")

	// 1. 24k -> 8k
	runFFmpeg("-y", "-hide_banner", "-loglevel", "error",
		"-i", inPath, "-ac", "1", "-ar", "8000",
		"-f", "wav", pcm8k)
	// 2. 8k PCM16 -> 8k PCMU
	runFFmpeg("-y", "-hide_banner", "-loglevel", "error",
		"-i", pcm8k, "-codec:a", "pcm_mulaw", "-ar", "8000", "-ac", "1",
		"-f", "wav", pcmu)
	// 3. 8k PCMU -> 8k PCM16 (roundtrip)
	runFFmpeg("-y", "-hide_banner", "-loglevel", "error",
		"-f", "mulaw", "-ar", "8000", "-ac", "1", "-i", pcmu,
		"-codec:a", "pcm_s16le", "-ar", "8000", "-ac", "1",
		"-f", "wav", pcm8kRT)
	// 4. 8k PCM16 -> 24k PCM16 (upsample)
	runFFmpeg("-y", "-hide_banner", "-loglevel", "error",
		"-i", pcm8kRT, "-ac", "1", "-ar", "24000",
		"-codec:a", "pcm_s16le",
		"-f", "wav", pcm24kRT)

	// 5. Read the upsampled WAV into PCM16 samples
	pcm, sampleRate, err := readWAVMonoPCM16(pcm24kRT)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("roundtripped: %d samples at %d Hz (%.2f seconds)\n",
		len(pcm), sampleRate, float64(len(pcm))/float64(sampleRate))

	// 6. Connect to xAI Voice Agent and feed the audio
	transcripts, err := feedAndTranscribe(apiKey, pcm, sampleRate)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println()
	fmt.Println("=== xAI user transcripts (roundtripped audio) ===")
	for i, t := range transcripts {
		fmt.Printf("  [%d] %s\n", i+1, t)
	}
	if expected != "" {
		fmt.Println()
		fmt.Println("=== Expected ===")
		fmt.Printf("  %s\n", expected)
		fmt.Println()
		// Simple match: does the transcript contain any of the expected words?
		joined := ""
		for _, t := range transcripts {
			joined += t + " "
		}
		fmt.Printf("Match: %v\n", containsAnyWord(joined, expected))
	}
}

func containsAnyWord(haystack, needlePhrase string) bool {
	// Return true if any whitespace-separated word from needlePhrase
	// appears in haystack. Case-insensitive.
	for _, w := range splitWords(needlePhrase) {
		if len(w) < 3 {
			continue
		}
		if containsWord(haystack, w) {
			return true
		}
	}
	return false
}

func splitWords(s string) []string {
	var out []string
	cur := ""
	for _, r := range s {
		if r == ' ' || r == '\n' || r == '\t' || r == ',' || r == '.' {
			if cur != "" {
				out = append(out, cur)
				cur = ""
			}
		} else {
			cur += string(r)
		}
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}

func containsWord(haystack, word string) bool {
	hl := toLower(haystack)
	wl := toLower(word)
	if len(wl) > len(hl) {
		return false
	}
	for i := 0; i+len(wl) <= len(hl); i++ {
		if hl[i:i+len(wl)] == wl {
			before := i == 0 || !isAlpha(hl[i-1])
			after := i+len(wl) == len(hl) || !isAlpha(hl[i+len(wl)])
			if before && after {
				return true
			}
		}
	}
	return false
}

func toLower(s string) string {
	out := []byte(s)
	for i, b := range out {
		if b >= 'A' && b <= 'Z' {
			out[i] = b + 32
		}
	}
	return string(out)
}

func isAlpha(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

func runFFmpeg(args ...string) {
	bin := "ffmpeg"
	// Windows: ShareX installs ffmpeg here
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		if _, err := os.Stat(`C:\Program Files\ShareX\ffmpeg.exe`); err == nil {
			bin = `C:\Program Files\ShareX\ffmpeg.exe`
		} else {
			log.Fatalf("ffmpeg not found in PATH and not at C:\\Program Files\\ShareX\\ffmpeg.exe")
		}
	}
	cmd := exec.Command(bin, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		log.Fatalf("ffmpeg %v failed: %v\n%s", args, err, string(out))
	}
}

func readWAVMonoPCM16(path string) ([]int16, int, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()
	// Skip 44-byte WAV header (assume standard PCM)
	header := make([]byte, 44)
	if _, err := io.ReadFull(f, header); err != nil {
		return nil, 0, fmt.Errorf("read header: %w", err)
	}
	if string(header[0:4]) != "RIFF" || string(header[8:12]) != "WAVE" {
		return nil, 0, fmt.Errorf("not a WAV file")
	}
	sampleRate := int(binary.LittleEndian.Uint32(header[24:28]))
	channels := int(binary.LittleEndian.Uint16(header[22:24]))
	bitsPerSample := int(binary.LittleEndian.Uint16(header[34:36]))
	if channels != 1 || bitsPerSample != 16 {
		return nil, 0, fmt.Errorf("expected mono 16-bit, got %d ch %d bps", channels, bitsPerSample)
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return nil, 0, err
	}
	if len(data)%2 != 0 {
		data = data[:len(data)-1]
	}
	pcm := make([]int16, len(data)/2)
	for i := range pcm {
		pcm[i] = int16(binary.LittleEndian.Uint16(data[i*2:]))
	}
	return pcm, sampleRate, nil
}

func feedAndTranscribe(apiKey string, pcm []int16, sampleRate int) ([]string, error) {
	dialer := newWebSocketDialer()
	headers := httpHeaderWithBearer(apiKey)
	url := "wss://api.x.ai/v1/realtime?model=grok-voice-latest"
	fmt.Println("dialing xAI WSS...")
	conn, _, err := dialer.Dial(url, headers)
	if err != nil {
		return nil, fmt.Errorf("dial: %w", err)
	}
	defer conn.Close()
	fmt.Println("dial OK; reading first event...")
	// Read the first event (likely session.created)
	_, first, err := conn.ReadMessage()
	if err != nil {
		return nil, fmt.Errorf("read first: %w", err)
	}
	fmt.Printf("first event: %.200s\n", string(first))
	sess := map[string]any{
		"voice":        "eve",
		"instructions": "Echo back what the user said, briefly.",
		"turn_detection": map[string]any{
			"type":                "server_vad",
			"threshold":           0.7,
			"silence_duration_ms": 500,
			"prefix_padding_ms":   200,
			"create_response":     true,
			"interrupt_response":  true,
		},
	}
	if err := writeJSON(conn, map[string]any{
		"type": "session.update", "session": sess,
	}); err != nil {
		return nil, err
	}
	fmt.Println("session.update sent; feeding audio...")
	chunkSamples := sampleRate / 10 // 100ms
	var transcripts []string
	done := make(chan struct{})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	go func() {
		defer close(done)
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				fmt.Printf("reader err: %v\n", err)
				return
			}
			var ev map[string]any
			if err := jsonUnmarshal(data, &ev); err != nil {
				continue
			}
			evType, _ := ev["type"].(string)
			fmt.Printf("  event: %s\n", evType)
			if evType == "conversation.item.input_audio_transcription.completed" {
				if item, ok := ev["transcript"].(string); ok && item != "" {
					transcripts = append(transcripts, item)
					fmt.Printf("  transcript: %s\n", item)
				}
			}
		}
	}()
	totalChunks := (len(pcm) + chunkSamples - 1) / chunkSamples
	for i := 0; i < len(pcm); i += chunkSamples {
		end := i + chunkSamples
		if end > len(pcm) {
			end = len(pcm)
		}
		chunk := pcm[i:end]
		buf := new(bytes.Buffer)
		for _, s := range chunk {
			binary.Write(buf, binary.LittleEndian, uint16(s))
		}
		chunkB64 := base64StdEncoding(buf.Bytes())
		if err := writeJSON(conn, map[string]any{
			"type":  "input_audio_buffer.append",
			"audio": chunkB64,
		}); err != nil {
			return transcripts, err
		}
		// Pace at real-time so xAI's VAD sees the audio at the right speed
		select {
		case <-time.After(100 * time.Millisecond):
		case <-ctx.Done():
			return transcripts, ctx.Err()
		}
	}
	fmt.Printf("fed %d chunks (%d samples); committing...\n", totalChunks, len(pcm))
	_ = writeJSON(conn, map[string]any{"type": "input_audio_buffer.commit"})
	select {
	case <-time.After(3 * time.Second):
	case <-ctx.Done():
	}
	cancel()
	<-done
	return transcripts, nil
}
