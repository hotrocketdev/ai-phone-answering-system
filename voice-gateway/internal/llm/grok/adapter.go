// Package grok implements the llm.Session interface for Grok.
// STATUS: Scaffold — not yet implemented.
package grok

import (
	"context"
	"fmt"

	"github.com/voxlane/voice-gateway/internal/llm"
)

type Adapter struct{ cfg llm.Config }

func New(_ context.Context, cfg llm.Config) (*Adapter, error) {
	return &Adapter{cfg: cfg}, nil
}

func (a *Adapter) Start(_ context.Context) error {
	return fmt.Errorf("grok: not implemented")
}
func (a *Adapter) ReadLoop()                        {}
func (a *Adapter) SendAudio(_ string) error         { return fmt.Errorf("grok: not implemented") }
func (a *Adapter) CancelResponse() error            { return fmt.Errorf("grok: not implemented") }
func (a *Adapter) FeedToolResult(_, _ string) error { return fmt.Errorf("grok: not implemented") }
func (a *Adapter) CreateResponse() error            { return fmt.Errorf("grok: not implemented") }
func (a *Adapter) Close() error                     { return nil }
func (a *Adapter) IsClosed() bool                   { return false }
func (a *Adapter) Provider() llm.Provider           { return llm.ProviderGrok }
func (a *Adapter) AudioOut() chan []byte            { return nil }
func (a *Adapter) Events() chan llm.Event           { return nil }
func (a *Adapter) Done() chan struct{}              { return nil }
