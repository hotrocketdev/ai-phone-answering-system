// Package session manages the full lifecycle of a voice call session.
// It orchestrates Twilio, OpenAI, audio pipeline, Redis, and tool execution.
package session

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/voxlane/voice-gateway/internal/audio"
	"github.com/voxlane/voice-gateway/internal/config"
	"github.com/voxlane/voice-gateway/internal/openai"
	"github.com/voxlane/voice-gateway/internal/provider"
	providertwilio "github.com/voxlane/voice-gateway/internal/provider/twilio"
	goredis "github.com/voxlane/voice-gateway/internal/redis"
	"github.com/voxlane/voice-gateway/internal/session/sm"
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
	provAdapter provider.Adapter
	openaiS     *openai.Session
	audioP      *audio.Pipeline
	redisC      *goredis.Client

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
func NewSession(callSid string, adapter provider.Adapter, cfg *config.Config, redisC *goredis.Client) *Session {
	return &Session{
		ID:           callSid,
		Config:       cfg,
		provAdapter:  adapter,
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
	log.Printf("[%s] connecting to OpenAI Realtime (model=%s)...", s.ID, s.Config.OpenAIRealtimeModel)
	oaCfg := openai.Config{
		APIKey:       s.Config.OpenAIAPIKey,
		Model:        s.Config.OpenAIRealtimeModel,
		Voice:        "shimmer", // warm, natural feminine voice — best for restaurant reception
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
	go s.runProviderLoop(ctx)
	go s.runOpenAILoop(ctx)
	go s.runSupervisor(ctx)

	// Trigger initial AI response (greeting)
	if err := s.openaiS.CreateResponse(); err != nil {
		log.Printf("[%s] CreateResponse failed: %v", s.ID, err)
	} else {
		log.Printf("[%s] CreateResponse sent", s.ID)
	}

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

func (s *Session) runProviderLoop(ctx context.Context) {
	switch s.provAdapter.Type() {
	case provider.TypeTwilio:
		s.runProviderChannels(ctx)
	default:
		log.Printf("[%s] provider %s not yet supported in run loop", s.ID, s.provAdapter.Type())
	}
}

// runProviderChannels handles providers that expose channel-based I/O (Twilio).
func (s *Session) runProviderChannels(ctx context.Context) {
	adapter, ok := s.provAdapter.(*providertwilio.Adapter)
	if !ok {
		return
	}
	for {
		select {
		case <-s.stopCh:
			return
		case frame, ok := <-adapter.Frames:
			if !ok {
				s.handleProviderDisconnect(ctx)
				return
			}
			b64 := s.audioP.ProcessInbound(frame.Payload)
			s.inputSecs += 0.020
			_ = s.openaiS.SendAudio(b64)

		case evt, ok := <-adapter.Events:
			if !ok {
				return
			}
			if evt.Type == provider.EventStopped || evt.Type == provider.EventDisconnected {
				s.handleProviderDisconnect(ctx)
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
			if s.isBargingIn() {
				continue
			}
			mulaw, err := s.audioP.ProcessOutboundBytes(pcm24k)
			if err != nil {
				continue
			}
			if mulaw == nil {
				continue // buffering — not a full frame yet
			}
			s.outputSecs += 0.020

			// Log first frame and every 50th
			if s.outputSecs <= 0.04 {
				log.Printf("[%s] audio out: first frame sent (%.0f sec)", s.ID, s.outputSecs)
			}

			// Encode and send via provider adapter
			if msg, err := s.provAdapter.EncodeAudio(provider.AudioFrame{
				Codec: "ulaw", SampleRate: 8000, Payload: mulaw, Direction: "outbound",
			}); err == nil && msg != nil {
				s.sendProviderMessage(msg)
			}

		case evt, ok := <-s.openaiS.Events:
			if !ok {
				return
			}
			s.handleOpenAIEvent(ctx, evt)
		}
	}
}

// sendProviderMessage writes a raw message to the provider WebSocket.
// Uses the Twilio adapter's conn for now; future providers will use the interface.
func (s *Session) sendProviderMessage(data []byte) {
	if adapter, ok := s.provAdapter.(*providertwilio.Adapter); ok {
		adapter.WriteRaw(data)
	}
}

func (s *Session) runSupervisor(ctx context.Context) {
	// Restaurant calls have natural pauses — give callers time to think
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
				log.Printf("[%s] silence — nudging caller", s.ID)
				// Inject context to make AI check if caller is still there
				s.injectSilencePrompt()
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

// injectSilencePrompt sends a system message to make the AI check on the caller.
func (s *Session) injectSilencePrompt() {
	if s.openaiS == nil || s.openaiS.IsClosed() {
		return
	}
	// Use conversation.item.create to inject a system nudge
	msg := map[string]interface{}{
		"type": "conversation.item.create",
		"item": map[string]interface{}{
			"type": "message",
			"role": "system",
			"content": []map[string]interface{}{
				{"type": "input_text", "text": "The caller has been silent. Gently check if they're still there in a natural way — like \"still with me?\" or \"did you have a date in mind?\" Don't sound automated."},
			},
		},
	}
	// Send the nudge and trigger a new response
	s.openaiS.WriteRaw(msg)
	s.openaiS.WriteRaw(map[string]interface{}{"type": "response.create"})
}

// ─── OpenAI Event Handling ───────────────────────────────────────────────

func (s *Session) handleOpenAIEvent(ctx context.Context, evt openai.Event) {
	switch evt.Type {
	case "speech_started":
		s.handleBargeIn()

	case "speech_stopped":
		// Reset silence timer

	case "audio.done":
		// Flush any remaining buffered audio frames
		if mulaw := s.audioP.FlushOutbound(); mulaw != nil {
			if msg, err := s.provAdapter.EncodeAudio(provider.AudioFrame{
				Codec: "ulaw", SampleRate: 8000, Payload: mulaw, Direction: "outbound",
			}); err == nil && msg != nil {
				s.sendProviderMessage(msg)
			}
		}

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

	// Audit (graceful if Redis unavailable)
	if s.redisC != nil {
		s.redisC.AppendToolCall(ctx, s.ID, goredis.ToolCallRecord{
			Tool:      name,
			Args:      args,
			Result:    json.RawMessage(result),
			Timestamp: time.Now(),
		})
	}
}

// ─── Tool Execution ──────────────────────────────────────────────────────

// toolCallRequest is the HMAC-signed payload sent to NestJS.
type toolCallRequest struct {
	CallSid   string          `json:"callSid"`
	TenantID  string          `json:"tenantId"`
	ToolName  string          `json:"toolName"`
	Arguments json.RawMessage `json:"arguments"`
	Signature string          `json:"signature"`
	Timestamp int64           `json:"timestamp"`
}

func (s *Session) executeToolCall(ctx context.Context, name string, args json.RawMessage) (string, error) {
	now := time.Now().UnixMilli()

	// Build request body
	body := toolCallRequest{
		CallSid:   s.ID,
		TenantID:  "default", // TODO: real tenant ID when multi-tenant
		ToolName:  name,
		Arguments: args,
		Timestamp: now,
	}

	// HMAC-sign: SHA256(callSid:tenantId:toolName:timestamp)
	payload := fmt.Sprintf("%s:%s:%s:%d", body.CallSid, body.TenantID, body.ToolName, body.Timestamp)
	mac := hmac.New(sha256.New, []byte(s.Config.HMACSecret))
	mac.Write([]byte(payload))
	body.Signature = hex.EncodeToString(mac.Sum(nil))

	// Marshal body
	bodyJSON, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("marshal tool call: %w", err)
	}

	// Build HTTP request
	url := fmt.Sprintf("%s/api/internal/tools/%s", s.Config.NestJSUrl, name)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyJSON))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-HMAC-Signature", body.Signature)
	req.Header.Set("X-Timestamp", fmt.Sprintf("%d", body.Timestamp))

	// Execute HTTP call
	log.Printf("[%s] POST %s", s.ID, url)
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("http POST: %w", err)
	}
	defer resp.Body.Close()

	// Parse response
	var result json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}

	log.Printf("[%s] tool response (%d): %s", s.ID, resp.StatusCode, string(result))
	return string(result), nil
}

// ─── Graceful Shutdown ───────────────────────────────────────────────────

func (s *Session) handleProviderDisconnect(ctx context.Context) {
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
	if s.provAdapter != nil {
		s.provAdapter.Close()
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
