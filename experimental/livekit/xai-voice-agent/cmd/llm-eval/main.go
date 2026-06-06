// xai_llm_test: validate xAI's standalone LLM (grok-4.20-non-reasoning) for
// VoxLane receptionist behaviour. Tests 9 base + 6 edge utterances plus
// function-calling schema.
//
// This is an isolated validation test, NOT production code. Run via
// `go run .` from experimental/livekit/xai-voice-agent/.
//
// All calls use the XAI_API_KEY loaded from .env via godotenv.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

const systemPrompt = `You are the receptionist at Porto Douro, a family-run Portuguese restaurant in Solihull, UK. You answer the phone and help customers with bookings, menu questions, and general enquiries.

Rules:
- Always speak in UK English. Use UK date and time formats (e.g., "half past seven", "Tuesday the seventeenth of June").
- Be brief and natural. One or two sentences per turn.
- For bookings, capture: party size, date, time, customer name, phone number. Ask one question at a time.
- For menu/allergen questions, do NOT make up answers. Say: "I'll check with the kitchen and give you a call back to confirm."
- For anything outside your scope, say: "I'll have the manager call you back. What's the best number to reach you on?"
- Never invent hours, menu items, or bookings.
- Today is 2026-06-06 (Saturday). Tomorrow is 2026-06-07 (Sunday).
- You may use the availability.check and manager.escalate tools when appropriate.`

var tools = []map[string]interface{}{
	{
		"type": "function",
		"function": map[string]interface{}{
			"name":        "availability.check",
			"description": "Check table availability for a given date, time, and party size",
			"parameters": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"date":       map[string]interface{}{"type": "string", "description": "ISO date YYYY-MM-DD"},
					"party_size": map[string]interface{}{"type": "integer"},
					"time":       map[string]interface{}{"type": "string", "description": "24h HH:MM or 12h h:mmam/pm"},
				},
				"required": []string{"date", "party_size", "time"},
			},
		},
	},
	{
		"type": "function",
		"function": map[string]interface{}{
			"name":        "manager.escalate",
			"description": "Escalate a call to the manager (callback request, complaint, special request)",
			"parameters": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"reason": map[string]interface{}{"type": "string"},
					"name":   map[string]interface{}{"type": "string"},
					"phone":  map[string]interface{}{"type": "string"},
					"notes":  map[string]interface{}{"type": "string"},
				},
				"required": []string{"reason", "phone"},
			},
		},
	},
	{
		"type": "function",
		"function": map[string]interface{}{
			"name":        "booking.create",
			"description": "Create a confirmed booking (only after all 5 fields captured)",
			"parameters": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"party_size": map[string]interface{}{"type": "integer"},
					"date":       map[string]interface{}{"type": "string"},
					"time":       map[string]interface{}{"type": "string"},
					"name":       map[string]interface{}{"type": "string"},
					"phone":      map[string]interface{}{"type": "string"},
					"notes":      map[string]interface{}{"type": "string"},
				},
				"required": []string{"party_size", "date", "time", "name", "phone"},
			},
		},
	},
}

type testCase struct {
	utterance    string
	expectedTool string // empty if no tool expected
	expectedKeys []string // required keys in tool args
	passText     string // text-based pass criteria (substring match)
}

var testCases = []testCase{
	// 9 base utterances
	{"Hello, can you hear me?", "", nil, "yes"},
	{"Can I book a table?", "", nil, "happy to help"},
	{"Do you have outdoor seating tomorrow?", "", nil, ""},
	{"Can I book for four tomorrow at seven?", "", nil, ""},
	{"Actually make that six, not four.", "", nil, ""},
	{"Can I speak to the manager?", "manager.escalate", []string{"phone"}, ""},
	{"What time do you close?", "", nil, ""},
	{"My phone number is 07917 715734.", "", nil, ""},
	{"Can you repeat that please?", "", nil, ""},
	// 6 edge cases
	{"I spoke to the manager yesterday and he was meant to call me back.", "manager.escalate", []string{"phone"}, ""},
	{"Do you have gluten-free options?", "", nil, ""},
	{"Can I book outside if the weather is nice?", "availability.check", []string{"date", "party_size", "time"}, ""},
	{"Change the booking from four to six.", "", nil, ""},
	{"My number is 07917 715734, but call me after 5.", "", nil, "after 5"},
	{"Actually, never mind, can someone call me back?", "manager.escalate", []string{"phone"}, ""},
}

