# VoxLane Voice PoC — Implementation Plan

## Goal

Validate realtime voice pipeline: Twilio → Go Gateway → OpenAI Realtime → fake booking tool → natural AI response.

## Phases

### Phase 1: Foundation
**T1: Monorepo structure + shared types**
- Create directory structure: `voice-gateway/`, `backend/`, `shared/`
- Initialize Go module (`voice-gateway/go.mod`)
- Initialize NestJS project (`backend/`)
- Create `shared/` TypeScript package with tool/session types
- Create `go.mod`, `package.json`, `tsconfig.json` files

**T2: Docker + environment system**
- Create `docker-compose.yml` with Redis, Go gateway, NestJS
- Create `.env.example` with all required variables
- Create Go config loader (`internal/config/`) with validation
- Create NestJS config module

**T3: Go project skeleton**
- Create `cmd/gateway/main.go` — HTTP server + WebSocket upgrade
- Create internal package structure
- Health endpoint at `/health`
- Metrics endpoint at `/metrics`

### Phase 2: Core Gateway
**T4: Audio pipeline**
- u-law decode/encode table (256-entry lookup)
- Polyphase FIR resampler (3x up/down for 8kHz↔24kHz)
- Audio pipeline orchestration (`internal/audio/pipeline.go`)
- Unit tests with sine wave validation

**T5: Twilio WebSocket handler**
- Parse Twilio Media Streams JSON messages
- Handle `connected`, `media`, `stop`, `mark` events
- Extract u-law audio payloads
- Send outbound media events
- Unit tests with mock WebSocket

**T6: OpenAI WebSocket client**
- Connect to OpenAI Realtime API (WSS)
- Send `session.update` with voice config + tools
- Handle `session.created`, `session.updated`, `response.audio.delta`, `response.done`, `response.function_call_arguments.done`
- Stream audio via `input_audio_buffer.append`
- Handle barge-in via `response.cancel`
- Unit tests with mock OpenAI server

**T7: Redis session store**
- Redis client setup (`internal/redis/`)
- Session state persistence (JSON to Redis hash)
- Active call index (SET)
- Atomic state transitions (Lua scripts)
- TTL management

### Phase 3: Session & Intelligence
**T8: Session manager**
- `Session{}` struct with channels
- Session lifecycle: CREATE → CONNECTING → ACTIVE → ENDING → CLEANUP
- 3-goroutine model (twilioReader, openaiWriter, sessionSupervisor)
- Session creation, teardown, cleanup flows
- Connect T4-T7 into unified session

**T9: Conversation state machine**
- States: GREETING, FAQ, COLLECT_BOOKING_DETAILS, CHECK_AVAILABILITY, CONFIRM_BOOKING, CLOSING
- State transition validation (adjacency list)
- State-scoped tool availability
- Anti-hallucination guardrails
- State-specific system prompt injection

**T10: Fake booking tool (NestJS)**
- Create NestJS module with Fastify
- `POST /api/internal/tools/check-availability` — always returns available
- `POST /api/internal/tools/create-booking` — returns fake booking ref
- HMAC guard for internal auth
- Tool call audit logging placeholder

### Phase 4: Production Hardening
**T11: Structured logging + observability**
- Zerolog setup with callSid, tenantId, convState context
- Prometheus metrics: calls_active, calls_total, call_duration, openai_latency, barge_in_total, tool_call_latency
- Correlation ID propagation

**T12: Healthcheck + graceful shutdown**
- `/health` — liveness (200 if running)
- `/health/ready` — readiness (200 if Redis + OpenAI reachable)
- SIGTERM handler: drain connections, 30s grace period
- Cleanup on force kill

**T13: Reconnect + silence handling**
- OpenAI WS drop → 3 retries with exponential backoff
- State recovery on reconnect (replay conversation history)
- Silence detection: 8s prompt, 15s hangup
- Pre-recorded fallback audio messages

**T14: Integration + end-to-end flow**
- Wire everything together in `main.go`
- End-to-end flow: Twilio connect → OpenAI session → greeting → booking flow → closing → cleanup
- Manual testing checklist
- Performance notes

### Supporting Artifacts
**A1: Implementation task log** — created at start, updated after each task
**A2: Decision log** — key engineering decisions
**A3: Technical debt log** — intentional shortcuts
**A4: Risk log** — unresolved issues
**A5: Progress checklist** — completion status
