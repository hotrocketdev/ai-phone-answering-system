# AI Voice Receptionist — Session Handoff / Continuation Context

Date: 2026-05-26
Project: AI Voice Receptionist / VoxLane
Primary Goal: Build a premium realtime AI phone receptionist SaaS for restaurants and local businesses.

---

# IMPORTANT PROJECT STATE

The project has moved beyond architecture/planning.

Current phase:

PHASE 6 — LIVE REALTIME VOICE TESTING

We are now debugging and stabilising the realtime PSTN → AI → audio return pipeline.

DO NOT:
- redesign architecture
- rewrite prompts broadly
- rebuild the SaaS
- add frontend/dashboard work
- tune prompts/VAD/voice without evidence
- add speculative features
- make uncontrolled refactors

The project must continue using:
- controlled execution
- surgical fixes
- task-by-task implementation
- architecture discipline
- runtime evidence

The workflow is now:

1. Real call test
2. Find exact failure point
3. Fix one confirmed blocker
4. Retest
5. Repeat

---

# CURRENT ARCHITECTURE

## Core Stack

### Voice Gateway
- Go
- Realtime websocket handling
- Twilio Media Streams
- Provider abstraction layer
- OpenAI realtime websocket integration
- Redis session handling

### Backend
- NestJS
- Voice webhook generation
- Tool execution
- Future SaaS APIs

### AI
- OpenAI Realtime API
- realtime voice/audio streaming

### Realtime Audio
- µ-law / PCMU 8kHz handling
- WebSocket media streaming
- Twilio-compatible media events

### State/Infra
- Redis
- Docker Compose
- ngrok for local exposure

---

# PROVIDER-AGNOSTIC ARCHITECTURE

Provider abstraction has already been implemented.

Supported providers:

```ts
VoiceProvider = "twilio" | "telnyx" | "signalwire"
```

Current provider status:

| Provider | Status |
|---|---|
| Twilio | ✅ Implemented |
| Telnyx | 🔶 Scaffold |
| SignalWire | ⬜ Placeholder |

IMPORTANT:
- Twilio path must continue working
- Telnyx is intended as likely production provider
- SignalWire remains placeholder only

Provider abstraction docs:
- docs/VOICE_PROVIDER_ABSTRACTION.md

---

# CURRENT WORKING COMPONENTS

Verified working:

## Infrastructure
- Redis
- Docker compose
- NestJS backend
- Go gateway
- ngrok exposure
- Twilio webhook
- TwiML generation
- provider abstraction
- OpenAI credential loading
- health endpoints
- websocket exposure

## Testing
- 55 tests passing
- Go builds cleanly
- NestJS builds cleanly

## ngrok
Current ngrok URL:

```txt
https://kemberly-diastolic-subopaquely.ngrok-free.dev
```

Current webhook:

```txt
https://kemberly-diastolic-subopaquely.ngrok-free.dev/api/public/voice/webhook
```

Current WSS stream base:

```txt
wss://kemberly-diastolic-subopaquely.ngrok-free.dev/stream
```

---

# CRITICAL FIXES ALREADY COMPLETED

## TwiML Placeholder Bug

FIXED in commit:

```txt
f0e0b08
```

Original bug:

```xml
{{CallSid}}
{{From}}
```

were being returned literally.

This caused:

```txt
Twilio Error 11100 Invalid URL
```

Now fixed.

Real CallSid values are injected server-side.

---

# MOST RECENT LIVE CALL STATUS

Latest call details:

- Twilio webhook succeeded
- TwiML succeeded
- Twilio accepted WSS URL
- Call duration increased from 6s → 12s
- No Twilio Invalid URL errors anymore

Important observation:

The caller heard:
- NO AI voice
- NO fallback <Say> message

Interpretation:

Twilio likely entered:

```xml
<Connect>
  <Stream>
```

and held the stream open.

The failure is now deeper in:

```txt
Twilio Media Stream
→ Go gateway
→ OpenAI realtime
→ outbound audio pipeline
→ Twilio outbound media events
```

NOT in:
- webhook
- TwiML
- URL generation

---

# CURRENT MOST LIKELY FAILURE AREAS

The next debugging focus is ONLY:

## Possible failure points

1. WebSocket upgrade handling
2. Twilio stream start event parsing
3. streamSid handling
4. OpenAI realtime session creation
5. missing response.create for initial greeting
6. outbound audio generation
7. outbound Twilio media event format
8. audio codec mismatch
9. outbound media JSON schema mismatch
10. Twilio media stream event parsing

