# VoxLane — Project Status Audit

**Date**: 2026-05-23  
**Audit type**: Full project recovery audit  
**Commits analyzed**: 13 (637761c → 855b63e)

---

## Current Runtime Update — 2026-06-01

Telnyx + OpenAI Realtime + Cartesia is the active validation path. Fast static Cartesia greeting is enabled, caller speech reaches OpenAI, and the deterministic booking state layer controls the booking slot order.

Booking flow must remain:

```text
date -> time -> guest count -> name -> contact details
```

A natural receptionist wording layer now formats the next state-selected question. The wording layer may add short acknowledgements such as "Lovely", "Perfect", or "Thanks, George", but it must not choose the next slot or return booking collection to a fully LLM-driven flow.

Current behaviour focus:

- keep Alex calm, brief, and natural
- ask one question at a time
- avoid blunt phrases such as "For when?"
- avoid repeated identical questions
- ask callers to repeat or spell unclear names

Codec-quality update:

- Optional G722 outbound support was implemented behind env flags.
- One live G722 test was run and then reverted.
- G722 did not pass live validation because Telnyx sent inbound media as G722 and the gateway did not decode inbound G722 before OpenAI.
- Inbound G722 decode is now implemented and deployed in commit `2d871096fb1317a9847eed4c894ae513ce1034b8`.
- The next required checkpoint is a normal PCMU regression call before enabling G722 again.
- Production/runtime baseline remains PCMU with Cartesia `pcm_mulaw` at 8000 Hz.

Known follow-up:

- During contact-number collection, a longer phrase such as "my number is the one I'm calling from, 079..." can be interrupted before the number is complete. Handle this later as a phone-slot/VAD issue, not as part of codec work.

## 1. ORIGINAL PROJECT DIRECTION

### What VoxLane Was Intended to Become

An AI Voice Receptionist SaaS for restaurants. The AI answers phone calls, books tables, modifies reservations, cancels bookings, answers FAQs, and transfers to humans. Premium natural conversation with low latency. Sold as a monthly SaaS subscription to restaurants.

### Intended Architecture

```
Twilio PSTN → Twilio Media Streams (WSS)
  → Go Voice Gateway (realtime audio, WebSocket, state machine)
    → OpenAI Realtime API (premium conversation, tool calls)
    → NestJS Backend (business logic, booking tools, SMS)
      → Supabase Postgres (persistent data)
      → Redis + BullMQ (session state + async queue)
  → Next.js Frontend (tenant dashboard)
```

**Architecture rules**: AI never owns business truth. Tool-call-driven orchestration. State-machine conversation. Anti-hallucination guardrails.

### Intended MVP

- Twilio inbound call → Go gateway → OpenAI Realtime → fake booking tool → natural AI response → interruption handling → clean session ending
- Validate: conversation quality, latency, interruption, WebSocket stability, voice quality, tool-call orchestration

### Intended Execution Phases

| Phase | Weeks | Goal |
|-------|-------|------|
| 0: Foundation | 1-2 | Monorepo, Docker, skeletons |
| 1: Core Voice | 3-5 | First end-to-end call |
| 2: Conversation | 6-8 | Full booking conversation |
| 3: Resilience | 9-11 | Production hardening |
| 4: Hybrid/Cost | 12-14 | Hybrid AI architecture |
| 5: Frontend | 15-18 | Tenant dashboard |

### Intended Business Model — Hybrid AI Architecture

The architecture critique identified OpenAI Realtime as economically unviable at SME price points. The plan specified a **hybrid architecture**:
- Phase 1 (Greeting): OpenAI Realtime ~20-30s
- Phase 2 (Data Collection): DeepSeek V4 text + ElevenLabs TTS ~1-2 min
- Phase 3 (Closing): OpenAI Realtime ~20-30s
- Target per-call cost: $0.25-0.35 (vs $0.59 pure Realtime)

### Intended State Machine

10 states: GREETING, FAQ_ANSWER, COLLECT_BOOKING_DETAILS, CHECK_AVAILABILITY, CONFIRM_BOOKING, MODIFY_RESERVATION, CANCEL_RESERVATION, HUMAN_TRANSFER, HANDLE_UNAVAILABLE, CLOSING — with state-scoped tool availability and anti-hallucination guardrails.

---

## 2. CURRENT IMPLEMENTATION STATUS

### Phase 0: Foundation — ✅ COMPLETE

