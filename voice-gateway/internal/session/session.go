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
	"strings"
	"sync"
	"time"

	"github.com/voxlane/voice-gateway/internal/audio"
	"github.com/voxlane/voice-gateway/internal/config"
	"github.com/voxlane/voice-gateway/internal/openai"
	"github.com/voxlane/voice-gateway/internal/provider"
	providertelnyx "github.com/voxlane/voice-gateway/internal/provider/telnyx"
	providertwilio "github.com/voxlane/voice-gateway/internal/provider/twilio"
	goredis "github.com/voxlane/voice-gateway/internal/redis"
	cartesiarend "github.com/voxlane/voice-gateway/internal/renderer/cartesia"
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

	mu           sync.RWMutex
	metaState    MetaState
	stateMachine *sm.Machine

	// Components
	provAdapter    provider.Adapter
	openaiS        *openai.Session
	audioP         *audio.Pipeline
	redisC         *goredis.Client
	cartesiaRender *cartesiarend.Renderer // active when VOICE_RENDERER=cartesia

	// Channels (internal)
	stopCh chan struct{}
	doneCh chan struct{}

	// Metrics
	inputSecs  float64
	outputSecs float64
	bargeIns   int
	toolCalls  int
	startTime  time.Time

	// Tool call state
	pendingToolCallID string

	// Cartesia state
	cartesiaMu             sync.Mutex
	cartesiaText           strings.Builder
	cartesiaRemain         []byte // accumulates partial u-law frames
	cartesiaTranscriptSeen bool
	openAIAudioDropLogged  bool
	openAIAudioDropCount   int
	openAIResponseActive   bool
	cartesiaRenderQueue    chan string

	// Manual turn detection
	lastAudioTime time.Time
}

// ─── Creation ────────────────────────────────────────────────────────────

// NewSession creates a new call session.
func NewSession(callSid string, adapter provider.Adapter, cfg *config.Config, redisC *goredis.Client) *Session {
	return &Session{
		ID:                  callSid,
		Config:              cfg,
		provAdapter:         adapter,
		redisC:              redisC,
		audioP:              audio.NewPipeline(),
		stateMachine:        sm.New(cfg.BusinessName),
		metaState:           MetaCreated,
		stopCh:              make(chan struct{}),
		doneCh:              make(chan struct{}),
		cartesiaRenderQueue: make(chan string, 32),
		startTime:           time.Now(),
	}
}

// ─── Lifecycle ───────────────────────────────────────────────────────────