type chatResult struct {
	utterance     string
	assistantText string
	toolName      string
	toolArgs      map[string]interface{}
	latencyMs     int64
	promptTokens  int
	complTokens   int
	costUSD       float64
	err           error
}

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

	fmt.Println("=== xAI LLM standalone test (grok-4.20-non-reasoning) ===")
	fmt.Printf("time: %s\n", time.Now().UTC().Format(time.RFC3339))
	fmt.Printf("system prompt: %d chars\n", len(systemPrompt))
	fmt.Printf("tools: %d\n", len(tools))
	fmt.Printf("test cases: %d\n\n", len(testCases))

	results := make([]chatResult, 0, len(testCases))
	for i, tc := range testCases {
		fmt.Printf("--- Test %d/%d: %q ---\n", i+1, len(testCases), tc.utterance)
		r := runOne(apiKey, tc)
		results = append(results, r)
		if r.err != nil {
			fmt.Printf("  ERROR: %v\n\n", r.err)
			continue
		}
		fmt.Printf("  text:     %s\n", truncate(r.assistantText, 200))
		if r.toolName != "" {
			fmt.Printf("  tool:     %s(%v)\n", r.toolName, r.toolArgs)
		}
		fmt.Printf("  latency:  %dms\n", r.latencyMs)
		fmt.Printf("  tokens:   %d in / %d out\n", r.promptTokens, r.complTokens)
		fmt.Printf("  cost:     $%.6f\n\n", r.costUSD)
	}

	// Summary
	fmt.Println("=== Summary ===")
	passes, fails := 0, 0
	for i, r := range results {
		tc := testCases[i]
		ok := true
		reasons := []string{}
		if r.err != nil {
			ok = false
			reasons = append(reasons, "error")
		} else {
			if tc.expectedTool != "" && r.toolName != tc.expectedTool {
				ok = false
				reasons = append(reasons, fmt.Sprintf("expected tool %q, got %q", tc.expectedTool, r.toolName))
			}
			if tc.expectedTool != "" && r.toolName == tc.expectedTool {
				for _, k := range tc.expectedKeys {
					if _, has := r.toolArgs[k]; !has {
						ok = false
						reasons = append(reasons, fmt.Sprintf("missing tool arg %q", k))
					}
				}
			}
			if tc.passText != "" && !strings.Contains(strings.ToLower(r.assistantText), strings.ToLower(tc.passText)) {
				ok = false
				reasons = append(reasons, fmt.Sprintf("text missing %q", tc.passText))
			}
		}
		status := "PASS"
		if !ok {
			status = "FAIL"
			fails++
		} else {
			passes++
		}
		fmt.Printf("  [%s] %q — %s", status, tc.utterance, strings.Join(reasons, ", "))
		if r.toolName != "" {
			fmt.Printf(" (tool=%s)", r.toolName)
		}
		fmt.Println()
	}
	fmt.Printf("\nTotal: %d/%d passed (%d failed)\n", passes, passes+fails, fails)
}

func runOne(apiKey string, tc testCase) chatResult {
	r := chatResult{utterance: tc.utterance}
	body := map[string]interface{}{
		"model": "grok-4.20-0309-non-reasoning",
		"messages": []map[string]string{
			{"role": "system", "content": systemPrompt},
			{"role": "user", "content": tc.utterance},
		},
		"tools": tools,
	}
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest("POST", "https://api.x.ai/v1/chat/completions", bytes.NewReader(b))
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	t0 := time.Now()
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		r.err = err
		return r
	}
	defer resp.Body.Close()
	rb, _ := io.ReadAll(resp.Body)
	r.latencyMs = time.Since(t0).Milliseconds()

	if resp.StatusCode != 200 {
		r.err = fmt.Errorf("status %d: %s", resp.StatusCode, string(rb))
		return r
	}

	var parsed struct {
		Choices []struct {
			Message struct {
				Content   string `json:"content"`
				ToolCalls []struct {
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			CostInUsdTicks   int `json:"cost_in_usd_ticks"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(rb, &parsed); err != nil {
		r.err = fmt.Errorf("parse: %w (raw=%s)", err, truncate(string(rb), 200))
		return r
	}

	if len(parsed.Choices) > 0 {
		msg := parsed.Choices[0].Message
		r.assistantText = msg.Content
		if len(msg.ToolCalls) > 0 {
			r.toolName = msg.ToolCalls[0].Function.Name
			var args map[string]interface{}
			if err := json.Unmarshal([]byte(msg.ToolCalls[0].Function.Arguments), &args); err != nil {
				r.err = fmt.Errorf("parse tool args: %w", err)
				return r
			}
			r.toolArgs = args
		}
	}
	r.promptTokens = parsed.Usage.PromptTokens
	r.complTokens = parsed.Usage.CompletionTokens
	r.costUSD = float64(parsed.Usage.CostInUsdTicks) / 1e9 // xAI returns cost in 1e-9 USD ticks per the field name
	return r
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
