// OpenAI gpt-4o-mini LLM client (non-streaming).
//
// Endpoint: POST https://api.openai.com/v1/chat/completions
// Auth: Authorization: Bearer <key>
//
// The spike uses a short receptionist system prompt and keeps a
// rolling history of the last N exchanges for multi-turn context.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// chatMessage is one turn in the conversation history.
type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// receptionistSystemPrompt is the spike's system prompt. It tells
// the model to act as Alex, a Porto Douro receptionist, and to keep
// replies brief (phone-friendly). It also forbids tools/booking:
// this is a spike, not production.
const receptionistSystemPrompt = `You are Alex, a friendly receptionist at Porto Douro Restaurants. \
You are answering a phone call from a real customer. \
Greet the caller once at the start, ask how you can help, and respond briefly. \
Keep replies under 2 short sentences; this is a phone call. \
Do not use bullet points, lists, or markdown. \
Do not invent menu items, prices, opening hours, or reservation details. \
If asked something you do not know, say you will check and someone will get back to them.`

// CompleteLLM calls OpenAI chat completions (gpt-4o-mini) with the
// receptionist system prompt and the rolling history, then appends
// the latest user turn and returns the assistant's reply text.
func CompleteLLM(ctx context.Context, apiKey string, history []chatMessage, userTurn string) (string, error) {
	if apiKey == "" {
		return "", fmt.Errorf("llm: empty API key")
	}

	messages := make([]chatMessage, 0, len(history)+2)
	messages = append(messages, chatMessage{Role: "system", Content: receptionistSystemPrompt})
	messages = append(messages, history...)
	messages = append(messages, chatMessage{Role: "user", Content: userTurn})

	body := map[string]interface{}{
		"model":       "gpt-4o-mini",
		"messages":    messages,
		"temperature": 0.5,
		"max_tokens":  60,
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("llm: marshal: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.openai.com/v1/chat/completions", bytes.NewReader(raw))
	if err != nil {
		return "", fmt.Errorf("llm: new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	cli := &http.Client{Timeout: 20 * time.Second}
	resp, err := cli.Do(req)
	if err != nil {
		return "", fmt.Errorf("llm: http: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("llm: http %d: %s", resp.StatusCode, string(respBody))
	}

	var out struct {
		Choices []struct {
			Message chatMessage `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(respBody, &out); err != nil {
		return "", fmt.Errorf("llm: decode json: %w (body=%q)", err, string(respBody))
	}
	if len(out.Choices) == 0 {
		return "", fmt.Errorf("llm: empty choices (body=%q)", string(respBody))
	}
	reply := out.Choices[0].Message.Content
	if reply == "" {
		return "", fmt.Errorf("llm: empty assistant content")
	}
	return reply, nil
}