// Run executes the full session lifecycle. Blocks until the call ends.
func (s *Session) Run(ctx context.Context) error {
	// Phase 1: Connect to OpenAI
	s.setMeta(MetaConnecting)
	log.Printf("[%s] connecting to OpenAI Realtime (model=%s)...", s.ID, s.Config.OpenAIRealtimeModel)

	// Create Cartesia renderer if configured
	if s.Config.VoiceRenderer == "cartesia" {
		if s.Config.CartesiaAPIKey == "" {
			return fmt.Errorf("VOICE_RENDERER=cartesia requires CARTESIA_API_KEY")
		}
		s.cartesiaRender = cartesiarend.New(cartesiarend.Config{
			APIKey:   s.Config.CartesiaAPIKey,
			VoiceID:  s.Config.CartesiaVoiceID,
			ModelID:  s.Config.CartesiaModel,
			Language: s.Config.CartesiaLanguage,
			Speed:    s.Config.CartesiaSpeed,
			Volume:   s.Config.CartesiaVolume,
			Emotion:  s.Config.CartesiaEmotion,
		})
		log.Printf("[%s] voice renderer: cartesia", s.ID)
	}

	oaCfg := openai.Config{
		APIKey:       s.Config.OpenAIAPIKey,
		Model:        s.Config.OpenAIRealtimeModel,
		Voice:        "marin",
		Instructions: s.stateMachine.BuildSystemPrompt(),
		Tools:        convertTools(s.stateMachine.AvailableTools()),
	}
	log.Printf("[%s] instruction source: business_name=%q", s.ID, s.Config.BusinessName)
	log.Printf("[%s] OpenAI instructions preview: %.200s", s.ID, strings.ReplaceAll(oaCfg.Instructions, "\n", " "))

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
	go s.runCartesiaRenderLoop(ctx)
	log.Printf("[%s] manual turn detection disabled; using OpenAI Realtime turn events", s.ID)

	// Trigger initial AI response (greeting)
	if s.openaiS != nil && !s.openaiS.IsClosed() {
		if err := s.openaiS.CreateResponse(); err != nil {
			log.Printf("[%s] CreateResponse failed: %v", s.ID, err)
		} else {
			log.Printf("[%s] CreateResponse sent", s.ID)
		}
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

// runProviderChannels handles providers that expose channel-based I/O.
func (s *Session) runProviderChannels(ctx context.Context) {
	var frames chan provider.AudioFrame
	var events chan provider.Event

	switch a := s.provAdapter.(type) {
	case *providertwilio.Adapter:
		frames = a.Frames
		events = a.Events
	case *providertelnyx.Adapter:
		frames = a.Frames
		events = a.Events
	default:
		return
	}

	for {
		select {
		case <-s.stopCh:
			return
		case frame, ok := <-frames:
			if !ok {
				s.handleProviderDisconnect(ctx)
				return
			}
			b64 := s.audioP.ProcessInbound(frame.Payload)
			s.inputSecs += 0.020
			if s.openaiS != nil {
				if err := s.openaiS.SendAudio(b64); err != nil {
					log.Printf("[%s] OpenAI SendAudio skipped: %v", s.ID, err)
				}
			}
			s.lastAudioTime = time.Now()

		case evt, ok := <-events:
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

// checkUserTurn periodically commits audio buffer when caller stops speaking.
func (s *Session) runTurnDetection(ctx context.Context) {
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	var hasSpeech bool

	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			if s.lastAudioTime.IsZero() {
				continue
			}
			silence := time.Since(s.lastAudioTime)
			if silence < 200*time.Millisecond {
				hasSpeech = true
				continue
			}
			if hasSpeech && silence > 600*time.Millisecond {
				if s.openaiS == nil || s.openaiS.IsClosed() {
					hasSpeech = false
					continue
				}
				log.Printf("[%s] turn detection: committing audio", s.ID)
				if err := s.openaiS.CommitAudio(); err != nil {
					log.Printf("[%s] CommitAudio skipped: %v", s.ID, err)
				}
				if err := s.openaiS.CreateResponse(); err != nil {
					log.Printf("[%s] CreateResponse skipped: %v", s.ID, err)
				}
				s.lastAudioTime = time.Time{}
				hasSpeech = false
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
			if s.cartesiaRender != nil {
				if !s.openAIAudioDropLogged {
					log.Printf("[%s] DROPPED_OPENAI_AUDIO_CARTESIA_ACTIVE", s.ID)
					s.openAIAudioDropLogged = true
				}
				s.mu.Lock()
				s.openAIAudioDropCount++
				s.mu.Unlock()
				continue
			}
			if s.isBargingIn() {
				continue
			}

			// Process PCM16 24kHz → u-law 8kHz → Twilio
			frames := s.audioP.ProcessOutboundBytes(pcm24k)
			for _, mulaw := range frames {
				s.outputSecs += 0.020
				if msg, err := s.provAdapter.EncodeAudio(provider.AudioFrame{
					Codec: "ulaw", SampleRate: 8000, Payload: mulaw, Direction: "outbound",
				}); err == nil && msg != nil {
					s.sendProviderMessage(msg, "openai")
				}
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
func (s *Session) sendProviderMessage(data []byte, source string) {
	switch a := s.provAdapter.(type) {
	case *providertwilio.Adapter:
		if err := a.WriteRaw(data); err != nil {
			log.Printf("[%s] outbound media write failed source=%s: %v", s.ID, source, err)
			return
		}
	case *providertelnyx.Adapter:
		if err := a.WriteRaw(data); err != nil {
			log.Printf("[%s] outbound media write failed source=%s: %v", s.ID, source, err)
			return
		}
	default:
		return
	}
	log.Printf("[%s] outbound media frame sent source=%s", s.ID, source)
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
	if err := s.openaiS.WriteRaw(msg); err != nil {
		log.Printf("[%s] silence prompt skipped: %v", s.ID, err)
		return
	}
	if err := s.openaiS.WriteRaw(map[string]interface{}{"type": "response.create"}); err != nil {
		log.Printf("[%s] silence response skipped: %v", s.ID, err)
	}
}

// ─── OpenAI Event Handling ───────────────────────────────────────────────

func (s *Session) handleOpenAIEvent(ctx context.Context, evt openai.Event) {
	switch evt.Type {
	case "speech_started":
		s.handleBargeIn()

	case "speech_stopped":
		s.clearBargeIn()
		log.Printf("[%s] speech_stopped received; letting OpenAI Realtime create the response", s.ID)

	case "text.delta":
		// Accumulate text for Cartesia rendering
		if s.cartesiaRender != nil {
			var delta struct {
				Delta string `json:"delta"`
			}
			if json.Unmarshal(evt.Data, &delta) == nil {
				s.bufferCartesiaTranscript(ctx, delta.Delta)
			}
		}

	case "audio_transcript.delta":
		if s.cartesiaRender != nil {
			var delta struct {
				Delta string `json:"delta"`
			}
			if json.Unmarshal(evt.Data, &delta) == nil {
				s.bufferCartesiaTranscript(ctx, delta.Delta)
			}
		}

	case "audio.done":
		if s.cartesiaRender != nil {
			return
		}
		// Flush any remaining buffered audio frames
		if mulaw := s.audioP.FlushOutbound(); mulaw != nil {
			if msg, err := s.provAdapter.EncodeAudio(provider.AudioFrame{
				Codec: "ulaw", SampleRate: 8000, Payload: mulaw, Direction: "outbound",
			}); err == nil && msg != nil {
				s.sendProviderMessage(msg, "openai")
			}
		}

	case "function_call.done":
		s.handleToolCall(ctx, evt.Data)

	case "response.created":
		s.setOpenAIResponseActive(true)

	case "response.done":
		s.setOpenAIResponseActive(false)
		status, _ := openai.ParseResponseDone(evt.Data)
		if status == "cancelled" {
			s.clearBargeIn()
		}

		// Route text to Cartesia for British voice rendering
		if s.cartesiaRender != nil {
			if !s.flushCartesiaTranscript(ctx) {
				text := extractTranscript(evt.Data)
				if text != "" {
					s.enqueueCartesiaText(text)
				}
			}
		}

	case "error":
		log.Printf("[%s] OpenAI error: %s", s.ID, string(evt.Data))
	}
}

func (s *Session) bufferCartesiaTranscript(ctx context.Context, delta string) {
	if delta == "" {
		return
	}

	s.cartesiaMu.Lock()
	s.cartesiaTranscriptSeen = true
	s.cartesiaText.WriteString(delta)
	text := s.cartesiaText.String()
	hasBoundary := strings.ContainsAny(delta, ".!?;:\n")
	shouldFlush := (hasBoundary && len(text) >= 20) || len(text) >= 100
	if !shouldFlush {
		s.cartesiaMu.Unlock()
		return
	}
	s.cartesiaText.Reset()
	s.cartesiaMu.Unlock()

	text = strings.TrimSpace(text)
	if text != "" {
		s.enqueueCartesiaText(text)
	}
}

func (s *Session) flushCartesiaTranscript(ctx context.Context) bool {
	s.cartesiaMu.Lock()
	seen := s.cartesiaTranscriptSeen
	text := strings.TrimSpace(s.cartesiaText.String())
	s.cartesiaText.Reset()
	s.cartesiaMu.Unlock()

	if text != "" {
		s.enqueueCartesiaText(text)
		return true
	}
	return seen
}

func (s *Session) enqueueCartesiaText(text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}

	select {
	case s.cartesiaRenderQueue <- text:
		log.Printf("[%s] cartesia: queued text (%d chars)", s.ID, len(text))
	case <-s.stopCh:
		log.Printf("[%s] cartesia: queue skipped; session stopping", s.ID)
	default:
		log.Printf("[%s] cartesia: queue full; dropping text (%d chars)", s.ID, len(text))
	}
}

func (s *Session) runCartesiaRenderLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stopCh:
			return
		case text := <-s.cartesiaRenderQueue:
			s.renderWithCartesia(ctx, text)
		}
	}
}

func (s *Session) sendMulawToTwilio(mulaw []byte) int {
	s.cartesiaRemain = append(s.cartesiaRemain, mulaw...)
	frameCount := 0

	for len(s.cartesiaRemain) >= 160 {
		frame := s.cartesiaRemain[:160]
		s.cartesiaRemain = s.cartesiaRemain[160:]

		if msg, err := s.provAdapter.EncodeAudio(provider.AudioFrame{
			Codec: "ulaw", SampleRate: 8000, Payload: frame, Direction: "outbound",
		}); err == nil && msg != nil {
			s.sendProviderMessage(msg, "cartesia")
			frameCount++
		}
	}
	return frameCount
}

func (s *Session) handleBargeIn() {
	s.mu.Lock()
	s.bargeIns++
	active := s.openAIResponseActive
	s.mu.Unlock()
	if !active {
		log.Printf("[%s] CancelResponse skipped: no active OpenAI response", s.ID)
		return
	}
	if s.openaiS != nil {
		if err := s.openaiS.CancelResponse(); err != nil {
			log.Printf("[%s] CancelResponse skipped: %v", s.ID, err)
		}
	}
}

func (s *Session) setOpenAIResponseActive(active bool) {
	s.mu.Lock()
	s.openAIResponseActive = active
	s.mu.Unlock()
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

// renderWithCartesia sends text to Cartesia and routes PCM16 audio to Twilio.
func (s *Session) renderWithCartesia(ctx context.Context, text string) {
	if s.cartesiaRender == nil {
		return
	}
	log.Printf("[%s] cartesia: sending text (%d chars)", s.ID, len(text))

	audioCh, err := s.cartesiaRender.RenderStream(ctx, text)
	if err != nil {
		log.Printf("[%s] cartesia render failed: %v", s.ID, err)
		return
	}

	chunkCount := 0
	for chunk := range audioCh {
		chunkCount++
		// Cartesia outputs pcm_mulaw 8kHz natively — send directly
		s.outputSecs += float64(len(chunk)) / 8000.0
		s.sendMulawToTwilio(chunk)
	}
	log.Printf("[%s] cartesia: %d chunks sent to Twilio", s.ID, chunkCount)
	log.Printf("[%s] cartesia: audio complete", s.ID)
}

// extractTranscript pulls the assistant text from a response.done event.
func extractTranscript(raw json.RawMessage) string {
	var event struct {
		Response struct {
			Output []struct {
				Content []struct {
					Transcript string `json:"transcript"`
				} `json:"content"`
			} `json:"output"`
		} `json:"response"`
	}
	if json.Unmarshal(raw, &event) != nil {
		return ""
	}
	if len(event.Response.Output) > 0 && len(event.Response.Output[0].Content) > 0 {
		return event.Response.Output[0].Content[0].Transcript
	}
	return ""
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
