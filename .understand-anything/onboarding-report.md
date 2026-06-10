# VoxLane — Onboarding Report

**Working dir:** `C:\Builds\AI-Phone-Answer-System` (also `C:\builds\...` — Windows case-insensitive)
**Branch:** `main` · **HEAD:** `e193459b` · **Up to date** with `origin/main`
**Uncommitted local changes (not from this session):** `backend/tsconfig.tsbuildinfo` (build artifact), `.env.example` (template tweak)
**Local `.env`:** present (49 lines, in sync with VPS — no secrets printed)
**Analysis artifacts:** `C:\Builds\AI-Phone-Answer-System\.understand-anything\` (scan, import map, batches, ignore file)

---

## 1. What this app does
**VoxLane** — a real-time AI voice receptionist SaaS. Inbound phone calls (UK restaurant vertical today, generalized later) connect through a telephony provider; an OpenAI Realtime voice agent answers, greets, handles FAQs, takes reservations (date / time / party / name / phone), modifies/cancels them, or transfers to a human. Multi-tenant design (single tenant in prod today: `tenantId = "default"`).

## 2. Tech stack
- **Backend** — NestJS 11 (Fastify platform), TypeScript 5.8, Node 22, ioredis, BullMQ (deps; unused in code I've seen), twilio SDK (declarative only). Listens on **:3003** in prod.
- **Voice gateway** — Go 1.24, `gorilla/websocket`, `gotranspile/g722` (G.722 codec), `redis/go-redis`, `prometheus/client_golang`, `zerolog`. Listens on **:8080** in prod.
- **Shared types** — small TS package `shared/` (5 files: session, booking, tools, tenant, index).
- **Telephony providers** — Twilio (default), **Telnyx (active in prod)**, SignalWire (stub).
- **LLM** — OpenAI Realtime API (`gpt-realtime-mini` default, `gpt-realtime-1.5` in `.env.example`).
- **TTS / renderer** — Cartesia (active in prod, British voice), OpenAI TTS (option), ElevenLabs (stub).
- **Runtimes** — `custom` (active, OpenAI+Cartesia pipeline) and `deepgram_agent` (alternative path).
- **Storage** — Redis only (systemd `redis-server.service` on VPS, NOT docker in prod).
- **Reverse proxy** — Caddy on 80/443.
- **Observability** — Prometheus metrics at gateway `/metrics`, structured logs.
- **No SQL database in use**; no payment/billing code.

## 3. Folder structure
```
backend/        NestJS — webhook + tool endpoints (15 files)
voice-gateway/  Go — WebSocket gateway, audio pipeline, OpenAI/Cartesia clients (45 files)
shared/         TS type package (4 source files)
deploy/         systemd/*.example, Caddyfile.example, env.production.example, scripts/
docs/           29 markdown files — ADRs, runbooks, status, plans, handoffs
experimental/   livekit/, telnyx/ — only READMEs/PLANS, no code
.sisyphus/      Sisyphus planning tool working dir (not project code)
*.md at root    handoffs, LIVE_CALL_TESTING, VoxLane_Implementation_Blueprint, architecture html+critique
```

## 4. Frontend / pages / routes
**No frontend.** This is a backend/voice service. No web UI, no admin panel, no marketing site. The only "pages" are the telephony webhook entry points and the WebSocket stream:
- `POST /api/public/voice/webhook` — Twilio voice webhook
- `POST /api/public/voice/webhook/telnyx` — Telnyx call.initiated
- `POST /api/public/voice/webhook/signalwire` — 501 stub
- `POST /api/public/voice/status-callback` — 204 no-op
- `GET  /stream/{callSid}` — WebSocket media stream (gateway)
- `GET  /health`, `/health/ready`, `/metrics` — ops (gateway)

## 5. Backend / API / server action structure
**NestJS at `backend/src/`** — minimal app: 2 modules, 2 controllers, 1 guard.
- `main.ts` → `AppModule` → `ToolsModule` + `VoiceModule`
- `voice.controller.ts:122` — Twilio/Telnyx/SignalWire webhooks. Telnyx path answers the call + schedules `streaming_start` after `TELNYX_STREAM_START_DELAY_MS` (500ms default) with configurable codec/track/target-legs.
- `tools.controller.ts:88` — **6 endpoints, all stubs returning hardcoded data** (`check-availability`, `create-booking`, `cancel-booking`, `modify-booking`, `lookup-reservation`, `transfer-call`).
- `common/guards/hmac.guard.ts:38` — HMAC-SHA256 guard, 30-second replay window, signs `callSid:tenantId:toolName:timestamp`.

**Go gateway at `voice-gateway/`:**
- `cmd/gateway/main.go:509` — WebSocket mux, provider adapter selection, runtime selection (custom / deepgram_agent), startup banner with safe config status, graceful shutdown.
- `internal/session/session.go:1607` — **the runtime heart**: provider loop, OpenAI loop, Cartesia render loop, supervisor (silence timer), tool call execution, echo suppression, manual VAD fallback, booking slot tracking, fast static greeting.
- `internal/session/sm/state_machine.go:490` — 10-state conversation machine (GREETING → FAQ → COLLECT_BOOKING → CHECK_AVAILABILITY → CONFIRM → MODIFY/CANCEL → HUMAN_TRANSFER → HANDLE_UNAVAILABLE → CLOSING), state-scoped tool availability, anti-hallucination guardrails.
- `internal/provider/telnyx/adapter.go:477` — Telnyx Media Streaming adapter, bidirectional RTP-over-WS, codec negotiation (PCMU/PCMA/G.722), track stats, optional debug capture to .wav.
- `internal/provider/twilio/adapter.go` — Twilio Media Streams adapter.
- `internal/openai/client.go:459` — OpenAI Realtime WebSocket client.
- `internal/renderer/cartesia/renderer.go:249` — Cartesia Sonic TTS WebSocket client, pcm_mulaw → u-law conversion.
- `internal/audio/` — codec pipeline (`alaw.go`, `mulaw.go`, `g722.go`, `pipeline.go`, `resampler.go`).
- `internal/redis/session_store.go:246` — Redis-backed session state (35-min TTL, key prefix `call:session:`).

## 6. Database / schema / storage / auth
- **No SQL database.** `shared/src/tenant.ts` defines `TenantConfig` interface; `.env.example` lists `SUPABASE_*` vars; **no code reads/writes Supabase**. Multi-tenant is a TODO: `session.go:1339` hardcodes `TenantID: "default"`.
- **Redis** — sole persistent store: `call:session:{callSid}` (SessionState JSON, 35-min TTL) + tool call audit log. Key namespace: `call:session:`, `call:active`. Set is in `internal/redis/session_store.go`.
- **Auth** — HMAC-SHA256 between gateway and backend (`HMAC_SECRET`, 30s replay window via `x-hmac-signature` + `x-timestamp` headers). No user-facing auth (product is inbound-call driven).

## 7. Payments / billing / subscriptions
**None.** No Stripe, no metering, no admin portal, no subscription state. The product is positioned as B2B SaaS but billing isn't implemented. See `docs/MULTI_TENANCY_ARCHITECTURE_NOTE.md` for the multi-tenant design notes.

## 8. Environment / config (no values printed)
The full env contract lives in **`voice-gateway/internal/config/config.go`** and is validated at startup. The most load-bearing vars:
- **Required**: `OPENAI_API_KEY`, `HMAC_SECRET`
- **VOICE_PROVIDER** = `twilio` | `telnyx` | `signalwire` (last is unimplemented)
- **VOICE_RUNTIME** = `custom` | `deepgram_agent` (production is `custom`)
- **VOICE_RENDERER** = `openai` | `cartesia` | `elevenlabs` (production is `cartesia`)
- **TELNYX_STREAM_BIDIRECTIONAL_CODEC** = `PCMU` (default) | `G722` (newly enabled)
- **AUDIO_TRANSCODE_OUTBOUND_TO** = `none` | `g722`
- **CARTESIA_***, **TWILIO_***, **DEEPGRAM_***, **SIGNALWIRE_*** family
- **GATEWAY_PORT** (8080), **NESTJS_PORT** (3000 dev / 3003 prod), **REDIS_ADDR**, **NESTJS_URL**
- **BUSINESS_NAME** (platform) vs **TENANT_BUSINESS_NAME** (per-tenant override; falls back to `BUSINESS_NAME`)
- **Debug toggles**: `DEBUG_TELNYX_TEST_TONE`, `DEBUG_CARTESIA_DIRECT_GREETING`, `DEBUG_TELNYX_TRACK_CAPTURE`

`.env.example` has 52 lines; the active `.env` (49 lines) is in sync with the VPS, both have all keys present.

## 9. Most important files / folders
- `voice-gateway/internal/session/session.go` (1607 lines — runtime heart)
- `voice-gateway/internal/session/sm/state_machine.go` (490 lines — conversation logic)
- `voice-gateway/cmd/gateway/main.go` (509 lines — entry, routing, lifecycle)
- `voice-gateway/internal/config/config.go` (257 lines — env contract + validation)
- `voice-gateway/internal/provider/telnyx/adapter.go` + `twilio/adapter.go` (media streams)
- `voice-gateway/internal/openai/client.go` (Realtime API client)
- `voice-gateway/internal/renderer/cartesia/renderer.go` (TTS path)
- `voice-gateway/internal/audio/` (codec pipeline)
- `backend/src/modules/voice/voice.controller.ts` (telephony webhook)
- `backend/src/modules/tools/tools.controller.ts` (placeholder tool endpoints)
- `backend/src/common/guards/hmac.guard.ts` (security boundary)
- `shared/src/*` (underused TS types)
- `deploy/systemd/*.example`, `deploy/Caddyfile.example`
- `docs/` (architecture decisions, runbooks, status)

## 10. Risky areas — don't change without permission
- `voice-gateway/internal/session/session.go` — 1607 lines, multiple intertwined concerns (manual VAD, echo suppression, booking slot tracking, fast greeting, multi-codec routing). High regression risk.
- `voice-gateway/internal/session/sm/state_machine.go` — defines AI behavior and tool availability per state. Changes the caller experience.
- `voice-gateway/internal/provider/telnyx/adapter.go` — production-active; codec negotiation around PCMU/PCMA/G.722 is subtle (just shipped in recent commits `e7c746d` G722 decode, `0d75f43` G722 outbound).
- `voice-gateway/internal/audio/pipeline.go` + codec files — central to all audio paths.
- `backend/src/common/guards/hmac.guard.ts` — security boundary; 30-second replay window is intentional.
- VPS `/opt/ai-voice-receptionist/.env` and the two `*.service` drop-in files (`10-telnyx-target-test.conf`, `40-telnyx-matrix.conf` on backend; `10-runtime-telnyx-normal.conf`, `20-audio-debug.conf`, `30-track-debug.conf` on gateway) — production state.
- `CLAUDE.md` — context-mode routing rules (preserved across sessions).

## 11. Incomplete / broken / duplicated / confusing
- **Repo name vs code name**: `ai-phone-answering-system` (repo) vs `voxlane` / `VoxLane` (Go module, systemd units, env, code). Pick one and rename the other.
- **`shared/` is underused by the Go side** — Go has its own `BookingData`, `SessionState`, `TenantConfig` in `internal/redis/session_store.go` and `internal/session/sm/state_machine.go`. The TS types are only consumed by the backend. Either delete `shared/` or wire it into the Go side (replace duplicated structs).
- **All 6 tool endpoints are stubs** — `check-availability` returns fake slots, `create-booking` returns `BK-{base36 timestamp}`, `lookup-reservation` returns a fake reservation. Real booking/availability logic is the biggest product gap.
- **No real database** — Supabase vars in `.env.example`, `TenantConfig` interface in `shared/src/tenant.ts`, but zero code reads/writes a DB. Multi-tenant is `// TODO: real tenant ID when multi-tenant` in `session.go:1339`.
- **Multiple `node dist/main` processes on VPS** (pids 1322256, 1322257, 1345536 in earlier inspection) — only one is the active systemd-managed process. Others are stale or transients. Worth investigating.
- **`.env.example` and `backend/tsconfig.tsbuildinfo` are locally modified** (uncommitted from before this session) — polluting `git status`.
- **`voice-gateway/Dockerfile:18`** — `COPY voice-gateway/audio/fallback/ /etc/voxlane/audio/` has a stale `voice-gateway/` prefix (the build context is the gateway dir, so the path should be `audio/fallback/`). Dormant bug since prod uses systemd, not docker for the gateway.
- **`deploy/scripts/validate-public-endpoint.sh:42`** — references backend on port `3001`, but prod uses `3003`. Stale script.
- **`voice-gateway/gateway.exe`** is a built Windows binary checked into the working tree (from my own `go build` earlier). Not in git history, but should be added to `.gitignore`.
- **`voxlane_architecture.html`** — a pre-built analysis artifact committed to the repo. Unclear why.
- **`experimental/`** — only contains READMEs/PLANS despite the name implying code. Misleading.
- **`docs/`** — 29 markdown files with significant overlap (multiple status docs, plans, handoffs, runbooks). Document sprawl.
- **`start.sh`** (on VPS, not in repo) inlines `REDIS_PASSWORD` and Twilio creds as shell exports — fine for the local `node dist/main` invocation that isn't running, but it's a smell.

## 12. Recommended next steps before development continues
1. **Rename the repo** to `voxlane` (or rename the Go module + systemd units to `ai-phone-answering-system`) to resolve the naming drift.
2. **Add `voice-gateway/gateway.exe` and `backend/tsconfig.tsbuildinfo` to `.gitignore`** — both are build artifacts.
3. **Decide on `shared/`** — either delete it, or migrate the Go side to consume it (replace duplicated structs in `session_store.go` and `state_machine.go`).
4. **Implement real tool endpoints** in `backend/src/modules/tools/tools.controller.ts` — the current stubs are the largest product gap. Requires a database decision (Supabase is mentioned in `.env.example`).
5. **Establish a multi-tenant plan** — the `// TODO: real tenant ID when multi-tenant` in `session.go:1339` is the largest architectural debt. Pick a DB and wire `TenantConfig` through the pipeline (the type already exists in `shared/src/tenant.ts`).
6. **Split `session.go`** — 1607 lines is a maintenance liability. Natural seams: echo suppression, manual VAD, booking slot tracking, fast greeting, Cartesia routing, supervisor.
7. **Clean up `docs/`** — keep current status + ADRs + runbooks; archive completed plans; consolidate the duplicate handoff docs at root.
8. **Fix dormant bugs**: `Dockerfile:18` COPY path, `validate-public-endpoint.sh:42` port number.
9. **Investigate the stale `node dist/main` processes on VPS** (3 instances running, only 1 is the live systemd service).
10. **Commit or revert the local `git status` noise** — `.env.example` and `backend/tsconfig.tsbuildinfo` are polluting status.
