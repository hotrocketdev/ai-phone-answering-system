# VoxLane Context-Load Report

**Date:** 2026-06-02
**Session type:** Onboarding + context loading (READ-ONLY)
**Source:** 21 markdown docs read directly from the working tree
**Working dir:** `C:\Builds\AI-Phone-Answer-System`
**HEAD:** `e193459b` on `main`

---

## 1. Files successfully read (21/23)

| # | Path | Lines |
|---|------|------:|
| 1 | `docs/context/VOXLANE_MASTER_CONTEXT.md` | 660 |
| 2 | `docs/context/HANDOVER_CURRENT_STATE.md` | 202 |
| 3 | `docs/context/RECEPTIONIST_CORE_SPEC.md` | 314 |
| 4 | `docs/context/tenant-behaviours/RESTAURANT_BEHAVIOUR_PACK.md` | 318 |
| 5 | `docs/context/AUDIO_QUALITY_CODEC_TEST_PLAN.md` | 291 |
| 6 | `docs/TENANT_CONFIGURATION_MVP.md` | 86 |
| 7 | `docs/MULTI_TENANCY_ARCHITECTURE_NOTE.md` | 108 |
| 8 | `docs/TELNYX_DIRECT_WEBSOCKET_ADAPTER.md` | 246 |
| 9 | `docs/TELNYX_OUTBOUND_AUDIO_DEBUG.md` | 84 |
| 10 | `docs/VOICE-STACK-DECISION-MATRIX.md` | 66 |
| 11 | `experimental/telnyx/TELNYX_ADAPTER_IMPLEMENTATION_PLAN.md` | 170 |
| 12 | `docs/PROJECT_STATUS.md` | 403 |
| 13 | `docs/CURRENT_RUNTIME_LOCK.md` | 60 |
| 14 | `docs/PRODUCTION_RUNTIME_BOUNDARIES.md` | 81 |
| 15 | `docs/MVP_LATENCY_BREAKDOWN.md` | 76 |
| 16 | `docs/AGENT_RUNTIME_EVALUATION.md` | 67 |
| 17 | `docs/PRODUCTION-ROADMAP-VOICE-QUALITY-FIRST.md` | 100 |
| 18 | `docs/NEXTGEN-LIVEKIT-HD-AUDIO-PLAN.md` | 141 |
| 19 | `experimental/livekit/README.md` | 70 |
| 20 | `ai_voice_receptionist_session_handoff_may_2026_UPDATED_FINAL.md` | 951 |

## 2. Files missing (2/23)

- `docs/context/ARCHITECTURAL_DECISIONS.md` — **does not exist**
- `docs/context/AI_WORKER_RULES.md` — **does not exist**

Both are referenced in `VOXLANE_MASTER_CONTEXT.md` §14 as the **first** files a new AI must read. Their absence is a real gap in the documentation set the user is asking me to load from. Likely the user intended these to be created — but per the original "READ in order" instruction, they were never written.

## 3. Empty / placeholder files (1/23)

- `docs/HANDOVER_2026-05_VOXLANE_VOICE_PLATFORM.md` — **0 bytes**, completely empty. The filename implies a major handover doc that was never written.

## 4. My understanding of VoxLane

**VoxLane is a multi-tenant SaaS AI Voice Receptionist platform.** The platform itself is called VoxLane. **Porto Douro Restaurants is the first tenant**, not the platform. The product is positioned as a B2B SaaS eventually serving many business types (restaurants, dentists, hotels, salons, garages, estate agents, legal firms, medical clinics, trades businesses).

The current commercial/use-case target is restaurants, starting with Porto Douro Restaurants, on a UK phone number (`+44 121 823 0230` is the current Telnyx number; Twilio number is `+441789336134`).

The product promise: **a business gets a natural-sounding AI receptionist that answers calls, understands the caller, handles bookings or enquiries, and sounds professional enough to represent that business.** The voice experience is the product, not a side feature.

Hard rule from `VOXLANE_MASTER_CONTEXT.md` §3: **never treat Porto Douro as the platform.** Tenant-specific information must not be hardcoded into voice gateway core runtime, telephony provider code, Cartesia renderer, OpenAI session logic, deployment infrastructure, or SaaS platform docs.

## 5. My understanding of the current tenant model

**Current state:** single-tenant via env vars, NOT multi-tenant yet. All inbound calls are treated as the same tenant.

