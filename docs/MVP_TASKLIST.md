# VoxLane — Master MVP Task List

**Status**: Single source of truth for implementation tracking  
**Date**: 2026-05-23  
**Current phase**: PHASE 6 (Live Testing) — first real call not yet made

---

## PHASE 1 — CORE CALL PIPELINE ✅

| ID | Task | Status | Verified |
|----|------|--------|----------|
| P1.1 | Monorepo structure (voice-gateway, backend, shared) | [x] Complete | Build passes |
| P1.2 | Go module + NestJS project scaffold | [x] Complete | Build passes |
| P1.3 | Shared TypeScript types (@voxlane/types) | [x] Complete | N/A |
| P1.4 | Docker Compose (redis, gateway, backend) | [x] Complete | ⚠️ References non-existent ./frontend |
| P1.5 | .env.example + Go config loader + validation | [x] Complete | 5/5 tests |
| P1.6 | u-law encode/decode (256-entry lookup, G.711) | [x] Complete | 16/16 tests |
| P1.7 | Polyphase FIR resampler (8kHz↔24kHz, 48-tap) | [x] Complete | integrated |
| P1.8 | Audio pipeline (u-law→PCM16→resample→base64) | [x] Complete | integrated |
| P1.9 | Twilio Media Streams WS handler | [x] Complete | 9/9 tests |
| P1.10 | OpenAI Realtime WS client | [x] Complete | 7/7 tests |
| P1.11 | Redis session store client | [x] Complete | 3/3 tests |
| P1.12 | Session manager (wires Twilio+OpenAI+audio+Redis) | [x] Complete | Build passes |
| P1.13 | Health endpoint (/health) | [x] Complete | 200 OK |
| P1.14 | Readiness endpoint (/health/ready) | [x] Complete | 200 OK |
| P1.15 | Prometheus /metrics endpoint | [x] Complete | Registered |
| P1.16 | Graceful shutdown (SIGTERM, 30s drain) | [x] Complete | Code present |

## PHASE 2 — CONVERSATION ENGINE ✅

| ID | Task | Status | Verified |
|----|------|--------|----------|
| P2.1 | State machine (10 states) | [x] Complete | 20/20 tests |
| P2.2 | State transition validation (adjacency list) | [x] Complete | tested |
| P2.3 | State-scoped tool availability | [x] Complete | tested |
| P2.4 | Anti-hallucination guardrails | [x] Complete | tested |
| P2.5 | State-specific prompt injection | [x] Complete | tested |
| P2.6 | FAQ state with return-to-previous | [x] Complete | tested |
| P2.7 | Booking data accumulation | [x] Complete | tested |
| P2.8 | Barge-in handling (response.cancel) | [~] Partial | Cancels response, audio buffer not fully flushed |

## PHASE 3 — TOOLING + BOOKINGS ⚠️

| ID | Task | Status | Verified |
|----|------|--------|----------|
| P3.1 | NestJS tool API (6 endpoints) | [x] Complete | Build passes |
| P3.2 | HMAC guard for internal API | [x] Complete | Code present |
| P3.3 | Fake booking tools (always succeed) | [x] Complete | Code present |
| **P3.4** | **Go → NestJS tool call HTTP path with HMAC** | **[x] Complete** | Build passes, 55 tests |
| P3.5 | Tool call audit logging | [~] Partial | Redis AppendToolCall exists but never called from real path |
| P3.6 | ResDiary adapter | [ ] Not started | Directory exists, empty |
| P3.7 | BullMQ SMS queue setup | [ ] Not started | Directory exists, empty |
| P3.8 | SMS confirmation processor | [ ] Not started | N/A |
| P3.9 | Session cleanup queue | [ ] Not started | N/A |
| P3.10 | Twilio transfer implementation | [ ] Not started | Tool defined, no transfer logic |

## PHASE 4 — RESILIENCE ⚠️

| ID | Task | Status | Verified |
|----|------|--------|----------|
| P4.1 | OpenAI WS reconnection with state recovery | [ ] Not started | N/A |
| P4.2 | Circuit breaker (OpenAI) | [ ] Not started | N/A |
| P4.3 | Circuit breaker (ResDiary) | [ ] Not started | N/A |
| P4.4 | Structured logging wired to session (zerolog) | [ ] Not started | Logger exists, session uses stdlib log |
| P4.5 | Call Detail Record (CDR) database table | [ ] Not started | N/A |
| P4.6 | CDR recording on call completion | [ ] Not started | N/A |
| P4.7 | Per-call cost calculation | [ ] Not started | N/A |
| P4.8 | Synthetic call test (cron/15min) | [ ] Not started | N/A |
| P4.9 | Alerting (circuit breaker triggers) | [ ] Not started | N/A |
| P4.10 | Pre-recorded fallback audio (.wav files) | [ ] Not started | Directory exists, empty |
| P4.11 | Silence nudge — proper conversation.item.create | [~] Partial | Uses raw WriteRaw, not structured API |
| P4.12 | Graceful degradation paths | [ ] Not started | N/A |

