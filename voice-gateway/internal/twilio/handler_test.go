package twilio

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
)

func TestHandler_MediaEvent(t *testing.T) {
	// Set up test WebSocket server
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("upgrade failed: %v", err)
		}
		defer conn.Close()

		handler := NewHandler(conn, "CA_TEST_001")
		go handler.ReadLoop()

		// Wait for audio
		select {
		case audio := <-handler.AudioIn:
			if len(audio) != 160 {
				t.Errorf("expected 160 bytes of u-law audio, got %d", len(audio))
			}
		}
	}))
	defer server.Close()

	// Connect to test server
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer conn.Close()

	// Send a media event with test audio
	audio := make([]byte, 160)
	for i := range audio {
		audio[i] = 0xFF // silence
	}
	payload := base64.StdEncoding.EncodeToString(audio)

	msg := `{"event":"media","media":{"track":"inbound","payload":"` + payload + `"},"streamSid":"MZ_TEST"}`

	if err := conn.WriteMessage(websocket.TextMessage, []byte(msg)); err != nil {
		t.Fatalf("write failed: %v", err)
	}
}

func TestHandler_ConnectedEvent(t *testing.T) {
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, _ := upgrader.Upgrade(w, r, nil)
		defer conn.Close()

		handler := NewHandler(conn, "CA_TEST_002")
		go handler.ReadLoop()

		evt := <-handler.Events
		if evt.Type != "connected" {
			t.Errorf("expected 'connected' event, got '%s'", evt.Type)
		}
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, _, _ := websocket.DefaultDialer.Dial(wsURL, nil)
	defer conn.Close()

	conn.WriteMessage(websocket.TextMessage, []byte(`{"event":"connected"}`))
}

func TestHandler_StopEvent(t *testing.T) {
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, _ := upgrader.Upgrade(w, r, nil)
		defer conn.Close()

		handler := NewHandler(conn, "CA_TEST_003")
		go handler.ReadLoop()

		evt := <-handler.Events
		if evt.Type != "stopped" {
			t.Errorf("expected 'stopped' event, got '%s'", evt.Type)
		}
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, _, _ := websocket.DefaultDialer.Dial(wsURL, nil)
	defer conn.Close()

	conn.WriteMessage(websocket.TextMessage, []byte(`{"event":"stop","stop":{"callSid":"CA_TEST_003","accountSid":"AC_TEST"}}`))
}

func TestHandler_IgnoresOutboundTrack(t *testing.T) {
	upgrader := websocket.Upgrader{}
	done := make(chan struct{})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, _ := upgrader.Upgrade(w, r, nil)
		defer conn.Close()

		handler := NewHandler(conn, "CA_TEST_004")
		go handler.ReadLoop()

		// Wait for connected event after outbound media is filtered
		evt := <-handler.Events
		if evt.Type != "connected" {
			t.Errorf("expected 'connected' after outbound media, got '%s'", evt.Type)
		}
		close(done)
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, _, _ := websocket.DefaultDialer.Dial(wsURL, nil)
	defer conn.Close()

	// Send outbound media — should be ignored by handler
	conn.WriteMessage(websocket.TextMessage, []byte(`{"event":"media","media":{"track":"outbound","payload":"AAAA"},"streamSid":"MZ_TEST"}`))
	// Send connected event
	conn.WriteMessage(websocket.TextMessage, []byte(`{"event":"connected"}`))

	<-done
}

func TestHandler_SendAudio(t *testing.T) {
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, _ := upgrader.Upgrade(w, r, nil)
		defer conn.Close()

		handler := NewHandler(conn, "CA_TEST_005")
		// Don't start ReadLoop — we're testing outbound

		audio := make([]byte, 160)
		err := handler.SendAudio(audio)
		if err != nil {
			t.Errorf("SendAudio failed: %v", err)
		}
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, _, _ := websocket.DefaultDialer.Dial(wsURL, nil)
	defer conn.Close()

	// Read the outbound message from server
	_, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}

	if !strings.Contains(string(msg), `"event":"media"`) {
		t.Errorf("expected media event, got: %s", string(msg))
	}
	if !strings.Contains(string(msg), `"track":"outbound"`) {
		t.Errorf("expected outbound track: %s", string(msg))
	}
}

func TestHandler_DisconnectDetection(t *testing.T) {
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, _ := upgrader.Upgrade(w, r, nil)

		handler := NewHandler(conn, "CA_TEST_006")
		go handler.ReadLoop()

		evt := <-handler.Events
		if evt.Type != "disconnected" {
			t.Errorf("expected 'disconnected' event, got '%s'", evt.Type)
		}
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, _, _ := websocket.DefaultDialer.Dial(wsURL, nil)

	// Close without proper close frame — triggers read error
	conn.Close()
}

func TestHandler_DropsFrameOnFullChannel(t *testing.T) {
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, _ := upgrader.Upgrade(w, r, nil)
		defer conn.Close()

		// Create handler with unbuffered channel (capacity 0 means synchronous)
		h := &Handler{
			conn:    conn,
			callSid: "CA_TEST_007",
			AudioIn: make(chan []byte, 1), // buffer of 1
			Events:  make(chan HandlerEvent, 1),
		}

		// Fill the buffer
		h.AudioIn <- make([]byte, 160)

		// Now process a media event — should drop frame since channel is full
		h.handleMessage([]byte(`{"event":"media","media":{"track":"inbound","payload":"` +
			base64.StdEncoding.EncodeToString(make([]byte, 160)) + `"}}`))

		// Verify channel still has exactly 1 item (the original, not the new one)
		if len(h.AudioIn) != 1 {
			t.Errorf("expected 1 buffered frame, got %d", len(h.AudioIn))
		}
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, _, _ := websocket.DefaultDialer.Dial(wsURL, nil)
	defer conn.Close()
}

func TestHandler_InvalidJSON(t *testing.T) {
	upgrader := websocket.Upgrader{}
	done := make(chan struct{})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, _ := upgrader.Upgrade(w, r, nil)
		defer conn.Close()

		handler := NewHandler(conn, "CA_TEST_008")
		go handler.ReadLoop()

		// Wait for connected event (after we send valid JSON on client)
		evt := <-handler.Events
		if evt.Type != "connected" {
			t.Errorf("expected 'connected' after invalid JSON, got '%s'", evt.Type)
		}
		close(done)
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, _, _ := websocket.DefaultDialer.Dial(wsURL, nil)
	defer conn.Close()

	// Send invalid JSON — handler should ignore it
	conn.WriteMessage(websocket.TextMessage, []byte(`{invalid`))
	// Send valid event after
	conn.WriteMessage(websocket.TextMessage, []byte(`{"event":"connected"}`))

	<-done // wait for server to process
}

func TestHandler_MarkEvent(t *testing.T) {
	upgrader := websocket.Upgrader{}
	done := make(chan struct{})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, _ := upgrader.Upgrade(w, r, nil)
		defer conn.Close()

		handler := NewHandler(conn, "CA_TEST_009")
		go handler.ReadLoop()

		evt := <-handler.Events
		if evt.Type != "mark" {
			t.Errorf("expected 'mark' event, got '%s'", evt.Type)
		}
		if evt.Label != "turn-complete" {
			t.Errorf("expected label 'turn-complete', got '%s'", evt.Label)
		}
		close(done)
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, _, _ := websocket.DefaultDialer.Dial(wsURL, nil)
	defer conn.Close()

	conn.WriteMessage(websocket.TextMessage, []byte(`{"event":"mark","mark":{"name":"turn-complete"},"streamSid":"MZ_TEST"}`))
	<-done
}