- `BUSINESS_NAME` — platform name (used for logs, admin, dashboard) — defaults to `VoxLane`
- `TENANT_BUSINESS_NAME` — tenant name (used for caller-facing greetings and AI prompts) — set per-deploy
- `config.CustomerName()` returns `TENANT_BUSINESS_NAME` if set, else falls back to `BUSINESS_NAME`
- All tenant facts (business name, address, phone, opening hours, parking, live music, manager names, staff names) **must come from tenant configuration or tools**, never from hardcoded strings
- In code: `session.go:1339` hardcodes `TenantID: "default", // TODO: real tenant ID when multi-tenant`

**Future model** (per VOXLANE_MASTER_CONTEXT §3 + TENANT_CONFIGURATION_MVP):
```
Phone number → tenant → business name → greeting → voice ID → booking provider → knowledge base → opening hours → call rules
```

Phased approach: env → JSON file → Redis lookup → database tenant table → admin dashboard. None of the later phases are implemented.

## 6. My understanding of the receptionist architecture

**Layered prompt architecture** (assembled at runtime, in `state_machine.go` BuildSystemPrompt):

```text
VoxLane Core Receptionist
+ Industry Behaviour Pack          (e.g. Restaurant Behaviour Pack)
+ Tenant Configuration             (business name, agent name, tenant facts)
+ Current Conversation State       (from the 10-state state machine)
= Live System Prompt
```

This must NOT be collapsed into a single Porto Douro prompt.

**VoxLane Core Receptionist** (`RECEPTIONIST_CORE_SPEC.md`) defines universal phone behaviour applicable to every business:
- Greeting format: `{Business name}, {agent name} speaking. How can I help?` (British English, calm, brief)
- Tone: professional, warm, brief, efficient, calm, natural
- Forbidden: over-excitement, "I'm all ears", "happy to help with anything", assistant-style chatter, long preambles
- Transfer/manager/staff: handle directly when available, take a message otherwise, never invent availability
- Message taking: ask one item at a time (who, name, number, message)
- Complaints: calm, collect details, pass to right person, never argue/admit liability
- Emergency: direct to 999 first, then offer to take a message
- Escalation: when caller asks for manager/owner/staff, wants transfer, is angry/distressed, request is outside industry pack, info unavailable, asks for legal/medical/financial/safety advice, automation fails
- Closing: short, polite, no assistant-style "anything else I can help you explore today"

**Restaurant Behaviour Pack** (the active industry pack) adds restaurant-specific flows:
- Booking workflow: collect date → time → party size → name → contact details, one at a time, deterministic order enforced by state machine
- Deterministic booking state decides the next missing item; a natural wording layer formats the question — **must not let the LLM improvise the booking order**
- Reservation changes/cancellations, opening times, address, parking, live music/events, dietary, menu enquiries, group bookings, special occasions, waiting list, general restaurant enquiries
- Unknown-answer fallback: "I don't have the confirmed details in front of me, but I can take your number and ask someone to call you back."

**Tenant Configuration** supplies: business name, agent name (default "Alex"), industry pack selection, and a reminder that tenant facts (address, phone, hours, parking, manager names, staff names) must come from config/tools, not from the prompt itself.

**10-state conversation state machine** (in `voice-gateway/internal/session/sm/state_machine.go`):
`GREETING → FAQ_ANSWER → COLLECT_BOOKING_DETAILS → CHECK_AVAILABILITY → CONFIRM_BOOKING → MODIFY_RESERVATION / CANCEL_RESERVATION → HUMAN_TRANSFER → HANDLE_UNAVAILABLE → CLOSING`
- State-scoped tool availability (`AvailableTools()`)
- Anti-hallucination guardrails (`ValidateResponse()`)
- State transition validation (`isValidTransition()`)
- FAQ return state tracking

**Natural receptionist wording layer** (current focus area per HANDOVER_CURRENT_STATE): the wording layer formats the deterministic state-selected question with short natural acknowledgements like "Lovely", "Perfect", "Thanks, George" — but does NOT choose the next slot or return booking collection to a fully LLM-driven flow.

## 7. My understanding of the current Telnyx / OpenAI / Cartesia runtime

**Current safe runtime baseline** (per HANDOVER_CURRENT_STATE + AUDIO_QUALITY_CODEC_TEST_PLAN):

