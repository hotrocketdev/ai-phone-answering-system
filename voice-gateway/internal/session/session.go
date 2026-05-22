// Package session manages the full lifecycle of a voice call session.
// It orchestrates Twilio, OpenAI, audio pipeline, Redis, and tool execution.
package session

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/voxlane/voice-gateway/internal/audio"
	"github.com/voxlane/voice-gateway/internal/config"
	"github.com/voxlane/voice-gateway/internal/openai"
	goredis "github.com/voxlane/voice-gateway/internal/redis"
	"github.com/voxlane/voice-gateway/internal/session/sm"
	"github.com/voxlane/voice-gateway/internal/twilio"
)

// ─── Meta States ─────────────────────────────────────────────────────────

type MetaState string

const (
	MetaCreated      MetaState = "CREATED"
	MetaConnecting   MetaState = "CONNECTING"
	MetaActive       MetaState = "ACTIVE"
	MetaReconnecting MetaState = "RECONNECTING"
	MetaEnding       MetaState = "ENDING"
	MetaCleaningUp   MetaState = "CLEANUP"
)

// ─── Session ─────────────────────────────────────────────────────────────

type Session struct {
	ID     string // CallSid
	Config *config.Config

	mu       sync.RWMutex
	metaState MetaState
	stateMachine *sm.Machine

	// Components
	twilioH  *twilio.Handler
	openaiS  *openai.Session
	audioP   *audio.Pipeline
	redisC   *goredis.Client

	// Channels (internal)
	stopCh   chan struct{}
	doneCh   chan struct{}

	// Metrics
	inputSecs  float64
	outputSecs float64
	bargeIns   int
	toolCalls  int
	startTime  time.Time

	// Tool call state
	pendingToolCallID string
}

// ─── Creation ────────────────────────────────────────────────────────────

// NewSession creates a new call session.
func NewSession(callSid string, tw *twilio.Handler, cfg *config.Config, redisC *goredis.Client) *Session {
	return &Session{
		ID:           callSid,
		Config:       cfg,
		twilioH:      tw,
		redisC:       redisC,
		audioP:       audio.NewPipeline(),
		stateMachine: sm.New("Bella Roma"), // TODO: tenant config
		metaState:    MetaCreated,
		stopCh:       make(chan struct{}),
		doneCh:       make(chan struct{}),
		startTime:    time.Now(),
	}
}

// ─── Lifecycle ───────────────────────────────────────────────────────────

// Run executes the full session lifecycle. Blocks until the call ends.
func (s *Session) Run(ctx context.Context) error {
	// Phase 1: Connect to OpenAI
	s.setMeta(MetaConnecting)
	oaCfg := openai.Config{
		APIKey:       s.Config.OpenAIAPIKey,
		Model:        s.Config.OpenAIRealtimeModel,
		Voice:        "alloy",
		Instructions: s.stateMachine.BuildSystemPrompt(),
		Tools:        convertTools(s.stateMachine.AvailableTools()),
	}

	oaSess, err := openai.NewSession(ctx, oaCfg)
	if err != nil {
		return fmt.Errorf("openai connect: %w", err)
	}
	s.openaiS = oaSess

	if err := s.openaiS.Start(ctx); err != nil {
		return fmt.Errorf("openai start: %w", err)
	}
	s.setMeta(MetaActive)

	// Persist initial state
	s.saveState(ctx)

	// Start read loops
	go s.openaiS.ReadLoop()
	go s.runTwilioLoop(ctx)
	go s.runOpenAILoop(ctx)
	go s.runSupervisor(ctx)

	// Wait for completion
	<-s.doneCh
	return nil
}

// Stop initiates session teardown.
func (s *Session) Stop() {
	select {
	case <-s.stopCh:
		// Already stopping
	default:
		close(s.stopCh)
	}
}

// Wait blocks until the session ends.
func (s *Session) Wait() {
	<-s.doneCh
}

