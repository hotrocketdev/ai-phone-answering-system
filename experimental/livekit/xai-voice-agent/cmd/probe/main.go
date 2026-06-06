// xai_probe: probe the xAI REST API to discover which standalone
// endpoints exist (STT, TTS, LLM) and what models/voices are available.
//
// This is an isolated validation test, NOT production code. It is run
// manually via `go run .` from experimental/livekit/xai-voice-agent/.
//
// All calls use the XAI_API_KEY loaded from .env via godotenv.
// Output is written to stdout in human-readable form. No secrets are
// logged; only model names, voice names, and example response shapes.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/joho/godotenv"
)

func main() {
	for _, f := range []string{".env", "../.env", "../../.env", "xai-voice-agent.env"} {
		if _, err := os.Stat(f); err == nil {
			if err := godotenv.Load(f); err != nil {
				log.Printf("warn: load %s: %v", f, err)
			} else {
				log.Printf("loaded %s", f)
			}
		}
	}
	apiKey := os.Getenv("XAI_API_KEY")
	if apiKey == "" {
		log.Fatal("XAI_API_KEY not set")
	}

	fmt.Println("=== xAI REST API probe ===")
	fmt.Printf("time: %s\n", time.Now().UTC().Format(time.RFC3339))
	fmt.Println()

	// 1. List models
	fmt.Println("--- 1. GET /v1/models ---")
	if resp, err := get("https://api.x.ai/v1/models", apiKey); err != nil {
		fmt.Printf("error: %v\n", err)
	} else {
		fmt.Println(prettyJSON(resp))
	}
	fmt.Println()

	// 2. Try standalone TTS
	fmt.Println("--- 2. POST /v1/audio/speech (TTS) ---")
	ttsBody := map[string]interface{}{
		"model": "grok-voice-tts", // guess; OpenAI uses "tts-1"
		"input": "Hello, this is a test of the xAI text to speech API.",
		"voice": "eve",
	}
	if status, body, err := post("https://api.x.ai/v1/audio/speech", apiKey, ttsBody); err != nil {
		fmt.Printf("error: %v\n", err)
	} else {
		fmt.Printf("status: %d\n", status)
		fmt.Printf("content-type: %s\n", body[:min(200, len(body))])
	}
	fmt.Println()

	// 3. Try standalone STT (no audio bytes; just probe the endpoint shape)
	fmt.Println("--- 3. POST /v1/audio/transcriptions (STT, empty file probe) ---")
	sttProbe := map[string]interface{}{
		"model": "grok-voice-stt", // guess; OpenAI uses "whisper-1"
	}
	if status, body, err := post("https://api.x.ai/v1/audio/transcriptions", apiKey, sttProbe); err != nil {
		fmt.Printf("error: %v\n", err)
	} else {
		fmt.Printf("status: %d\n", status)
		fmt.Println(body)
	}
	fmt.Println()

	// 4. Try chat completions (LLM) with tool calling (correct schema)
	fmt.Println("--- 4. POST /v1/chat/completions (LLM with tools, type:function) ---")
	chatBody := map[string]interface{}{
		"model": "grok-4.20-0309-non-reasoning",
		"messages": []map[string]string{
			{"role": "system", "content": "You are a VoxLane receptionist. Always answer in UK English. Be brief."},
			{"role": "user", "content": "What time do you close on Saturdays?"},
		},
		"tools": []map[string]interface{}{
			{
				"type": "function",
				"function": map[string]interface{}{
					"name":        "availability.check",
					"description": "Check table availability for a given date and party size",
					"parameters": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"date":       map[string]interface{}{"type": "string", "description": "ISO date YYYY-MM-DD"},
							"party_size": map[string]interface{}{"type": "integer"},
						},
						"required": []string{"date", "party_size"},
					},
				},
			},
		},
	}
	if status, body, err := postJSON("https://api.x.ai/v1/chat/completions", apiKey, chatBody); err != nil {
		fmt.Printf("error: %v\n", err)
	} else {
		fmt.Printf("status: %d\n", status)
		fmt.Println(prettyJSON(body))
	}
	fmt.Println()

	// 5. Try chat completions forcing tool use
	fmt.Println("--- 5. POST /v1/chat/completions (force tool use) ---")
	chatBody2 := map[string]interface{}{
		"model": "grok-4.20-0309-non-reasoning",
		"messages": []map[string]string{
			{"role": "system", "content": "You are a VoxLane receptionist."},
			{"role": "user", "content": "Do you have a table for four at seven tomorrow?"},
		},
		"tools": []map[string]interface{}{
			{
				"type": "function",
				"function": map[string]interface{}{
					"name":        "availability.check",
					"description": "Check table availability",
					"parameters": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"date":       map[string]interface{}{"type": "string"},
							"party_size": map[string]interface{}{"type": "integer"},
							"time":       map[string]interface{}{"type": "string"},
						},
						"required": []string{"date", "party_size", "time"},
					},
				},
			},
		},
		"tool_choice": map[string]interface{}{"type": "function", "function": map[string]interface{}{"name": "availability.check"}},
	}
	if status, body, err := postJSON("https://api.x.ai/v1/chat/completions", apiKey, chatBody2); err != nil {
		fmt.Printf("error: %v\n", err)
	} else {
		fmt.Printf("status: %d\n", status)
		fmt.Println(prettyJSON(body))
	}
}

func get(url, key string) (string, error) {
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Authorization", "Bearer "+key)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return string(b), nil
}

func post(url, key string, body map[string]interface{}) (int, string, error) {
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest("POST", url, bytes.NewReader(b))
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	rb, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(rb), nil
}

func postJSON(url, key string, body map[string]interface{}) (int, string, error) {
	return post(url, key, body)
}

func prettyJSON(s string) string {
	var v interface{}
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		return s
	}
	b, _ := json.MarshalIndent(v, "", "  ")
	return string(b)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