```
TELEPHONY_PROVIDER=telnyx          (was twilio as MVP, now telnyx as primary)
VOICE_RUNTIME=custom               (deepgram_agent is an experimental alt runtime)
VOICE_RENDERER=cartesia            (openai native, deepgram voice, elevenlabs are all NOT production)
FAST_STATIC_GREETING=true
TELNYX_STREAM_TRACK=inbound_track
TELNYX_STREAM_BIDIRECTIONAL_TARGET_LEGS=self     (was opposite, which caused silence)
TELNYX_STREAM_BIDIRECTIONAL_CODEC=PCMU          (G722 inbound decode added 2d87109, NOT yet live-tested)
CARTESIA_OUTPUT_ENCODING=pcm_mulaw
CARTESIA_OUTPUT_SAMPLE_RATE=8000
AUDIO_TRANSCODE_OUTBOUND_TO=none
CARTESIA_SPEED=1
```

**Pipeline (active path):**
```
UK caller → +44 121 823 0230 (Telnyx) → 
  POST /api/public/voice/webhook (NestJS) → 
    Telnyx REST: answer → setTimeout(500ms) → streaming_start →
Telnyx WebSocket → wss://voice.voxlane.co.uk/stream/{call_control_id} →
  Go voice-gateway (port 8080) → 
    Telnyx adapter (bidirectional RTP-over-WS, JSON media events with base64 payload) →
    Session lifecycle →
      OpenAI Realtime (gpt-realtime-1.5 per .env.example, gpt-realtime-mini per gateway startup banner — see §9 contradiction) →
        drops OpenAI audio when Cartesia is active
      Cartesia Sonic (sonic-3.5 per .env.example, sonic-2 per renderer default) for spoken output →
    u-law 8kHz frames → Telnyx → caller
```

**Telnyx adapter** (the active boundary):
- Bidirectional RTP mode (not raw binary)
- JSON text WebSocket media events: `{"event":"media","media":{"payload":"<base64 raw PCMU>"}}` (no 12-byte RTP header)
- Inbound media: PCMA/PCMU/G722 (PCMA is what Telnyx currently sends inbound per AUDIO_QUALITY_CODEC_TEST_PLAN §"Latest observed baseline")
- `stream_bidirectional_target_legs` MUST be `self` or `both`; `opposite` causes caller silence (proven via target-leg matrix in TELNYX_OUTBOUND_AUDIO_DEBUG.md)
- Inbound G722 decode implemented in commit `2d87109` — Telnyx G722 → PCM16 16kHz → PCM16 24kHz for OpenAI; PCMA/PCMU paths unchanged

**OpenAI Realtime client** (`internal/openai/client.go`):
- WebSocket client to `wss://api.openai.com/v1/realtime?model={model}`
- Manages session lifecycle, audio streaming (PCM16 24kHz), tool call handling
- Tools declared: `check_availability`, `create_booking`, `cancel_booking`, `modify_booking`, `lookup_reservation`, `transfer_call` (defined in state machine, passed to OpenAI as functions)
- Server VAD with manual turn fallback (`OPENAI_MANUAL_TURN_FALLBACK`)

**Cartesia renderer** (`internal/renderer/cartesia/renderer.go`):
- WebSocket client to `wss://api.cartesia.ai/tts/websocket`
- Outputs pcm_mulaw 8kHz default (production), pcm_s16le 16kHz for G722 path
- Streaming chunks → split into 160-byte frames → encoded to Telnyx/Twilio

**OpenAI audio MUST be dropped when Cartesia is active** (CURRENT_RUNTIME_LOCK hard rule) — no American voice leakage. When VOICE_RENDERER=cartesia, the OpenAI audio delta is counted-and-dropped (DROPPED_OPENAI_AUDIO_CARTESIA_ACTIVE) because the text is what gets routed to Cartesia for rendering.

**Fast static greeting** (`FAST_STATIC_GREETING=true`): greeting text is known at compile time (`"{Business name}, {agent name} speaking. How can I help?"`) → Cartesia greeting sent in a goroutine before OpenAI connects → first OpenAI greeting suppressed to avoid double-greeting → 150-350ms first-audio vs 1200-2200ms baseline. The first outbound frame is ~360-430ms after render start in recent logs.

**Current boundary** (per HANDOVER_CURRENT_STATE §"Current Active Product Boundary"):
"Receptionist behaviour and prompt architecture." Do not debug or change Telnyx, Cartesia, OpenAI model selection, codecs, Twilio fallback, G722, or LiveKit while this boundary is active.