## PHASE 5 — COST OPTIMISATION ❌

| ID | Task | Status | Verified |
|----|------|--------|----------|
| P5.1 | DeepSeek V4 integration | [ ] Not started | N/A |
| P5.2 | ElevenLabs TTS integration | [ ] Not started | N/A |
| P5.3 | Phase switching (realtime ↔ text mode) | [ ] Not started | N/A |
| P5.4 | Per-call cost dashboard | [ ] Not started | N/A |
| P5.5 | Token usage tracking per call | [ ] Not started | N/A |

## PHASE 6 — LIVE TESTING 🟡 (4/11 complete, system validates locally)

| ID | Task | Status | Verified |
|----|------|--------|----------|
| **P6.1** | **Fix docker-compose.yml (remove obsolete version, verify parse)** | **[x] Complete** | docker compose config exit 0 |
| **P6.2** | **Fix Go→NestJS tool call HTTP path with HMAC** | **[x] Complete** | HMAC-signed POST to NestJS, response parsed |
| **P6.3** | **Wire Redis client in main.go (pass to sessions)** | **[x] Complete** | Graceful if unavailable, readiness check |
| P6.4 | Twilio voice webhook handler → correct TwiML | [x] Complete | Returns XML |
| P6.5 | Local integration test (docker compose up → curl endpoints) | [x] Complete | 16/16 tests pass |
| P6.6 | ngrok setup + GATEWAY_WS_URL update | [~] Partial | ngrok installed, authtoken required |
| P6.7 | Twilio phone number webhook configured | [ ] Not done | Awaiting number approval |
| **P6.8** | **First live phone call test** | **[ ] NOT DONE** | **GOAL** |
| P6.9 | Voice quality assessment (real call) | [ ] Not done | Depends on P6.8 |
| P6.10 | Latency measurement (real call) | [ ] Not done | Depends on P6.8 |
| P6.11 | Barge-in behaviour validation (real call) | [ ] Not done | Depends on P6.8 |

## PHASE 7 — MVP RELEASE READINESS ❌

| ID | Task | Status | Verified |
|----|------|--------|----------|
| P7.1 | Next.js frontend scaffold | [ ] Not started | Not a single file |
| P7.2 | Tenant onboarding flow | [ ] Not started | N/A |
| P7.3 | Call history CDR viewer | [ ] Not started | N/A |
| P7.4 | Basic analytics dashboard | [ ] Not started | N/A |
| P7.5 | Tenant settings (voice, greeting, hours) | [ ] Not started | N/A |
| P7.6 | Supabase database + migrations | [ ] Not started | N/A |
| P7.7 | Multi-tenancy (tenant isolation) | [ ] Not started | N/A |
| P7.8 | VPS deployment (Hetzner) | [ ] Not started | N/A |
| P7.9 | SSL/TLS via Caddy | [ ] Not started | Caddy config exists, empty |
| P7.10 | GDPR compliance (SAR, erasure, consent) | [ ] Not started | N/A |

---

## IMMEDIATE PRIORITY — NEXT 3 TASKS

These are the **only** tasks that should be worked on right now:

### Task 1: Fix docker-compose.yml
Remove or comment out the frontend service (directory doesn't exist).

### Task 2: Wire Go → NestJS Tool Calls
Replace `session.go:executeToolCall()` with:
- Build ToolCallRequest struct
- HMAC-sign it
- HTTP POST to `http://backend:3000/api/internal/tools/{toolName}`
- Parse ToolResult response
- Feed result back to OpenAI

### Task 3: Wire Redis in main.go
- Create `redis.NewClient()` in main.go
- Pass it to `session.NewSession()`
- Remove the `nil` argument

---

## KEY

| Symbol | Meaning |
|--------|---------|
| [x] | Complete |
| [~] | Partial / needs work |
| [ ] | Not started |
| **Bold** | Blocker / critical path |

---

*This task list is the single source of truth. No work happens outside it. Update status after every completed task.*