// ─── Internal Loops ──────────────────────────────────────────────────────

func (s *Session) runTwilioLoop(ctx context.Context) {
	for {
		select {
		case <-s.stopCh:
			return
		case audio, ok := <-s.twilioH.AudioIn:
			if !ok {
				s.handleTwilioDisconnect(ctx)
				return
			}
			// Process inbound audio: u-law → base64 PCM16 24kHz → OpenAI
			b64 := s.audioP.ProcessInbound(audio)
			s.inputSecs += 0.020 // 20ms per frame

			// Non-blocking send to OpenAI
			_ = s.openaiS.SendAudio(b64)

		case evt, ok := <-s.twilioH.Events:
			if !ok {
				return
			}
			switch evt.Type {
			case "stopped", "disconnected":
				s.handleTwilioDisconnect(ctx)
				return
			}
		}
	}
}

func (s *Session) runOpenAILoop(ctx context.Context) {
	for {
		select {
		case <-s.stopCh:
			return

		case pcm24k, ok := <-s.openaiS.AudioOut:
			if !ok {
				return
			}
			// Process outbound audio: PCM16 24kHz → u-law → Twilio
			if s.isBargingIn() {
				continue // Drop frame during barge-in
			}
			mulaw, err := s.audioP.ProcessOutboundBytes(pcm24k)
			if err != nil {
				continue
			}
			s.outputSecs += 0.020
			_ = s.twilioH.SendAudio(mulaw)

		case evt, ok := <-s.openaiS.Events:
			if !ok {
				return
			}
			s.handleOpenAIEvent(ctx, evt)
		}
	}
}

func (s *Session) runSupervisor(ctx context.Context) {
	silenceTimer := time.NewTimer(s.Config.SilencePromptDuration())
	silenceTimer.Stop()
	var prompted bool

	for {
		select {
		case <-s.stopCh:
			return

		case <-silenceTimer.C:
			if s.isAISpeaking() {
				silenceTimer.Reset(s.Config.SilencePromptDuration())
				prompted = false
				continue
			}
			if !prompted {
				log.Printf("[%s] silence timeout — prompting caller", s.ID)
				// TODO: inject system message to AI
				prompted = true
				silenceTimer.Reset(s.Config.SilenceHangupDuration())
			} else {
				log.Printf("[%s] prolonged silence — ending call", s.ID)
				s.Stop()
				return
			}
		}
	}
}

// ─── OpenAI Event Handling ───────────────────────────────────────────────

func (s *Session) handleOpenAIEvent(ctx context.Context, evt openai.Event) {
	switch evt.Type {
	case "speech_started":
		s.handleBargeIn()

	case "speech_stopped":
		// Reset silence timer

	case "function_call.done":
		s.handleToolCall(ctx, evt.Data)

	case "response.done":
		status, _ := openai.ParseResponseDone(evt.Data)
		if status == "cancelled" {
			s.clearBargeIn()
		}

	case "error":
		log.Printf("[%s] OpenAI error: %s", s.ID, string(evt.Data))
	}
}

func (s *Session) handleBargeIn() {
	s.mu.Lock()
	s.bargeIns++
	s.mu.Unlock()
	s.openaiS.CancelResponse()
}

func (s *Session) handleToolCall(ctx context.Context, raw json.RawMessage) {
	callID, name, args, err := openai.ParseFunctionCall(raw)
	if err != nil {
		log.Printf("[%s] failed to parse tool call: %v", s.ID, err)
		return
	}

	s.mu.Lock()
	s.toolCalls++
	s.pendingToolCallID = callID
	s.mu.Unlock()

	log.Printf("[%s] tool call: %s(%s)", s.ID, name, string(args))

	// Execute tool call (HTTP to NestJS)
	result, err := s.executeToolCall(ctx, name, args)
	if err != nil {
		log.Printf("[%s] tool call failed: %v", s.ID, err)
		result = `{"success":false,"error":"system error"}`
	}

	// Feed result back to OpenAI
	if err := s.openaiS.FeedToolResult(callID, result); err != nil {
		log.Printf("[%s] failed to feed tool result: %v", s.ID, err)
	}

	// Audit
	s.redisC.AppendToolCall(ctx, s.ID, goredis.ToolCallRecord{
		Tool:      name,
		Args:      args,
		Result:    json.RawMessage(result),
		Timestamp: time.Now(),
	})
}