## 8. My understanding of the codec situation

**Codec ladder** (per AUDIO_QUALITY_CODEC_TEST_PLAN §13):
1. **PCMU (G711U) 8kHz** — compatibility-only, prove the pipe, current production baseline
2. **G722 16kHz** — first real HD target
3. **L16 16kHz** — possible next quality test if supported cleanly
4. **Opus** — later if supported cleanly
5. **LiveKit** — future if direct WebSocket remains limited

**PCMU remains the safe runtime baseline.** G722 outbound is implemented behind env flags. G722 inbound decode was just added (commit `2d87109`, deployed but not yet live-tested with the new decode in the loop). VPS is reverted to PCMU baseline:
```
TELNYX_STREAM_BIDIRECTIONAL_CODEC=PCMU
CARTESIA_OUTPUT_ENCODING=pcm_mulaw
CARTESIA_OUTPUT_SAMPLE_RATE=8000
AUDIO_TRANSCODE_OUTBOUND_TO=none
```

**G722 history (the recent debugging arc):**
- Commit `0d75f43` — G722 outbound support added (Cartesia `pcm_s16le`/`16000` → G722 encoder → Telnyx JSON media)
- Live test on 2026-06-01: caller heard greeting (yes), voice quality worse (call was noisy), booking flow failed after caller said they wanted to book, **exact failure boundary**: Telnyx sent inbound as G722, but gateway inbound path only supported G.711 PCMA/PCMU for OpenAI input — caller audio was dropped with `unsupported inbound G.711 codec "G722"`
- VPS reverted to PCMU immediately
- Commit `2d87109` (2026-06-02) — Telnyx G722 inbound decode implemented using `github.com/gotranspile/g722`, decoded PCM16 16kHz resampled to PCM16 24kHz for OpenAI
- **G722 has not yet been live-tested with the new inbound decode**
- **Pre-condition for any G722 test:** a normal PCMU regression call must pass first

**Telnyx codec support (official):**
- `stream_track`: `inbound_track`, `outbound_track`, `both_tracks`
- `stream_codec`: `PCMU`, `PCMA`, `G722`, `OPUS`, `AMR-WB`, `L16`, `default`
- `stream_bidirectional_mode`: `mp3`, `rtp`
- `stream_bidirectional_codec`: `PCMU`, `PCMA`, `G722`, `OPUS`, `AMR-WB`, `L16` (only when mode=rtp)
- `stream_bidirectional_sampling_rate`: 8000, 16000, 22050, 24000, 48000
- Bidirectional RTP audio via WebSocket text JSON media events with base64 payload
- Telnyx warns: if streamed audio codec differs from the call codec, Telnyx may transcode and quality can degrade

**Cartesia does NOT support G722 directly** — its official encodings are `pcm_f32le`, `pcm_s16le`, `pcm_mulaw`, `pcm_alaw`. For G722, Cartesia outputs pcm_s16le/16kHz → gateway G722 encoder → Telnyx JSON media payload.

**Telnyx outbound audio history (proven via test tone matrix in TELNYX_OUTBOUND_AUDIO_DEBUG.md):**
- Attempt 1 (raw binary PCMU) → silence
- Attempt 2 (12-byte RTP header + PCMU as binary) → silence
- Attempt 3 (JSON media payload base64 raw PCMU, no RTP header) → STILL silence on `opposite` target_legs
- Attempt 4 (test tone path, OpenAI/Cartesia bypassed) → silence on `opposite`
- Attempt 5 (timing fix — tone after stream readiness) → still silence on `opposite`
- **Proven fix:** `stream_bidirectional_target_legs=self` and `both` work; `opposite` was the silent boundary

## 9. Contradictions between docs and code

These are the contradictions I found by cross-reading the docs against the actual code in this session and the prior verification sessions.

### 9.1 `MULTI_TENANCY_ARCHITECTURE_NOTE.md` lists hardcodes that are already fixed

The doc (dated 2026-05-30) flags 6 hardcoded Porto Douro references as needing fixes. Cross-checking against the current code:

- `voice-gateway/internal/config/config.go:115` — doc says hardcoded `"Porto Douro Restaurants"` default. **Actual code (line 134):** `BusinessName: getEnv("BUSINESS_NAME", "VoxLane"),` — **already fixed**.
- `voice-gateway/cmd/gateway/main.go:339` — doc says hardcoded greeting. **Actual code (line 378):** `text := fmt.Sprintf("Good afternoon, %s, how can I help?", cfg.CustomerName())` — **already fixed**, uses tenant name.
- `voice-gateway/internal/runtime/deepgram/agent.go:102` — not yet cross-checked but the doc flags it.
- `deploy/env.production.example:62` — not yet cross-checked.
- `docs/VPS_CADDY_DEPLOYMENT_CHECKLIST.md:102` — not in the user's read list, doc is also missing/stale (see §10).

The doc is **partially stale** — the fixes were applied but the doc was never updated. Three of six items need re-verification.

### 9.2 `CURRENT_RUNTIME_LOCK.md` describes the pre-Telnyx state

The doc (dated 2026-05-28) says telephony layer is **Twilio Media Streams** and the runtime lock is `VOICE_RUNTIME=custom / VOICE_RENDERER=cartesia`. Reality per HANDOVER_CURRENT_STATE (dated 2026-05-31) and the actual prod VPS:

- Telephony is now **Telnyx**, not Twilio
- Twilio is now **fallback only**, not the MVP transport
- `VOICE_RUNTIME=deepgram_agent` is now operational, not just "archived"

The runtime lock is **out of date**. The hard rules are still valid (no American voice leakage, no OpenAI native voice, etc.) but the actual layers list is stale.

### 9.3 `PRODUCTION_RUNTIME_BOUNDARIES.md` describes Twilio as production transport

The doc (dated 2026-05-28) shows `Twilio Media Streams → Go Gateway → OpenAI Realtime → Cartesia Sonic 3.5 → Twilio`. Reality: the production runtime is **Telnyx → ... → Telnyx**, not Twilio. The transport-specific code table also says `cmd/gateway/main.go` is "tightly coupled to Twilio" — but the current main.go selects provider by `cfg.VoiceProvider` and supports both Twilio and Telnyx adapters. The doc is **stale** on the current production reality.

### 9.4 OpenAI model name disagreement

- `VOXLANE_MASTER_CONTEXT.md`, `CURRENT_RUNTIME_LOCK.md`, `MVP_LATENCY_BREAKDOWN.md`, `ai_voice_receptionist_session_handoff_may_2026_UPDATED_FINAL.md`, `AGENT_RUNTIME_EVALUATION.md`: `OPENAI_REALTIME_MODEL=gpt-realtime-1.5`
- `.env.example`: `OPENAI_REALTIME_MODEL=gpt-realtime-1.5`
- `voice-gateway/internal/config/config.go:94`: `OpenAIRealtimeModel: getEnv("OPENAI_REALTIME_MODEL", "gpt-realtime-mini"),` — code default is **`gpt-realtime-mini`**, not 1.5
- Gateway startup banner on the actual VPS (from my earlier verification): `Model: gpt-realtime-1.5` — so prod env is set to 1.5 explicitly

So the **prod env is 1.5** but the **code default is mini**. If a developer runs locally without the env var, they get mini, not 1.5. This is a real drift between documented and coded defaults.

### 9.5 Cartesia model name disagreement

