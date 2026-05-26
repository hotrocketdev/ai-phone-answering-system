// Package openai implements the llm.Session interface for OpenAI Realtime API.
// Wraps the existing openai.Session (speech-to-speech path).
package openai

import (
	"context"

	"github.com/voxlane/voice-gateway/internal/llm"
	existing "github.com/voxlane/voice-gateway/internal/openai"
)

// Adapter wraps the existing OpenAI client behind the llm.Session interface.
type Adapter struct {
	sess   *existing.Session
	cfg    llm.Config
}

// New creates an OpenAI LLM session adapter.
func New(ctx context.Context, cfg llm.Config) (*Adapter, error) {
	oaCfg := existing.Config{
		APIKey:       cfg.APIKey,
		Model:        cfg.Model,
		Voice:        cfg.Voice,
		Instructions: cfg.Instructions,
		Tools:        convertTools(cfg.Tools),
	}

	sess, err := existing.NewSession(ctx, oaCfg)
	if err != nil {
		return nil, err
	}

	return &Adapter{sess: sess, cfg: cfg}, nil
}

func (a *Adapter) Start(ctx context.Context) error               { return a.sess.Start(ctx) }
func (a *Adapter) ReadLoop()                                       { a.sess.ReadLoop() }
func (a *Adapter) SendAudio(b64 string) error                      { return a.sess.SendAudio(b64) }
func (a *Adapter) CancelResponse() error                           { return a.sess.CancelResponse() }
func (a *Adapter) FeedToolResult(callID, output string) error      { return a.sess.FeedToolResult(callID, output) }
func (a *Adapter) CreateResponse() error                           { return a.sess.CreateResponse() }
func (a *Adapter) Close() error                                    { return a.sess.Close() }
func (a *Adapter) IsClosed() bool                                  { return a.sess.IsClosed() }
func (a *Adapter) Provider() llm.Provider                          { return llm.ProviderOpenAI }

func (a *Adapter) AudioOut() chan []byte { return a.sess.AudioOut }

func (a *Adapter) Events() chan llm.Event {
	ch := make(chan llm.Event, 16)
	go func() {
		defer close(ch)
		for evt := range a.sess.Events {
			ch <- llm.Event{
				Type: llm.EventType(evt.Type),
				Data: evt.Data,
			}
		}
	}()
	return ch
}

func (a *Adapter) Done() chan struct{} { return a.sess.Done }

func convertTools(tools []llm.Tool) []existing.Tool {
	result := make([]existing.Tool, len(tools))
	for i, t := range tools {
		result[i] = existing.Tool{
			Name:        t.Name,
			Description: t.Description,
			Parameters:  t.Parameters,
		}
	}
	return result
}
