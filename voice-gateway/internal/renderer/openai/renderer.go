// Package openai implements the renderer.Renderer interface using OpenAI's native voice.
// This is the current speech-to-speech path where voice rendering happens inside the LLM.
package openai

import (
	"context"
	"fmt"

	"github.com/voxlane/voice-gateway/internal/renderer"
)

// Renderer represents the OpenAI native voice rendering path.
// In speech-to-speech mode, the voice is rendered server-side by the LLM.
type Renderer struct {
	cfg renderer.Config
}

func New(cfg renderer.Config) *Renderer {
	return &Renderer{cfg: cfg}
}

func (r *Renderer) Render(_ context.Context, _ string) ([]byte, error) {
	return nil, fmt.Errorf("openai renderer: text-to-speech not supported — use speech-to-speech mode")
}

func (r *Renderer) RenderStream(_ context.Context, _ string) (<-chan []byte, error) {
	return nil, fmt.Errorf("openai renderer: text streaming not supported — use speech-to-speech mode")
}

func (r *Renderer) Provider() renderer.Provider { return renderer.ProviderOpenAI }
func (r *Renderer) Close() error                { return nil }