- `.env.example`: `CARTESIA_MODEL=sonic-3.5`
- `voice-gateway/internal/renderer/cartesia/renderer.go` bottom: `const DefaultModel = "sonic-2"`
- `voice-gateway/internal/config/config.go:117`: `CartesiaModel: getEnv("CARTESIA_MODEL", "sonic-2"),` — code default is **`sonic-2`**
- Live VPS env: not in the safe status I checked (didn't include CARTESIA_MODEL in the safe report)

Prod is 3.5, code default is 2. Same drift pattern as OpenAI.

### 9.6 `MVP_LATENCY_BREAKDOWN.md` is for the Twilio path, not the Telnyx path

The doc measures "first u-law frame to Twilio" and "first u-law frame reaching Twilio" — entirely Twilio framing. The current path is Telnyx, where the JSON media envelope adds overhead. Numbers in the doc are not directly applicable to the current prod runtime.

### 9.7 Reverse proxy: nginx in prod vs Caddy in templates

- `VOXLANE_MASTER_CONTEXT.md` §5: "Reverse proxy: nginx, not Caddy" (current prod is nginx)
- `deploy/Caddyfile.example`, `deploy/VPS_CADDY_DEPLOYMENT_CHECKLIST.md` — templates are for Caddy
- `experimental/telnyx/TELNYX_ADAPTER_IMPLEMENTATION_PLAN.md` Phase 1 prerequisites: "VPS provisioned with Caddy"

So the deploy templates and the Telnyx spike plan both assume Caddy, but actual prod is nginx. Templates are either aspirational (never used) or were once used and replaced. This is a deployment-template drift that will trip up any new dev trying to follow the checklist.

### 9.8 G722 status: doc-vs-code

- `HANDOVER_CURRENT_STATE.md` says: "G722 inbound decode is now implemented and deployed in commit `2d871096fb1317a9847eed4c894ae513ce1034b8`. The VPS was reverted to the PCMU baseline."
- `AUDIO_QUALITY_CODEC_TEST_PLAN.md` §"G722 Inbound Decode Implementation" says the same.
- `PROJECT_STATUS.md` top update (2026-06-01) says the same.

These are consistent. ✓ But the runtime still has G722 code paths active behind env flags even on the PCMU baseline — config.go has both `TELNYX_STREAM_CODEC` and `TELNYX_STREAM_BIDIRECTIONAL_CODEC` env vars, the audio package has a G722 encoder, the session.go has a G722 outbound path. So G722 capability is shipped, just not enabled in prod. This matches the doc.

### 9.9 Telnyx track: doc-vs-doc

- `VOXLANE_MASTER_CONTEXT.md` §9/§12 says current was `stream_bidirectional_target_legs=opposite` (the silent boundary) and `stream_track=both_tracks`
- `TELNYX_OUTBOUND_AUDIO_DEBUG.md` matrix confirms: `both_tracks` works with `self` or `both` target_legs
- `AUDIO_QUALITY_CODEC_TEST_PLAN.md` §"Current Baseline" lists: `TELNYX_STREAM_TRACK=inbound_track`, `TELNYX_STREAM_BIDIRECTIONAL_TARGET_LEGS=self`

So the **current safe runtime** uses `inbound_track` + `self`, but the **target-leg matrix** in the debug doc was tested with `both_tracks`. There's no test result published for `inbound_track` + `self` end-to-end (only the matrix and the inferred safe state). This is a gap — the current safe state isn't explicitly proven in the docs.

### 9.10 Handoff doc says Cartesia is "next major priority"

`ai_voice_receptionist_session_handoff_may_2026_UPDATED_FINAL.md` line 738: "NOW: Cartesia should become the next major live voice experiment." But the HANDOVER_CURRENT_STATE and AUDIO_QUALITY_CODEC_TEST_PLAN confirm Cartesia is now the **active production renderer**, with `greeting first outbound frame ~360-430ms after render start`. The handoff is from before Cartesia was validated; it doesn't know Cartesia is now the locked renderer.

### 9.11 Handoff doc has hardcoded tenant strings and old ngrok URLs

- Line 499: `"Hi Porto Douro Restaurants, how can I help?"` — verbatim quote in the handoff
- Line 134: `https://kemberly-diastolic-subopaquely.ngrok-free.dev` — ngrok URL that no longer applies (VPS public endpoint is now used per VOXLANE_MASTER_CONTEXT §5)
- Line 28: "ngrok for local exposure" listed as part of core stack
- Section "MOST RECENT LIVE CALL STATUS" describes a state (caller heard nothing, debug into stream lifecycle) that is now historical

The handoff is **significantly stale** despite the "UPDATED_FINAL" suffix.

### 9.12 `experimental/telnyx/TELNYX_ADAPTER_IMPLEMENTATION_PLAN.md` says "Pre-implementation"

The plan doc is dated 2026-05-30 with status "Pre-implementation" — but the Telnyx adapter is now fully implemented and shipping in production (commits `0d75f43`, `2d87109`, etc.). The plan was never marked as completed or archived.

## 10. Stale docs (superseded by newer commits but never updated)

| Doc | Last updated | Status vs current state |
|-----|--------------|------------------------|
| `docs/CURRENT_RUNTIME_LOCK.md` | 2026-05-28 | Stale. Says Twilio. Reality: Telnyx primary, Twilio fallback. |
| `docs/PRODUCTION_RUNTIME_BOUNDARIES.md` | 2026-05-28 | Stale. Same. |
| `docs/MVP_LATENCY_BREAKDOWN.md` | 2026-05-28 | Stale. Twilio framing. |
| `docs/VOICE-STACK-DECISION-MATRIX.md` | 2026-05-28 | Stale. Pre-Telnyx-decision matrix. |
| `docs/PRODUCTION-ROADMAP-VOICE-QUALITY-FIRST.md` | 2026-05-28 | Stale. Stage 3 was "HD audio spike" but G722 has been implemented and partly live-tested. |
| `docs/NEXTGEN-LIVEKIT-HD-AUDIO-PLAN.md` | 2026-05-28 | Stale. LiveKit still listed as future spike, but Telnyx path is now the primary transport. |
| `docs/AGENT_RUNTIME_EVALUATION.md` | 2026-05-28 | Mostly current. Production decision still pending. |
| `docs/PROJECT_STATUS.md` | 2026-05-23 with 2026-06-01 update at top | Body is stale; top update is current. Has both old runtime lock info AND new G722 status. |
| `docs/MULTI_TENANCY_ARCHITECTURE_NOTE.md` | 2026-05-30 | Partially stale. Three of six hardcodes already fixed in code but not removed from the doc. |
| `ai_voice_receptionist_session_handoff_may_2026_UPDATED_FINAL.md` | 2026-05-26 / 2026-05-27 | Significantly stale. Pre-Cartesia-active. Has ngrok URL and hardcoded Porto Douro quote. |
| `experimental/telnyx/TELNYX_ADAPTER_IMPLEMENTATION_PLAN.md` | 2026-05-30 | Stale. Says "Pre-implementation" but the adapter is shipped. |
| `docs/context/ARCHITECTURAL_DECISIONS.md` | N/A | **Does not exist** — referenced as a must-read in VOXLANE_MASTER_CONTEXT §14. |
| `docs/context/AI_WORKER_RULES.md` | N/A | **Does not exist** — same. |
| `docs/HANDOVER_2026-05_VOXLANE_VOICE_PLATFORM.md` | N/A | **0 bytes** — placeholder file. |

**Current docs** (not stale):
- `docs/context/VOXLANE_MASTER_CONTEXT.md` (2026-05-31)
- `docs/context/HANDOVER_CURRENT_STATE.md` (2026-05-31)
- `docs/context/RECEPTIONIST_CORE_SPEC.md` (2026-05-31)
- `docs/context/tenant-behaviours/RESTAURANT_BEHAVIOUR_PACK.md` (2026-05-31)
- `docs/context/AUDIO_QUALITY_CODEC_TEST_PLAN.md` (2026-06-01 with 2026-06-02 G722 inbound decode addendum)
- `docs/TENANT_CONFIGURATION_MVP.md` (2026-05-30)
- `docs/TELNYX_DIRECT_WEBSOCKET_ADAPTER.md` (2026-05-30)
- `docs/TELNYX_OUTBOUND_AUDIO_DEBUG.md` (2026-05-31)
- `experimental/livekit/README.md` (2026-05-28) — accurate as a research-only workspace
- The 2026-06-01 update at the top of `docs/PROJECT_STATUS.md` is the freshest project-wide status snapshot.

## 11. Confirmation

**I will not make any code changes until given the next development prompt.** This session was read-only — I read 21 of 23 files, noted 2 missing and 1 empty placeholder, documented the layered receptionist architecture, the Telnyx+OpenAI+Cartesia runtime, the PCMU-safe codec state, the 12+ contradictions and stale docs, and the safe-runtime baseline. Awaiting next instruction.

## 12. Workflow model

The project uses (per VOXLANE_MASTER_CONTEXT §16):
- `superpowers:subagent-driven-development`
- `superpowers:executing-plans`
- Controlled MVP-style worker prompts
- "Change one variable at a time. Do not assume docs marketing means WebSocket contract works the same way. Do not move codecs until the basic audio pipe is proven. Do not treat WebSocket writes as success; success is caller hears audio. Do not commit secrets. Do not trust ngrok for stable voice testing. Do not hardcode tenants. Do not optimise latency before audio works. Do not keep debugging Cartesia when the boundary is Telnyx. Do not keep debugging OpenAI when Cartesia/test tone are bypassed. Do not remove Twilio fallback until Telnyx is stable. Do not implement LiveKit until direct Telnyx has been properly evaluated. Always document the exact boundary." (VOXLANE_MASTER_CONTEXT §17)
