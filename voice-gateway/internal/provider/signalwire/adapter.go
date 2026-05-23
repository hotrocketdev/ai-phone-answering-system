// Package signalwire implements the provider.Adapter for SignalWire.
// STATUS: Placeholder — not implemented.
package signalwire

import (
	"context"
	"fmt"

	"github.com/voxlane/voice-gateway/internal/provider"
)

// Adapter implements provider.Adapter for SignalWire.
type Adapter struct {
	cfg    provider.SignalWireConfig
	callID string
}

// New creates a SignalWire adapter (placeholder).
func New(callID string, cfg provider.SignalWireConfig) *Adapter {
	return &Adapter{cfg: cfg, callID: callID}
}

func (a *Adapter) Type() provider.Type { return provider.TypeSignalWire }

func (a *Adapter) ValidateRequest(_ context.Context, _ map[string]string, _ []byte) (string, error) {
	return "", fmt.Errorf("signalwire: not implemented — provider is a placeholder")
}

func (a *Adapter) GenerateCallControl(_ string, _ provider.CallControlResponse) ([]byte, string, error) {
	return nil, "", fmt.Errorf("signalwire: not implemented")
}

func (a *Adapter) ParseMediaEvent(_ []byte) (*provider.AudioFrame, *provider.Event) {
	return nil, &provider.Event{Type: provider.EventError,
		Error: fmt.Errorf("signalwire: not implemented")}
}

func (a *Adapter) EncodeAudio(_ provider.AudioFrame) ([]byte, error) {
	return nil, fmt.Errorf("signalwire: not implemented")
}

func (a *Adapter) EncodeMark(_ string) ([]byte, error) { return nil, fmt.Errorf("signalwire: not implemented") }
func (a *Adapter) CloseMessage() []byte                 { return nil }
func (a *Adapter) CallID() string                        { return a.callID }
func (a *Adapter) StreamID() string                      { return "" }
func (a *Adapter) Close() error                          { return nil }
