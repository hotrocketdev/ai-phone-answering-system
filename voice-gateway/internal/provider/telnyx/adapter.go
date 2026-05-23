// Package telnyx implements the provider.Adapter for Telnyx Voice.
// STATUS: Scaffold — not yet tested with a real Telnyx account.
package telnyx

import (
	"context"
	"fmt"

	"github.com/voxlane/voice-gateway/internal/provider"
)

// Adapter implements provider.Adapter for Telnyx.
type Adapter struct {
	cfg    provider.TelnyxConfig
	callID string
	// stream provider.StreamHandler — to be connected when WebSocket is established
}

// New creates a Telnyx adapter.
func New(callID string, cfg provider.TelnyxConfig) *Adapter {
	return &Adapter{
		cfg:    cfg,
		callID: callID,
	}
}

func (a *Adapter) Type() provider.Type { return provider.TypeTelnyx }

func (a *Adapter) ValidateRequest(_ context.Context, _ map[string]string, _ []byte) (string, error) {
	return "", fmt.Errorf("telnyx: ValidateRequest not yet implemented")
}

func (a *Adapter) GenerateCallControl(_ string, ctrl provider.CallControlResponse) ([]byte, string, error) {
	// Telnyx uses JSON call control, not XML
	// https://developers.telnyx.com/docs/api/v2/call-control/Call-Commands
	body := fmt.Sprintf(`{
  "stream_url": "%s",
  "stream_track": "both_tracks",
  "client_state": "%s"
}`, ctrl.StreamURL, a.callID)
	return []byte(body), "application/json", nil
}

func (a *Adapter) ParseMediaEvent(_ []byte) (*provider.AudioFrame, *provider.Event) {
	return nil, &provider.Event{Type: provider.EventError,
		Error: fmt.Errorf("telnyx: ParseMediaEvent not yet implemented")}
}

func (a *Adapter) EncodeAudio(_ provider.AudioFrame) ([]byte, error) {
	return nil, fmt.Errorf("telnyx: EncodeAudio not yet implemented")
}

func (a *Adapter) EncodeMark(_ string) ([]byte, error) {
	return nil, fmt.Errorf("telnyx: EncodeMark not yet implemented")
}

func (a *Adapter) CloseMessage() []byte { return nil }

func (a *Adapter) CallID() string  { return a.callID }
func (a *Adapter) StreamID() string { return "" }
func (a *Adapter) Close() error    { return nil }