| Item | Status | Files | Matches Blueprint |
|------|--------|-------|-------------------|
| Monorepo structure | ✅ Complete | voice-gateway/, backend/, shared/ | Yes |
| Go module init | ✅ Complete | voice-gateway/go.mod | Yes |
| NestJS project scaffold | ✅ Complete | backend/package.json, tsconfig.json | Yes |
| Shared TypeScript types | ✅ Complete | shared/src/*.ts | Yes |
| Docker Compose | ✅ Complete | docker-compose.yml (Redis, gateway, backend) | Yes |
| .env structure | ✅ Complete | .env, .env.example | Yes |
| Go config loader | ✅ Complete | voice-gateway/internal/config/ | Yes |

### Phase 1: Core Voice Pipeline — ✅ COMPLETE

| Item | Status | Files | Matches Blueprint |
|------|--------|-------|-------------------|
| u-law → PCM16 codec | ✅ Complete | internal/audio/mulaw.go | Yes (with deviation — see §3) |
| Polyphase FIR resampler | ✅ Complete | internal/audio/resampler.go | Yes |
| Audio pipeline | ✅ Complete | internal/audio/pipeline.go | Yes |
| Twilio WS handler | ✅ Complete | internal/twilio/handler.go | Yes |
| OpenAI Realtime WS client | ✅ Complete | internal/openai/client.go | Yes |
| Session lifecycle | ✅ Complete | internal/session/session.go | Yes |
| Health/readiness endpoints | ✅ Complete | cmd/gateway/main.go | Yes |
| Graceful shutdown | ✅ Complete | cmd/gateway/main.go | Yes |
| Prometheus metrics | ✅ Complete | internal/metrics/metrics.go | Partial — metrics defined but not wired into session |

### Phase 2: Conversation Engine — ⚠️ PARTIAL

| Item | Status | Files | Matches Blueprint |
|------|--------|-------|-------------------|
| State machine (all 10 states) | ✅ Complete | internal/session/sm/state_machine.go | Yes |
| State-scoped tool availability | ✅ Complete | sm/state_machine.go (AvailableTools) | Yes |
| State transition validation | ✅ Complete | sm/state_machine.go (isValidTransition) | Yes |
| Anti-hallucination guardrails | ✅ Complete | sm/state_machine.go (ValidateResponse) | Yes |
| State-specific prompt injection | ✅ Complete | sm/state_machine.go (BuildSystemPrompt) | Now layered as Core Receptionist + Restaurant Behaviour Pack + Tenant Configuration |
| Barge-in handling | ⚠️ Partial | session.go (handleBargeIn) | Partial — cancels response but doesn't clear audio buffer |
| Silence detection | ⚠️ Partial | session.go (runSupervisor) | Partial — has timers but nudge injects raw JSON, not proper conversation.item.create |
| Tool call pipeline (Go→NestJS) | ❌ Missing | N/A | Blueprint specifies HMAC-signed HTTP to NestJS. Session.go uses fake in-process tools. |
| FAQ handling with state return | ✅ Complete | sm/state_machine.go (ReturnFromFAQ) | Yes |
| Human transfer flow | ⚠️ Partial | Tool defined, no actual Twilio transfer | Partial |
| Fallback audio messages | ❌ Missing | N/A | voice-gateway/audio/fallback/ exists as empty directory |

### Phase 3: Tooling + Bookings — ⚠️ PARTIAL

| Item | Status | Files | Matches Blueprint |
|------|--------|-------|-------------------|
| NestJS tool API endpoints | ✅ Complete | backend/src/modules/tools/tools.controller.ts | Yes — 6 endpoints |
| HMAC guard | ✅ Complete | backend/src/common/guards/hmac.guard.ts | Yes |
| Fake booking tools | ✅ Complete | tools.controller.ts (always returns success) | Yes — PoC appropriate |
| ResDiary adapter | ❌ Missing | backend/src/adapters/resdiary/ is empty | No |
| SMS queue (BullMQ) | ❌ Not started | backend/src/modules/queue/processors/ is empty | No |
| SMS confirmation | ❌ Not started | N/A | No |
| Session cleanup queue | ❌ Not started | N/A | No |
| Dead letter queue | ❌ Not started | N/A | No |
| Redis session persistence | ⚠️ Partial | internal/redis/session_store.go | Partial — Redis client exists but session.go passes nil for Redis |

### Phase 4: Resilience — ⚠️ PARTIAL

| Item | Status | Files | Matches Blueprint |
|------|--------|-------|-------------------|
| OpenAI WS reconnection | ❌ Not started | N/A | No — session.go has no reconnect logic |
| Circuit breakers | ❌ Not started | N/A | No |
| Graceful degradation | ❌ Not started | N/A | No |
| CDR recording | ❌ Not started | N/A | No |
| Cost tracking | ❌ Not started | N/A | No |
| Structured logging (zerolog) | ✅ Complete | internal/logging/logging.go | Yes — but session.go uses stdlib `log`, not zerolog |
| Synthetic call test | ❌ Not started | N/A | No |
| Alerting | ❌ Not started | N/A | No |

### Phase 5: Hybrid Architecture / Cost — ❌ NOT STARTED

Nothing implemented. No DeepSeek integration. No ElevenLabs TTS. No phase switching logic. No per-call cost calculation.

### Phase 6: Live Testing — ⚠️ PARTIAL

| Item | Status | Files | Notes |
|------|--------|-------|-------|
| Twilio webhook handler | ✅ Complete | backend/src/modules/voice/voice.controller.ts | Returns correct TwiML |
| Live call testing checklist | ✅ Complete | LIVE_CALL_TESTING.md | Step-by-step doc |
| Twilio credentials | ✅ Configured | .env | ACdc0d... / auth token set |
| OpenAI credentials | ✅ Configured | .env | sk-proj-... with Realtime API access |
| ngrok setup | ⚠️ Documented | LIVE_CALL_TESTING.md | Not yet run |
| Actual live call test | ❌ Not performed | N/A | Never tested with real phone |

### Phase 7: MVP Release Readiness — ❌ NOT STARTED

No frontend, no tenant management, no production deployment, no multi-tenancy.

---

## 3. ARCHITECTURAL DRIFT / CONFUSION DETECTED

### 3.1 Tool Call Path: Blueprint vs Reality

**Blueprint says**: OpenAI → Go gateway validates → HMAC-signs → HTTP POST to NestJS → NestJS executes → returns result → Go feeds back to OpenAI.

**Reality**: `session.go:executeToolCall()` has hardcoded `switch name { case "check_availability": return fake JSON }`. The HMAC-signed HTTP path to NestJS is **not implemented**. The fake tools return static JSON directly in Go.

**Severity**: Medium. This works for PoC but the tool call boundary is completely bypassed. When real ResDiary integration is needed, the entire tool execution path must be rewritten.

### 3.2 Redis Connection: Nil Pointer

**Blueprint says**: Go gateway connects to Redis for session state persistence.

**Reality**: `session.go:NewSession()` passes `nil` for the Redis client:
```go
sess := session.NewSession(callSid, tw, cfg, nil)
```
The session store exists but is **never connected**. Session state is purely in-memory.

**Severity**: Medium. Sessions survive Go process lifetime but are lost on restart. For PoC this is acceptable, but the wiring is incomplete.

### 3.3 System Prompt Architecture

Current prompt assembly now follows:

```text
VoxLane Core Receptionist
+ Restaurant Behaviour Pack
+ Tenant Configuration
+ Current Conversation State
= Live System Prompt
```

The implementation lives in `voice-gateway/internal/session/sm/state_machine.go`.

Layer responsibilities:

- Core Receptionist: universal phone behaviour, tone, transfers, manager/staff requests, messages, complaints, emergencies, closing.
- Restaurant Behaviour Pack: bookings, changes, cancellations, opening days, address, parking, events, dietary requirements, menu enquiries, group bookings, occasions, waiting list, and general restaurant enquiries.
- Tenant Configuration: business name, agent name, industry pack, and reminder that tenant facts such as address, phone, opening hours, parking, live music, manager names, and staff names must come from config/tools.

This prevents VoxLane from becoming a single hardcoded restaurant assistant.

### 3.4 Historical System Prompt Rewrite Without State Machine Tests

The system prompt was rewritten in commit `ab74218` with a complete persona overhaul. The state machine tests were updated to match the new text. However:

- The rewrite went far beyond what the original blueprint specified
- The original blueprint's prompt strategy (concise, state-rotated, ~240 tokens) was replaced with a verbose natural-language persona (~600+ tokens)
- No A/B testing was done
- The rewrite happened in a single commit without incremental validation
- The prompt quality is **untested** against a real phone call

**Severity**: Low for PoC, but represents a pattern of speculative improvement without validation.

### 3.5 VAD/voice Tuning Without Validation

Commit `ab74218` changed VAD threshold (0.5→0.4), silence duration (500→400ms), voice (alloy→shimmer), temperature (0.7→0.8), and silence timers (8s→10s, 15s→20s) — all without a single live call test.

**Severity**: Low for PoC but these values are guesses. They need real-call validation.

### 3.6 Go-to-NestJS Tool Call Not Wired

The Go session manager has `executeToolCall()` with fake responses. The NestJS `/api/internal/tools/*` endpoints are built and tested, but Go never calls them. The HMAC guard exists but is never invoked from Go.

**Severity**: High. This is the core architectural boundary (Go↔NestJS) and it's bypassed entirely.

### 3.7 Session Manager Missing Key Features

The session manager (T8) was marked "complete" but has significant gaps:
- No OpenAI reconnection logic
- No circuit breaker
- No Twilio transfer implementation
- Silence nudge uses raw `WriteRaw` instead of proper `conversation.item.create`
- Supervisor goroutine is a skeleton
- Metrics are defined but not emitted

### 3.8 Empty Directories (Abandoned Modules)

These directories exist but are empty — suggesting planned work that was abandoned:
- `backend/src/adapters/resdiary/`
- `backend/src/modules/booking/`
- `backend/src/modules/queue/processors/`
- `backend/src/modules/sessions/`
- `backend/src/modules/sms/`
- `backend/src/modules/tenants/`
- `backend/src/modules/webhooks/`
- `voice-gateway/internal/tools/`
- `voice-gateway/audio/fallback/`

### 3.9 Frontend Never Started

The blueprint specifies Next.js App Router with React 19 and Tailwind. Zero frontend code exists. The `frontend/` directory from the blueprint was never created.

### 3.9 Next.js Dockerfile Path Mismatch

`docker-compose.yml` references `./frontend` for the Next.js service, but `./frontend` doesn't exist. Running `docker compose up` would fail for the frontend service.

---

## 4. MASTER MVP TASK LIST

*See separate file: docs/MVP_TASKLIST.md*

---

## 5. CURRENT BLOCKERS

### Technical Blockers

| Blocker | Severity | Detail |
|---------|----------|--------|
| No Tool Call HTTP Path | High | Go doesn't call NestJS for tools — HMAC-signed POST not implemented |
| Redis Not Connected | Medium | Session passes nil for Redis client |
| docker-compose.yml has obsolete version attribute | Low | `version: "3.9"` triggers warning, removed |
| No Real Call Test | High | System has never been tested with a live phone call |

### Infrastructure Blockers

| Blocker | Status |
|---------|--------|
| Twilio phone number | ❓ Unknown — user mentioned "once the number is approved" |
| ngrok | Not configured for this specific test |
| OpenAI Realtime API access | ✅ Confirmed (API key has `gpt-realtime-mini` access) |
| VPS deployment | Not started |

### Unknowns

- Does the Twilio phone number support Media Streams? (Some Twilio numbers don't)
- What is the actual p50 latency from OpenAI Realtime API?
- What does the shimmer voice sound like on a real phone call?
- Does the polyphase resampler produce acceptable voice quality over PSTN?
- Does the barge-in flow work correctly with real audio?

### Risky Assumptions

- Twilio Media Streams will connect reliably to ngrok
- OpenAI Realtime won't rate-limit our test calls
- The Go gateway can handle concurrent WebSocket connections
- The audio pipeline latency is within conversational thresholds (not yet measured)

---

## 6. WHAT SHOULD HAPPEN NEXT

**Goal**: First real live phone call test.

### Immediate Next Steps (in order):

1. **Remove or comment out the non-existent frontend service from docker-compose.yml**
   - Currently references `./frontend` which doesn't exist

2. **Fix the Go→NestJS tool call path**
   - Replace `executeToolCall()` fake switch with HMAC-signed HTTP POST to NestJS
   - This validates the core architectural boundary

3. **Wire the Redis client in main.go**
   - Create a real Redis connection, pass it to new sessions
   - Even if PoC doesn't need persistence, the wiring should exist

4. **Run a local integration test**
   - Start docker-compose (redis + backend)
   - Start Go gateway locally
   - curl the webhook endpoint, verify TwiML response
   - Send a mock WebSocket message to /stream/{testCallSid}
   - Verify OpenAI session is created (watch logs)

5. **Set up ngrok**
   - `ngrok http 8080`
   - Update `GATEWAY_WS_URL` in `.env`
   - Restart backend

6. **Configure Twilio phone number**
   - Point voice webhook to `https://xxxx.ngrok-free.app/api/public/voice/webhook`

7. **Make the first live call**
   - Call the Twilio number from a real phone
   - Verify: greeting plays, AI responds, barge-in works, tools execute

---

## 7. REPOSITORY HEALTH ASSESSMENT

| Dimension | Rating | Notes |
|-----------|--------|-------|
| Code organisation | 🟢 Good | Clean package structure, clear separation |
| Architecture consistency | 🟡 Fair | Blueprint followed in structure, several key paths incomplete |
| Technical debt level | 🟡 Moderate | Fake tool calls, nil Redis, unreachable code paths, empty dirs |
| Test coverage | 🟢 Good | 55 tests, 7 packages with tests, 0 test files for session/logging/metrics |
| Production readiness | 🔴 Not Ready | No live test, no reconnect, no cost tracking, no CDR |
| Maintainability | 🟢 Good | Clean Go code, well-documented, consistent style |
| Security | 🟡 Fair | HMAC guard exists but not exercised; .env is gitignored |
| Risk level | 🟡 Moderate | Core pipeline works, but untested end-to-end |

---

## 8. EXECUTION MODE GOING FORWARD

### Strict Rules

1. **No broad optimisation prompts.** VAD tuning, prompt rewriting, voice selection changes must be driven by live call test data, not speculation.

2. **No architecture rewrites.** The blueprint is the single source of truth.

3. **Small scoped tasks only.** Each commit touches one concern.

4. **Mandatory task tracking.** Every task starts as an item in `docs/MVP_TASKLIST.md`. No work happens outside the task list.

5. **Mandatory implementation verification.** Every task must include explicit verification: test run, build check, or manual validation step.

6. **Mandatory commit discipline.** One commit per logical change. Push after each milestone. Descriptive messages.

7. **Live call testing before further tuning.** No more prompt/VAD/voice changes until a real phone call has been made and the results analysed.

8. **Complete wire paths before optimising them.** The Go→NestJS tool call path, Redis wiring, and Silence nudge must work end-to-end before any conversational quality tuning.

---

*This document supersedes all previous informal tracking. The MVP_TASKLIST.md is the canonical task list going forward.*

## 9. PCMU REGRESSION RESULT — 2026-06-02 (FAILED)

A PCMU regression call was run on the active production runtime to gate the G722 controlled test. The regression FAILED.

Log-verified call (`v3:K8ohmgKkCrqM4g1Rf3D8J-c5yIMytOWNB7jW7j4ULRl10YZS7n0xEw`, 15:18:21-15:18:57 UTC, 35 s):

- Gateway first outbound frame: 499 ms after `static_greeting_render_start` (within 360-430 ms baseline).
- Cartesia stream completion: 16 chunks in 32 s for a 55-char static greeting — ~10x stretch on the Cartesia→Telnyx path.
- Echo suppression: 318 frames (6.36 s) suppressed, 1340 frames (26.8 s) appended to OpenAI.
- OpenAI: only 1.6 s of caller audio was appended as a turn; `response_active=false` and `cartesia_active=false` for the entire call. No OpenAI reply was generated.
- Booking flow never started; `date` slot was never captured.

Caller perception vs log reality: "5 s pickup" is most likely Telnyx / PSTN ring time before `call.initiated`; gateway pickup latency is within baseline. "Noisy" / repeated-question perception is consistent with the 32 s stretched greeting audio. "2-3 s reply" is a misperception — there was no reply.

Per protocol, **G722 was NOT enabled**. PCMU remains the safe runtime. No code change was made.

Exact failure boundary: Cartesia→Telnyx outbound playback pacing (10x stretch on the static greeting), echo-suppression window of 6.36 s mis-aligned with the stretched greeting, and OpenAI not producing a reply on the 1.6 s of caller audio that was forwarded. PCMU audio path itself is not the suspect.

The next step is gated on user instruction. Do not modify the active PCMU runtime. Do not enable G722 until a normal PCMU call passes.

## 10. NATURAL BOOKING-FLOW CHANGE — 2026-06-03 (LIVE, improved)

The deterministic booking fix was overriding OpenAI's natural response on every turn after the user provided booking info, making the receptionist sound mechanical. The fix's "ask for next missing slot" path was removed from `handleCallerTranscript` (`voice-gateway/internal/session/session.go:950-957`).

Behaviour after the change:
- **Normal valid partial input** (e.g., "Tomorrow at seven p.m.") → OpenAI handles the natural reply. No deterministic question is enqueued.
- **Unclear / unparseable input** (e.g., STT heard "Ja, ik doad" when the user said their name) → deterministic clarification fires ("Sorry, could you say your name again please?").
- **All required slots captured** → deterministic completion fires ("One moment, I'll check that.").
- **No duplicate Cartesia requests** for the same slot (the pre-emptive OpenAI→Cartesia enqueue skip and the merge sentinel overrides remain as safety nets).

Live regression confirmed the receptionist sounds more natural than the previous deterministic slot-by-slot version. The change is not being rolled back.

Tests added: 5 new test cases in `voice-gateway/internal/session/booking_slots_test.go` (partial-info, unparseable-input, all-slots-captured, no-duplicate-name, no-duplicate-phone). Full session test suite passes (`go test -count=1 -timeout 60s ./internal/session/` → ok 1.5s).

Deployed binary: `/opt/ai-voice-receptionist/voice-gateway/gateway`, SHA256 `24052C82492EE36A07D43554BB1B85FD207B3089E88988A0A355E93EC90CBAFE`, 13,557,922 bytes. Backup: `gateway.bak-pre-naturalflow-2026-06-03-0034`.

PCMU runtime confirmed unchanged. G722 is **not enabled** and must remain disabled.

## 11. OPEN ISSUE — PCMU line still has interference / noise (2026-06-03)

After the natural-flow regression call, the caller still reported some sound interference / noise on the line. This is an **audio-quality issue on the PCMU path**, not a natural-flow or booking-state issue.

Status: **Open, not yet investigated at frame level for the natural-flow regression call.**

Scope:
- PCMU outbound (Cartesia → Telnyx) and / or PCMU inbound (Telnyx → OpenAI) still has audible interference.
- Separate from the 2026-06-02 10x playback-stretch failure (that was a different call, on the static greeting, and was a pacing failure).
- The outbound capture module (`DEBUG_OUTBOUND_TTS_CAPTURE=true`) is available for the next live call to provide frame-level evidence.

Next step (gated on user instruction): capture the next natural-flow regression call's outbound + inbound audio, run frame-level analysis (RMS, silence runs, amplitude jumps, PCMU decode) on both directions, and classify the noise source. Do not modify the active PCMU runtime. Do not enable G722.

## 12. PCMU AUDIO-QUALITY CLASSIFICATION — 2026-06-03 (COMPLETE)

The remaining "little bit of noise" on the natural-flow PCMU call was classified as **normal G.711 narrowband quality ceiling (codec quantization noise)**. The outbound Cartesia capture was clean locally; the inbound capture showed a constant −34.6 dB noise floor from frame 0 to frame 899 (present before any outbound audio, ruling out echo). The noise floor is consistent with G.711's theoretical SNR of ~35–40 dB. All other boundaries ruled out: Cartesia clean, gateway pacing correct, no echo, no duplicate TTS, no frame drops, no WebSocket errors. **PCMU path is technically clean; remaining issue is codec quality ceiling.**

## 13. G722 CONTROLLED LIVE TEST — 2026-06-03 (COMPLETED, REVERTED TO PCMU)

G722 was enabled for one controlled live test (`v3:kYO2YB4ycL6HUvJrLLHucjIQIfIyaKykNx1qVjFKNBSbfWA_9yRJCw`, 00:50:54–00:51:36 UTC, 42 s). The main audio pipeline decoded G.722 correctly — all 4 caller turns captured, all booking slots captured, completion message fired, no Telnyx errors, no WebSocket errors, no codec errors.

User-reported perception vs PCMU:
- Voice quality: **more or less the same** (no dramatic improvement)
- Line noise: **still had a bit of noise** (noise floor not eliminated)
- Mechanical sound: **a bit reduced** (marginal)
- Latency: **a bit better** (marginal)
- Transcript quality: **a bit better** (marginal)
- Booking flow: natural, all data captured

Conclusion: G722 is technically viable but does not dramatically improve perceived audio quality over PCMU. The remaining noise is present on both codecs, confirming it is not a codec quantization issue. **PCMU was restored immediately.** G722 is documented as a viable alternative but not promoted to the default runtime. L16 is not recommended (not a standard Telnyx codec, and the noise is not codec-related).

Debug capture module limitation noted: the inbound audio capture's decode function only supports PCMU/PCMA, not G.722. It logs `inbound audio capture decode failed codec=g722: unsupported inbound G.711 codec "g722"` for every inbound frame on G722 calls. This is debug-only and does not affect the live call. Fixing this is a separate future task.

PCMU runtime confirmed restored: `TELNYX_STREAM_BIDIRECTIONAL_CODEC=PCMU`, `CARTESIA_OUTPUT_ENCODING=pcm_mulaw`, `CARTESIA_OUTPUT_SAMPLE_RATE=8000`, `AUDIO_TRANSCODE_OUTBOUND_TO=none`. G722 is **not enabled** and must remain disabled unless re-authorized.

## 14. NOISE SOURCE INVESTIGATION — 2026-06-03 (COMPLETE)

Two controlled silence tests (caller completely silent for ~10 seconds) from two different physical locations:

- **Test A** (01:04 UTC, 7s): inbound noise floor median = −34.6 dB
- **Test B** (01:11 UTC, 9.92s, different location): inbound noise floor median = −34.6 dB

Outbound capture both tests: clean (silence floor −77 to −78 dB, normal G.711 quantization).

**Classification**: Noise is **identical at both locations** → caller's local environment ruled out. Constant exact level (−34.6 dB across all frames) suggests a **generated signal**, most likely **Telnyx comfort noise generation (CNG)** on the inbound leg. This is a Telnyx-side behavior, not a VoxLane bug.

**Recommended action**: Document as expected Telnyx comfort noise. No code change. If objectionable, contact Telnyx support to ask if CNG can be disabled. Do not add a comfort noise gate in the gateway (would break VAD).

## 15. RUNTIME CLEANUP AND BASELINE LOCK — 2026-06-03 (COMPLETE)

**PCMU is the locked production runtime.** Audio investigation is complete. Debug capture is disabled in production.

Runtime state (locked):
- `TELNYX_STREAM_BIDIRECTIONAL_CODEC=PCMU`
- `CARTESIA_OUTPUT_ENCODING=pcm_mulaw`
- `CARTESIA_OUTPUT_SAMPLE_RATE=8000`
- `AUDIO_TRANSCODE_OUTBOUND_TO=none`
- `TELNYX_STREAM_TRACK=inbound_track`, `TELNYX_STREAM_BIDIRECTIONAL_TARGET_LEGS=self`
- `FAST_STATIC_GREETING=true`, `VOICE_RUNTIME=custom`, `VOICE_RENDERER=cartesia`, `CARTESIA_SPEED=1`, `TELEPHONY_PROVIDER=telnyx`

Debug capture flags: all disabled (`DEBUG_OUTBOUND_TTS_CAPTURE=false`, `DEBUG_TELNYX_TRACK_CAPTURE=false`, `DEBUG_TELNYX_CAPTURE_AUDIO=false`, `DEBUG_TELNYX_TEST_TONE=false`).

Services: gateway active, backend active, `/healthz` 200, Telnyx webhook 200, no codec errors.

Debug artifacts cleaned: 239 capture files + 2 directories removed from `/tmp` (66 MB). No source code or logs removed. `.env.bak-pre-cleanup-2026-06-03` and `.env.bak-pre-g722test-2026-06-03` preserved on VPS as rollback safety nets.

G722 is available behind env flags only (not default). Telnyx comfort noise is documented as an expected/current limitation. No further codec experiments are planned.

## 16. VOICE QUALITY STACK STRATEGY — 2026-06-03 (CORRECTION)

**The product goal is near-human voice quality for a premium AI receptionist, not just "working phone bot".**

The codec investigation proved that **Telnyx direct WebSocket + PCMU/G722 cannot reach the near-human quality target**. The constraint is PSTN itself: G.711 narrowband (~3.4 kHz) and G.722 wideband (~7 kHz) are the ceiling for any PSTN caller, regardless of codec or provider. Cartesia's HD audio (24 kHz) is lost in the downsampling to PSTN levels.

**PCMU is the stable MVP baseline, not the final product quality.** The only path to near-human quality is a non-PSTN media path (WebRTC/Opus at 48 kHz) via LiveKit or similar.

**Recommended next spike:** Path C — LiveKit HD media path spike. LiveKit gives ~20 kHz frequency response (near-human) for non-PSTN callers (web app, mobile app, SIP clients). PSTN callers would continue to get G.722 wideband via LiveKit SIP → Telnyx. The spike is a proof-of-concept on a feature branch, not a production change. PCMU production remains unchanged and is the safe fallback.

Full strategy: `docs/context/VOICE_QUALITY_STACK_STRATEGY.md`.

## 17. LIVEKIT HD SPIKE — DESIGN + SCAFFOLD COMPLETE (2026-06-03)

**Branch:** `feat/livekit-hd-spike` (created from `main`)

**Commit:** `32f9ccb` — "docs: LiveKit HD audio spike — design + scaffold on feat/livekit-hd-spike" (pushed)

**Goal:** Prove that VoxLane can deliver near-human voice quality through a non-PSTN media path (LiveKit + Opus at 48 kHz) without disturbing the current PCMU production runtime.

**Spike scope (minimal):** One-way audio proof only. Cartesia HD PCM (24 kHz) → Go publisher → LiveKit room → browser client hears HD voice. No SIP, no OpenAI, no booking, no Telnyx changes. Phase 2 (two-way conversation) and Phase 3 (PSTN bridge via LiveKit SIP) are explicitly out of scope.

**Why this is the right path:** PSTN is the ceiling. G.711 narrowband ~3.4 kHz, G.722 wideband ~7 kHz. No codec or provider change within PSTN can exceed this. The only way to deliver HD audio (~20 kHz, near-human) to a caller is a non-PSTN media path using Opus/WebRTC. G722 test (2026-06-03) confirmed: marginal improvement, voice quality "more or less the same" as PCMU, comfort noise unchanged.

**Files created/modified (8 files, 729 insertions, 47 deletions):**
- `docs/context/LIVEKIT_HD_SPIKE_PLAN.md` (new, 16 sections): full spike design — purpose, PSTN ceiling analysis, current PCMU baseline, G722 result, target architecture, spike scope, infrastructure options, required env vars, security, rollback, success criteria, directory structure, recommended next steps, references.
- `experimental/livekit/README.md` (updated from 2026-05-28 research-only): now reflects 2026-06-03 minimal spike, supersedes old phases.
- `experimental/livekit/server-notes.md` (new): LiveKit Cloud (recommended) vs self-hosted Docker vs local Docker setup notes.
- `experimental/livekit/publisher/README.md` (new): Go publisher scaffold (Cartesia HD → Opus → LiveKit).
- `experimental/livekit/publisher/.env.example` (new): placeholder env names only (no real secrets).
- `experimental/livekit/web-client/README.md` (new): HTML web client scaffold.
- `experimental/livekit/web-client/index.html` (new): minimal HTML scaffold with form for LiveKit URL + token, audio element, log area. LiveKit integration not yet implemented.
- `experimental/livekit/results/README.md` (new): results template (empty until spike runs).

**Infrastructure decision:** Use LiveKit Cloud free tier for the spike. Fastest to set up, no infrastructure overhead, sufficient for proving the audio path. Can switch to self-hosted later if needed.

**Key technical findings (from official LiveKit docs):**
- Go SDK `github.com/livekit/server-sdk-go` supports room creation, token generation, track publishing, SIP client.
- Browser client `livekit-client` is standard WebRTC, supports Opus audio.
- Opus natively supports 8/12/16/24/48 kHz; LiveKit uses 48 kHz internally.
- Cartesia PCM (pcm_s16le, 24 kHz) can be published directly via custom `SampleProvider.NextSample(ctx)` — no transcoding needed.
- Simple HTML client can connect to a room and play audio.
- SIP/PSTN integration (LiveKit SIP service) is a separate Docker image, not needed for this spike.

**Safety guarantees (verified):**
- Production PCMU runtime on VPS is completely untouched. No binary rebuild, no env change, no service restart.
- Production Telnyx webhook, OpenAI Realtime config, Cartesia production config — all unchanged.
- No production credentials used in spike. Placeholder env names only.
- Spike publisher runs as standalone Go process, not inside production voice-gateway.
- Web client is a single HTML file, not integrated into any production frontend.
- Token generation uses short-lived (1 hour TTL) room-scoped permissions.
- Rollback is simply deleting the feature branch: `git checkout main && git branch -D feat/livekit-hd-spike`.

**Success criteria for the spike (when implemented and run):**
1. LiveKit room can be created.
2. Browser client can connect.
3. Cartesia HD PCM/Opus audio can be heard in browser.
4. Audio quality is clearly better than PCMU phone path.
5. Latency measured and < 3s for greeting.
6. Production PCMU path remains unchanged.
7. Rollback is verified (deleting branch has no effect on production).

**Next steps (deferred until plan is reviewed):**
- Implement Go publisher (`experimental/livekit/publisher/main.go`): connect to LiveKit room, create Opus audio track, implement `SampleProvider` that streams Cartesia HD PCM.
- Implement working web client (`experimental/livekit/web-client/app.js`): connect to room, subscribe to audio track, attach to `<audio>` element.
- Set up LiveKit Cloud project and generate test token.
- Run spike: hear Cartesia greeting in HD through browser.
- Measure latency, compare audio quality to PCMU, document results in `experimental/livekit/results/`.

**Stop conditions (do NOT do without explicit approval):**
- Do NOT wire LiveKit into production VoxLane.
- Do NOT replace Telnyx.
- Do NOT replace OpenAI Realtime.
- Do NOT change Cartesia production config.
- Do NOT remove PCMU/Twilio fallbacks.
- Do NOT add LiveKit SIP trunk to Telnyx.
- Do NOT build two-way conversation (Phase 2) unless one-way proof succeeds.
- Do NOT deploy LiveKit to production VPS.
- Do NOT modify production systemd services or nginx config.
