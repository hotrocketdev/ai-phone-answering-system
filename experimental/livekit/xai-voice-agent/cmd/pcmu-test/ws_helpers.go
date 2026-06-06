// WebSocket helpers shared by cmd/probe, cmd/llm-eval, cmd/pcmu-test.
// This file is excluded from the main xai-voice-agent binary via the
// //go:build ignore tag. Build with `go build file.go` or `go run file.go`.
package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

func newWebSocketDialer() *websocket.Dialer {
	return &websocket.Dialer{
		HandshakeTimeout: 15 * time.Second,
		EnableCompression: false,
	}
}

func httpHeaderWithBearer(token string) http.Header {
	h := http.Header{}
	h.Set("Authorization", "Bearer "+token)
	return h
}

func readOneEvent(conn *websocket.Conn, wantType string) error {
	_, data, err := conn.ReadMessage()
	if err != nil {
		return fmt.Errorf("read %s: %w", wantType, err)
	}
	var ev map[string]any
	if err := json.Unmarshal(data, &ev); err != nil {
		return fmt.Errorf("parse: %w (raw=%s)", err, string(data))
	}
	got, _ := ev["type"].(string)
	if got != wantType {
		return fmt.Errorf("expected %s, got %s (raw=%s)", wantType, got, string(data))
	}
	return nil
}

func writeJSON(conn *websocket.Conn, v any) error {
	return conn.WriteJSON(v)
}

func jsonUnmarshal(data []byte, v any) error {
	return json.Unmarshal(data, v)
}

func base64StdEncoding(b []byte) string {
	return base64.StdEncoding.EncodeToString(b)
}

// readBody is a generic helper for HTTP responses.
func readBody(r io.Reader) string {
	b, _ := io.ReadAll(r)
	return string(b)
}

// drainHTTP reads and discards the body.
func drainHTTP(resp *http.Response) {
	if resp == nil || resp.Body == nil {
		return
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
}

// postJSONBody is a small helper to POST JSON to an xAI endpoint.
func postJSONBody(url, token string, body any) (int, []byte, error) {
	buf, _ := json.Marshal(body)
	req, _ := http.NewRequest("POST", url, bytes.NewReader(buf))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b, nil
}
