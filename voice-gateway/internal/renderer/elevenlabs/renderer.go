// Package elevenlabs implements the renderer.Renderer interface for ElevenLabs TTS.
// STATUS: Scaffold — future premium voice tier with cloning support.
package elevenlabs

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
	return nil, fmt.Errorf("elevenlabs: not yet integrated")
}

func (r *Renderer) RenderStream(_ context.Context, _ string) (<-chan []byte, error) {
	return nil, fmt.Errorf("elevenlabs: not yet integrated")
}

func (r *Renderer) Provider() renderer.Provider { return renderer.ProviderElevenLabs }
func (r *Renderer) Close() error                { return nil }
