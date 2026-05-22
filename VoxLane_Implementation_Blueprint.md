# VoxLane — Implementation Blueprint

**Version**: 1.0  
**Date**: May 2026  
**Status**: Pre-implementation engineering plan  
**Target**: MVP — 10 restaurant tenants, 500 calls/day peak

---

## Table of Contents

1. [Go Realtime Voice Gateway Architecture](#1-go-realtime-voice-gateway-architecture)
2. [Twilio Media Streams Handling](#2-twilio-media-streams-handling)
3. [OpenAI Realtime WebSocket Lifecycle](#3-openai-realtime-websocket-lifecycle)
4. [Audio Transcoding Architecture](#4-audio-transcoding-architecture)
5. [Proper Audio Resampling Approach](#5-proper-audio-resampling-approach)
6. [Session Lifecycle Management](#6-session-lifecycle-management)
7. [Redis Session State Design](#7-redis-session-state-design)
8. [Conversation State Machine Implementation](#8-conversation-state-machine-implementation)
9. [Tool Calling Architecture](#9-tool-calling-architecture)
10. [Backend API Boundaries](#10-backend-api-boundaries)
11. [Interruption / Barge-In Handling](#11-interruption--barge-in-handling)
12. [Silence Detection Strategy](#12-silence-detection-strategy)
13. [Retry and Reconnection Handling](#13-retry-and-reconnection-handling)
14. [Failure Recovery Flows](#14-failure-recovery-flows)
15. [Queue Architecture](#15-queue-architecture)
16. [Logging and Observability](#16-logging-and-observability)
17. [Cost Optimisation Strategy](#17-cost-optimisation-strategy)
18. [Token Optimisation Strategy](#18-token-optimisation-strategy)
19. [Call Lifecycle Flow](#19-call-lifecycle-flow)
20. [Deployment Structure](#20-deployment-structure)
21. [Docker Architecture](#21-docker-architecture)
22. [VPS Deployment Approach](#22-vps-deployment-approach)
23. [Repository Structure](#23-repository-structure)
24. [Shared Type Strategy](#24-shared-type-strategy)
25. [Environment Variable Structure](#25-environment-variable-structure)
26. [Security Considerations](#26-security-considerations)
27. [GDPR Considerations](#27-gdpr-considerations)
28. [Testing Strategy](#28-testing-strategy)
29. [Technical Implementation Phases](#29-technical-implementation-phases)
30. [Recommended Order of Development](#30-recommended-order-of-development)

---

## 1. Go Realtime Voice Gateway Architecture

### 1.1 Purpose

The Go Voice Gateway is the single most latency-sensitive component. It bridges Twilio Media Streams (inbound PSTN audio) to OpenAI Realtime API (AI audio), handles transcoding, manages conversation state, and enforces anti-hallucination guardrails. It runs as a single statically-linked binary.

### 1.2 Internal Component Architecture

```
┌─────────────────────────────────────────────────────────┐
│                   Go Voice Gateway                       │
│                                                         │
│  ┌──────────────┐  ┌──────────────┐  ┌───────────────┐ │
│  │ Twilio WS    │  │ OpenAI WS    │  │ HTTP Client   │ │
│  │ Handler      │  │ Handler      │  │ (NestJS API)  │ │
│  └──────┬───────┘  └──────┬───────┘  └───────┬───────┘ │
│         │                 │                   │         │
│  ┌──────┴─────────────────┴───────────────────┴───────┐ │
│  │              Session Manager                       │ │
│  │  ┌──────────┐  ┌──────────┐  ┌──────────────────┐ │ │
│  │  │State     │  │Audio     │  │Conversation      │ │ │
│  │  │Machine   │  │Pipeline  │  │Context Store     │ │ │
│  │  └──────────┘  └──────────┘  └──────────────────┘ │ │
│  └──────────────────────┬────────────────────────────┘ │
│                         │                               │
│  ┌──────────────────────┴────────────────────────────┐ │
│  │              Shared Infrastructure                 │ │
│  │  Redis Pool  │  Logger  │  Metrics  │  Tracer     │ │
│  └───────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────┘
```

### 1.3 Goroutine Model

Each active call consumes exactly 3 long-lived goroutines:

| Goroutine | Responsibility | Lifetime |
|-----------|---------------|----------|
| `twilioReader` | Read audio frames from Twilio WS, push to audio pipeline | Call duration |
| `openaiWriter` | Pull processed audio from pipeline, write to OpenAI WS | Call duration |
| `sessionSupervisor` | Manage state machine transitions, timeouts, tool calls | Call duration + 30s cleanup |

Short-lived goroutines spawn for tool call execution, Redis writes, and API calls to NestJS. These are fire-and-forget with context-based cancellation.

### 1.4 Memory Management

- **Audio buffers**: Pre-allocated ring buffer of 10 x 20ms frames (9,600 bytes for 24kHz PCM16). Reused per call.
- **Conversation context**: Truncated to last 20 turns in memory. Full history in Redis.
- **Zero allocation target**: Hot path (audio read/write) must allocate zero bytes per frame after initial setup. Use `sync.Pool` for frame buffers.

### 1.5 Concurrency Model

- One call = one `Session` struct. No shared mutable state between calls.
- `Session` contains exclusively-owned channels for cross-goroutine communication:
  - `audioIn chan []byte` (twilioReader to openaiWriter)
  - `events chan SessionEvent` (twilioReader to sessionSupervisor)
  - `commands chan SupervisorCommand` (sessionSupervisor to openaiWriter)
  - `toolResults chan ToolResult` (HTTP client goroutines to sessionSupervisor)
- All channels are buffered (capacity 8) to prevent blocking on fast paths.

### 1.6 Graceful Shutdown

```
SIGTERM received
  → Stop accepting new Twilio connections (close listener)
  → Set drain flag on all active sessions
  → Wait up to 30s for active calls to complete
  → Force-close remaining sessions, write final state to Redis
  → Exit
```

---

## 2. Twilio Media Streams Handling

### 2.1 Connection Flow

```
PSTN Call → Twilio → HTTP POST /voice/webhook → NestJS
  → NestJS returns TwiML: <Connect><Stream url="wss://voice.voxlane.com/stream/{callSid}"/></Connect>
  → Twilio opens WebSocket to Go Gateway
  → Go Gateway accepts, begins media stream processing
```

### 2.2 WebSocket Protocol

Twilio sends bidirectional JSON + base64-encoded audio over a single WebSocket.

**Inbound message (Twilio to Go)**:
```json
{
  "event": "media",
  "media": {
    "track": "inbound",
    "chunk": "1579364035",
    "timestamp": "5",
    "payload": "f39/+vr5+Pn6+vr7/Pz7+vr5+fr6+f...",
    "streamSid": "MZXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX"
  },
  "streamSid": "MZXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX",
  "sequenceNumber": "4"
}
```

**Outbound message (Go to Twilio)**:
```json
{
  "event": "media",
  "media": {
    "track": "outbound",
    "chunk": "1579364035",
    "timestamp": "5",
    "payload": "f39/+vr5+Pn6+vr7/Pz7+vr5+fr6+f..."
  },
  "streamSid": "MZXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX"
}
```

### 2.3 Handler Implementation

```go
type TwilioStreamHandler struct {
    streamSid    string
    callSid      string
    conn         *websocket.Conn
    audioOut     chan<- []byte       // to OpenAI writer
    events       chan<- SessionEvent  // to supervisor
    cancel       context.CancelFunc
}

func (h *TwilioStreamHandler) ReadLoop(ctx context.Context) {
    defer h.conn.Close()
    for {
        select {
        case <-ctx.Done():
            return
        default:
            _, msg, err := h.conn.ReadMessage()
            if err != nil {
                h.events <- SessionEvent{Type: EventTwilioDisconnected, Err: err}
                return
            }
            h.handleMessage(msg)
        }
    }
}

func (h *TwilioStreamHandler) handleMessage(raw []byte) {
    var event TwilioMediaEvent
    json.Unmarshal(raw, &event)
    switch event.Event {
    case "media":
        audio, _ := base64.StdEncoding.DecodeString(event.Media.Payload)
        h.audioOut <- audio // raw u-law bytes, 160 bytes per 20ms frame
    case "connected":
        h.events <- SessionEvent{Type: EventTwilioConnected}
    case "stop":
        h.events <- SessionEvent{Type: EventTwilioStopped}
    case "mark":
        h.events <- SessionEvent{Type: EventTwilioMark, Label: event.Mark.Name}
    }
}
```

### 2.4 Audio Framing

Twilio sends 20ms frames at 8kHz u-law = 160 bytes per frame, arriving every 20ms (50 frames/sec). The Go reader must never block processing -- if decoding or downstream pipeline falls behind, drop frames rather than backpressure Twilio. Twilio does not retransmit dropped audio.

### 2.5 StreamSid vs CallSid

- `CallSid` (`CAxxx`): Identifies the PSTN call leg. Stable for full call duration.
- `StreamSid` (`MZxxx`): Identifies the media stream. One per call. Used for outbound media routing.
- The Go Gateway uses `CallSid` as the session key. `StreamSid` is only used for outbound media events.

---

## 3. OpenAI Realtime WebSocket Lifecycle

### 3.1 Connection Establishment

```
Go Gateway                                          OpenAI
    │                                                   │
    │── WebSocket connect ──────────────────────────────→│
    │   wss://api.openai.com/v1/realtime?model=gpt-4o-realtime-preview
    │   Headers: Authorization: Bearer sk-..., Openai-Beta: realtime=v1
    │                                                   │
    │←── session.created ───────────────────────────────│
    │   { session: { id, object, model, ... } }         │
    │                                                   │
    │── session.update ─────────────────────────────────→│
    │   { instructions, voice, input_audio_format,       │
    │     output_audio_format, turn_detection, tools }   │
    │                                                   │
    │←── session.updated ───────────────────────────────│
```

### 3.2 Session Configuration

```go
sessionConfig := map[string]interface{}{
    "type": "session.update",
    "session": {
        "modalities":          []string{"text", "audio"},
        "instructions":        systemPrompt,
        "voice":               "alloy",
        "input_audio_format":  "pcm16",
        "output_audio_format": "pcm16",
        "input_audio_transcription": nil,
        "turn_detection": map[string]interface{}{
            "type":                  "server_vad",
            "threshold":             0.5,
            "prefix_padding_ms":     300,
            "silence_duration_ms":   500,
        },
        "tools":                 activeTools,
        "tool_choice":           "auto",
        "temperature":           0.7,
        "max_response_output_tokens": "inf",
    },
}
```

### 3.3 Audio Flow

```
Caller speech arrives at Twilio
  → u-law 8kHz 20ms frames → Go decodes to PCM16 8kHz
  → Resampled to PCM16 24kHz → appended to buffer
  → OpenAI input_audio_buffer.append (base64-encoded)
  → OpenAI VAD detects end of turn → starts processing
  → OpenAI response.audio.delta events stream back
  → Go receives PCM16 24kHz → resamples to 8kHz
  → Encodes to u-law → sends to Twilio as outbound media
```

### 3.4 Event Types Handled

| Event | Direction | Action |
|-------|-----------|--------|
| `session.created` | OpenAI to Go | Store session ID, send config |
| `session.updated` | OpenAI to Go | Log, proceed |
| `input_audio_buffer.speech_started` | OpenAI to Go | Cancel current AI speech (barge-in) |
| `input_audio_buffer.speech_stopped` | OpenAI to Go | Reset silence timer |
| `response.audio.delta` | OpenAI to Go | Decode, resample, send to Twilio |
| `response.audio.done` | OpenAI to Go | AI finished speaking |
| `response.text.delta` | OpenAI to Go | Accumulate transcript for logging |
| `response.done` | OpenAI to Go | Process any tool calls from response |
| `response.function_call_arguments.done` | OpenAI to Go | Full tool call arguments received |
| `error` | OpenAI to Go | Handle error, attempt recovery |
| `input_audio_buffer.committed` | OpenAI to Go | Audio buffer submitted for processing |

### 3.5 Sending Audio

```go
func (s *Session) sendAudioToOpenAI(pcm []byte) error {
    msg := map[string]interface{}{
        "type":  "input_audio_buffer.append",
        "audio": base64.StdEncoding.EncodeToString(pcm),
    }
    return s.openaiConn.WriteJSON(msg)
}

func (s *Session) commitAudioBuffer() error {
    msg := map[string]interface{}{
        "type": "input_audio_buffer.commit",
    }
    return s.openaiConn.WriteJSON(msg)
}
```

### 3.6 Connection Teardown

```
Call ends (either side)
  → Go sends: {"type": "input_audio_buffer.clear"}
  → Go closes OpenAI WS with normal closure code (1000)
  → Session object transitions to CLEANUP state
  → Final metrics written to Redis
```

---

## 4. Audio Transcoding Architecture

### 4.1 The Pipeline

```
PSTN (G.711 u-law)                                       OpenAI (PCM16)
    8kHz, mono                                            24kHz, mono

  ┌──────────────┐     ┌──────────────┐     ┌──────────────┐
  │ u-law Decode │────→│ Resample     │────→│ Base64       │────→ OpenAI WS
  │ (lossy)      │     │ 8k to 24k   │     │ Encode       │
  └──────────────┘     └──────────────┘     └──────────────┘

  ┌──────────────┐     ┌──────────────┐     ┌──────────────┐
  │ u-law Encode │←────│ Resample     │←────│ Base64       │←── OpenAI WS
  │ (lossy)      │     │ 24k to 8k   │     │ Decode       │
  └──────────────┘     └──────────────┘     └──────────────┘
```

### 4.2 Quality Tradeoffs

| Quality Loss Point | Severity | Mitigation |
|-------------------|----------|------------|
| PSTN itself (300-3400 Hz) | Inherent | None -- phone network limitation |
| u-law encoding | Low | Inherent to G.711, ~38dB SNR |
| u-law to PCM decode | None | Perfect reconstruction of u-law compressed signal |
| 8kHz to 24kHz resample | Depends on algorithm | Use polyphase FIR, not linear interpolation (see SS5) |
| 24kHz to 8kHz resample | Low | Downsampling preserves spectral content up to 4kHz |

### 4.3 Frame Sizes

| Stage | Sample Rate | Frame Duration | Bytes Per Frame |
|-------|------------|----------------|-----------------|
| Twilio inbound (u-law) | 8,000 Hz | 20 ms | 160 |
| PCM intermediate | 8,000 Hz | 20 ms | 320 (int16) |
| PCM OpenAI | 24,000 Hz | 20 ms | 960 (int16) |

### 4.4 Zero-Copy Implementation

```go
var framePool = sync.Pool{
    New: func() interface{} {
        buf := make([]byte, 960) // largest frame size (24kHz PCM16)
        return &buf
    },
}

func (p *AudioPipeline) processInbound(mulaw []byte) []byte {
    bufPtr := framePool.Get().(*[]byte)
    buf := *bufPtr

    // Step 1: u-law decode to PCM16 8kHz (in-place where possible)
    mulawToPCM16(mulaw, buf[:320])

    // Step 2: Resample 8kHz to 24kHz
    result := p.resampler.Upsample(buf[:320], buf[320:960])

    return result
}
```

### 4.5 u-law to PCM16 Conversion

```go
var mulawToPCM16Table [256]int16

func init() {
    for i := 0; i < 256; i++ {
        mulawToPCM16Table[i] = decodeMulaw(byte(i))
    }
}

func mulawToPCM16(mulaw, pcm16 []byte) {
    for i := 0; i < len(mulaw); i++ {
        sample := mulawToPCM16Table[mulaw[i]]
        binary.LittleEndian.PutUint16(pcm16[i*2:], uint16(sample))
    }
}

func pcm16ToMulaw(pcm16, mulaw []byte) {
    for i := 0; i < len(pcm16); i += 2 {
        sample := int16(binary.LittleEndian.Uint16(pcm16[i:]))
        mulaw[i/2] = encodeMulaw(sample)
    }
}
```

---

## 5. Proper Audio Resampling Approach

### 5.1 Why Not Linear Interpolation

Linear interpolation introduces aliasing artifacts when upsampling. At 8kHz to 24kHz (3x integer ratio), the images of the original spectrum appear at multiples of 8kHz. Without an anti-imaging (lowpass) filter, these aliases fold back into the audible band, creating metallic/tinny artifacts.

### 5.2 Recommended Approach: Polyphase FIR

Use a polyphase FIR filter for integer-ratio resampling. For 3x upsampling:
- Design an FIR lowpass filter with cutoff at 4kHz (Nyquist of 8kHz source)
- Decompose into 3 polyphase subfilters
- For each input sample, compute 3 output samples by applying each subfilter

**Library**: Custom implementation (no CGo dependency). Pure Go, zero allocations on hot path.

### 5.3 Custom Resampler

```go
type PolyphaseResampler struct {
    taps      []float64      // prototype lowpass filter
    phases    [][]float64    // 3 polyphase subfilters (for upsample)
    delayLine []float64      // FIFO for FIR history
    delayIdx  int
}

func NewResampler() *PolyphaseResampler {
    // 48-tap Kaiser window FIR, cutoff 4kHz, stopband 5.3kHz
    // 48 taps = 2ms latency at 24kHz output
    taps := designKaiserFilter(48, 4000.0/24000.0)
    r := &PolyphaseResampler{
        taps:      taps,
        phases:    decomposePolyphase(taps, 3),
        delayLine: make([]float64, len(taps)/3),
    }
    return r
}

// Upsample8to24 converts 160-sample frame to 480-sample frame
func (r *PolyphaseResampler) Upsample8to24(in []float64, out []float64) {
    outIdx := 0
    for _, sample := range in {
        r.delayLine[r.delayIdx] = sample
        r.delayIdx = (r.delayIdx + 1) % len(r.delayLine)

        for phase := 0; phase < 3; phase++ {
            out[outIdx] = r.applyPhase(phase)
            outIdx++
        }
    }
}

// Downsample24to8 converts 480-sample frame to 160-sample frame
func (r *PolyphaseResampler) Downsample24to8(in []float64, out []float64) {
    filtered := make([]float64, len(in))
    // Apply lowpass anti-aliasing before decimation by 3
    for i := 0; i < len(out); i++ {
        out[i] = filtered[i*3]
    }
}
```

### 5.4 Latency Budget

| Step | Latency |
|------|---------|
| u-law decode (160 samples) | ~0.01 ms |
| Resample 8k to 24k (polyphase, 48 taps) | ~0.05 ms |
| Base64 encode | ~0.01 ms |
| OpenAI network round trip (p50) | 400 ms |
| OpenAI processing (p50) | 300 ms |
| Base64 decode | ~0.01 ms |
| Resample 24k to 8k | ~0.05 ms |
| u-law encode | ~0.01 ms |
| **Total end-to-end (p50)** | **~700 ms** |

### 5.5 Fallback: If OpenAI Supports 8kHz

Monitor OpenAI changelog for native 8kHz audio format support. If added, eliminate resampling entirely:

```go
sessionConfig["session"]["input_audio_format"] = "pcm16_8khz"
sessionConfig["session"]["output_audio_format"] = "pcm16_8khz"
```

---

## 6. Session Lifecycle Management

### 6.1 Session States (Meta-State Machine)

```
                    ┌─────────────┐
                    │  CREATED    │ ← WebSocket accepted
                    └──────┬──────┘
                           │ OpenAI session established
                    ┌──────▼──────┐
                    │  CONNECTING │
                    └──────┬──────┘
                           │ session.updated received
                    ┌──────▼──────┐
            ┌───────│   ACTIVE    │───────┐
            │       └──────┬──────┘       │
            │              │               │
    ┌───────▼──────┐ ┌────▼─────┐ ┌───────▼──────┐
    │  RECONNECTING│ │ TRANSFER │ │   ENDING     │
    └───────┬──────┘ └────┬─────┘ └───────┬──────┘
            │              │               │
            │         ┌────▼─────┐         │
            └─────────│  ACTIVE  │─────────┘
                      └──────────┘
                              │
                      ┌───────▼──────┐
                      │   CLEANUP    │ ← 30s max
                      └──────────────┘
```

### 6.2 Session Struct

```go
type Session struct {
    ID            string          // CallSid
    MetaState     MetaState       // CREATED | CONNECTING | ACTIVE | RECONNECTING | ENDING | CLEANUP
    TwilioConn    *websocket.Conn
    OpenAIConn    *websocket.Conn
    OpenAISessID  string          // OpenAI session ID
    Conversation  *ConversationStateMachine
    AudioPipeline *AudioPipeline
    RedisClient   *redis.Client
    Config        *TenantConfig   // loaded from Redis at session start

    // Channels
    audioIn       chan []byte         // Twilio to OpenAI
    audioOut      chan []byte         // OpenAI to Twilio
    events        chan SessionEvent
    commands      chan SupervisorCmd
    toolResults   chan ToolResult

    // Timing
    createdAt     time.Time
    lastActivity  time.Time
    silenceStart  time.Time
    callDuration  time.Duration

    // Metrics
    inputAudioSecs  float64
    outputAudioSecs float64
    textTokensIn    int
    textTokensOut   int
    toolCallsMade   int

    // Cancellation
    ctx    context.Context
    cancel context.CancelFunc

    // Reconnection
    reconnectCount int
    maxReconnects  int // default 3
}
```

### 6.3 Session Creation Flow

```go
func NewSession(callSid string, twilioConn *websocket.Conn, redis *redis.Client, cfg *TenantConfig) *Session {
    ctx, cancel := context.WithTimeout(context.Background(), 35*time.Minute)

    s := &Session{
        ID:           callSid,
        MetaState:    MetaStateCreated,
        TwilioConn:   twilioConn,
        RedisClient:  redis,
        Config:       cfg,
        audioIn:      make(chan []byte, 8),
        audioOut:     make(chan []byte, 8),
        events:       make(chan SessionEvent, 16),
        commands:     make(chan SupervisorCmd, 8),
        toolResults:  make(chan ToolResult, 4),
        createdAt:    time.Now(),
        maxReconnects: 3,
        ctx:           ctx,
        cancel:        cancel,
    }

    s.Conversation = NewConversationStateMachine(cfg)
    s.AudioPipeline = NewAudioPipeline()
    return s
}
```

### 6.4 Session Teardown

```go
func (s *Session) cleanup() {
    s.MetaState = MetaStateCleaningUp

    // 1. Stop audio pipeline
    close(s.audioIn)
    close(s.audioOut)

    // 2. Close WebSockets gracefully
    s.OpenAIConn.WriteMessage(websocket.CloseMessage,
        websocket.FormatCloseMessage(websocket.CloseNormalClosure, "call ended"))
    s.OpenAIConn.Close()
    s.TwilioConn.Close()

    // 3. Write final session state to Redis
    s.persistSession()

    // 4. Emit metrics
    s.emitMetrics()

    // 5. Cancel context (stops all goroutines)
    s.cancel()
}
```

### 6.5 Session TTL

- Redis session key TTL: 35 minutes (covers max 30-min call + 5-min buffer)
- Call hard timeout: 30 minutes. At 29:30, inject "wrapping up" message. At 30:00, call ends.
- Idle timeout: 8 minutes of no caller speech. At 7:00, prompt "Are you still there?" At 8:00, call ends.

---

## 7. Redis Session State Design

### 7.1 Key Schema

```
# Per-call session state (primary)
call:session:{callSid} → JSON {
    callSid, tenantId, restaurantId, phoneFrom, phoneTo,
    metaState, convState, conversationHistory[], toolCallHistory[],
    inputAudioSecs, outputAudioSecs, textTokensIn, textTokensOut,
    createdAt, lastActivity, callDuration
}
TTL: 35 minutes

# Active call index (for monitoring, drain operations)
call:active → SET of {callSid}
calls:active:{tenantId} → SET of {callSid}

# Tenant configuration (cached from NestJS, refreshed on change)
tenant:config:{tenantId} → JSON {
    voice, greeting, businessHours, maxPartySize, enableSmsConfirmations, ...
}
TTL: 1 hour

# OpenAI session mapping (for reconnection)
call:openai_session:{callSid} → STRING {openaiSessionId}
TTL: 35 minutes

# Rate limit tracking
ratelimit:openai:minute → STRING (counter)
ratelimit:calls:tenant:{tenantId}:hour → STRING (counter)

# Tool call audit log (permanent)
call:tool_audit:{callSid} → LIST of JSON {tool, args, result, timestamp, hmac}
TTL: 90 days (GDPR retention)

# Conversation transcript (permanent)
call:transcript:{callSid} → LIST of JSON {role, content, timestamp}
TTL: 90 days (GDPR retention)
```

### 7.2 Example Session State JSON

```json
{
  "callSid": "CA1234567890abcdef",
  "tenantId": "tenant_01",
  "restaurantId": "rest_123",
  "phoneFrom": "+442071234567",
  "phoneTo": "+442070000000",
  "metaState": "ACTIVE",
  "convState": "COLLECT_BOOKING_DETAILS",
  "conversationHistory": [
    {"role": "assistant", "content": "Good evening, thank you for calling Bella Roma. How can I help you today?"},
    {"role": "user", "content": "I'd like to book a table for tonight please."},
    {"role": "assistant", "content": "I'd be happy to help you book a table. How many people will be dining?"}
  ],
  "toolCallHistory": [
    {"tool": "check_availability", "args": {"date":"2026-05-22","partySize":4,"time":"19:00"}, "result": {"available":true,"slots":["19:00","19:15"]}, "timestamp": "2026-05-22T18:05:23Z"},
    {"tool": "create_booking", "args": {"date":"2026-05-22","time":"19:00","partySize":4,"name":"James","phone":"+442071234567"}, "result": {"success":true,"bookingRef":"BK-12345"}, "timestamp": "2026-05-22T18:05:45Z"}
  ],
  "inputAudioSecs": 45.2,
  "outputAudioSecs": 23.8,
  "textTokensIn": 450,
  "textTokensOut": 320,
  "createdAt": "2026-05-22T18:04:00Z",
  "lastActivity": "2026-05-22T18:05:45Z"
}
```

### 7.3 Redis Operations Pattern

```go
// Atomic state transition with Lua scripting
const transitionStateScript = `
local key = KEYS[1]
local newState = ARGV[1]
local expectedState = ARGV[2]
local current = redis.call('HGET', key, 'convState')
if current == expectedState then
    redis.call('HSET', key, 'convState', newState)
    return 1
end
return 0
`

result, err := redis.Eval(transitionStateScript,
    []string{fmt.Sprintf("call:session:%s", callSid)},
    "COLLECT_BOOKING_DETAILS",  // new state
    "GREETING",                  // expected current state
).Int()
```

### 7.4 Redis Client Configuration

```go
func NewRedisClient(cfg RedisConfig) *redis.Client {
    return redis.NewClient(&redis.Options{
        Addr:         cfg.Addr,
        Password:     cfg.Password,
        DB:           0,
        PoolSize:     20,
        MinIdleConns: 5,
        MaxRetries:   3,
        DialTimeout:  2 * time.Second,
        ReadTimeout:  500 * time.Millisecond,
        WriteTimeout: 500 * time.Millisecond,
        PoolTimeout:  1 * time.Second,
    })
}
```

---

## 8. Conversation State Machine Implementation

### 8.1 State Definitions

```
┌──────────┐
│ GREETING │ ← Initial state. AI answers phone with restaurant greeting.
└────┬─────┘
     │ intent detected (from AI natural language understanding)
     ▼
┌──────────────┐     ┌──────────────┐
│ FAQ_ANSWER   │     │ TRANSFER     │
│ (temporary)  │     │ (final)      │
└──────┬───────┘     └──────────────┘
       │ return to previous state after answer
       ▼
┌──────────────────────┐
│ COLLECT_BOOKING_DETAILS │ ← Sequential: party_size → date → time → name
└──────────┬───────────┘
           │ all fields collected
           ▼
┌──────────────────────┐
│ CHECK_AVAILABILITY   │ ← call tool. May loop back.
└──────────┬───────────┘
           │ available
           ▼
┌──────────────────────┐
│ CONFIRM_BOOKING      │ ← Present summary, wait for yes/no
└──────────┬───────────┘
           │ confirmed
           ▼
┌──────────────────────┐
│ CLOSING              │ ← "Your booking is confirmed. Goodbye."
└──────────┴───────────┘

Additional states:
- MODIFY_RESERVATION: Same flow as booking but pre-fill from reference
- CANCEL_RESERVATION: Confirm cancellation, call cancel tool
- HUMAN_TRANSFER: Initiate Twilio transfer, update state
- HANDLE_UNAVAILABLE: Offer alternatives, suggest different time
```

### 8.2 State Machine Implementation

```go
type ConversationState string

const (
    StateGreeting              ConversationState = "GREETING"
    StateFAQAnswer             ConversationState = "FAQ_ANSWER"
    StateCollectBookingDetails ConversationState = "COLLECT_BOOKING_DETAILS"
    StateCheckAvailability     ConversationState = "CHECK_AVAILABILITY"
    StateConfirmBooking        ConversationState = "CONFIRM_BOOKING"
    StateModifyReservation     ConversationState = "MODIFY_RESERVATION"
    StateCancelReservation     ConversationState = "CANCEL_RESERVATION"
    StateHumanTransfer         ConversationState = "HUMAN_TRANSFER"
    StateHandleUnavailable     ConversationState = "HANDLE_UNAVAILABLE"
    StateClosing               ConversationState = "CLOSING"
)

type ConversationStateMachine struct {
    current        ConversationState
    previous       ConversationState
    bookingData    BookingData
    faqReturnState ConversationState
    config         *TenantConfig
}

type BookingData struct {
    PartySize int       `json:"partySize"`
    Date      string    `json:"date"`
    Time      string    `json:"time"`
    Name      string    `json:"name"`
    Phone     string    `json:"phone"`
    Email     string    `json:"email,omitempty"`
    Notes     string    `json:"notes,omitempty"`
    Reference string    `json:"reference,omitempty"`
}
```

### 8.3 State-Scoped Tool Availability

```go
func (sm *ConversationStateMachine) AvailableTools() []Tool {
    switch sm.current {
    case StateGreeting:
        return []Tool{} // No tools, AI just talks
    case StateFAQAnswer:
        return []Tool{ToolGetFAQ}
    case StateCollectBookingDetails:
        return []Tool{} // AI collects data, no tools yet
    case StateCheckAvailability:
        return []Tool{ToolCheckAvailability}
    case StateConfirmBooking:
        return []Tool{ToolCreateBooking, ToolCancelFlow}
    case StateModifyReservation:
        return []Tool{ToolLookupReservation, ToolModifyBooking, ToolCancelBooking}
    case StateCancelReservation:
        return []Tool{ToolLookupReservation, ToolCancelBooking}
    case StateHumanTransfer:
        return []Tool{ToolTransferCall}
    case StateHandleUnavailable:
        return []Tool{ToolCheckAvailability, ToolSuggestAlternativeTimes}
    case StateClosing:
        return []Tool{}
    default:
        return []Tool{}
    }
}
```

### 8.4 State Transition Validation

```go
func (sm *ConversationStateMachine) Transition(newState ConversationState) error {
    if !isValidTransition(sm.current, newState) {
        return fmt.Errorf("invalid state transition: %s to %s", sm.current, newState)
    }
    sm.previous = sm.current
    sm.current = newState
    return nil
}

func isValidTransition(from, to ConversationState) bool {
    transitions := map[ConversationState][]ConversationState{
        StateGreeting:              {StateFAQAnswer, StateCollectBookingDetails, StateHumanTransfer, StateClosing},
        StateFAQAnswer:             {},
        StateCollectBookingDetails: {StateCheckAvailability, StateHumanTransfer, StateModifyReservation, StateCancelReservation, StateFAQAnswer},
        StateCheckAvailability:     {StateConfirmBooking, StateHandleUnavailable, StateCollectBookingDetails},
        StateConfirmBooking:        {StateClosing, StateCollectBookingDetails},
        StateModifyReservation:     {StateCheckAvailability, StateConfirmBooking, StateClosing},
        StateCancelReservation:     {StateClosing, StateGreeting},
        StateHumanTransfer:         {StateClosing},
        StateHandleUnavailable:     {StateCollectBookingDetails, StateClosing},
        StateClosing:               {},
    }
    for _, allowed := range transitions[from] {
        if to == allowed { return true }
    }
    return false
}
```

### 8.5 Anti-Hallucination Guardrails

```go
func (s *Session) verifyResponseGuardrails() error {
    // 1. AI can NEVER transition state on its own
    //    Only tool call results trigger state transitions

    // 2. AI can NEVER confirm a booking unless create_booking returned success
    if s.Conversation.current == StateConfirmBooking {
        lastTool := s.lastBookingToolCall()
        if lastTool == nil || lastTool.Name != "create_booking" || !lastTool.Result.Success {
            return fmt.Errorf("guardrail: AI attempted to confirm booking without successful create_booking")
        }
    }

    // 3. AI cannot use tools outside current state scope
    for _, tc := range s.pendingToolCalls {
        if !sm.isToolAvailable(tc.Name) {
            return fmt.Errorf("guardrail: AI called tool %s outside scope in state %s",
                tc.Name, s.Conversation.current)
        }
    }
    return nil
}
```

### 8.6 AI Prompt Injection by State

```go
func (sm *ConversationStateMachine) BuildSystemPrompt() string {
    base := sm.config.BasePrompt

    var statePrompt string
    switch sm.current {
    case StateGreeting:
        statePrompt = `You have just answered the phone. Greet warmly. Detect intent: booking, modification, cancellation, FAQ, or speak-to-human. Do not ask for details yet.`

    case StateCollectBookingDetails:
        fields := sm.missingFields()
        statePrompt = fmt.Sprintf(`Collecting booking details. Still needed: %s. Ask one at a time, conversationally. Current: party_size=%d, date=%s, time=%s, name=%s`,
            strings.Join(fields, ", "), sm.bookingData.PartySize, sm.bookingData.Date, sm.bookingData.Time, sm.bookingData.Name)

    case StateCheckAvailability:
        statePrompt = fmt.Sprintf(`Call check_availability with party_size=%d, date=%s, time=%s. Report naturally.`,
            sm.bookingData.PartySize, sm.bookingData.Date, sm.bookingData.Time)

    case StateConfirmBooking:
        statePrompt = fmt.Sprintf(`Booking ready: %d people, %s at %s, Name: %s. Ask caller to confirm. Only call create_booking when they say yes. Do NOT say "booked" until create_booking returns success.`,
            sm.bookingData.PartySize, sm.bookingData.Date, sm.bookingData.Time, sm.bookingData.Name)
    }
    return base + "\n\n" + statePrompt
}
```

---

## 9. Tool Calling Architecture

### 9.1 Design Principle

**AI never owns business truth.** All tools execute on the NestJS backend. The Go gateway is a pass-through and validation layer. It relays tool calls from OpenAI to NestJS, verifies responses, and enforces HMAC integrity.

### 9.2 Tool Call Flow

```
OpenAI Realtime → "I'll check availability..."
  → response.function_call_arguments.done
  → Go parses tool call, validates against state scope
  → Go HMAC-signs the tool call payload
  → HTTP POST → NestJS POST /api/internal/tools/{toolName}
  → NestJS executes business logic (ResDiary API, DB queries)
  → NestJS returns structured result
  → Go validates HMAC response
  → Go feeds result back to OpenAI as function_call_output
  → OpenAI processes result, generates next response
  → Go validates response against guardrails
  → If tool result triggers state transition, Go transitions state machine
```

### 9.3 Tool Definitions (Go Side)

```go
type Tool struct {
    Name        string                 `json:"name"`
    Description string                 `json:"description"`
    Parameters  map[string]interface{} `json:"parameters"`
}

var ToolCheckAvailability = Tool{
    Name:        "check_availability",
    Description: "Check table availability for a given date, time, and party size",
    Parameters: map[string]interface{}{
        "type": "object",
        "properties": map[string]interface{}{
            "date":      map[string]interface{}{"type": "string", "description": "Date in YYYY-MM-DD format"},
            "time":      map[string]interface{}{"type": "string", "description": "Time in HH:MM 24-hour format"},
            "partySize": map[string]interface{}{"type": "integer", "description": "Number of guests"},
        },
        "required": []string{"date", "time", "partySize"},
    },
}

var ToolCreateBooking = Tool{
    Name:        "create_booking",
    Description: "Create a confirmed table booking",
    Parameters: map[string]interface{}{
        "type": "object",
        "properties": map[string]interface{}{
            "date":      map[string]interface{}{"type": "string"},
            "time":      map[string]interface{}{"type": "string"},
            "partySize": map[string]interface{}{"type": "integer"},
            "name":      map[string]interface{}{"type": "string"},
            "phone":     map[string]interface{}{"type": "string"},
            "email":     map[string]interface{}{"type": "string"},
            "notes":     map[string]interface{}{"type": "string"},
        },
        "required": []string{"date", "time", "partySize", "name", "phone"},
    },
}
```

### 9.4 HMAC Signing

```go
type ToolCallRequest struct {
    CallSid    string                 `json:"callSid"`
    TenantID   string                 `json:"tenantId"`
    ToolName   string                 `json:"toolName"`
    Arguments  map[string]interface{} `json:"arguments"`
    Signature  string                 `json:"signature"`
    Timestamp  int64                  `json:"timestamp"`
}

func signToolCall(req ToolCallRequest, secret string) string {
    payload := fmt.Sprintf("%s:%s:%s:%d", req.CallSid, req.TenantID, req.ToolName, req.Timestamp)
    mac := hmac.New(sha256.New, []byte(secret))
    mac.Write([]byte(payload))
    return hex.EncodeToString(mac.Sum(nil))
}
```

NestJS verifies the HMAC on every `/api/internal/tools/*` endpoint. Rejects calls where signature doesn't match, timestamp is more than 30 seconds old (replay protection), or tenant ID doesn't match a valid tenant.

### 9.5 Tool Result Handling

```go
type ToolResult struct {
    Success      bool                   `json:"success"`
    Data         map[string]interface{} `json:"data,omitempty"`
    Error        string                 `json:"error,omitempty"`
    Alternatives []string               `json:"alternatives,omitempty"`
}

func (s *Session) feedToolResultToOpenAI(callID string, result ToolResult) error {
    output, _ := json.Marshal(result)
    msg := map[string]interface{}{
        "type": "conversation.item.create",
        "item": map[string]interface{}{
            "type":    "function_call_output",
            "call_id": callID,
            "output":  string(output),
        },
    }
    return s.OpenAIConn.WriteJSON(msg)
}
```

### 9.6 Tool Call Timeouts

| Tool | Timeout | On Timeout |
|------|---------|------------|
| `check_availability` | 5s | Inject "one moment please" filler, retry once |
| `create_booking` | 10s | Inject "confirming your booking, bear with me" |
| `modify_booking` | 10s | Same as create |
| `cancel_booking` | 10s | Same as create |
| `lookup_reservation` | 5s | Retry once, then "I'm having trouble finding it" |
| `get_faq` | 2s | Fall back to OpenAI answering from own knowledge |
| `transfer_call` | 15s | Twilio transfer -- callback-driven, longer timeout |

During tool call timeout, Go injects filler audio: a short "let me check that for you" message to prevent dead air.

---

## 10. Backend API Boundaries

### 10.1 API Architecture

```
Go Gateway (internal client)        External (Twilio, Next.js)
          │                                    │
          ▼                                    ▼
┌─────────────────────────────────────────────────────┐
│                   NestJS + Fastify                   │
│                                                     │
│  ┌───────────────────┐  ┌─────────────────────────┐ │
│  │ Internal API      │  │ Public API              │ │
│  │ /api/internal/*   │  │ /api/public/*           │ │
│  │ (HMAC auth)       │  │ (JWT auth)              │ │
│  └────────┬──────────┘  └──────────┬──────────────┘ │
│           │                        │                 │
│  ┌────────┴────────────────────────┴──────────────┐ │
│  │              Service Layer                      │ │
│  │  BookingService  │  FAQService  │  TenantService│ │
│  └──────────────────────┬─────────────────────────┘ │
│                         │                            │
│  ┌──────────────────────┴─────────────────────────┐ │
│  │              Adapter Layer                       │ │
│  │  ResDiaryAdapter  │  TwilioAdapter  │  SMSAdapter│ │
│  └──────────────────────┬─────────────────────────┘ │
│                         │                            │
│                    Supabase                           │
└─────────────────────────────────────────────────────┘
```

### 10.2 Internal API Endpoints (Go to NestJS)

All internal endpoints require `X-HMAC-Signature` and `X-Call-Sid` headers.

```
POST   /api/internal/tools/check-availability
POST   /api/internal/tools/create-booking
POST   /api/internal/tools/modify-booking
POST   /api/internal/tools/cancel-booking
POST   /api/internal/tools/lookup-reservation
POST   /api/internal/tools/transfer-call
GET    /api/internal/tools/faq?query={query}
GET    /api/internal/tenants/{tenantId}/config
POST   /api/internal/sessions/{callSid}/complete
POST   /api/internal/sessions/{callSid}/metrics
GET    /api/internal/health
```

### 10.3 Public API Endpoints (Twilio, Next.js Frontend)

```
POST   /api/public/voice/webhook           ← Twilio inbound call webhook
POST   /api/public/voice/status-callback   ← Twilio call status updates
POST   /api/public/sms/status-callback     ← Twilio SMS delivery status
GET    /api/public/tenants/{slug}          ← Public restaurant info
```

### 10.4 NestJS Module Structure

```
src/
├── main.ts
├── app.module.ts
├── modules/
│   ├── voice/            # Twilio webhook handlers
│   ├── tools/            # Internal API tool endpoints + HMAC guard
│   ├── booking/          # Booking CRUD + ResDiary adapter
│   ├── tenants/          # Tenant config CRUD
│   ├── sessions/         # Session completion + metrics
│   ├── sms/              # Twilio SMS sending
│   ├── queue/            # BullMQ setup + processors
│   └── webhooks/         # ResDiary callback, Twilio
├── adapters/
│   └── resdiary/         # ResDiary API client
├── common/
│   ├── guards/           # HMAC guard
│   ├── decorators/
│   ├── filters/
│   └── interceptors/
└── shared/
    └── types/            # Shared TypeScript types
```

### 10.5 NestJS Controller Example

```typescript
@Controller('api/internal/tools')
export class ToolsController {
  constructor(
    private readonly toolsService: ToolsService,
    private readonly sessionService: SessionService,
  ) {}

  @Post('check-availability')
  @UseGuards(HmacGuard)
  async checkAvailability(@Body() body: ToolCallRequest): Promise<ToolResult> {
    const args = body.arguments as CheckAvailabilityArgs;
    if (!args.date || !args.time || !args.partySize) {
      return { success: false, error: 'Missing required fields' };
    }

    const result = await this.toolsService.checkAvailability(
      body.tenantId, args.date, args.time, args.partySize
    );

    await this.sessionService.recordToolCall(body.callSid, {
      tool: 'check_availability', args, result, timestamp: new Date(),
    });

    return { success: true, data: result };
  }
}
```

---

## 11. Interruption / Barge-In Handling

### 11.1 Architecture

Barge-in is driven by OpenAI's server-side VAD. When the caller starts speaking while the AI is mid-response:

```
AI is speaking (OpenAI streaming audio to Go to Twilio to caller)
       │
       ▼ Caller starts talking
Twilio sends inbound audio to Go
       │
       ▼ Go forwards to OpenAI input_audio_buffer.append
       │  NOTE: Go does NOT stop forwarding outbound AI audio at this point.
       │  OpenAI handles the overlap.
       │
       ▼ OpenAI VAD detects speech_started in input buffer
OpenAI emits: input_audio_buffer.speech_started
       │
       ▼ Go receives speech_started event
Go sends: response.cancel to OpenAI
       │
       ▼ OpenAI stops streaming response audio immediately
OpenAI emits: response.done (status: "cancelled")
       │
       ▼ Go stops sending audio to Twilio, flushes outbound buffer
       │
       ▼ Caller finishes speaking
OpenAI VAD detects silence → speech_stopped
       │
       ▼ OpenAI processes new user turn
New response begins
```

### 11.2 Go Implementation

```go
func (s *Session) handleBargeIn() {
    // 1. Cancel current AI response
    cancelMsg := map[string]interface{}{"type": "response.cancel"}
    s.OpenAIConn.WriteJSON(cancelMsg)

    // 2. Clear the outbound audio buffer
    s.AudioPipeline.FlushOutbound()

    // 3. Mark barge-in for metrics
    s.bargeInCount++
    log.Info("Barge-in detected", "callSid", s.ID, "count", s.bargeInCount)
}
```

### 11.3 Twilio-Side Audio Management

When barge-in occurs, Go must stop sending `media` events to Twilio's outbound track:

```go
func (s *Session) writeOutboundAudio(frame []byte) {
    if s.bargingIn {
        s.bargeInDroppedFrames++
        return // Drop frame
    }
    msg := TwilioMediaEvent{
        Event: "media",
        Media: MediaPayload{
            Track: "outbound", Chunk: s.nextChunkID(),
            Timestamp: s.nextTimestamp(),
            Payload: base64.StdEncoding.EncodeToString(frame),
        },
        StreamSid: s.streamSid,
    }
    s.TwilioConn.WriteJSON(msg)
}
```

### 11.4 Graceful Resumption

After barge-in, the AI's new response should acknowledge the interruption naturally. The system prompt includes:

```
If the caller interrupted your previous response, acknowledge what they said
naturally. Do not apologize for being interrupted -- just respond directly to
what they just said. Do not finish your previous sentence.
```

---

## 12. Silence Detection Strategy

### 12.1 Dual-Layer Approach

| Layer | Mechanism | Timeout | Action |
|-------|-----------|---------|--------|
| OpenAI VAD (server-side) | `turn_detection` with `silence_duration_ms: 500` | 500ms of caller silence | End of turn -- process and respond |
| Go custom timer (client-side) | Time since last `speech_stopped` event | 8s long silence | AI prompts "Are you still there?" |
| Go custom timer (client-side) | Time since last `speech_stopped` event | 15s long silence | End call |

### 12.2 Why Both?

OpenAI VAD handles turn-taking (short silences between sentences). The Go custom timer handles the case where the caller has walked away, put the phone down, or the line has gone dead. OpenAI VAD does not emit events during sustained silence -- it just waits. The Go timer fills this gap.

### 12.3 Implementation

```go
func (s *Session) silenceMonitor(ctx context.Context) {
    ticker := time.NewTicker(1 * time.Second)
    defer ticker.Stop()
    var prompted bool

    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            if s.isAISpeaking() {
                s.silenceStart = time.Time{}
                prompted = false
                continue
            }
            if s.silenceStart.IsZero() { continue }

            elapsed := time.Since(s.silenceStart)

            if elapsed > 8*time.Second && !prompted {
                s.injectSystemMessage("The caller has been silent for 8 seconds. Politely ask if they are still there.")
                prompted = true
            }
            if elapsed > 15*time.Second {
                log.Warn("Call ended due to prolonged silence", "callSid", s.ID)
                s.transitionToClosing()
                return
            }
        }
    }
}
```

### 12.4 Synchronisation with OpenAI VAD

The Go timer and OpenAI VAD operate independently. The only synchronisation point:
- On `speech_started`: Reset Go timer (caller is speaking)
- On `speech_stopped`: Start Go timer (caller may have finished)
- When AI starts responding: Reset Go timer and `prompted` flag

No conflict resolution logic is needed because the Go timer only fires when OpenAI is idle (not processing and not speaking).

---

## 13. Retry and Reconnection Handling

### 13.1 OpenAI WebSocket Drop -- Full Reconnection Flow

```
OpenAI WebSocket drops mid-call
  │
  ▼ Go detects: websocket read error or close frame
Session enters MetaStateReconnecting
  │
  ▼ Attempt 1 (immediate)
Go opens new WebSocket to OpenAI
  → session.update with previous session config
  → conversation.item.create for each turn in history (last 20 turns)
  → If call was mid-booking, inject: "The caller was in the middle of [state].
    Continue where you left off. Last thing you said: [last assistant message]."
  │
  ├─ Success → MetaStateActive, continue
  │
  └─ Fail → Wait 1s (exponential: 1s, 2s, 4s)
       │
       ▼ Attempt 2 ...
       │
       ▼ Attempt 3 (final)
       │
       ├─ Success → MetaStateActive, continue
       │
       └─ Fail → MetaStateEnding
            │
            ▼ Go plays pre-recorded audio: "I'm sorry, I'm having technical
              difficulties. Please call back or try again shortly."
            → Go sends cached audio to Twilio
            → Ends call gracefully
            → Logs full error context to Redis
```

### 13.2 Reconnection State Recovery

```go
func (s *Session) rebuildConversationState() error {
    // 1. Restore conversation history (last 20 turns)
    for _, turn := range s.Conversation.History {
        if turn.Role == "user" {
            msg := map[string]interface{}{
                "type": "conversation.item.create",
                "item": map[string]interface{}{
                    "type": "message", "role": "user",
                    "content": []map[string]interface{}{{"type": "input_text", "text": turn.Content}},
                },
            }
            s.OpenAIConn.WriteJSON(msg)
        } else {
            msg := map[string]interface{}{
                "type": "conversation.item.create",
                "item": map[string]interface{}{
                    "type": "message", "role": "assistant",
                    "content": []map[string]interface{}{{"type": "text", "text": turn.Content}},
                },
            }
            s.OpenAIConn.WriteJSON(msg)
        }
    }

    // 2. Restore tool call history (paired function_call + function_call_output)
    for _, tc := range s.Conversation.ToolCalls {
        // Create function_call item and output item
    }

    // 3. Inject recovery instruction
    msg := map[string]interface{}{
        "type": "conversation.item.create",
        "item": map[string]interface{}{
            "type": "message", "role": "system",
            "content": []map[string]interface{}{{"type": "input_text", "text": fmt.Sprintf(
                "SYSTEM: Connection temporarily interrupted. Current state: %s. Continue naturally. Do not mention the interruption.",
                s.Conversation.current,
            )}},
        },
    }
    s.OpenAIConn.WriteJSON(msg)

    // 4. Trigger response
    s.OpenAIConn.WriteJSON(map[string]interface{}{"type": "response.create"})
    return nil
}
```

### 13.3 What the Caller Hears During Reconnection

| Phase | Caller Experience |
|-------|-------------------|
| OpenAI WS drops | Silence (no AI audio playing) |
| Reconnection attempt (0-1.5s) | Silence |
| Reconnection succeeds | AI continues naturally |
| Reconnection fails (all 3 attempts) | Pre-recorded audio: "We're experiencing technical difficulties..." |

Pre-recorded WAV files (encoded as u-law) are embedded in the Go binary for fallback messages.

### 13.4 Retry Policy Summary

| Component | Max Retries | Backoff | Fallback |
|-----------|-------------|---------|----------|
| OpenAI WS reconnect | 3 | 1s, 2s, 4s | Pre-recorded message, end call |
| Tool call HTTP (NestJS) | 2 | Immediate, 1s | Return error to AI |
| Redis operations | 3 | 100ms, 200ms, 400ms | Log, continue degraded |
| NestJS health check | Continuous | 5s intervals | Circuit breaker after 3 failures |
| ResDiary API | 3 | 1s, 2s, 4s | Circuit breaker, "system unavailable" |

---

## 14. Failure Recovery Flows

### 14.1 OpenAI API Unavailable

```go
type OpenAICircuitBreaker struct {
    failures    int
    lastFailure time.Time
    state       string // CLOSED, OPEN, HALF_OPEN
}

func (cb *OpenAICircuitBreaker) AllowCall() bool {
    switch cb.state {
    case "CLOSED":
        return true
    case "OPEN":
        if time.Since(cb.lastFailure) > 60*time.Second {
            cb.state = "HALF_OPEN"
            return true
        }
        return false
    case "HALF_OPEN":
        return false
    }
}

func (cb *OpenAICircuitBreaker) RecordFailure() {
    cb.failures++
    cb.lastFailure = time.Now()
    if cb.failures >= 5 {
        cb.state = "OPEN"
        alerting.TriggerAlert("openai_unavailable", "Circuit breaker opened after 5 consecutive failures")
    }
}
```

When circuit breaker is OPEN:
- New inbound calls: Play pre-recorded "We're experiencing high call volumes. Please call back or book online at [website]."
- Active calls: Continue if already connected.

### 14.2 ResDiary API Unavailable

Tool result when ResDiary is down:
```json
{
    "success": false,
    "error": "Our booking system is temporarily unavailable",
    "alternatives": [
        "I can take your details and we'll call you back to confirm",
        "You can book online at [restaurant-website]",
        "I can transfer you to a member of staff"
    ]
}
```

The AI receives this structured error and presents alternatives to the caller conversationally. No dead air.

### 14.3 Redis Unavailable

Go Gateway behavior when Redis is unreachable:
- **Existing calls**: Continue. All session state is in-memory on the Go instance. Redis is the persistence layer, not the runtime.
- **New calls**: Accept and process. Session state stored only in memory.
- **State transitions**: Still functional (in memory). Just not persisted.
- **Tool calls**: HMAC signing is stateless (shared secret from env). Still works.
- **Tenant config**: Cached in memory from last fetch. If no cache, use defaults.

The system degrades but does not fail. Active calls survive a Redis outage.

### 14.4 NestJS Backend Unavailable

If NestJS is unreachable (tool calls fail):
- Go has a 5-second timeout on tool call HTTP requests
- On timeout: return structured error to OpenAI
- OpenAI can present appropriate message based on context
- Mid-booking: "I'm having trouble checking availability. Let me take your details and we'll confirm by SMS."
- FAQ: Fall back to OpenAI's own knowledge for simple questions

---

## 15. Queue Architecture

### 15.1 BullMQ + Redis

```
┌─────────────────────────────────────────────┐
│                  Redis                       │
│                                              │
│  ┌────────────┐  ┌────────────┐             │
│  │ Session    │  │ BullMQ     │             │
│  │ State      │  │ Queues     │             │
│  └────────────┘  └─────┬──────┘             │
│                         │                     │
└─────────────────────────┼─────────────────────┘
                          │
              ┌───────────┼───────────┐
              │           │           │
         ┌────▼────┐ ┌───▼────┐ ┌───▼──────────┐
         │  SMS    │ │Session │ │  Dead Letter  │
         │  Queue  │ │Cleanup │ │  Queue        │
         └────┬────┘ └───┬────┘ └───┬──────────┘
              │          │          │
         ┌────▼────┐     │     ┌────▼──────────┐
         │  SMS    │     │     │  Admin Alert   │
         │ Worker  │     │     │  + Retry       │
         └─────────┘     │     └───────────────┘
                    ┌────▼────┐
                    │ Session │
                    │ Worker  │
                    └─────────┘
```

### 15.2 Queue Definitions

```typescript
@Injectable()
export class QueueService {
  public smsQueue: Queue;
  public sessionCleanupQueue: Queue;
  public deadLetterQueue: Queue;

  constructor(private readonly config: ConfigService) {
    const connection = { host: config.get('REDIS_HOST'), port: config.get('REDIS_PORT') };

    this.smsQueue = new Queue('sms', { connection });
    new QueueScheduler('sms', { connection });

    this.sessionCleanupQueue = new Queue('session-cleanup', { connection });
    new QueueScheduler('session-cleanup', { connection });

    this.deadLetterQueue = new Queue('dead-letter', { connection });
  }
}
```

### 15.3 SMS Processor

```typescript
@Processor('sms')
export class SmsProcessor {
  @Process()
  async handleSms(job: Job<SmsJobData>): Promise<void> {
    try {
      await this.smsService.send({
        to: job.data.to, body: job.data.body,
        messagingServiceSid: job.data.messagingServiceSid,
      });
    } catch (error) {
      if (job.attemptsMade < 3) throw error; // BullMQ will retry
      await job.moveToFailed({ message: `SMS failed after ${job.attemptsMade} attempts` }, true);
    }
  }
}
```

### 15.4 Job Scheduling

```typescript
async function scheduleSmsConfirmation(data: SmsJobData): Promise<void> {
  await smsQueue.add('send-confirmation', data, {
    delay: 5000,         // 5s delay -- wait for call to finish naturally
    attempts: 3,
    backoff: { type: 'exponential', delay: 5000 },
    removeOnComplete: true,
    removeOnFail: 100,
  });
}
```

### 15.5 Dead Letter Handling

```typescript
@Processor('dead-letter')
export class DeadLetterProcessor {
  @Process()
  async handleDeadLetter(job: Job): Promise<void> {
    const originalQueue = job.data?.originalQueue || 'unknown';
    if (originalQueue === 'sms') {
      await this.adminAlertService.createTask({
        type: 'sms_failed_permanently', priority: 'medium',
        data: job.data?.originalJobData,
        description: `SMS permanently failed. Booking ref: ${job.data?.originalJobData?.bookingRef || 'N/A'}`,
      });
    }
  }
}
```

---

## 16. Logging and Observability

### 16.1 Structured Logging (Go)

```go
import "github.com/rs/zerolog"

func (s *Session) logger() zerolog.Logger {
    return log.With().
        Str("callSid", s.ID).
        Str("tenantId", s.Config.TenantID).
        Str("convState", string(s.Conversation.current)).
        Str("metaState", string(s.MetaState)).
        Logger()
}

// Usage:
s.logger().Info().
    Str("event", "openai_connected").
    Str("sessionId", openaiSessID).
    Dur("connectTime", time.Since(start)).
    Msg("OpenAI session established")

s.logger().Warn().
    Str("event", "barge_in").
    Int("count", s.bargeInCount).
    Msg("Caller interrupted AI speech")
```

### 16.2 Key Metrics (Prometheus)

```go
var (
    callsActive = promauto.NewGauge(prometheus.GaugeOpts{
        Name: "voxlane_calls_active", Help: "Number of active calls",
    })
    callsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
        Name: "voxlane_calls_total", Help: "Total calls handled",
    }, []string{"outcome"})
    callDuration = promauto.NewHistogram(prometheus.HistogramOpts{
        Name: "voxlane_call_duration_seconds", Help: "Call duration distribution",
        Buckets: []float64{30, 60, 90, 120, 180, 300, 600, 900, 1800},
    })
    openaiLatency = promauto.NewHistogram(prometheus.HistogramOpts{
        Name: "voxlane_openai_response_latency_ms", Help: "OpenAI first-response latency",
        Buckets: []float64{200, 300, 400, 500, 750, 1000, 1500, 2000, 5000},
    })
    openaiReconnects = promauto.NewCounter(prometheus.CounterOpts{
        Name: "voxlane_openai_reconnects_total", Help: "Total OpenAI WebSocket reconnection attempts",
    })
    audioInputSeconds = promauto.NewCounter(prometheus.CounterOpts{
        Name: "voxlane_audio_input_seconds_total", Help: "Total seconds of audio sent to OpenAI",
    })
    audioOutputSeconds = promauto.NewCounter(prometheus.CounterOpts{
        Name: "voxlane_audio_output_seconds_total", Help: "Total seconds of AI audio played to callers",
    })
    toolCallLatency = promauto.NewHistogramVec(prometheus.HistogramOpts{
        Name: "voxlane_tool_call_latency_ms", Help: "Tool call latency by tool name",
        Buckets: []float64{10, 25, 50, 100, 250, 500, 1000, 2500},
    }, []string{"tool"})
    toolCallErrors = promauto.NewCounterVec(prometheus.CounterOpts{
        Name: "voxlane_tool_call_errors_total", Help: "Tool call errors by tool name and error type",
    }, []string{"tool", "error"})
    bargeInTotal = promauto.NewCounter(prometheus.CounterOpts{
        Name: "voxlane_barge_in_total", Help: "Total barge-in events",
    })
    circuitBreakerState = promauto.NewGaugeVec(prometheus.GaugeOpts{
        Name: "voxlane_circuit_breaker_state", Help: "Circuit breaker state (0=CLOSED, 1=OPEN, 2=HALF_OPEN)",
    }, []string{"service"})
)
```

### 16.3 Health Check Endpoints

```
Go Gateway:
  GET /health          → 200 OK if accepting connections
  GET /health/ready    → 200 OK if Redis + OpenAI reachable
  GET /metrics         → Prometheus metrics endpoint

NestJS:
  GET /api/health      → 200 OK + DB + Redis status
  GET /api/metrics     → Prometheus metrics endpoint
```

### 16.4 Synthetic Call Test

A cron job runs every 15 minutes:

```typescript
@Cron('*/15 * * * *')
async syntheticCallTest() {
  const result = await this.goGatewayClient.runSyntheticTest();
  syntheticTestGauge.set(result.success ? 1 : 0);
  if (!result.success) {
    await this.alerting.alert('synthetic_call_failed', {
      error: result.error, latencyMs: result.latencyMs,
    });
  }
}
```

### 16.5 Call Detail Record (CDR)

On call completion, NestJS receives the full session metrics and stores a CDR:

```sql
CREATE TABLE call_detail_records (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL,
    call_sid VARCHAR(34) NOT NULL UNIQUE,
    phone_from VARCHAR(20),
    phone_to VARCHAR(20),
    direction VARCHAR(10),      -- 'inbound'
    status VARCHAR(20),         -- 'completed', 'transferred', 'failed', 'abandoned'
    duration_seconds INTEGER,
    ai_input_audio_seconds FLOAT,
    ai_output_audio_seconds FLOAT,
    ai_text_tokens_in INTEGER,
    ai_text_tokens_out INTEGER,
    estimated_cost_cents FLOAT,
    barge_in_count INTEGER,
    openai_reconnects INTEGER,
    conversation_state_final VARCHAR(50),
    booking_ref VARCHAR(50),
    created_at TIMESTAMPTZ DEFAULT NOW(),
    CONSTRAINT fk_tenant FOREIGN KEY (tenant_id) REFERENCES tenants(id)
);

CREATE INDEX idx_cdr_tenant_date ON call_detail_records(tenant_id, created_at);
```

---

## 17. Cost Optimisation Strategy

### 17.1 Hybrid Architecture Implementation

Per the architecture critique, pure Realtime API for the full call is economically unviable. The implementation uses a **three-phase hybrid model**:

```
┌──────────────┐    ┌──────────────────┐    ┌──────────────┐
│  Phase 1     │    │  Phase 2         │    │  Phase 3     │
│  Greeting    │    │  Data Collection │    │  Closing     │
│  (Realtime)  │───→│  (Text + TTS)    │───→│  (Realtime)  │
│  20-30s      │    │  1-2 min         │    │  20-30s      │
└──────────────┘    └──────────────────┘    └──────────────┘
   OpenAI RT          DeepSeek V4 +          OpenAI RT
   $0.30/min          ElevenLabs TTS         $0.30/min
   ~$0.15             ~$0.03/min              ~$0.15
                      ~$0.06 (2 min)
                                      Total AI cost: ~$0.36
                                      vs pure RT: ~$1.50 (for 5 min)
```

### 17.2 Phase Switching Logic

```go
func (s *Session) shouldSwitchToTextMode() bool {
    switch s.Conversation.current {
    case StateCollectBookingDetails,
         StateCheckAvailability,
         StateConfirmBooking,
         StateModifyReservation,
         StateCancelReservation:
        return true // eligible for text mode
    default:
        return false
    }
}
```

### 17.3 Text Mode Implementation

In text mode, the Go gateway:
1. Stops sending audio to OpenAI Realtime
2. Sends text transcription to DeepSeek V4 API (HTTP)
3. Receives text response
4. Sends text to ElevenLabs TTS for synthesis
5. Streams TTS audio back to Twilio

```go
func (s *Session) executeTextMode(transcript string) (string, error) {
    resp, err := s.nestjsClient.TextConversationTurn(context.Background(), TextTurnRequest{
        CallSid: s.ID, TenantID: s.Config.TenantID, Transcript: transcript,
        ConversationState: s.Conversation.current, BookingData: s.Conversation.bookingData,
        History: s.Conversation.History,
    })
    if err != nil { return "", err }

    audio, err := s.ttsClient.Synthesize(context.Background(), TTSRequest{
        Text: resp.Response, VoiceID: s.Config.TTSVoiceID, ModelID: "eleven_flash_v2_5",
    })

    for _, chunk := range audio.Chunks {
        s.audioOut <- chunk
    }
    return resp.Response, nil
}
```

### 17.4 Phase Transition Triggers

| Transition | Trigger | Latency Impact |
|------------|---------|----------------|
| Realtime to Text | Intent detected, state = COLLECT_BOOKING_DETAILS | ~300ms extra for first TTS round |
| Text to Realtime | State = CONFIRM_BOOKING or CLOSING | ~500ms for OpenAI session to start |
| Text to Realtime (barge-in) | Caller interrupts during text mode | Switch back to realtime immediately |

### 17.5 Cost Tracking Per Call

At session end, the Go gateway calculates and stores:

```go
type CallCosts struct {
    RealtimeInputAudioSecs  float64 `json:"realtimeInputAudioSecs"`
    RealtimeOutputAudioSecs float64 `json:"realtimeOutputAudioSecs"`
    RealtimeTextTokensIn    int     `json:"realtimeTextTokensIn"`
    RealtimeTextTokensOut   int     `json:"realtimeTextTokensOut"`
    TextModeTokensIn        int     `json:"textModeTokensIn"`
    TextModeTokensOut       int     `json:"textModeTokensOut"`
    TextModeTTSCharacters   int     `json:"textModeTTSCharacters"`
    TotalCostCents          float64 `json:"totalCostCents"`
}

func (s *Session) calculateCost() CallCosts {
    cc := CallCosts{
        RealtimeInputAudioSecs:  s.inputAudioSecs,
        RealtimeOutputAudioSecs: s.outputAudioSecs,
    }

    rtCost := (cc.RealtimeInputAudioSecs/60 * 0.06) +
              (cc.RealtimeOutputAudioSecs/60 * 0.24) +
              (float64(cc.RealtimeTextTokensIn)/1_000_000 * 2.50) +
              (float64(cc.RealtimeTextTokensOut)/1_000_000 * 10.00)

    textCost := (float64(cc.TextModeTokensIn)/1_000_000 * 0.14) +
                (float64(cc.TextModeTokensOut)/1_000_000 * 0.28)

    ttsCost := float64(cc.TextModeTTSCharacters) / 1000 * 0.015

    cc.TotalCostCents = (rtCost + textCost + ttsCost) * 100
    return cc
}
```

---

## 18. Token Optimisation Strategy

### 18.1 System Prompt Size Control

```go
func (sm *ConversationStateMachine) BuildSystemPrompt() string {
    identity := fmt.Sprintf("You are the AI receptionist for %s, %s cuisine at %s.",
        sm.config.RestaurantName, sm.config.CuisineType, sm.config.Address)

    stateInstructions := sm.getStateInstructions() // ~100 tokens, rotated per state

    guardrails := `
CRITICAL RULES:
- Never confirm a booking until the create_booking tool returns success.
- Never transition conversation state yourself. Only tool results change state.
- If caller asks to speak to a human, transfer immediately.
- Do not apologise excessively. Be warm but concise.`

    return identity + "\n\n" + stateInstructions + "\n\n" + guardrails
}
// Total: ~240 tokens for system prompt
```

### 18.2 Tool Definition Pruning

Only include tools relevant to the current conversation state (see SS8.3). This reduces tool definitions from ~8 tools to 1-2 tools, saving ~500 tokens per turn.

### 18.3 Conversation History Truncation

```go
func (sm *ConversationStateMachine) buildHistoryForPrompt() []Turn {
    const maxTurns = 10
    history := sm.History
    if len(history) > maxTurns {
        history = history[len(history)-maxTurns:]
    }
    return history
}
```

### 18.4 Tool Result Compression

```go
func compressToolResultForAI(result ToolResult) string {
    if result.Success {
        return formatAvailabilityBriefly(result.Data)
        // "Available: 19:00, 19:15, 19:30" instead of full JSON ~120 tokens to ~15 tokens
    }
    return fmt.Sprintf("Error: %s. Alternatives: %s",
        result.Error, strings.Join(result.Alternatives, ", "))
}
```

### 18.5 Token Budget Per Call

| Component | Budget (tokens) | Strategy |
|-----------|-----------------|----------|
| System prompt | 250 | Compact, state-rotated |
| Tool definitions | 200 | State-scoped (1-3 tools) |
| Conversation history (10 turns) | 800 | Truncated |
| Tool call output | 150 | Compressed summaries |
| AI response | 200 | Natural but concise |
| **Total per turn** | **~1,600** | |

For a 5-turn booking call: 5 x 1,600 = 8,000 text tokens + audio tokens.

---

## 19. Call Lifecycle Flow

### 19.1 End-to-End Call Flow

```
Time  Event                                    System Action
────  ─────────────────────────────────────    ──────────────────────────────
0.0s  PSTN call arrives at Twilio number
0.1s  Twilio POST /voice/webhook              NestJS returns TwiML with <Stream>
0.3s  Twilio opens WebSocket to Go Gateway    Go accepts, creates Session{}
0.4s  Session init                            Go opens OpenAI WS, sends config
0.8s  OpenAI session.updated                  Session to ACTIVE, ConvState to GREETING
1.0s  Go sends OpenAI silent audio frame       Triggers first response
1.5s  AI generates greeting audio             "Good evening, Bella Roma..."
1.7s  Go sends outbound media to Twilio       Caller hears greeting
3.0s  AI: "How can I help?"
4.0s  Caller: "Book a table for 4"            Audio flows in, decoded, resampled
4.5s  Caller finishes speaking                OpenAI VAD: silence → turn end
5.5s  AI: "Certainly! When would you like?"    ConvState → COLLECT_BOOKING_DETAILS
6.0s  Caller: "Tonight at 7"
7.0s  AI: "How many people?"
7.5s  Caller: "4 people"
8.0s  AI: "And the name?"
8.5s  Caller: "James Wilson"
9.0s  AI: "Let me check availability"          State → CHECK_AVAILABILITY
9.5s  OpenAI calls check_availability          Go HMAC-signs, POST to NestJS
10.0s NestJS → ResDiary → result               Available: 19:00, 19:15, 19:30
10.5s Go feeds result to OpenAI
11.0s AI: "We have 7pm, 7:15, or 7:30"
11.5s Caller: "7pm please"
12.0s AI: "Booking for 4 at 7pm. Confirm?"     State → CONFIRM_BOOKING
12.5s Caller: "Yes, perfect"
13.0s AI calls create_booking                  Go forwards to NestJS
13.3s NestJS → ResDiary → booking created      bookingRef: "BK-12345"
13.5s Go validates, transitions state          ConvState → CLOSING
14.0s AI: "All confirmed! Ref BK-12345."
14.5s Caller: "Thank you!"
15.0s AI: "Goodbye!"
15.2s Twilio detects caller hangup            NestJS receives status callback
15.3s NestJS queues SMS confirmation          delay: 5s
15.8s SMS sent to +442071234567               "Booking confirmed. 4 people, 7pm. Ref: BK-12345"
16.0s Session cleanup                         Go persists metrics, closes WS
16.1s Call completed
```

### 19.2 Alternative Flows

**FAQ Call** (~45s, ~$0.08 cost):
```
Greeting → "What time do you close?" → FAQ_ANSWER → "We close at 11pm"
→ "Anything else?" → "No thanks" → Closing
```

**Transfer Call** (~30s, ~$0.05 cost):
```
Greeting → "Can I speak to the manager?" → HUMAN_TRANSFER
→ transfer_call tool → Twilio <Dial> → "Transferring you now" → Cleanup
```

**Unavailable → Callback**:
```
... → CHECK_AVAILABILITY → No tables at 7pm → HANDLE_UNAVAILABLE
→ "We have 6pm, 6:15pm, or 8:30pm. Or I can call if there's a cancellation."
→ Caller: "Take my number" → callback request stored → CLOSING
```

---

## 20. Deployment Structure

### 20.1 VPS Topology (MVP -- Single VPS)

```
┌──────────────────────────────────────────────────────────┐
│                    VPS (4 vCPU, 8GB RAM)                  │
│                    Ubuntu 24.04 LTS                        │
│                                                          │
│  ┌────────────────────────────────────────────────────┐  │
│  │              Docker Compose Stack                    │  │
│  │                                                    │  │
│  │  ┌───────────┐  ┌───────────┐  ┌───────────────┐  │  │
│  │  │ Go Voice  │  │ NestJS    │  │ Next.js       │  │  │
│  │  │ Gateway   │  │ Backend   │  │ Frontend      │  │  │
│  │  │ :8080     │  │ :3000     │  │ :3001         │  │  │
│  │  └─────┬─────┘  └─────┬─────┘  └───────┬───────┘  │  │
│  │        │              │                │           │  │
│  │  ┌─────┴──────────────┴────────────────┴───────┐   │  │
│  │  │                  Redis :6379                  │   │  │
│  │  └──────────────────────────────────────────────┘   │  │
│  │                                                    │  │
│  │  ┌──────────────────────────────────────────────┐   │  │
│  │  │            Caddy (Reverse Proxy)              │   │  │
│  │  │  :443 → Go Gateway, NestJS, Next.js           │   │  │
│  │  │  Auto TLS via Let's Encrypt                   │   │  │
│  │  └──────────────────────────────────────────────┘   │  │
│  └────────────────────────────────────────────────────┘  │
│                                                          │
│  Supabase (managed, not on VPS)                          │
└──────────────────────────────────────────────────────────┘
```

### 20.2 Resource Allocation

| Service | CPU | Memory | Notes |
|---------|-----|--------|-------|
| Go Gateway | 2 vCPU | 2 GB | ~50MB per active call, 500 calls = ~250MB + buffers |
| NestJS | 1 vCPU | 1 GB | API server, queue workers |
| Next.js (standalone) | 0.5 vCPU | 512 MB | Low traffic (admin dashboard only) |
| Redis | 0.5 vCPU | 1 GB | In-memory session state |
| Caddy | 0.1 vCPU | 128 MB | Reverse proxy + TLS |

### 20.3 Scaling Triggers

| Metric | Threshold | Action |
|--------|-----------|--------|
| Active calls | > 100 | Scale Go Gateway to 2 instances (different VPS) |
| NestJS CPU | > 70% sustained | Scale NestJS horizontally |
| Redis memory | > 70% | Increase Redis maxmemory or scale |
| OpenAI circuit breaker | OPEN > 5 min | Escalate to on-call |

---

## 21. Docker Architecture

### 21.1 Go Gateway Dockerfile

```dockerfile
# Stage 1: Build
FROM golang:1.24-alpine AS builder
RUN apk add --no-cache gcc musl-dev
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /gateway ./cmd/gateway

# Stage 2: Run
FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata
COPY --from=builder /gateway /usr/local/bin/gateway
COPY audio/fallback/ /etc/voxlane/audio/
EXPOSE 8080
USER nobody
ENTRYPOINT ["/usr/local/bin/gateway"]
```

### 21.2 NestJS Dockerfile

```dockerfile
FROM node:22-alpine AS builder
WORKDIR /build
COPY package*.json ./
RUN npm ci
COPY . .
RUN npm run build
RUN npm ci --omit=dev

FROM node:22-alpine
RUN apk add --no-cache tzdata
WORKDIR /app
COPY --from=builder /build/node_modules ./node_modules
COPY --from=builder /build/dist ./dist
COPY --from=builder /build/package.json ./
EXPOSE 3000
USER node
ENTRYPOINT ["node", "dist/main.js"]
```

### 21.3 docker-compose.yml

```yaml
version: "3.9"

services:
  redis:
    image: redis:7-alpine
    command: redis-server --maxmemory 1gb --maxmemory-policy allkeys-lru
    volumes:
      - redis_data:/data
    networks:
      - voxlane
    healthcheck:
      test: ["CMD", "redis-cli", "ping"]
      interval: 10s
      timeout: 3s
      retries: 3
    restart: unless-stopped

  gateway:
    build:
      context: ./voice-gateway
      dockerfile: Dockerfile
    ports:
      - "8080:8080"
    env_file:
      - .env
    environment:
      - REDIS_ADDR=redis:6379
      - NESTJS_URL=http://backend:3000
    depends_on:
      redis:
        condition: service_healthy
    networks:
      - voxlane
    restart: unless-stopped
    deploy:
      resources:
        limits:
          cpus: "2"
          memory: 2G

  backend:
    build:
      context: ./backend
      dockerfile: Dockerfile
    ports:
      - "3000:3000"
    env_file:
      - .env
    environment:
      - REDIS_HOST=redis
      - REDIS_PORT=6379
      - DATABASE_URL=${SUPABASE_DATABASE_URL}
    depends_on:
      redis:
        condition: service_healthy
    networks:
      - voxlane
    restart: unless-stopped
    deploy:
      resources:
        limits:
          cpus: "1"
          memory: 1G

  frontend:
    build:
      context: ./frontend
      dockerfile: Dockerfile
    ports:
      - "3001:3000"
    env_file:
      - .env
    environment:
      - NEXT_PUBLIC_API_URL=https://api.voxlane.com
    networks:
      - voxlane
    restart: unless-stopped

  caddy:
    image: caddy:2-alpine
    ports:
      - "80:80"
      - "443:443"
    volumes:
      - ./caddy/Caddyfile:/etc/caddy/Caddyfile
      - caddy_data:/data
    networks:
      - voxlane
    restart: unless-stopped

networks:
  voxlane:
    driver: bridge

volumes:
  redis_data:
  caddy_data:
```

### 21.4 Caddyfile

```caddyfile
voice.voxlane.com {
    reverse_proxy gateway:8080
}

api.voxlane.com {
    reverse_proxy backend:3000
}

app.voxlane.com {
    reverse_proxy frontend:3000
}
```

---

## 22. VPS Deployment Approach

### 22.1 Provider Selection

**Recommended: Hetzner CX32** (4 vCPU, 8 GB RAM, 80 GB NVMe) -- ~EUR13/month.
- EU-based (Frankfurt/Helsinki) -- GDPR compliant
- Excellent price/performance
- Reliable network for low-latency VoIP traffic

**Alternative: DigitalOcean** (4 vCPU, 8 GB) -- $48/month for US customers.

### 22.2 Deployment Script

```bash
#!/bin/bash
set -euo pipefail
git pull origin main
docker compose build
docker compose up -d --remove-orphans
sleep 5
curl -f http://localhost:8080/health || { echo "Gateway health check failed"; exit 1; }
curl -f http://localhost:3000/api/health || { echo "Backend health check failed"; exit 1; }
echo "Deployment successful"
```

### 22.3 CI/CD (GitHub Actions)

```yaml
name: Deploy
on:
  push:
    branches: [main]
jobs:
  deploy:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - name: Deploy to VPS
        uses: appleboy/ssh-action@v1
        with:
          host: ${{ secrets.VPS_HOST }}
          username: deploy
          key: ${{ secrets.VPS_SSH_KEY }}
          script: |
            cd /opt/voxlane
            git pull origin main
            docker compose build
            docker compose up -d --remove-orphans
```

### 22.4 Backup Strategy

| Resource | Backup | Frequency | Retention |
|----------|--------|-----------|-----------|
| Supabase DB | Managed backups | Continuous | 7 days |
| Redis session data | Not backed up (ephemeral) | N/A | N/A |
| Caddy config | Git-tracked | Per commit | Indefinite |
| Docker images | GHCR | Per build | 30 days |
| VPS disk | Hetzner snapshot | Weekly | 4 weeks |

---

## 23. Repository Structure

```
voxlane/
├── voice-gateway/                  # Go Voice Gateway
│   ├── cmd/gateway/main.go         # Entry point
│   ├── internal/
│   │   ├── session/                # Session struct + lifecycle + state machine
│   │   ├── twilio/                 # Twilio WS handler + media processing
│   │   ├── openai/                 # OpenAI WS client + event handlers
│   │   ├── audio/                  # Audio pipeline + resampler + u-law codec
│   │   ├── tools/                  # Tool definitions + executor + HMAC
│   │   ├── redis/                  # Redis client + session store
│   │   ├── config/                 # Env parsing + validation
│   │   ├── metrics/                # Prometheus metrics
│   │   └── logging/                # Zerolog setup
│   ├── audio/fallback/             # Pre-recorded .wav fallback messages
│   ├── go.mod, go.sum, Dockerfile, Makefile
│
├── backend/                        # NestJS + Fastify
│   ├── src/
│   │   ├── main.ts, app.module.ts
│   │   ├── modules/
│   │   │   ├── voice/              # Twilio webhooks
│   │   │   ├── tools/              # Internal tool API
│   │   │   ├── booking/            # Booking service + ResDiary adapter
│   │   │   ├── tenants/            # Tenant management
│   │   │   ├── sessions/           # Session metrics + CDR
│   │   │   ├── sms/                # SMS service
│   │   │   ├── queue/              # BullMQ setup + processors
│   │   │   └── webhooks/           # External webhook receivers
│   │   ├── adapters/resdiary/      # ResDiary API client
│   │   ├── common/                 # Guards, decorators, filters, interceptors
│   │   └── shared/types/           # Shared TypeScript types
│   ├── test/, package.json, tsconfig.json, Dockerfile
│
├── frontend/                       # Next.js App Router
│   ├── src/app/                    # Pages + layouts
│   ├── src/components/, src/lib/
│   ├── package.json, next.config.ts, tailwind.config.ts, Dockerfile
│
├── shared/                         # Shared TypeScript types package
│   ├── package.json, src/
│
├── caddy/Caddyfile
├── docker-compose.yml, docker-compose.prod.yml
├── .env.example, .gitignore, README.md
├── .github/workflows/deploy.yml, test.yml
└── docs/architecture.md, api.md
```

---

## 24. Shared Type Strategy

### 24.1 Approach

Use a shared `@voxlane/types` package in the monorepo. Both `backend/` and `frontend/` reference it. The Go gateway does NOT share types -- it defines its own Go structs that mirror the JSON schema. This avoids cross-language type coupling.

### 24.2 shared/package.json

```json
{
  "name": "@voxlane/types",
  "version": "0.1.0",
  "main": "./src/index.ts",
  "types": "./src/index.ts"
}
```

### 24.3 Usage

```typescript
// backend/tsconfig.json
{ "compilerOptions": { "paths": { "@voxlane/types": ["../shared/src"] } } }

// backend/src/modules/tools/tools.controller.ts
import type { ToolResult, CheckAvailabilityArgs } from '@voxlane/types';
```

### 24.4 Go Type Mirroring

Go types are defined independently but validated against the shared JSON schema in tests:

```go
type CheckAvailabilityArgs struct {
    Date      string `json:"date"`
    Time      string `json:"time"`
    PartySize int    `json:"partySize"`
}
```

### 24.5 Contract Testing

A test suite validates that Go structs produce JSON matching the shared TypeScript schema, ensuring consistent serialization across the language boundary.

---

## 25. Environment Variable Structure

### 25.1 .env File

```bash
# === SUPABASE ===
SUPABASE_DATABASE_URL=postgresql://postgres:[password]@db.xxx.supabase.co:5432/postgres
SUPABASE_ANON_KEY=eyJh...
SUPABASE_SERVICE_ROLE_KEY=eyJh...

# === TWILIO ===
TWILIO_ACCOUNT_SID=ACxxx
TWILIO_AUTH_TOKEN=xxx
TWILIO_PHONE_NUMBER=+442070000000

# === OPENAI ===
OPENAI_API_KEY=sk-xxx
OPENAI_REALTIME_MODEL=gpt-4o-realtime-preview-2024-12-17

# === GO GATEWAY ===
GATEWAY_PORT=8080
GATEWAY_WS_URL=wss://voice.voxlane.com/stream
GATEWAY_AUDIO_SAMPLE_RATE=24000
GATEWAY_MAX_CONCURRENT_CALLS=100
GATEWAY_MAX_CALL_DURATION_SECONDS=1800
GATEWAY_SILENCE_TIMEOUT_PROMPT_SECONDS=8
GATEWAY_SILENCE_TIMEOUT_HANGUP_SECONDS=15

# === DEEPSEEK (Text Mode) ===
DEEPSEEK_API_KEY=sk-xxx
DEEPSEEK_MODEL=deepseek-chat

# === ELEVENLABS TTS ===
ELEVENLABS_API_KEY=xxx
ELEVENLABS_VOICE_ID=default

# === REDIS ===
REDIS_ADDR=redis:6379
REDIS_PASSWORD=

# === NESTJS ===
NESTJS_PORT=3000
NESTJS_URL=http://localhost:3000

# === NEXT.JS ===
NEXT_PUBLIC_API_URL=https://api.voxlane.com
NEXT_PUBLIC_SUPABASE_URL=https://xxx.supabase.co

# === JWT (NestJS Internal Auth) ===
JWT_SECRET=your-jwt-secret

# === HMAC (Go to NestJS) ===
HMAC_SECRET=your-shared-hmac-secret

# === RESDIARY ===
RESDIARY_API_KEY=xxx
RESDIARY_API_URL=https://api.resdiary.com

# === SENTRY ===
SENTRY_DSN=https://xxx@sentry.io/xxx

# === ENVIRONMENT ===
NODE_ENV=production
LOG_LEVEL=info
```

### 25.2 Config Validation

Both Go and NestJS validate all required environment variables at startup and refuse to start if any are missing.

```go
func LoadConfig() (*Config, error) {
    cfg := &Config{}
    // ... load from env ...
    if cfg.OpenAIAPIKey == "" {
        return nil, fmt.Errorf("OPENAI_API_KEY is required")
    }
    return cfg, nil
}
```

---

## 26. Security Considerations

### 26.1 Communication Security

| Path | Protocol | Authentication |
|------|----------|---------------|
| Go to OpenAI | WSS (TLS 1.3) | API key in header |
| Twilio to Go | WSS (TLS 1.3) | Twilio validates server cert |
| Go to NestJS (internal) | HTTPS (TLS 1.3) | HMAC-SHA256 signature |
| Twilio to NestJS (webhook) | HTTPS (TLS 1.3) | Twilio signature validation |
| NestJS to Supabase | TLS | Connection string + password |
| NestJS to Redis | Internal Docker network | Redis AUTH (password) |
| Next.js to NestJS | HTTPS (TLS 1.3) | JWT Bearer token |

### 26.2 API Key Management

- All API keys stored in environment variables (never in code)
- `.env` file is `.gitignore`'d
- Production secrets managed via Docker secrets or VPS environment
- API keys rotated quarterly
- Separate API keys for dev/staging/production

### 26.3 HMAC Implementation

```typescript
@Injectable()
export class HmacGuard implements CanActivate {
  canActivate(context: ExecutionContext): boolean {
    const request = context.switchToHttp().getRequest();
    const signature = request.headers['x-hmac-signature'];
    const timestamp = parseInt(request.headers['x-timestamp']);

    // Replay protection: 30-second window
    if (Math.abs(Date.now() - timestamp) > 30_000) {
      throw new UnauthorizedException('Timestamp expired');
    }

    // Verify signature
    const payload = `${request.body.callSid}:${request.body.tenantId}:${request.body.toolName}:${timestamp}`;
    const expected = createHmac('sha256', process.env.HMAC_SECRET)
      .update(payload).digest('hex');

    if (!timingSafeEqual(signature, expected)) {
      throw new UnauthorizedException('Invalid HMAC signature');
    }
    return true;
  }
}
```

### 26.4 Rate Limiting

| Endpoint | Limit | Window |
|----------|-------|--------|
| Twilio voice webhook | Per-tenant: 10 calls/min | 1 min |
| Internal tool API | Per Go instance: 100 req/s | 1 sec |
| Public API | Per IP: 60 req/min | 1 min |
| OpenAI WebSocket sessions | Global: 100 concurrent | Instant |

### 26.5 WebSocket Security

- Go Gateway validates `CallSid` format before accepting connection
- WebSocket connections authenticated via URL-bound token (CallSid is cryptographically random from Twilio)
- Maximum WebSocket frame size: 64KB
- Read/write timeouts: 30s (idle), 5s (audio frames)

---

## 27. GDPR Considerations

### 27.1 Data Classification

| Data | Classification | Storage | Retention |
|------|---------------|---------|-----------|
| Caller phone number | Personal data (PII) | Supabase, Redis | 90 days |
| Caller name | Personal data (PII) | Supabase, Redis | 90 days |
| Call audio (recordings) | Sensitive personal data | Not stored by default | Configurable opt-in |
| Call transcripts | Personal data | Supabase | 90 days |
| Booking details | Personal data | Supabase, ResDiary | Per restaurant policy |
| Call metadata (CDR) | Business data | Supabase | 2 years (anonymized after 90d) |
| AI training data | NOT used | N/A | OpenAI does not train on API data |

### 27.2 Call Recording

- **Default: OFF**. Call recording is an opt-in per-tenant feature.
- When enabled, callers hear: "This call may be recorded for quality purposes."
- Recordings stored in Supabase Storage (EU region), encrypted at rest.
- Recording retention: configurable (default 30 days, max 90 days).

### 27.3 Data Subject Access Requests (SAR)

```typescript
@Post('api/internal/gdpr/sar')
async handleSAR(@Body() body: { phoneNumber: string }) {
  const results = await this.gdprService.findByPhoneNumber(body.phoneNumber);
  return {
    callRecords: results.cdrs,
    transcripts: results.transcripts,
    recordings: results.recordings, // download URLs
  };
}
```

### 27.4 Right to Erasure

```typescript
@Post('api/internal/gdpr/erase')
async handleErasure(@Body() body: { phoneNumber: string }) {
  await this.gdprService.anonymizeCDRs(body.phoneNumber);
  await this.gdprService.deleteTranscripts(body.phoneNumber);
  await this.gdprService.deleteRecordings(body.phoneNumber);
  await this.gdprService.requestThirdPartyErasure(body.phoneNumber);
  return { success: true };
}
```

### 27.5 Data Residency

- All infrastructure in EU regions (Hetzner Frankfurt, Supabase EU)
- No data leaves the EU for processing or storage
- OpenAI API: data processed in OpenAI's infrastructure. OpenAI's DPA covers GDPR compliance for API usage. OpenAI does not train on API data.

### 27.6 Lawful Basis

- **Call processing**: Legitimate interest (answering restaurant phone calls)
- **Booking data**: Contract performance (completing the booking)
- **Call recording** (when enabled): Consent (pre-call notice)
- **SMS confirmations**: Consent (caller provides phone, expects confirmation)

---

## 28. Testing Strategy

### 28.1 Go Gateway Testing

| Layer | Framework | Scope |
|-------|-----------|-------|
| Unit tests | `testing` (stdlib) | Individual functions: resampler, u-law codec, state machine transitions, HMAC signing |
| Integration tests | `testing` + mock servers | WebSocket lifecycle, audio pipeline, Redis interactions |
| Load tests | `k6` or `vegeta` | Concurrent WebSocket connections, audio throughput |
| Contract tests | `testing` | Tool JSON schemas match NestJS definitions |

```go
func TestResampler_Upsample8to24(t *testing.T) {
    r := NewResampler()
    input := make([]float64, 160)
    for i := range input {
        input[i] = math.Sin(2 * math.Pi * 1000 * float64(i) / 8000)
    }
    output := make([]float64, 480)
    r.Upsample8to24(input, output)
    assert.Equal(t, 480, len(output))
    for _, sample := range output {
        assert.True(t, sample >= -1.0 && sample <= 1.0, "sample clipped: %f", sample)
    }
}

func TestStateMachine_InvalidTransition(t *testing.T) {
    sm := NewConversationStateMachine(testConfig())
    sm.current = StateClosing
    err := sm.Transition(StateCollectBookingDetails)
    assert.Error(t, err)
    assert.Contains(t, err.Error(), "invalid state transition")
    assert.Equal(t, StateClosing, sm.current)
}
```

### 28.2 NestJS Testing

| Layer | Framework | Scope |
|-------|-----------|-------|
| Unit tests | Jest | Services, guards, pipes |
| Integration tests | Jest + Supertest | API endpoints, database queries |
| E2E tests | Jest + Supertest | Full API flow: webhook to tools to SMS |

### 28.3 End-to-End Testing

Use Playwright for browser-based frontend tests. Use a dedicated test Twilio number for voice E2E tests.

### 28.4 CI Pipeline

```yaml
name: Test
on: [push, pull_request]
jobs:
  go-tests:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version: "1.24" }
      - run: go test ./... -race -coverprofile=coverage.out
        working-directory: voice-gateway

  backend-tests:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-node@v4
        with: { node-version: "22" }
      - run: npm ci && npm test
        working-directory: backend

  frontend-tests:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-node@v4
        with: { node-version: "22" }
      - run: npm ci && npm test
        working-directory: frontend
```

---

## 29. Technical Implementation Phases

### Phase 0: Foundation (Weeks 1-2)

**Goal**: Monorepo setup, core infrastructure, skeleton services that can run locally.

**Deliverables**:
- [ ] Monorepo structure with Go, NestJS, Next.js, shared types
- [ ] Docker Compose with Redis, Caddy
- [ ] Go Gateway skeleton: WebSocket server, accept connections, echo audio back
- [ ] NestJS skeleton: basic `/api/health`, Twilio webhook returning static TwiML
- [ ] Next.js skeleton: Tailwind, basic layout
- [ ] Supabase project created, schema defined
- [ ] CI pipeline (GitHub Actions) running tests on push

### Phase 1: Core Voice Pipeline (Weeks 3-5)

**Goal**: A single phone number that answers calls and speaks.

**Deliverables**:
- [ ] u-law to PCM16 codec (Go)
- [ ] Polyphase resampler 8kHz to 24kHz (Go)
- [ ] Audio pipeline: Twilio inbound to decode to resample to base64 to OpenAI
- [ ] OpenAI Realtime WebSocket client (Go)
- [ ] Basic session lifecycle: CREATE to ACTIVE to CLEANUP
- [ ] Greeting flow: AI answers phone, greets, detects intent
- **Milestone**: First successful end-to-end voice call.

### Phase 2: Conversation Engine (Weeks 6-8)

**Goal**: The AI can have a real conversation and complete bookings.

**Deliverables**:
- [ ] Conversation state machine (all 8 states)
- [ ] Tool definitions (all tools)
- [ ] Tool call execution pipeline (Go to HMAC to NestJS)
- [ ] NestJS tool API endpoints
- [ ] ResDiary adapter
- [ ] FAQ handling, human transfer flow
- [ ] Anti-hallucination guardrails
- [ ] Barge-in handling, silence detection
- **Milestone**: AI can complete a full booking call.

### Phase 3: Resilience & Productionisation (Weeks 9-11)

**Goal**: System survives real-world conditions.

**Deliverables**:
- [ ] OpenAI WebSocket reconnection with state recovery
- [ ] Circuit breakers (OpenAI, ResDiary)
- [ ] Graceful degradation paths
- [ ] CDR recording and per-call cost tracking
- [ ] SMS queue with BullMQ
- [ ] Synthetic call test (cron every 15 min)
- [ ] Structured logging, Prometheus + Grafana dashboards
- [ ] VPS deployment (Docker Compose on Hetzner)
- **Milestone**: Deployed to production VPS, answering real test calls.

### Phase 4: Hybrid Architecture & Cost (Weeks 12-14)

**Goal**: Implement cost-saving hybrid architecture.

**Deliverables**:
- [ ] Text-mode conversation path (DeepSeek V4)
- [ ] ElevenLabs TTS integration
- [ ] Phase switching logic (realtime to text mode)
- [ ] Per-call cost calculation with hybrid pricing
- **Milestone**: Per-call cost reduced to ~$0.25-0.35.

### Phase 5: Frontend & Tenant Onboarding (Weeks 15-18)

**Goal**: Operational dashboard for restaurants.

**Deliverables**:
- [ ] Tenant onboarding flow (Next.js)
- [ ] Call history / CDR viewer
- [ ] Basic analytics
- [ ] Tenant settings: voice selection, greeting customisation
- [ ] Auth (Supabase Auth, JWT)
- [ ] GDPR: SAR, erasure endpoints
- **Milestone**: First paying restaurant tenant onboarded.

---

## 30. Recommended Order of Development

### Week-by-Week Plan

| Week | Focus | Key Deliverable |
|------|-------|----------------|
| 1 | Monorepo, Docker, Go skeleton | Running Go WS server + NestJS + Redis locally |
| 2 | Twilio webhook, audio codec | Receive call to parse u-law to decode to PCM |
| 3 | Audio resampler, OpenAI WS client | Full audio pipeline: PSTN to OpenAI to PSTN |
| 4 | Greeting flow, session lifecycle | AI answers phone, says greeting |
| 5 | State machine (basic), tool definitions | State transitions work end-to-end |
| 6 | NestJS tool API, ResDiary adapter | `check_availability` tool works |
| 7 | Full booking flow (all booking tools) | Complete booking call works |
| 8 | FAQ, transfer, anti-hallucination | All conversation states functional |
| 9 | Barge-in, silence detection | Natural conversational flow |
| 10 | Reconnection, circuit breakers | Survives failures gracefully |
| 11 | CDR, metrics, dashboards, alerting | Production observability |
| 12 | Deployment, Docker Compose, Caddy | Live on VPS, real test calls |
| 13 | Hybrid architecture (text mode) | Phase switching works |
| 14 | Cost tracking, optimisation | Per-call cost dashboard |
| 15 | Next.js frontend -- auth + settings | Tenant can log in, change settings |
| 16 | Call history, analytics | Tenant can see their calls |
| 17 | SMS templates, onboarding flow | Self-service tenant onboarding |
| 18 | Polish, load testing, security audit | Production-ready MVP |

### Skills Required Per Phase

| Phase | Primary Skills |
|-------|---------------|
| 0 | DevOps (Docker, CI), Go, TypeScript |
| 1 | Go (WebSocket, audio DSP), Twilio integration |
| 2 | Go (state machines), NestJS (API design), ResDiary API |
| 3 | Go (resilience patterns), DevOps (monitoring, alerting) |
| 4 | Go (multi-model orchestration), TypeScript (cost analytics) |
| 5 | Next.js (React, Tailwind), Supabase (Auth, RLS) |

### Parallelisation Opportunities

- **Week 1-4**: NestJS backend and Go Gateway can be developed in parallel by different engineers
- **Week 5-8**: Frontend skeleton can begin while Go conversation engine is being built
- **Week 9-11**: Frontend feature development can continue while resilience work happens

### Risk Register

| Risk | Likelihood | Impact | Mitigation |
|------|-----------|--------|------------|
| OpenAI Realtime API deprecation | Medium | Critical | Abstract behind interface, maintain text-mode fallback |
| Twilio Media Streams instability | Low | High | Comprehensive error handling, graceful degradation |
| ResDiary API changes | Medium | Medium | Adapter pattern, second booking platform in backlog |
| Audio quality degradation | Medium | Medium | Polyphase resampler, real-device testing |
| Concurrent call limit hit | Low (MVP) | High | Queue/graceful busy signal, capacity planning |
| Latency exceeds target | Medium | Medium | Measure p95, filler audio, text-mode as faster alternative |

---

## Appendix A: Technology Stack Summary

| Component | Technology | Rationale |
|-----------|-----------|-----------|
| Voice Gateway | Go 1.24 | Low latency, GC control, static binary, excellent WS support |
| WebSocket library | `gorilla/websocket` | Industry standard, maintained |
| Audio processing | Custom (polyphase FIR) | Avoid CGo, zero allocations, <0.1ms latency |
| Logging | `rs/zerolog` | Zero-allocation structured logging |
| Metrics | `prometheus/client_golang` | Standard, integrates with Grafana |
| Redis client | `go-redis/v9` | Most maintained Go Redis client |
| HTTP client | `net/http` (stdlib) | No dependencies needed |
| Backend framework | NestJS + Fastify | TypeScript-first, DI, Fastify for throughput |
| Queue | BullMQ | Best Redis-backed queue for Node.js |
| ORM | Kysely or Supabase client | Type-safe queries, no heavy ORM |
| SMS | Twilio SDK | Direct integration |
| TTS | ElevenLabs API | Lowest latency TTS (<100ms), voice cloning |
| Text LLM | DeepSeek V4 | 10x cheaper than GPT-4o for text-only turns |
| Frontend | Next.js 14 App Router | Server components, streaming, ISR |
| Styling | Tailwind CSS | Utility-first, rapid development |
| Auth | Supabase Auth | Row-level security, JWT, built-in |
| Database | Supabase Postgres | Managed, EU region, generous free tier |
| Reverse proxy | Caddy | Auto TLS, simple config |
| Monitoring | Prometheus + Grafana | Standard, self-hosted or Grafana Cloud |
| Error tracking | Sentry | Production error monitoring |

---

## Appendix B: Key Architecture Decisions Log

| Decision | Rationale | Date |
|----------|-----------|------|
| Go for voice gateway | Lowest GC pause, excellent concurrency, static binary | May 2026 |
| Polyphase FIR resampler (not linear) | Voice quality is product-defining; 0.05ms latency is negligible | May 2026 |
| Hybrid AI architecture | Pure OpenAI Realtime is economically unviable at SME price points | May 2026 |
| Redis as session store + queue | Single Redis instance handles both; no need for separate message broker | May 2026 |
| Single VPS for MVP | Kubernetes is premature; Docker Compose on Hetzner is sufficient for 500 calls/day | May 2026 |
| HMAC internal auth (not mutual TLS) | Simpler to implement and debug; sufficient for same-network communication | May 2026 |
| Supabase Postgres (not self-hosted) | Managed backups, EU region, RLS built-in, free tier for MVP | May 2026 |
| ElevenLabs over OpenAI TTS | Lower latency (~100ms vs ~400ms), voice cloning, emotion control | May 2026 |
| Call recording default OFF | GDPR compliance, reduced storage costs, caller trust | May 2026 |
| BullMQ over SQS/RabbitMQ | Co-located with Redis, no additional service, excellent DX | May 2026 |

---

*Document prepared as an engineering implementation blueprint. All code samples are illustrative of production patterns but require completion, testing, and adaptation to the specific OpenAI Realtime API and Twilio Media Streams versions available at time of implementation.*