// ─── Tool Execution ──────────────────────────────────────────────────────

func (s *Session) executeToolCall(ctx context.Context, name string, args json.RawMessage) (string, error) {
	// TODO: HTTP call to NestJS for real tool execution
	// For PoC, return fake results based on tool name
	switch name {
	case "check_availability":
		return `{"success":true,"data":{"available":true,"slots":["19:00","19:15","19:30","19:45"]}}`, nil
	case "create_booking":
		return `{"success":true,"data":{"bookingRef":"BK-FAKE-001"}}`, nil
	default:
		return fmt.Sprintf(`{"success":true,"data":{"message":"%s completed"}}`, name), nil
	}
}

// ─── Graceful Shutdown ───────────────────────────────────────────────────

func (s *Session) handleTwilioDisconnect(ctx context.Context) {
	log.Printf("[%s] Twilio disconnected, cleaning up", s.ID)
	s.setMeta(MetaEnding)

	// Close OpenAI connection
	if s.openaiS != nil {
		s.openaiS.Close()
	}

	// Persist final state
	s.saveState(ctx)

	// Remove from active set
	if s.redisC != nil {
		s.redisC.DeleteSession(ctx, s.ID)
	}

	close(s.doneCh)
}

func (s *Session) cleanup() {
	if s.twilioH != nil {
		s.twilioH.Close()
	}
	if s.openaiS != nil {
		s.openaiS.Close()
	}
}

// ─── State Helpers ───────────────────────────────────────────────────────

func (s *Session) setMeta(state MetaState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.metaState = state
}

func (s *Session) isBargingIn() bool {
	// Simple implementation: if we have a pending tool call, suppress audio briefly
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.pendingToolCallID != ""
}

func (s *Session) clearBargeIn() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pendingToolCallID = ""
}

func (s *Session) isAISpeaking() bool {
	// Simplified: AI is "speaking" if output audio was sent recently
	return s.outputSecs > 0
}

func (s *Session) getActiveTools() []openai.Tool {
	return convertTools(s.stateMachine.AvailableTools())
}

func convertTools(smTools []sm.Tool) []openai.Tool {
	tools := make([]openai.Tool, len(smTools))
	for i, t := range smTools {
		tools[i] = openai.Tool{
			Name:        t.Name,
			Description: t.Description,
			Parameters:  t.Parameters,
		}
	}
	return tools
}

func (s *Session) saveState(ctx context.Context) {
	if s.redisC == nil {
		return
	}

	state := &goredis.SessionState{
		CallSid:         s.ID,
		MetaState:       string(s.metaState),
		ConvState:       string(s.stateMachine.Current()),
		InputAudioSecs:  s.inputSecs,
		OutputAudioSecs: s.outputSecs,
		CreatedAt:       s.startTime,
		LastActivity:    time.Now(),
	}
	_ = s.redisC.SaveSession(ctx, state)
}

// ─── Metrics ─────────────────────────────────────────────────────────────

func (s *Session) Metrics() map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return map[string]interface{}{
		"callSid":    s.ID,
		"metaState":  string(s.metaState),
		"convState":  string(s.stateMachine.Current()),
		"inputSecs":  s.inputSecs,
		"outputSecs": s.outputSecs,
		"bargeIns":   s.bargeIns,
		"toolCalls":  s.toolCalls,
		"duration":   time.Since(s.startTime).Seconds(),
	}
}
