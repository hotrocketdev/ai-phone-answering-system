// Package cartesia implements the renderer.Renderer interface for Cartesia Sonic.
// STATUS: Scaffold — not yet integrated with real API.
// Cartesia provides low-latency British TTS with streaming support.
package cartesia

import (
	"context"
	"fmt"

	"github.com/voxlane/voice-gateway/internal/renderer"
)

type Renderer struct {
	cfg renderer.Config
}

func New(cfg renderer.Config) *Renderer {
	return &Renderer{cfg: cfg}
}

func (r *Renderer) Render(_ context.Context, _ string) ([]byte, error) {
	return nil, fmt.Errorf("cartesia: not yet integrated")
}

func (r *Renderer) RenderStream(_ context.Context, _ string) (<-chan []byte, error) {
	return nil, fmt.Errorf("cartesia: not yet integrated")
}

func (r *Renderer) Provider() renderer.Provider { return renderer.ProviderCartesia }
func (r *Renderer) Close() error                { return nil }