---

# IMPORTANT HYPOTHESIS

VERY likely issue:

The system may not be triggering an initial AI response.

Meaning:

- OpenAI session connects
- but no `response.create` is sent
- AI waits forever for user speech/audio
- caller hears silence

This must be checked carefully.

---

# REQUIRED NEXT DEBUGGING FLOW

DO NOT BROADLY CHANGE CODE.

ONLY trace:

```txt
Twilio PSTN
→ webhook
→ TwiML
→ WSS connect
→ gateway stream route
→ Twilio start/media events
→ OpenAI session
→ outbound audio
→ Twilio media event
→ caller hears AI
```

Need exact deepest successful stage.

---

# REQUIRED LOGGING TO ADD / VERIFY

Safe targeted logs only.

Need evidence for:

- websocket upgrade success
- Twilio connected/start/media/stop events
- streamSid received
- OpenAI session.created
- OpenAI response.audio.delta
- outbound Twilio media events
- websocket close reason

DO NOT:
- log secrets
- log raw audio payloads

---

# IMPORTANT PROJECT FILES

These are canonical project tracking docs.

## MUST READ FIRST

- docs/PROJECT_STATUS.md
- docs/MVP_TASKLIST.md
- docs/FIRST_CALL_FAILURE_DIAGNOSTIC.md
- docs/FIRST_REAL_TWILIO_CALL_RESULTS.md
- docs/VOICE_PROVIDER_ABSTRACTION.md
- docs/LOCAL_INTEGRATION_RESULTS.md
- docs/NGROK_EXPOSURE_RESULTS.md
- docs/LOCAL_LIVE_TEST_RUNBOOK.md

---

# IMPLEMENTATION DISCIPLINE

The project previously drifted badly because of:
- giant prompts
- uncontrolled optimisation
- speculative rewrites
- prompt tuning without runtime evidence

That must NOT happen again.

ALL future work must:

- use small scoped tasks
- fix one thing at a time
- stop after each phase
- provide evidence
- provide logs
- provide exact failure stage
- update tracking docs

---

# KNOWN GOOD CONFIGURATION

Twilio Voice webhook:

```txt
POST
https://kemberly-diastolic-subopaquely.ngrok-free.dev/api/public/voice/webhook
```

VOICE_PROVIDER:

```env
VOICE_PROVIDER=twilio
```

Current local environment:
- Windows
- PowerShell
- local ngrok
- local gateway
- local NestJS
- local Redis

---

# NEXT TASK FOR FUTURE SESSION

NEXT TASK:

Diagnose why the caller hears silence after Twilio accepts the Media Stream.

DO NOT:
- redesign architecture
- change providers
- add features
- tune prompts

DO:
- trace stream lifecycle
- verify streamSid handling
- verify OpenAI session creation
- verify initial AI response trigger
- verify outbound media event generation
- verify Twilio outbound media format
- identify exact deepest successful stage

Goal:

FIRST REAL AI AUDIO PLAYBACK OVER PSTN.

That is the ONLY current milestone.

---

# IMPORTANT SECURITY NOTE

Secrets were accidentally exposed multiple times during development.

Before continuing:
- rotate OpenAI API keys
- rotate ngrok tokens if exposed
- rotate Twilio auth token if exposed

NEVER print secrets into logs/output again.

---

# PROJECT MINDSET

This is no longer:

```txt
an AI demo
```

This is now:

```txt
a realtime AI telephony platform
```

The hardest part now is:
- realtime voice orchestration
- audio streaming
- latency
- interruption handling
- telephony compatibility
- outbound audio correctness

The architecture is already strong.

The work now is runtime stabilisation.



---

# SESSION UPDATE — 2026-05-27 (DEEPGRAM + REALTIME VOICE BREAKTHROUGH)

This section documents the MAJOR progress made after the original handoff.

The project has now moved from:
- infrastructure/debugging
into:
- realtime conversational optimisation
- voice quality optimisation
- premium hospitality experience design

---

# MASSIVE MILESTONE ACHIEVED

The AI receptionist now successfully performs:

```txt
PSTN Phone Call
→ Twilio Media Stream
→ Go Gateway
→ Deepgram Voice Runtime
→ Realtime AI Conversation
→ Audio back to Twilio
→ Caller hears AI receptionist
```

