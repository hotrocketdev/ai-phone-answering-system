// Package session manages the full lifecycle of a voice call session.
// It orchestrates Twilio, OpenAI, audio pipeline, Redis, and tool execution.
package session

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
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

const (
	manualVADSpeechThreshold = 1500
	manualVADStartFrames     = 3
	manualVADStopFrames      = 35
)

// ─── Session ─────────────────────────────────────────────────────────────

type Session struct {
	ID     string // CallSid
	Config *config.Config

	mu           sync.RWMutex
	metaState    MetaState
	stateMachine *sm.Machine
	booking      sm.BookingData
	bookingLive  bool
	bookingAsked string
	capture      *inboundAudioCapture

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
	cartesiaMu                     sync.Mutex
	cartesiaText                   strings.Builder
	cartesiaRemain                 []byte // accumulates partial u-law frames
	cartesiaTranscriptSeen         bool
	openAIAudioDropLogged          bool
	openAIAudioDropCount           int
	openAIResponseActive           bool
	cartesiaResponseActive         bool
	cartesiaFirstFrameAt           time.Time
	cartesiaLastFrameAt            time.Time
	assistantSuppressUntil         time.Time
	cartesiaRenderQueue            chan string
	suppressInputUntil             time.Time
	suppressedInputFrames          int
	suppressionStartedAt           time.Time
	suppressionLastAt              time.Time
	suppressedInputMs              int
	appendedInputFrames            int
	appendAfterSuppressLogged      bool
	speechStartedDuringSuppression bool

	// Manual turn detection
	lastAudioTime         time.Time
	inboundFrames         int
	droppedProviderFrames int
	manualTurnActive      bool
	manualTurnVoiced      int
	manualTurnSilent      int
	manualTurnServerSeen  bool
	manualTurnLastCommit  time.Time
	serverVADLastSeen     time.Time
	pendingInputFrames    int
	pendingInputBytes     int
	turnAppendFrames      int
	turnAppendBytes       int
	currentTurnID         int
	turnFirstAppendAt     time.Time
	turnLastAppendAt      time.Time
	turnSpeechStartedAt   time.Time
	turnSpeechStoppedAt   time.Time
	manualFallbackFired   int
	manualCommitAttempts  int
	manualCommitRejected  int

	// Fast static greeting
	staticGreetingSent       bool
	staticGreetingTextValue  string
	staticGreetingRenderAt   time.Time
	staticGreetingFirstFrame bool
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
		stateMachine:        sm.New(cfg.CustomerName()),
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
	// Phase 1: Prepare local renderer/provider loops before OpenAI so an
	// opt-in static Cartesia greeting can start as soon as the stream is ready.
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

	go s.runProviderLoop(ctx)
	go s.runCartesiaRenderLoop(ctx)

	oaCfg := openai.Config{
		APIKey:       s.Config.OpenAIAPIKey,
		Model:        s.Config.OpenAIRealtimeModel,
		Voice:        "marin",
		Instructions: s.stateMachine.BuildSystemPrompt(),
		Tools:        allTools(),
	}
	log.Printf("[%s] instruction source: business_name=%q", s.ID, s.Config.BusinessName)
	log.Printf("[%s] OpenAI instructions preview: %.200s", s.ID, strings.ReplaceAll(oaCfg.Instructions, "\n", " "))

	oaSess, err := openai.NewSession(ctx, oaCfg)
	if err != nil {
		s.Stop()
		return fmt.Errorf("openai connect: %w", err)
	}
	s.openaiS = oaSess

	if err := s.openaiS.Start(ctx); err != nil {
		s.Stop()
		return fmt.Errorf("openai start: %w", err)
	}
	s.setMeta(MetaActive)

	// Persist initial state
	s.saveState(ctx)

	// Start read loops
	go s.openaiS.ReadLoop()
	go s.runOpenAILoop(ctx)
	go s.runSupervisor(ctx)
	log.Printf("[%s] turn mode: server_vad_only=%t manual_fallback_enabled=%t", s.ID, !s.Config.OpenAIManualTurnFallback, s.Config.OpenAIManualTurnFallback)
	log.Printf("[%s] telnyx echo suppression enabled=%t tail_ms=%d",
		s.ID, s.telnyxEchoSuppressionEnabled(), s.Config.TelnyxEchoSuppressionTailMs)

	// Trigger initial AI response (greeting)
	if s.Config.FastStaticGreeting && s.cartesiaRender != nil {
		log.Printf("[%s] openai_initial_greeting_skipped=true", s.ID)
		s.noteStaticGreetingInOpenAI()
	} else if s.openaiS != nil && !s.openaiS.IsClosed() {
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
	case provider.TypeTwilio, provider.TypeTelnyx:
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
			if frame.Direction != "" && frame.Direction != "inbound" {
				s.mu.Lock()
				s.droppedProviderFrames++
				dropped := s.droppedProviderFrames
				s.mu.Unlock()
				if dropped <= 5 || dropped%100 == 0 {
					log.Printf("[%s] dropping provider audio before OpenAI track=%s payload_len=%d count=%d sent_to_openai=false",
						s.ID, frame.Direction, len(frame.Payload), dropped)
				}
				continue
			}
			if s.isInputSuppressed() {
				s.noteSuppressedInputFrame()
				continue
			}
			pcm24k, err := s.audioP.ProcessInboundBytesForCodec(frame.Codec, frame.Payload)
			if err != nil {
				log.Printf("[%s] dropping provider audio before OpenAI codec=%s payload_len=%d error=%v sent_to_openai=false",
					s.ID, frame.Codec, len(frame.Payload), err)
				continue
			}
			inboundCount := s.noteInboundFrame(frame, pcm24k)
			b64 := base64.StdEncoding.EncodeToString(pcm24k)
			s.inputSecs += 0.020
			if s.openaiS != nil {
				if err := s.openaiS.SendAudio(b64); err != nil {
					log.Printf("[%s] OpenAI SendAudio skipped: %v", s.ID, err)
				} else {
					s.noteAppendedInputFrame()
					s.noteOpenAIAppend(len(b64))
				}
			}
			if s.Config.OpenAIManualTurnFallback {
				s.observeManualVAD(frame.Payload)
			}
			if inboundCount <= 5 || inboundCount%50 == 0 {
				log.Printf("[%s] provider audio sent to OpenAI track=%s payload_len=%d sent_to_openai=true",
					s.ID, frame.Direction, len(frame.Payload))
			}
			s.lastAudioTime = time.Now()

		case evt, ok := <-events:
			if !ok {
				return
			}
			if evt.Type == provider.EventError {
				log.Printf("[%s] provider event error: %v", s.ID, evt.Error)
			} else if evt.Type == provider.EventStarted || evt.Type == provider.EventMark {
				log.Printf("[%s] provider event: type=%s label=%s", s.ID, evt.Type, evt.Label)
			}
			if evt.Type == provider.EventStarted {
				s.maybeSendStaticGreeting()
			}
			if evt.Type == provider.EventStopped || evt.Type == provider.EventDisconnected {
				log.Printf("[%s] provider event: type=%s", s.ID, evt.Type)
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
	if source == "cartesia" {
		s.noteCartesiaOutboundFrame()
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

func (s *Session) staticGreetingText() string {
	return fmt.Sprintf("%s, Alex speaking. How can I help?", s.Config.CustomerName())
}

func (s *Session) maybeSendStaticGreeting() {
	if s.Config == nil || !s.Config.FastStaticGreeting || s.cartesiaRender == nil {
		return
	}

	text := s.staticGreetingText()
	now := time.Now()
	s.mu.Lock()
	if s.staticGreetingSent {
		s.mu.Unlock()
		return
	}
	s.staticGreetingSent = true
	s.staticGreetingTextValue = text
	s.staticGreetingRenderAt = now
	s.mu.Unlock()

	log.Printf("[%s] static_greeting_render_start at=%s text_len=%d",
		s.ID, now.Format(time.RFC3339Nano), len(text))
	s.enqueueCartesiaText(text)
	log.Printf("[%s] static_greeting_sent=true openai_initial_greeting_skipped=true", s.ID)
}

func (s *Session) noteStaticGreetingInOpenAI() {
	if s.openaiS == nil || s.openaiS.IsClosed() {
		return
	}
	text := s.staticGreetingText()
	msg := map[string]interface{}{
		"type": "conversation.item.create",
		"item": map[string]interface{}{
			"type": "message",
			"role": "system",
			"content": []map[string]interface{}{
				{"type": "input_text", "text": "The receptionist has already greeted the caller with: " + text + ". Do not greet again; respond directly to the caller's first request."},
			},
		},
	}
	if err := s.openaiS.WriteRaw(msg); err != nil {
		log.Printf("[%s] static greeting context skipped: %v", s.ID, err)
	}
}

// ─── OpenAI Event Handling ───────────────────────────────────────────────

func (s *Session) handleOpenAIEvent(ctx context.Context, evt openai.Event) {
	switch evt.Type {
	case "speech_started":
		s.noteServerVADSpeech()
		s.handleBargeIn()

	case "speech_stopped":
		s.noteServerVADStopped()
		s.clearBargeIn()

	case "input_audio_buffer.committed":
		s.noteOpenAIInputCommitted()

	case "conversation.item.input_audio_transcription.completed":
		transcript := logCallerTranscript(s.ID, evt.Data)
		s.noteOpenAITranscriptCompleted(transcript)
		s.handleCallerTranscript(ctx, transcript)

	case "conversation.item.input_audio_transcription.failed":
		log.Printf("[%s] OpenAI caller transcript failed: %s", s.ID, trimLogPayload(evt.Data, 200))

	case "text.delta":
		if s.isAssistantOutputSuppressed() {
			return
		}
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
		if s.isAssistantOutputSuppressed() {
			return
		}
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
		s.noteOpenAIResponseCreated()

	case "response.done":
		s.setOpenAIResponseActive(false)
		s.noteOpenAIResponseDone()
		status, _ := openai.ParseResponseDone(evt.Data)
		if status == "cancelled" {
			s.clearBargeIn()
		}
		s.noteAssistantBookingQuestion(evt.Data)
		if s.isAssistantOutputSuppressed() {
			return
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
		s.noteOpenAIError(evt.Data)
		log.Printf("[%s] OpenAI error: %s", s.ID, string(evt.Data))
	}
}

func (s *Session) noteServerVADSpeech() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.currentTurnID++
	s.serverVADLastSeen = time.Now()
	s.turnSpeechStartedAt = s.serverVADLastSeen
	s.turnSpeechStoppedAt = time.Time{}
	if s.manualTurnActive {
		s.manualTurnServerSeen = true
	}
	if s.inputSuppressedLocked(time.Now()) {
		s.speechStartedDuringSuppression = true
	}
	log.Printf("[%s] realtime turn=%d speech_started at=%s",
		s.ID, s.currentTurnID, s.turnSpeechStartedAt.Format(time.RFC3339Nano))
}

func (s *Session) noteServerVADStopped() {
	s.mu.Lock()
	turnID := s.currentTurnID
	stoppedAt := time.Now()
	s.turnSpeechStoppedAt = stoppedAt
	startedAt := s.turnSpeechStartedAt
	firstAppend := s.turnFirstAppendAt
	lastAppend := s.turnLastAppendAt
	frames := s.turnAppendFrames
	bytes := s.turnAppendBytes
	pendingFrames := s.pendingInputFrames
	pendingBytes := s.pendingInputBytes
	s.mu.Unlock()

	log.Printf("[%s] realtime turn=%d speech_stopped at=%s started_at=%s first_append=%s last_append=%s turn_frames=%d turn_encoded_bytes=%d pending_frames=%d pending_encoded_bytes=%d; letting OpenAI Realtime create the response",
		s.ID, turnID, stoppedAt.Format(time.RFC3339Nano), formatTimeForLog(startedAt), formatTimeForLog(firstAppend), formatTimeForLog(lastAppend), frames, bytes, pendingFrames, pendingBytes)
}

func (s *Session) noteOpenAIAppend(encodedBytes int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	if s.turnFirstAppendAt.IsZero() {
		s.turnFirstAppendAt = now
	}
	s.turnLastAppendAt = now
	s.pendingInputFrames++
	s.pendingInputBytes += encodedBytes
	s.turnAppendFrames++
	s.turnAppendBytes += encodedBytes
	if s.turnAppendFrames == 1 || s.turnAppendFrames%100 == 0 {
		log.Printf("[%s] openai append stats turn_frames=%d turn_encoded_bytes=%d pending_frames=%d pending_encoded_bytes=%d response_active=%t cartesia_active=%t",
			s.ID, s.turnAppendFrames, s.turnAppendBytes, s.pendingInputFrames, s.pendingInputBytes, s.openAIResponseActive, s.cartesiaResponseActive)
	}
}

func (s *Session) noteOpenAIInputCommitted() {
	s.mu.Lock()
	turnID := s.currentTurnID
	frames := s.pendingInputFrames
	bytes := s.pendingInputBytes
	startedAt := s.turnSpeechStartedAt
	stoppedAt := s.turnSpeechStoppedAt
	firstAppend := s.turnFirstAppendAt
	lastAppend := s.turnLastAppendAt
	s.pendingInputFrames = 0
	s.pendingInputBytes = 0
	s.turnAppendFrames = 0
	s.turnAppendBytes = 0
	s.turnFirstAppendAt = time.Time{}
	s.turnLastAppendAt = time.Time{}
	s.mu.Unlock()
	log.Printf("[%s] openai input committed turn=%d pending_frames=%d pending_encoded_bytes=%d speech_started=%s speech_stopped=%s first_append=%s last_append=%s buffer_cleared=false",
		s.ID, turnID, frames, bytes, formatTimeForLog(startedAt), formatTimeForLog(stoppedAt), formatTimeForLog(firstAppend), formatTimeForLog(lastAppend))
}

func (s *Session) noteOpenAITranscriptCompleted(transcript string) {
	s.mu.RLock()
	turnID := s.currentTurnID
	stoppedAt := s.turnSpeechStoppedAt
	s.mu.RUnlock()
	log.Printf("[%s] realtime turn=%d transcript_completed at=%s speech_stopped=%s transcript_chars=%d",
		s.ID, turnID, time.Now().Format(time.RFC3339Nano), formatTimeForLog(stoppedAt), len(transcript))
}

func (s *Session) noteOpenAIResponseCreated() {
	s.mu.RLock()
	turnID := s.currentTurnID
	stoppedAt := s.turnSpeechStoppedAt
	s.mu.RUnlock()
	log.Printf("[%s] realtime turn=%d response_created at=%s speech_stopped=%s",
		s.ID, turnID, time.Now().Format(time.RFC3339Nano), formatTimeForLog(stoppedAt))
}

func (s *Session) noteOpenAIResponseDone() {
	s.mu.RLock()
	turnID := s.currentTurnID
	stoppedAt := s.turnSpeechStoppedAt
	manualFired := s.manualFallbackFired
	commitAttempts := s.manualCommitAttempts
	commitRejected := s.manualCommitRejected
	suppressed := s.suppressedInputFrames
	appended := s.appendedInputFrames
	speechDuringSuppression := s.speechStartedDuringSuppression
	s.mu.RUnlock()
	log.Printf("[%s] realtime turn=%d response_done at=%s speech_stopped=%s manual_fallback_fired=%d manual_commit_attempts=%d manual_commit_rejected=%d suppressed_inbound_frames=%d appended_inbound_frames=%d speech_started_during_suppression=%t",
		s.ID, turnID, time.Now().Format(time.RFC3339Nano), formatTimeForLog(stoppedAt), manualFired, commitAttempts, commitRejected, suppressed, appended, speechDuringSuppression)
}

func (s *Session) noteOpenAIError(raw json.RawMessage) {
	if !strings.Contains(string(raw), "input_audio_buffer_commit_empty") {
		return
	}
	s.mu.Lock()
	s.manualCommitRejected++
	rejected := s.manualCommitRejected
	s.mu.Unlock()
	log.Printf("[%s] openai commit rejected code=input_audio_buffer_commit_empty count=%d", s.ID, rejected)
}

func formatTimeForLog(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.Format(time.RFC3339Nano)
}

func (s *Session) observeManualVAD(mulaw []byte) {
	avg := averageAbsMulaw(mulaw)

	s.mu.Lock()
	if avg > manualVADSpeechThreshold {
		if !s.manualTurnActive {
			s.manualTurnActive = true
			s.manualTurnVoiced = 0
			s.manualTurnSilent = 0
			s.manualTurnServerSeen = false
		}
		s.manualTurnVoiced++
		s.manualTurnSilent = 0
		s.mu.Unlock()
		return
	}

	if !s.manualTurnActive {
		s.mu.Unlock()
		return
	}

	s.manualTurnSilent++
	if s.manualTurnSilent < manualVADStopFrames {
		s.mu.Unlock()
		return
	}
	shouldCommit := s.manualTurnVoiced >= manualVADStartFrames &&
		!s.manualTurnServerSeen &&
		time.Since(s.serverVADLastSeen) > 3*time.Second &&
		time.Since(s.manualTurnLastCommit) > time.Second &&
		s.pendingInputFrames >= 5 &&
		!s.openAIResponseActive &&
		!s.cartesiaResponseActive
	if shouldCommit {
		s.manualTurnLastCommit = time.Now()
	}
	serverSeen := s.manualTurnServerSeen
	voiced := s.manualTurnVoiced
	pendingFrames := s.pendingInputFrames
	pendingBytes := s.pendingInputBytes
	responseActive := s.openAIResponseActive
	cartesiaActive := s.cartesiaResponseActive
	s.manualTurnActive = false
	s.manualTurnVoiced = 0
	s.manualTurnSilent = 0
	s.manualTurnServerSeen = false
	s.mu.Unlock()

	if !shouldCommit {
		if serverSeen {
			log.Printf("[%s] manual vad observed turn; OpenAI server VAD handled it", s.ID)
		} else if voiced >= manualVADStartFrames {
			log.Printf("[%s] manual vad did not commit: voiced_frames=%d pending_frames=%d response_active=%t cartesia_active=%t",
				s.ID, voiced, pendingFrames, responseActive, cartesiaActive)
		}
		return
	}
	if s.openaiS == nil || s.openaiS.IsClosed() {
		return
	}
	s.mu.Lock()
	s.manualFallbackFired++
	s.manualCommitAttempts++
	fired := s.manualFallbackFired
	attempts := s.manualCommitAttempts
	s.mu.Unlock()
	log.Printf("[%s] manual vad fallback firing: voiced_frames=%d pending_frames=%d pending_encoded_bytes=%d fired=%d commit_attempts=%d",
		s.ID, voiced, pendingFrames, pendingBytes, fired, attempts)
	if err := s.openaiS.CommitAudio(); err != nil {
		log.Printf("[%s] manual vad CommitAudio skipped: %v fired=%d commit_attempts=%d", s.ID, err, fired, attempts)
		return
	}
	if err := s.openaiS.CreateResponse(); err != nil {
		log.Printf("[%s] manual vad CreateResponse skipped: %v", s.ID, err)
	}
}

func averageAbsMulaw(mulaw []byte) int {
	if len(mulaw) == 0 {
		return 0
	}
	pcm := make([]byte, len(mulaw)*2)
	audio.MulawToPCM16(mulaw, pcm)
	total := 0
	for i := 0; i < len(pcm); i += 2 {
		v := int(int16(binary.LittleEndian.Uint16(pcm[i:])))
		if v < 0 {
			v = -v
		}
		total += v
	}
	return total / len(mulaw)
}

func logCallerTranscript(sessionID string, raw json.RawMessage) string {
	var evt struct {
		Transcript string `json:"transcript"`
	}
	if err := json.Unmarshal(raw, &evt); err != nil || strings.TrimSpace(evt.Transcript) == "" {
		log.Printf("[%s] OpenAI caller transcript event: %s", sessionID, trimLogPayload(raw, 200))
		return ""
	}
	log.Printf("[%s] OpenAI caller transcript: %q", sessionID, trimLogPayload([]byte(evt.Transcript), 200))
	return strings.TrimSpace(evt.Transcript)
}

func trimLogPayload(raw []byte, max int) string {
	text := strings.TrimSpace(string(raw))
	if len(text) <= max {
		return text
	}
	return text[:max]
}

func (s *Session) handleCallerTranscript(ctx context.Context, transcript string) {
	if strings.TrimSpace(transcript) == "" {
		return
	}

	s.mu.Lock()
	current := s.booking
	active := s.bookingLive || bookingIntent(transcript)
	update := parseBookingSlots(transcript, current)
	if !active && !update.hasAny() {
		s.mu.Unlock()
		return
	}
	s.bookingLive = true
	s.booking = mergeBookingSlots(current, update, correctionIntent(transcript))
	booking := s.booking
	missing := firstMissingBookingField(booking)
	s.mu.Unlock()

	log.Printf("[%s] booking slots: %s", s.ID, bookingSummary(booking))
	if missing == "" {
		s.forceBookingQuestion(ctx, "One moment, I'll check that.")
		return
	}
	s.forceBookingQuestion(ctx, nextBookingQuestion(missing))
}

func (s *Session) noteAssistantBookingQuestion(raw json.RawMessage) {
	text := extractTranscript(raw)
	field := expectedBookingFieldFromAssistant(text)
	if field == "" {
		return
	}
	s.mu.Lock()
	s.bookingLive = true
	s.bookingAsked = field
	s.booking = markSlotsImpliedByAssistantQuestion(s.booking, field)
	summary := bookingSummary(s.booking)
	s.mu.Unlock()
	log.Printf("[%s] booking assistant asked=%s slots=%s", s.ID, field, summary)
}

func (s *Session) forceBookingQuestion(ctx context.Context, question string) {
	question = strings.TrimSpace(question)
	if question == "" {
		return
	}
	if s.openaiS != nil && !s.openaiS.IsClosed() {
		if err := s.openaiS.CancelResponse(); err != nil {
			log.Printf("[%s] booking response cancel skipped: %v", s.ID, err)
		}
	}
	s.suppressAssistantOutputFor(2 * time.Second)
	log.Printf("[%s] booking next question: %q", s.ID, question)
	if s.cartesiaRender != nil {
		s.enqueueCartesiaText(question)
		return
	}
	if s.openaiS == nil || s.openaiS.IsClosed() {
		return
	}
	if err := s.openaiS.WriteRaw(map[string]interface{}{
		"type": "conversation.item.create",
		"item": map[string]interface{}{
			"type": "message",
			"role": "user",
			"content": []map[string]interface{}{
				{"type": "input_text", "text": "Ask the caller exactly: " + question},
			},
		},
	}); err != nil {
		log.Printf("[%s] booking prompt inject failed: %v", s.ID, err)
		return
	}
	if err := s.openaiS.CreateResponse(); err != nil {
		log.Printf("[%s] booking response create failed: %v", s.ID, err)
	}
}

func (s *Session) suppressAssistantOutputFor(d time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	until := time.Now().Add(d)
	if until.After(s.assistantSuppressUntil) {
		s.assistantSuppressUntil = until
	}
}

func (s *Session) isAssistantOutputSuppressed() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return !s.assistantSuppressUntil.IsZero() && time.Now().Before(s.assistantSuppressUntil)
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

func (s *Session) setCartesiaResponseActive(active bool) {
	s.mu.Lock()
	if active {
		s.cartesiaFirstFrameAt = time.Time{}
		s.cartesiaLastFrameAt = time.Time{}
		s.suppressionStartedAt = time.Time{}
		s.suppressionLastAt = time.Time{}
		s.appendAfterSuppressLogged = false
	}
	s.cartesiaResponseActive = active
	s.mu.Unlock()
}

func (s *Session) suppressInputFor(d time.Duration) {
	if s.provAdapter != nil && s.provAdapter.Type() == provider.TypeTelnyx {
		return
	}
	s.mu.Lock()
	until := time.Now().Add(d)
	if until.After(s.suppressInputUntil) {
		s.suppressInputUntil = until
	}
	s.mu.Unlock()
}

func (s *Session) isInputSuppressed() bool {
	s.mu.RLock()
	suppressed := s.inputSuppressedLocked(time.Now())
	s.mu.RUnlock()
	return suppressed
}

func (s *Session) inputSuppressedLocked(now time.Time) bool {
	if s.telnyxEchoSuppressionEnabled() {
		if s.cartesiaResponseActive {
			return true
		}
		if !s.cartesiaLastFrameAt.IsZero() {
			tail := time.Duration(s.Config.TelnyxEchoSuppressionTailMs) * time.Millisecond
			return now.Before(s.cartesiaLastFrameAt.Add(tail))
		}
		return false
	}
	until := s.suppressInputUntil
	return !until.IsZero() && now.Before(until)
}

func (s *Session) telnyxEchoSuppressionEnabled() bool {
	return s.provAdapter != nil &&
		s.provAdapter.Type() == provider.TypeTelnyx &&
		s.Config != nil &&
		s.Config.TelnyxEchoSuppressionTailMs > 0
}

func (s *Session) noteCartesiaOutboundFrame() {
	s.noteStaticGreetingOutboundFrame()
	if !s.telnyxEchoSuppressionEnabled() {
		return
	}
	now := time.Now()
	s.mu.Lock()
	if s.cartesiaFirstFrameAt.IsZero() {
		s.cartesiaFirstFrameAt = now
		log.Printf("[%s] echo suppression playback started at=%s tail_ms=%d",
			s.ID, now.Format(time.RFC3339Nano), s.Config.TelnyxEchoSuppressionTailMs)
	}
	s.cartesiaLastFrameAt = now
	s.mu.Unlock()
}

func (s *Session) noteStaticGreetingOutboundFrame() {
	now := time.Now()
	s.mu.Lock()
	active := s.staticGreetingSent && !s.staticGreetingFirstFrame && !s.staticGreetingRenderAt.IsZero()
	if active {
		s.staticGreetingFirstFrame = true
	}
	started := s.staticGreetingRenderAt
	s.mu.Unlock()
	if active {
		log.Printf("[%s] static_greeting_first_outbound_frame at=%s since_render_start_ms=%d",
			s.ID, now.Format(time.RFC3339Nano), now.Sub(started).Milliseconds())
	}
}

func (s *Session) noteSuppressedInputFrame() {
	s.mu.Lock()
	s.suppressedInputFrames++
	count := s.suppressedInputFrames
	now := time.Now()
	if s.suppressionStartedAt.IsZero() {
		s.suppressionStartedAt = now
		log.Printf("[%s] echo suppression started at=%s", s.ID, now.Format(time.RFC3339Nano))
	}
	s.suppressionLastAt = now
	s.suppressedInputMs += 20
	s.mu.Unlock()
	if count == 1 || count%50 == 0 {
		log.Printf("[%s] suppressing inbound audio during assistant playback count=%d duration_ms=%d", s.ID, count, count*20)
	}
}

func (s *Session) noteAppendedInputFrame() {
	s.mu.Lock()
	s.appendedInputFrames++
	appended := s.appendedInputFrames
	wasSuppressed := !s.suppressionLastAt.IsZero() && !s.appendAfterSuppressLogged
	if wasSuppressed {
		s.appendAfterSuppressLogged = true
	}
	suppressionEnd := s.suppressionLastAt
	suppressedFrames := s.suppressedInputFrames
	suppressedMs := s.suppressedInputMs
	s.mu.Unlock()

	if wasSuppressed {
		log.Printf("[%s] first inbound frame appended after echo suppression at=%s suppression_end=%s suppressed_frames=%d suppressed_duration_ms=%d appended_frames=%d",
			s.ID, time.Now().Format(time.RFC3339Nano), formatTimeForLog(suppressionEnd), suppressedFrames, suppressedMs, appended)
	}
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
	if s.capture != nil {
		s.capture.Close(s.ID)
		s.capture = nil
	}

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
	if s.capture != nil {
		s.capture.Close(s.ID)
		s.capture = nil
	}
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

func (s *Session) updateTools() {
	if s.openaiS == nil || s.openaiS.IsClosed() {
		return
	}
	tools := convertTools(s.stateMachine.AvailableTools())
	msg := map[string]interface{}{
		"type": "session.update",
		"session": map[string]interface{}{
			"type":  "realtime",
			"model": s.Config.OpenAIRealtimeModel,
			"tools": tools,
		},
	}
	if err := s.openaiS.WriteRaw(msg); err != nil {
		log.Printf("[%s] updateTools failed: %v", s.ID, err)
	}
}

func (s *Session) getActiveTools() []openai.Tool {
	return convertTools(s.stateMachine.AvailableTools())
}

func allTools() []openai.Tool {
	var all []openai.Tool
	for _, t := range sm.AllTools() {
		all = append(all, openai.Tool{
			Type:        "function",
			Name:        t.Name,
			Description: t.Description,
			Parameters:  t.Parameters,
		})
	}
	return all
}

func convertTools(smTools []sm.Tool) []openai.Tool {
	tools := make([]openai.Tool, len(smTools))
	for i, t := range smTools {
		tools[i] = openai.Tool{
			Type:        "function",
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
	isStaticGreeting := s.isStaticGreetingText(text)
	log.Printf("[%s] cartesia: sending text (%d chars)", s.ID, len(text))
	s.setCartesiaResponseActive(true)
	defer s.setCartesiaResponseActive(false)
	s.suppressInputFor(750 * time.Millisecond)

	audioCh, err := s.cartesiaRender.RenderStream(ctx, text)
	if err != nil {
		log.Printf("[%s] cartesia render failed: %v", s.ID, err)
		s.suppressInputFor(250 * time.Millisecond)
		return
	}

	chunkCount := 0
	for chunk := range audioCh {
		chunkCount++
		s.suppressInputFor(250 * time.Millisecond)
		// Cartesia outputs pcm_mulaw 8kHz natively — send directly
		s.outputSecs += float64(len(chunk)) / 8000.0
		s.sendMulawToTwilio(chunk)
	}
	s.suppressInputFor(250 * time.Millisecond)
	log.Printf("[%s] cartesia: %d chunks sent to Twilio", s.ID, chunkCount)
	if isStaticGreeting {
		log.Printf("[%s] static_greeting_playback_completed=true chunks=%d", s.ID, chunkCount)
	}
	log.Printf("[%s] cartesia: audio complete", s.ID)
}

func (s *Session) isStaticGreetingText(text string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.staticGreetingSent && s.staticGreetingTextValue != "" && text == s.staticGreetingTextValue
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
