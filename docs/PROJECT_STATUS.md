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
