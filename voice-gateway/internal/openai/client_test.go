package openai

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
)

func TestParseFunctionCall(t *testing.T) {
	raw := json.RawMessage(`{
		"type": "response.function_call_arguments.done",
		"call_id": "call_abc123",
		"name": "check_availability",
		"arguments": "{\"date\":\"2026-05-22\",\"time\":\"19:00\",\"partySize\":4}"
	}`)

	callID, name, args, err := ParseFunctionCall(raw)
	if err != nil {
		t.Fatalf("ParseFunctionCall failed: %v", err)
	}
	if callID != "call_abc123" {
		t.Errorf("expected call_id 'call_abc123', got '%s'", callID)
	}
	if name != "check_availability" {
		t.Errorf("expected name 'check_availability', got '%s'", name)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(args, &parsed); err != nil {
		t.Fatalf("failed to parse arguments: %v", err)
	}
	if parsed["date"] != "2026-05-22" {
		t.Errorf("expected date '2026-05-22', got '%v'", parsed["date"])
	}
	if parsed["partySize"] != float64(4) {
		t.Errorf("expected partySize 4, got %v", parsed["partySize"])
	}
}

func TestParseResponseDone(t *testing.T) {
	raw := json.RawMessage(`{
		"type": "response.done",
		"response": {
			"status": "completed"
		}
	}`)

	status, err := ParseResponseDone(raw)
	if err != nil {
		t.Fatalf("ParseResponseDone failed: %v", err)
	}
	if status != "completed" {
		t.Errorf("expected status 'completed', got '%s'", status)
	}
}

func TestParseResponseDone_Cancelled(t *testing.T) {
	raw := json.RawMessage(`{
		"type": "response.done",
		"response": {
			"status": "cancelled"
		}
	}`)

	status, err := ParseResponseDone(raw)
	if err != nil {
		t.Fatalf("ParseResponseDone failed: %v", err)
	}
	if status != "cancelled" {
		t.Errorf("expected status 'cancelled', got '%s'", status)
	}
}

func TestBuildGreetingPrompt(t *testing.T) {
	prompt := BuildGreetingPrompt("Bella Roma")
	if !strings.Contains(prompt, "Bella Roma") {
		t.Error("prompt should contain restaurant name")
	}
	if !strings.Contains(prompt, "warm") {
		t.Error("prompt should contain tone instructions")
	}
	if !strings.Contains(prompt, "create_booking") {
		t.Error("prompt should contain anti-hallucination rule")
	}
}

func TestToolIncludesRealtimeFunctionType(t *testing.T) {
	tool := Tool{
		Type:        "function",
		Name:        "check_availability",
		Description: "Check availability",
		Parameters:  map[string]interface{}{"type": "object"},
	}

	raw, err := json.Marshal(tool)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if !strings.Contains(string(raw), `"type":"function"`) {
		t.Fatalf("tool JSON missing function type: %s", raw)
	}
}

func TestSessionUpdateEnablesInputTranscription(t *testing.T) {
	s := &Session{config: Config{Model: "gpt-realtime-1.5", Instructions: "Test"}}
	raw, err := json.Marshal(s.sessionUpdateMessage())
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	text := string(raw)
	if !strings.Contains(text, `"transcription":{"model":"gpt-4o-mini-transcribe"}`) {
		t.Fatalf("session.update missing input transcription config: %s", text)
	}
}

// Mock OpenAI Realtime server for testing the WebSocket client
func setupMockOpenAIServer(t *testing.T) (*httptest.Server, string) {
	t.Helper()

	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Logf("mock upgrade failed: %v", err)
			return
		}
		defer conn.Close()

		// 1. Send session.created
		sessionCreated := map[string]interface{}{
			"type": "session.created",
			"session": map[string]interface{}{
				"id": "sess_mock_001",
			},
		}
		conn.WriteJSON(sessionCreated)

		// 2. Read session.update
		_, raw, err := conn.ReadMessage()
		if err != nil {
			return
		}
		var update struct {
			Type string `json:"type"`
		}
		json.Unmarshal(raw, &update)
		if update.Type != "session.update" {
			t.Logf("expected session.update, got %s", update.Type)
		}

		// 3. Send session.updated
		sessionUpdated := map[string]interface{}{
			"type": "session.updated",
		}
		conn.WriteJSON(sessionUpdated)

		// 4. Echo loop: wait for audio, send audio back
		for {
			_, raw, err := conn.ReadMessage()
			if err != nil {
				return
			}

			var base struct {
				Type string `json:"type"`
			}
			json.Unmarshal(raw, &base)

			switch base.Type {
			case "input_audio_buffer.append":
				// Echo back as audio delta
				var append struct {
					Audio string `json:"audio"`
				}
				json.Unmarshal(raw, &append)

				delta := map[string]interface{}{
					"type":  "response.audio.delta",
					"delta": append.Audio,
				}
				conn.WriteJSON(delta)

				// Send audio.done
				conn.WriteJSON(map[string]interface{}{
					"type": "response.audio.done",
				})

				// Send response.done with completed status
				conn.WriteJSON(map[string]interface{}{
					"type": "response.done",
					"response": map[string]interface{}{
						"status": "completed",
					},
				})

			case "response.cancel":
				conn.WriteJSON(map[string]interface{}{
					"type": "response.done",
					"response": map[string]interface{}{
						"status": "cancelled",
					},
				})
			}
		}
	}))

	return server, "ws" + strings.TrimPrefix(server.URL, "http")
}