This is the FIRST fully working end-to-end realtime voice pipeline.

The system now supports:
- greeting callers
- listening to callers
- replying conversationally
- realtime interruption/barge-in
- live PSTN conversations
- British-leaning voice path
- provider abstraction
- runtime abstraction

---

# CURRENT WORKING REALTIME FLOW

Current working stack:

```txt
Caller
→ Twilio
→ Media Stream
→ Go Gateway
→ Deepgram Voice Agent Runtime
→ Deepgram Aura Voice
→ Twilio
→ Caller
```

The AI now successfully says:

```txt
"Hi Porto Douro Restaurants, how can I help you?"
```

and can respond conversationally.

---

# CRITICAL BREAKTHROUGHS ACHIEVED

## 1. Twilio Realtime Audio Path Fully Working

Confirmed:
- inbound caller audio
- outbound AI audio
- streamSid handling
- media event formatting
- outbound media pacing
- websocket lifecycle
- Twilio PSTN audio playback

All operational.

---

## 2. OpenAI Realtime Runtime Working

The original OpenAI realtime path was eventually stabilised.

Key fixes:
- manual turn detection
- response.create timing
- commit handling
- barge-in state clearing
- outbound media routing
- greeting generation
- realtime response flow

The OpenAI runtime now works end-to-end.

BUT:
voice quality remains too American for UK hospitality deployment.

---

# IMPORTANT STRATEGIC CONCLUSION

The architecture is now officially:

```txt
Provider-Agnostic
Realtime AI Telephony Platform
```

NOT:
- a demo
- a toy assistant
- a single-provider experiment

The architecture now supports:
- telephony abstraction
- runtime abstraction
- LLM abstraction
- renderer abstraction

---

# NEW RUNTIME ARCHITECTURE

A new runtime abstraction layer now exists.

```ts
VoiceRuntime =
  | "custom"
  | "deepgram_agent"
```

## custom
Current OpenAI realtime orchestration runtime.

## deepgram_agent
Deepgram Voice Agent runtime.

This is now operational.

---

# PROVIDER / RUNTIME / RENDERER DIRECTION

## Telephony Providers

```ts
VoiceProvider =
  | "twilio"
  | "telnyx"
  | "signalwire"
```

Status:
- Twilio = working
- Telnyx = scaffolded
- SignalWire = placeholder

Strategic direction:
- likely production telephony provider = Telnyx
- Twilio remains excellent for MVP/testing

---

## LLM Providers

```ts
LLMProvider =
  | "openai"
  | "grok"
```

Status:
- OpenAI = working
- Grok/xAI = scaffolded but NOT tested live yet

IMPORTANT:
Grok/xAI remains strategically interesting because:
- OpenAI-compatible APIs
- potentially lower cost
- strong realtime potential

BUT:
Grok is NOT the current priority.

Current priority:
- premium hospitality voice quality
- latency
- naturalness
- interruption quality

---

## Voice Renderers

```ts
VoiceRenderer =
  | "openai"
  | "deepgram"
  | "cartesia"
  | "elevenlabs"
```

Status:
- OpenAI native voice = working but too American
- Deepgram voice = working but still mechanical
- Cartesia = scaffolded/integrated but not fully live-tested yet
- ElevenLabs = future premium voice path

---

# DEEPGRAM FINDINGS

Deepgram was successfully integrated and is now operational.

Major discoveries:

## Deepgram Strengths
- excellent realtime infrastructure
- low-latency streaming
- good interruption handling
- solid STT stack
- simpler integrated voice-agent runtime
- telephony-oriented architecture
- lower operational cost potential

## Deepgram Weaknesses
- voice sounds mechanical/robotic
- lacks emotional warmth
- insufficient hospitality feel
- pacing still unnatural
- not premium enough for luxury restaurant experience

---

# IMPORTANT DEEPGRAM AUDIO DISCOVERY

A major debugging breakthrough occurred.

Initially:
- Deepgram audio sounded scrambled/noisy

Root cause discovered:
Deepgram was outputting raw μ-law 8kHz audio.

BUT:
the gateway mistakenly treated the bytes as PCM16 audio and:
- resampled them
- re-encoded them
- destroyed the audio

Final fix:
- bypass PCM pipeline entirely
- send Deepgram μ-law directly to Twilio
- split into proper 160-byte Twilio frames
- base64 encode once only

This was a critical realtime audio protocol breakthrough.

---

# CURRENT DEEPGRAM STATUS

Deepgram runtime now works:
- greeting
- conversation
- realtime responses
- interruption/barge-in
- PSTN playback

BUT:
voice quality is still not premium enough.

---

# WHY CARTESIA IS NOW THE NEXT MAJOR PRIORITY

Cartesia is now the next major live-test target.

Reason:
Cartesia is specifically optimised for:
- realtime conversational voice
- premium naturalness
- low-latency streaming
- emotional realism
- natural pacing

The project already scaffolded:
- Cartesia renderer abstraction
- WebSocket client
- PCM/u-law routing
- interruption handling

But it was never fully stabilised in live PSTN testing because Deepgram became the immediate focus.

NOW:
Cartesia should become the next major live voice experiment.

---

# EXPECTATION FOR CARTESIA

Likely future production architecture:

```txt
Telnyx
+
OpenAI/Grok realtime orchestration
+
Cartesia British voice rendering
+
Custom restaurant/business orchestration layer
```

This is currently the strongest candidate for:
- premium hospitality voice quality
- realistic pauses
- natural pacing
- human warmth
- premium UK receptionist feel

---

# ELEVENLABS STRATEGY

ElevenLabs remains valuable but likely NOT the primary realtime runtime.

Most likely future role:
- premium plans
- cloned restaurant voices
- enterprise branding
- high-end white-label deployments

Potential future feature:
Businesses upload their own receptionist voice.

---

# CURRENT BIGGEST TECHNICAL PRIORITIES

The project is no longer blocked by:
- Twilio
- realtime audio
- websocket streaming
- PSTN playback
- interruption
- session handling

Those are now largely solved.

Current biggest priorities:

1. Premium voice quality
2. Natural pacing
3. Human-like pauses
4. UK hospitality realism
5. Low-latency response timing
6. Better emotional delivery
7. Natural interruption handling
8. Production-grade reliability

---

# CURRENT MOST IMPORTANT BUSINESS INSIGHT

Restaurant owners will forgive:
- slightly imperfect AI reasoning

They will NOT forgive:
- robotic voice
- American call-center feel
- unnatural pacing
- dead-air latency
- awkward interruption handling

Voice realism is now a CORE product feature.

---

# CURRENT LIVE TEST STATUS

The AI receptionist can now:
- answer calls
- greet callers
- understand caller speech
- reply conversationally
- continue multi-turn conversations

This is a major milestone.

---

# MOST IMPORTANT NEXT TASK

NEXT TASK:
Live-test Cartesia voice rendering properly.

Goals:
- compare against Deepgram directly
- compare warmth
- compare pacing
- compare realism
- compare interruption quality
- compare latency
- determine best hospitality voice stack

DO NOT:
- redesign architecture again
- remove Deepgram runtime
- remove OpenAI runtime
- broadly refactor providers

DO:
- perform controlled side-by-side runtime tests
- measure latency
- measure perceived realism
- optimise hospitality feel

---

# FUTURE STRATEGIC POSSIBILITY — xAI / GROK

xAI/Grok remains strategically important.

Potential advantages:
- lower cost
- strong conversational intelligence
- OpenAI-compatible APIs
- possible excellent realtime economics

Potential future architecture:

```txt
Grok
+
Cartesia
+
Telnyx
```

This could become:
- lower cost
- highly scalable
- premium sounding
- commercially viable at scale

BUT:
voice quality remains the immediate priority.

---

# PROJECT MATURITY LEVEL

The project is now far beyond:
- proof of concept
- simple Twilio bot
- AI demo

This is now:

```txt
A serious realtime AI telephony orchestration platform
```

The hardest technical parts:
- PSTN realtime streaming
- audio routing
- interruption
- session lifecycle
- outbound media correctness

have largely been solved.

The remaining challenge is:
making the AI feel HUMAN.

---

# IMPORTANT SECURITY NOTE

Secrets were exposed repeatedly during debugging:
- OpenAI keys
- Deepgram keys
- ngrok tokens

Before continuing:
- rotate all keys
- rotate Deepgram token
- rotate OpenAI keys
- rotate ngrok auth token
- NEVER print secrets again.

---

# CURRENT PROJECT MINDSET

The project should now prioritise:

```txt
quality over experimentation
```

We now need:
- measured voice testing
- hospitality realism
- production stability
- premium conversational quality
- operational SaaS readiness