func TestSession_StartAndStream(t *testing.T) {
	server, wsURL := setupMockOpenAIServer(t)
	defer server.Close()

	// Create session using raw WebSocket dial (bypass NewSession for mock URL)
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial mock server: %v", err)
	}

	s := &Session{
		conn:     conn,
		config:   Config{Voice: "alloy", Instructions: "Test prompt"},
		AudioOut: make(chan []byte, 8),
		Events:   make(chan Event, 16),
		Done:     make(chan struct{}),
	}

	// Start session
	if err := s.Start(t.Context()); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Start ReadLoop
	go s.ReadLoop()

	// Send test audio
	testAudio := make([]byte, 960) // PCM16 24kHz frame
	b64 := base64.StdEncoding.EncodeToString(testAudio)
	if err := s.SendAudio(b64); err != nil {
		t.Fatalf("SendAudio failed: %v", err)
	}

	// Wait for audio echo
	select {
	case audio := <-s.AudioOut:
		if len(audio) == 0 {
			t.Error("expected non-empty audio")
		}
	}

	// Drain audio.done event (sent before response.done by mock)
	<-s.Events

	// Wait for response.done
	select {
	case evt := <-s.Events:
		if evt.Type != "response.done" {
			t.Errorf("expected response.done, got %s", evt.Type)
		}
	}
}

func TestSession_Cancel(t *testing.T) {
	server, wsURL := setupMockOpenAIServer(t)
	defer server.Close()

	conn, _, _ := websocket.DefaultDialer.Dial(wsURL, nil)

	s := &Session{
		conn:     conn,
		config:   Config{Voice: "alloy"},
		AudioOut: make(chan []byte, 8),
		Events:   make(chan Event, 16),
		Done:     make(chan struct{}),
	}

	if err := s.Start(t.Context()); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	go s.ReadLoop()

	// Send cancel
	if err := s.CancelResponse(); err != nil {
		t.Fatalf("CancelResponse failed: %v", err)
	}

	// Wait for cancelled response.done
	select {
	case evt := <-s.Events:
		if evt.Type != "response.done" {
			t.Errorf("expected response.done, got %s", evt.Type)
		}
	}
}

func TestSession_SpeechEvents(t *testing.T) {
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, _ := upgrader.Upgrade(w, r, nil)
		defer conn.Close()

		// Session.created
		conn.WriteJSON(map[string]interface{}{
			"type":    "session.created",
			"session": map[string]interface{}{"id": "sess_test"},
		})

		// Wait for session.update
		conn.ReadMessage()

		// Session.updated
		conn.WriteJSON(map[string]interface{}{"type": "session.updated"})

		// Send speech_started
		conn.WriteJSON(map[string]interface{}{"type": "input_audio_buffer.speech_started"})

		// Send speech_stopped
		conn.WriteJSON(map[string]interface{}{"type": "input_audio_buffer.speech_stopped"})
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, _, _ := websocket.DefaultDialer.Dial(wsURL, nil)

	s := &Session{
		conn:     conn,
		config:   Config{Voice: "alloy"},
		AudioOut: make(chan []byte, 8),
		Events:   make(chan Event, 16),
		Done:     make(chan struct{}),
	}

	if err := s.Start(t.Context()); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	go s.ReadLoop()

	// Check speech_started
	evt := <-s.Events
	if evt.Type != "speech_started" {
		t.Errorf("expected speech_started, got %s", evt.Type)
	}

	// Check speech_stopped
	evt = <-s.Events
	if evt.Type != "speech_stopped" {
		t.Errorf("expected speech_stopped, got %s", evt.Type)
	}
}
