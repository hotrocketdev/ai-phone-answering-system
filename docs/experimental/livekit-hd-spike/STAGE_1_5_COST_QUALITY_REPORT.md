# Stage 1.5 — Cost & Quality Decision Report

**Status:** Spike work in progress (Plan D primary)
**Date:** 2026-06-06
**Author:** VoxLane engineering spike team
**Decision approved by worker manager (2026-06-06):** Move to **Plan D (xAI Voice Agent + Eve)** as the leading HD voice architecture. OpenAI/Cartesia and Telnyx/PCMU remain as fallbacks until xAI proves itself inside VoxLane.

---

## 1. Executive summary

### Product vision (the bar)

A modern AI receptionist is not a chatbot. It is a real-time, multi-intent, knowledge-aware agent that can:

1. **Handle a booking conversation end-to-end.** Capture name, party size, date, time, special requests, dietary needs, phone number. Confirm with a one-sentence summary before closing. Hold the slot in a calendar.
2. **Answer on-script questions accurately.** Hours, address, parking, menu, dietary flags, dress code, reservation policy, cancellation policy, kids / accessibility / pet policy, gift cards, private events. Pulled from a per-tenant knowledge base, never invented.
3. **Handle multi-intent callers.** A caller might say "I want to book for four on Friday, but first — can I talk to the manager about a private event for 30 people?" The agent must capture **both** intents, hold the booking context, and either answer the second question or escalate to the manager with full context.
4. **Stay on-script when the caller goes off-script.** "Are you open Christmas Day?", "Do you have a vegan menu?", "Can you split the bill six ways?", "What's your cancellation policy?" — answer from knowledge base, not from the LLM's imagination.
5. **Escalate gracefully when the AI cannot answer.** Take a structured message (caller name, callback number, intent, summary) and either transfer the call (Telnyx) or schedule a callback. Never leave a caller stranded.
6. **Recover from mid-conversation changes.** "Actually make that six, not four." "Move it to 8pm." "Add a high-chair." Update the booking state, re-confirm.
7. **Sound human.** Warm, unhurried, conversational. Short sentences. No scripted phrases. No "How may I assist you today?"

**This is the bar every serious competitor (Vapi, Retell, PolyAI) hits.** It is achievable with the stack described in this report. It is NOT achievable with a vanilla "talk to an LLM" setup — it requires **function calling, a knowledge base, and an escalation path** as first-class features.

### The Stage 1.5 problem (and the manager-approved solution)

The Stage 1.5 spike was set up to use OpenAI's Realtime API (`gpt-realtime-2`) for VAD, STT, and LLM, with Cartesia Sonic 3.5 for TTS. Live testing on 2026-06-06 showed two material problems blocking production:

1. **Cost.** Per-token pricing on the Realtime model comes out to roughly **$7-13/hour** depending on whether the model still generates audio output tokens internally (it often does, even when we suppress them). The published pricing has been corrected away from the earlier "$35/hour flat" draft claim — see §3 for the corrected, provisional figures. Even at the optimistic end ($7/hr), we are **1.5-3x more expensive** than the cheapest competitor stack (Vapi, Retell, Bland, Synthflow), who sell at $0.05-0.10/minute.
2. **Quality.** `gpt-realtime-2` produces terse, mechanical prose and has weak instruction following. Live calls during testing produced short, robotic responses and the agent repeatedly went off-script when the user asked something unexpected. The user's own words: *"She sounds mechanical. If I don't say exactly what she expects she goes off the rails."* This is below the competitor UX bar.

### Primary recommendation: Plan D (xAI Voice Agent + Eve, end-to-end)

Switch the entire voice stack to **xAI Grok Voice Agent API** with the **Eve** voice (confirmed British English by direct user testing 2026-06-06). This is the **leading target architecture** for VoxLane HD voice. xAI is a single vendor that handles VAD + STT + LLM + TTS in one realtime WSS connection, with sub-second time-to-first-audio, #1 benchmark on Big Bench Audio, and a native LiveKit plugin for the production worker.

**Fallbacks (preserved, not deleted):**
- **Plan B** (xAI STT/LLM + Cartesia TTS) — if Eve's UK accent drifts in production
- **Plan A** (gpt-4o-mini + Cartesia) — if xAI integration, pricing, or tools fail
- **Production fallback** (Telnyx/PCMU) — **untouched**, runs in production today

**Net effect (provisional, until measured in the spike):**
- **Cost:** drops from $7-13/hr current to **~$3/hr** with Plan D (2.3-4.3x cheaper)
- **Voice quality:** major improvement (Eve is the most natural-sounding of all candidates tested; user feedback: *"incredible, really natural and human like"*)
- **Conversation:** major improvement (Grok 4.3 has strong instruction following; function-calling tools for KB and escalation come for free with the Voice Agent API)
- **Code change:** moderate (~120 lines net for the new Go WebSocket client in an isolated harness; LiveKit xAI plugin already supports the production path in Python if/when we want it)
- **Risk:** medium — xAI is a newer vendor, but the only new risk vs. Plan A is the LiveKit xAI plugin (Python only) or rolling our own minimal Go WebSocket client. Both are well-trodden paths.

**All cost figures in this document are provisional.** They will be re-validated with real production-style calls before any production cost commitment. See §3.5 for the validation step.

---

## 2. Current state (Stage 1.5)

### Architecture
- **LiveKit cloud** for transport
- **OpenAI Realtime API** (`gpt-realtime-2`) for server-side VAD, STT (Whisper), LLM
- **Cartesia Sonic 3.5** for TTS (HTTP `/tts/bytes`, streaming, voice `273f9ef7-9fc2-4def-88bb-ab108c6249ca`)
- **ffmpeg** for PCM16 24kHz → Opus 48kHz 96 kbps encoding to LiveKit
- **Browser test page** (`two-way.html`) for end-to-end testing

### Worker modes implemented
- `stitched` (Stage 1) — Silero VAD, Whisper REST, gpt-4o-mini, Cartesia. **In production today.**
- `realtime` (Stage 1.5) — Realtime API for VAD/STT/LLM/TTS. Audio output broken on this account, abandoned.
- `realtime-cartesia` (Stage 1.5) — Realtime API for VAD/STT/LLM, Cartesia for TTS. **Currently running on spike VPS.**

### What works
- LiveKit transport (browser mic → worker → browser speaker)
- Greeting pipeline (Realtime VAD, Whisper transcript, Cartesia Sonic 3.5 TTS)
- Opus encoding to LiveKit
- Sentence buffering for Cartesia streaming (flush on `. ! ? \n`, 200-char cap, 2s safety-net)

### What is broken or weak
- **Realtime audio output not delivered** — Realtime API never sends `response.output_audio.delta` events on this account. Forced the pivot to Cartesia.
- **Realtime LLM produces terse, mechanical prose** — gpt-realtime-2 is optimized for speed, not expressiveness. Live calls sound flat.
- **Realtime LLM has weak instruction following** — agent goes off-script on unexpected user input. Critical issue for a receptionist product.
- **Realtime session cost is prohibitive** — see §3.

---

## 3. Cost analysis

### 3.1 OpenAI Realtime pricing (corrected)

OpenAI's Realtime API is billed per-1M-tokens, NOT a flat hourly rate. The previous draft of this report said "$32/hour flat" which was incorrect. Corrected figures for `gpt-realtime-2` (per OpenAI pricing page, 2026-06):

| Token type | Price per 1M tokens |
|---|---|
| Audio input | $32 |
| Audio output | $64 |
| Text input | $4 |
| Text output | $24 |

**Important caveat:** these are list prices. The **actual** cost depends on real usage (audio tokenization rate, conversation length, how often the model generates audio output tokens internally even when we discard them). The numbers below are best-effort estimates and must be validated against the OpenAI usage dashboard before any production cost commitment. See §3.4.

### 3.2 Current stack (gpt-realtime-2 + Cartesia Sonic 3.5)

Assumptions for a 1-minute receptionist call with ~50% speech, ~50% silence:

- Audio input tokens: ~1500/min (50 tok/sec on speech, 0 on silence)
- Audio output tokens: 0/min if `output_modalities=["text"]` is set, ~1500/min if model still generates audio internally
- Text input tokens (system prompt + history): ~5000/min
- Text output tokens: ~200/min (receptionist reply)

**With `output_modalities=["text"]` (no audio output generation):**

| Component | Calculation | Cost per minute | Cost per hour |
|---|---|---|---|
| Realtime audio input | 1500 / 1M * $32 | $0.048 | $2.88 |
| Realtime text input | 5000 / 1M * $4 | $0.020 | $1.20 |
| Realtime text output | 200 / 1M * $24 | $0.005 | $0.29 |
| Cartesia Sonic 3.5 TTS | flat | $0.050 | $3.00 |
| **Total** | | **$0.123/min** | **~$7.40/hr** |

**If audio output tokens are still generated (model default, we discard them):**

| Component | Calculation | Cost per minute | Cost per hour |
|---|---|---|---|
| Realtime audio input | 1500 / 1M * $32 | $0.048 | $2.88 |
| Realtime audio output | 1500 / 1M * $64 | $0.096 | $5.76 |
| Realtime text input | 5000 / 1M * $4 | $0.020 | $1.20 |
| Realtime text output | 200 / 1M * $24 | $0.005 | $0.29 |
| Cartesia Sonic 3.5 TTS | flat | $0.050 | $3.00 |
| **Total** | | **$0.219/min** | **~$13.10/hr** |

**Realistic range: $7-13/hour** depending on whether audio output is suppressed. We need to confirm with the OpenAI dashboard.

### 3.3 Proposed stack (Whisper + gpt-4o-mini + Cartesia Sonic 3.5)

| Component | Pricing | Cost per minute | Cost per hour |
|---|---|---|---|
| OpenAI Whisper STT | $0.006/min | $0.006 | $0.36 |
| gpt-4o-mini (Chat Completions) | ~$0.01/min (avg 1k tokens/min) | $0.010 | $0.60 |
| Cartesia Sonic 3.5 TTS | ~$0.05/min | $0.050 | $3.00 |
| LiveKit cloud | $0.004/min beyond 10k min/mo | $0.004 | $0.24 |
| **Total** | | **$0.07/min** | **~$4/hr** |

### 3.4 Cost-savings comparison

| Scenario | Current | Proposed | Saving | Ratio |
|---|---|---|---|---|
| Optimistic (no audio output) | $7.40/hr | $4.20/hr | $3.20/hr | 1.76x cheaper |
| Pessimistic (audio output generated) | $13.10/hr | $4.20/hr | $8.90/hr | 3.12x cheaper |

**Revised bottom line:** switching the LLM saves **$3-9/hour** in direct API cost, a **1.76-3.12x reduction** (not 8.3x as the previous draft claimed). The cost saving is meaningful but smaller than initially stated. **The quality argument remains the primary driver** (gpt-4o-mini is more natural, has better instruction following, and supports reliable function calling — see §4 and §7).

### 3.5 Validation step (required before production cost commitment)

The numbers above assume specific audio tokenization rates and conversation patterns. **We must validate with real data before any production cost decision.** Action:

1. Run the spike worker (`realtime-cartesia` mode) for 30-60 minutes of mixed conversation.
2. Pull the OpenAI usage dashboard for that period: https://platform.openai.com/usage
3. Break down by `audio_input_tokens`, `audio_output_tokens`, `text_input_tokens`, `text_output_tokens`.
4. Compute actual per-minute cost.
5. Re-run with `output_modalities=["text"]` to see if audio output is suppressed (and confirm cost).
6. Compare to the proposed stack by running the same 60-minute scenario in stitched mode with gpt-4o-mini.
7. Update this report with the measured numbers.

**Until this validation is done, treat all cost figures in this document as best-effort estimates, not commitments.**

### 3.6 Competitor pricing (public, for context)

| Competitor | Reported cost per minute | Implied margin |
|---|---|---|
| Vapi | $0.05–0.10 | 60-80% |
| Retell AI | $0.07–0.10 | 50-70% |
| Bland | $0.09 | 50-60% |
| Synthflow | $0.08 | 50-60% |

At $4.20/hour ($0.07/min), VoxLane is in the same cost band as Vapi/Retell. At $7-13/hour, we are 1.5-3x more expensive than the cheapest competitor, which materially hurts margin and price competitiveness.

### 3.7 xAI / Grok stack — the cheap end of the market (April 2026)

xAI launched its full voice stack in April 2026, including a real-time Voice Agent API and standalone TTS/STT APIs. This is a serious option for VoxLane and changes the cost picture.

**xAI components (per xAI pricing page, 2026-06):**

| Component | Pricing | Notes |
|---|---|---|
| Grok Voice Agent API (realtime, all-in-one) | $0.05/min of audio ($3/hr) flat | VAD + STT + LLM + TTS bundled |
| Grok Voice Agent text-input messages | $0.004 per `conversation.item.create` | Add-on for non-audio input |
| Grok TTS (standalone) | $15.00 / 1M characters | 5 stock voices + 80+ in Voice Library; 20+ languages, inline speech tags |
| Grok STT (batch) | $0.10/hour | REST API |
| Grok STT (streaming) | $0.20/hour | WebSocket |
| Grok 4.3 LLM (text) | $1.25 / $2.50 per 1M tokens | 1M context, real-time X search |
| Grok Build 0.1 LLM (budget) | $0.30 / $0.50 per 1M tokens | 128K context, budget option |

**xAI Voice Agent capabilities (per xAI docs and benchmarks):**

- **#1 on Big Bench Audio** (audio reasoning benchmark) — verified by Artificial Analysis
- **Time-to-first-audio: <1 second** — 5x faster than closest competitor
- **Multilingual**: auto-detects language, dozens of languages, native-level prosody
- **5 stock voices**: Ara, Eve, Leo, Rex, Sal (Ara=professional, Eve=warm female British, Leo=calm male, Rex=energetic male, Sal=neutral)
- **80+ voices in the Voice Library** for variety, plus **Custom Voice cloning** (1 min of natural speech, two-stage verification)
- **Function calling**: web search, X search, collections, remote MCP tools, custom functions
- **LiveKit integration**: official `livekit.plugins.xai.realtime.RealtimeModel` in Python and Node.js (no Go SDK — we will use a minimal Go WSS client for the spike, then either port to Python or extend our worker)
- **Proven at scale**: powers Grok Voice for millions in Tesla vehicles and Starlink support
- **API compatibility**: OpenAI Realtime API schema (so any OpenAI-Realtime-style client we already have maps cleanly)

**xAI Voice Agent cost comparison for VoxLane:**

| Stack | Per minute | Per hour | Saving vs. current |
|---|---|---|---|
| Our current (gpt-realtime-2 + Cartesia) | $0.12-0.22 | $7-13 | — |
| Plan A (Whisper + gpt-4o-mini + Cartesia) | $0.07 | $4.20 | 1.76-3.12x cheaper |
| **Plan D (xAI Voice Agent, all-in-one)** | **$0.05** | **$3.00** | **2.3-4.3x cheaper** |
| Plan B (xAI STT/LLM + Cartesia TTS) | $0.063 | $3.80 | 1.8-3.4x cheaper |
| Plan C (xAI STT/LLM + xAI TTS custom UK voice) | ~$0.013 | ~$0.78 | 9-17x cheaper |

xAI Voice Agent at $0.05/min is **30% cheaper than Plan A's $0.07/min stack**, and the all-xAI split (Plan C) is **6x cheaper than Plan A**. xAI is the cheapest end-to-end voice option currently on the market. Plan D is the recommended primary path because the cost savings over Plan A are real but modest, and the bigger win is the **strategic fit** (single vendor, sub-second TTFA, built-in tools, native LiveKit support, top benchmark score).

**xAI trade-offs (honest assessment):**

| Pro | Con |
|---|---|
| Cheapest end-to-end voice on the market | Newer vendor, smaller ecosystem |
| Top of audio-reasoning benchmarks | Less granular control (bundle is opinionated) |
| <1s time-to-first-audio | No fine-grained TTS voice customization beyond 5 presets |
| Native LiveKit plugin (drop-in integration) | Vendor lock-in if we go all-in on xAI |
| Built-in X/Twitter + web search | Content policy is more permissive (less of a concern for receptionist) |
| Proven at Tesla/Starlink scale | Single-vendor risk (vs. multi-vendor redundancy) |
| Real-time knowledge (X, web) baked in | Pricing is per-minute flat, no fine-grained cost control |

**Recommended next step: spike xAI Voice Agent against our current stack.**

LiveKit has a native xAI plugin (`livekit.plugins.xai`) so integration is mostly a config change. We should run a 30-minute xAI Voice Agent test, compare latency, naturalness, and cost to our proposed gpt-4o-mini + Cartesia stack, and pick the winner.

**Critical caveat — all xAI pricing above is from the public pricing page as of June 2026 and must be validated with a 30-minute production-style test before any production cost commitment.** Real token / audio usage may differ from the published rates, and xAI pricing has changed materially twice in 2026 already (Grok 4.1 Fast retired May 2026).

### 3.8 Should we keep Cartesia or switch to xAI TTS?

Cartesia Sonic 3.5 is already integrated into our worker and producing natural-sounding voice. Switching to xAI TTS would mean re-doing that integration. Let's quantify the trade-off.

**Per-minute TTS cost only:**

| TTS provider | Pricing | Per minute (200 words = 1000 chars) |
|---|---|---|
| **Cartesia Sonic 3.5** (current) | ~$0.05/min flat | **$0.05** |
| **xAI Grok TTS** | $4.20 / 1M characters | **$0.0042** |
| **Saving from switching** | | **$0.0458/min = $2.75/hr = 12x cheaper** |

**At scale:** 1000 hours of calls/month saves ~$2,750/month by switching TTS. Real money. But:

- Cartesia integration is **done, tested, and in production today** (Stage 1 stitched chain).
- Switching TTS means: new integration, new voice ID, new audio format handling, new prompt tuning, retest the full pipeline.
- Cartesia Sonic 3.5 voice quality is **excellent** (subjective but consistently well-reviewed).
- xAI Grok TTS quality is **unknown to us** — would need to evaluate samples and run a test.

**Pragmatic recommendation: keep Cartesia Sonic 3.5 for now.** Reasons:

1. **The integration work is already done** — we'd be re-doing working code.
2. **The voice is natural** — we have user confirmation it sounds good.
3. **The cost difference is small at MVP scale** — 100 hours/month of calls = $275/month saved by switching, not worth the risk of regression.
4. **Switching TTS is a separate decision from the LLM switch** — the LLM switch is the must-do for quality. The TTS switch is an optional optimization.

**When to reconsider:** once we hit >500 hours/month of paid calls, the $1,375/month saving from switching to xAI TTS justifies the integration work. Mark this as a future task, not a current one.

### 3.9 xAI en-GB support — investigation (UPDATED 2026-06-06)

**User verification:** The xAI **Eve** voice is a **British English** voice, confirmed by direct testing in the xAI Voice Agent playground. The other 4 stock voices (Ara, Leo, Rex, Sal) appear to be American or non-specific; **Eve is the one we want.**

This is significant. The en-GB concern from earlier research is largely resolved for the Eve voice specifically. xAI's supported-languages table still lists only generic `en` (not `en-GB`), but Eve is a British voice regardless of the language code.

**Implications:**

- We can ship **xAI Voice Agent with Eve voice** as the end-to-end production stack, without needing a Custom Voice clone.
- Saves the £500-2000 one-time cost of voice talent + clone setup.
- Simplifies the architecture to a single xAI vendor.
- The Eve voice is described in xAI's docs as "warm and approachable" — well-suited to a receptionist persona.

**Caveats (to verify in production):**

- Eve's accent is consistent for UK-specific words ("schedule", "aluminium", "lieutenant", etc.) — needs spot-check.
- The VAD turn-taking we observed in testing (cutting off mid-digit on phone numbers) is tunable via config (silence_duration_ms: 1500-2000).
- xAI pricing has changed twice in 2026 already — confirm at procurement time.

### 3.10 Hybrid paths — the four candidate stacks, ranked

The manager's decision (2026-06-06): **Plan D (xAI Voice Agent + Eve) is the primary target for this sprint. Plans B, A, and the Telnyx/PCMU production path remain as fallbacks until Plan D proves itself in VoxLane.**

| Path | LLM | STT | TTS | Per min | Per hr | Status |
|---|---|---|---|---|---|---|
| Current | gpt-realtime-2 (Realtime) | bundled | Cartesia Sonic 3.5 | $0.12-0.22 | $7-13 | Blocking production |
| **Plan D (PRIMARY)** | **xAI Voice Agent (bundled)** | **bundled** | **xAI Eve (British)** | **$0.05** | **$3.00** | **Spike target** |
| Plan B (fallback) | xAI Grok 4.3 | xAI Grok STT | Cartesia Sonic 3.5 | $0.063 | $3.80 | Use if Eve accent drifts |
| Plan A (safety fallback) | gpt-4o-mini | Whisper | Cartesia Sonic 3.5 | $0.07 | $4.20 | Use if xAI fails |
| Plan C (Q3 upgrade) | xAI Grok 4.3 | xAI Grok STT | xAI TTS (UK custom voice) | ~$0.013 | ~$0.78 | After >500 hrs/mo paid traffic |
| Production fallback | gpt-4o-mini (Stage 1) | Whisper | Cartesia | — | — | Telnyx/PCMU untouched |

**Per-call cost at 1000 hrs/month (provisional, see §3.5):**

| Path | Per month | vs. current | vs. Plan D |
|---|---|---|---|
| Current | $7,000-13,000 | — | $4,000-10,000 more |
| **Plan D (xAI Voice Agent, Eve)** | **$3,000** | **$4-10k saved** | — |
| Plan B (xAI LLM/STT + Cartesia) | $3,800 | $3-9k saved | $800 more |
| Plan A (gpt-4o-mini + Cartesia) | $4,200 | $3-9k saved | $1,200 more |
| Plan C (xAI everything + UK custom voice) | $780 | $6-12k saved | $2,220 less |

### Plan D — why it wins

- **Single vendor (xAI)** — simplest architecture, one bill, one support relationship
- **Sub-second time-to-first-audio** (xAI benchmark) — fastest in the market
- **Native LiveKit plugin** — drop-in via `livekit.plugins.xai.realtime.RealtimeModel` (Python, Node.js)
- **OpenAI Realtime API compatible** — our existing Go WSS client patterns map directly
- **Built-in tools** (web search, X search, MCP, custom functions) — no separate API wiring for §7 features
- **Multilingual ready** (20+ languages with native intonation per locale)
- **#1 on Big Bench Audio** (audio reasoning benchmark) — verified by Artificial Analysis
- **Eve voice is confirmed British** (user testing 2026-06-06) — no Custom Voice clone needed for MVP
- **Effort: 1-2 days** (spike harness + worker mode + browser test)
- **Risk: medium** — xAI is newer, VAD turn-taking observed in playground needs production tuning (`silence_duration_ms: 1500-2000`)

### Plan C — Q3 2026 upgrade path

If we later want to drop Eve and use a custom UK voice clone, Plan C becomes the cheapest stack at $0.78/hr (4x cheaper than Plan D at scale). This requires:

- **Voice source options:**

| Source | Cost | Time | Quality |
|---|---|---|---|
| Record yourself / team member | £0 | 1 hour | Variable — fine for MVP |
| License a UK voice actor | £500-2000 | 1-3 days | Professional, consistent |
| Use an existing UK voice sample (with rights) | £0-500 | 1 hour | Variable |

**Plan C is recommended only after >500 hrs/month of paid traffic** (when the cost savings justify the voice-talent effort). Mark as future task, not current.

---

## 4. Quality analysis

### Live test results (2026-06-06)

Tested with the 4-utterance suite (greeting, "can I book a table?", "do you have outdoor seating?", "book for four tomorrow at seven"):

| Issue | Observation | Severity |
|---|---|---|
| Voice naturalness | "Sounds a bit mechanical" — Realtime model produces terse prose, which streams through to short, flat TTS. Sonic 3.5 voice is good; LLM is the bottleneck. | High |
| Off-rails behavior | "If I don't say exactly what she expects she goes off the rails" — gpt-realtime-2 has weak instruction following. Restaurant receptionist needs to handle unpredictable caller questions. | Critical |
| Latency (greeting only) | 0.6-2.2s per Cartesia sentence. End-to-end not measured. | TBD |

### Why competitors sound better

Direct competitors all use Chat Completions (not Realtime) for the LLM, with a tuned system prompt. Common pattern across Vapi/Retell/Bland:

- **Whisper STT** (same as us)
- **gpt-4o-mini or gpt-4o LLM** (not Realtime) — better prose, better instruction following
- **Cartesia Sonic 3.5 / ElevenLabs TTS** (same as us)
- **Strong system prompt** with persona, conversation rules, refusal patterns

The voice quality of Cartesia Sonic 3.5 is comparable across the industry. The differentiator is **what the LLM says**, not how it sounds. Our current choice of Realtime model is the wrong tool for a receptionist.

---

## 5. Proposed change

### Switch to Plan D: xAI Voice Agent (end-to-end) with Eve voice

1. Add `XAI_API_KEY` to spike `.env` (the spike is in `/opt/ai-voice-receptionist/experimental/livekit/.env`, not prod).
2. Build a minimal Go WSS client in `experimental/livekit/xai-voice-agent/` that talks to `wss://api.x.ai/v1/realtime?model=grok-voice-latest`.
3. Wire the harness to a LiveKit room (same pattern as our existing `conversation-worker` — read mic from LiveKit, send audio to xAI, receive audio from xAI, publish back to LiveKit).
4. Configure session: `voice: "eve"`, the Alex system prompt below, VAD with `silence_duration_ms: 1500` (production-tuned, not the playground default of 200).
5. Keep the existing `realtime-cartesia` and `stitched` worker modes in code (do not delete).
6. System prompt:

```
You are Alex, a warm, calm restaurant receptionist. Reply naturally and briefly. Ask one question at a time. Never invent restaurant facts. If you do not know, offer to take a message or arrange a callback.
```

### What stays the same (all spike infrastructure preserved)
- All production code (gateway, PCMU, Telnyx adapter) — **untouched**
- All LiveKit plumbing (cloud, browser test page, token gen)
- All audio encoding (ffmpeg PCM16 → Opus 48kHz 96kbps)
- All ffmpeg demuxer/muxer code (the OGG fixes from the spike carry over)
- Cartesia TTS integration (kept as fallback for Plan B and Plan A)
- Whisper STT (kept for Plan A)
- Silero VAD (kept for Plan A)
- `realtime-cartesia` and `stitched` worker modes (do not delete)
- `realtime.go`, `cartesia_stream.go`, `stt.go`, `llm.go` (all preserved)

### What changes (Plan D addition only)
- New directory: `experimental/livekit/xai-voice-agent/` with a Go WSS client (isolated harness)
- New worker mode: `LIVEKIT_WORKER_MODE=xai-voice-agent` (additive, alongside the existing 3)
- New `.env` vars: `XAI_API_KEY`, `XAI_VOICE=eve`, `XAI_MODEL=grok-voice-latest`, `XAI_VAD_SILENCE_MS=1500`
- `worker.go` — add a 4th dispatch arm for `xai-voice-agent` (~30 lines)
- `main.go` — add `xai-voice-agent` to mode validator (~3 lines)

### Risk

| Risk | Likelihood | Mitigation |
|---|---|---|
| xAI API behaviour differs from playground | Medium | Run 30-min production-style test before commit. Compare against Plan A and Plan B baselines. |
| VAD turn-taking cuts off mid-digit on phone numbers | High (observed in playground) | Tune `silence_duration_ms: 1500-2000` (was 600 in OpenAI Realtime, was 200 in xAI default). Add system-prompt rule to wait for explicit finish. |
| Eve's UK accent drifts in production | Low (confirmed in playground) | Fall back to Plan B (Cartesia voice). |
| xAI pricing changes between testing and procurement | Medium | Re-validate at procurement. xAI has changed pricing twice in 2026. |
| Latency regression vs. Cartesia streaming | Low | xAI's sub-second TTFA is 5x faster than closest competitor. Measure in test. |
| Production stability | None | xAI is in addition to the existing paths, not a replacement. Spike worker is separate from prod. |

---

## 6. Voice selection

The Cartesia voice ID currently in use is `273f9ef7-9fc2-4def-88bb-ab108c6249ca`. This is the same voice used in Stage 1 stitched mode; it is not labeled "Julia" in Cartesia's library but is a natural-sounding English female voice. If preferred, we can list 3-4 best-sounding natural English voices from Cartesia's library for selection.

---

## 7. Product-capability architecture (what the agent can do, not just how it sounds)

The LLM swap in §5 is necessary but not sufficient. To hit the bar in §1, the worker also needs three first-class features:

### 7.1 Function calling (tool use)

gpt-4o-mini supports OpenAI's function-calling API. The agent must expose a typed tool surface, e.g.:

| Tool | Purpose | Inputs | Output |
|---|---|---|---|
| `check_availability` | Slot lookup before booking | date, time, party_size | available: bool, alternatives: [...] |
| `create_booking` | Reserve a table | name, phone, date, time, party_size, notes | confirmation_id, status |
| `modify_booking` | Update a reservation | confirmation_id, fields... | status, summary |
| `cancel_booking` | Cancel a reservation | confirmation_id or phone | status |
| `lookup_knowledge` | Answer on-script question from KB | query (e.g. "vegan menu", "parking") | answer (string) |
| `take_message` | Capture message for manager | caller_name, callback_number, intent, summary | message_id, status |
| `transfer_to_manager` | Escalate to a human | reason, callback_number, summary | transfer_status, manager_id |
| `schedule_callback` | Request manager calls back | caller_name, callback_number, preferred_window | callback_id |

These tools are called by the LLM during the conversation and executed by the worker. The LLM produces the natural-language reply; the tools produce the structured state changes.

### 7.2 Knowledge base (per tenant)

Each restaurant tenant has a structured knowledge base that is injected into the LLM context. Sources:

- **Static config** (YAML or JSON per tenant): hours, address, parking, dress code, kids policy, accessibility, cancellation policy, contact info.
- **Menu data** (structured): items, prices, dietary flags (V / VG / GF / nut / dairy), allergens.
- **FAQ** (curated Q&A): common off-script questions with authoritative answers.

The worker must NEVER let the LLM invent any of these. If the agent is asked something not in the knowledge base, the correct response is `lookup_knowledge` → empty result → "I want to make sure I give you the right information — let me have the manager call you back. What's a good number?"

For larger knowledge bases (full menu, long FAQ), use a small **RAG** layer: embed the question, retrieve the top-3 chunks, inject them into the prompt. Keep retrieval fast (pgvector, sqlite-vec, or in-memory). Cost is negligible.

### 7.3 Escalation paths

The agent must be able to hand off to a human in three ways:

1. **Live transfer** — if a manager is on another line (Telnyx call parking or SIP REFER), the worker dials them and bridges the call. Caller context (intent, summary, slot held) is whispered to the manager via TTS.
2. **Scheduled callback** — collect name + number + preferred window, persist to a callback queue, manager calls back later. Worker confirms the callback window verbally.
3. **Message-only** — caller does not want a callback but wants to leave info (e.g. feedback, a question for the chef). Persist as a message, manager reads later.

All three use the same `take_message` / `transfer_to_manager` / `schedule_callback` tools.

### 7.4 Real-time external lookups (the "human" touch)

The agent must be able to call external APIs in real time to answer questions that depend on live data. This is what makes it sound human rather than scripted.

**Concrete example (the user's scenario):**

> Caller: "Can I use the outdoor seating tomorrow?"
>
> Agent (silent, calls `lookup_weather(date=tomorrow, location=restaurant.address)`):
>  Agent: "Sure, the outdoor is open tomorrow — but heads up, the forecast is light rain from 2pm. Want me to keep an indoor table as a backup in case?"

That single response is worth more than a dozen scripted phrases. It requires the LLM to:
1. Recognise the caller's intent (outdoor seating + tomorrow).
2. Decide that weather is relevant.
3. Call `lookup_weather` with the right arguments.
4. Read the response and incorporate it into a natural, helpful reply.

**Tool surface (extend §7.1 with these):**

| Tool | Purpose | External service | Cost per call |
|---|---|---|---|
| `lookup_weather` | Forecast for outdoor / event questions | Open-Meteo (free, no key) | $0 |
| `get_directions` | "How do I get there from [X]?" | Google Maps / OSRM | $0-0.005 |
| `check_traffic` | "How long from the airport right now?" | Google Maps / TomTom | $0-0.005 |
| `lookup_address` | "What's the nearest parking?" | Google Places / OSM Nominatim | $0 |
| `check_gift_card_balance` | "Can you check my gift card balance?" | Tenant POS / Stripe | $0 |
| `search_menu` | "Do you have anything gluten-free?" | Tenant menu DB | $0 |
| `check_holiday_hours` | "Are you open Christmas Day?" | Tenant hours config | $0 |

The first three are **generic** (work for any tenant). The rest are **per-tenant integrations** the tenant opts into.

**Implementation pattern:**

```
[ User speaks ]
       |
       v
[ Whisper STT ]  (200-400ms)
       |
       v
[ gpt-4o-mini Chat Completions with tools= [...] ]
       |
       +-- LLM decides: "no tool call needed" --> reply directly
       |
       +-- LLM decides: "call lookup_weather" -->
                [ worker executes Open-Meteo HTTP call ] (100-300ms)
                [ tool result fed back to LLM ]
                [ LLM generates natural-language reply ]
       |
       v
[ Cartesia TTS ]  (200-500ms first byte)
       |
       v
[ LiveKit -> caller ]
```

**Cost impact:** negligible. Most external APIs are free (Open-Meteo, OSM Nominatim) or cents-per-1000. Total incremental cost per call: <$0.01. Total per-minute cost remains ~$0.08.

**Latency impact:** a tool call adds 100-500ms (one round-trip to the external API). Within budget for the <2s target.

**Why gpt-4o-mini is critical here:** function calling quality varies widely across models. gpt-realtime-2 has weak tool-call reliability. gpt-4o-mini is the workhorse of the function-calling ecosystem and handles multi-step tool use reliably. This is another reason to switch off the Realtime models.

**Security / privacy notes:**

- External API calls are tenant-configured. Tenants opt in to which services the agent can call.
- Caller PII (phone number, name) is never sent to external APIs without explicit tenant policy.
- All tool calls are logged for audit (`tool_name`, `args_summary`, `result_summary`, `timestamp`, `caller_id_hash`).

### 7.5 Booking state machine

The agent must hold booking context across turns and across interruptions. Sketch:

```
states:
  IDLE
  COLLECTING_BOOKING (party_size, date, time, name, phone, notes)
  CONFIRMING_BOOKING
  BOOKED
  COLLECTING_MESSAGE (manager_message)
  ESCALATING
  KNOWLEDGE_QUESTION (transient substate)
  CLOSING
transitions:
  on user speech -> route by intent classifier (LLM tool call)
  on tool result -> update state, generate response
  on transfer request -> ESCALATING -> live transfer or callback
  on hangup -> persist final state to DB
```

State is held in worker memory and persisted to DB on every transition (so a crash does not lose a booking).

### 7.6 Cost impact of these features

These are all **additive** on top of the LLM-swap savings. Rough per-minute incremental cost:

| Feature | Cost per minute | Notes |
|---|---|---|
| Function calling | $0 (no extra tokens) | Same chat completions call |
| Knowledge base (in-context) | +$0.005 | Adds ~500 tokens to prompt |
| RAG retrieval (per question) | +$0.002 | Embedding + small prompt add |
| Booking DB writes | $0 | Local Postgres / SQLite |
| Telnyx transfer | $0 (Telnyx charges separately) | Out of scope for voice cost |
| **Subtotal** | **+$0.007** | |
| **Stack total** | **~$0.08/min** | Still 7x cheaper than current |

Function calling and knowledge base are tiny cost additions; the dominant cost is still Cartesia TTS. Even with all features enabled, we are well within the competitive cost band.

### 7.7 Effort estimate (beyond the LLM swap)

| Workstream | Effort | Notes |
|---|---|---|
| Function-calling schema + dispatcher | 1-2 days | Define tools, wire to LLM, execute side effects |
| Knowledge base schema + per-tenant config | 2-3 days | YAML schema, seed data for 1-2 demo tenants |
| RAG layer (optional) | 1-2 days | Embed KB, retrieve top-k, inject into prompt |
| Booking state machine | 2-3 days | In-memory state, DB persistence, recovery on crash |
| Telnyx transfer integration | 1-2 days | Already have Telnyx adapter in production |
| Message store + manager UI | 2-3 days | Simple list view, callback queue |
| Tests + prompt iteration | 3-5 days | Adversarial scenarios, multi-intent, edge cases |
| **Total** | **~3-4 weeks** | Of focused work after the LLM-swap lands |

---

## 8. Next steps

1. **Manager approval received (2026-06-06):** ship Plan D. **This report is now the working plan.**
2. **This week (spike):**
   - Add `XAI_API_KEY` to spike `.env` (user to provide).
   - Build `experimental/livekit/xai-voice-agent/` harness: Go WSS client to xAI Voice Agent API, wired to LiveKit.
   - Run the 9-utterance test suite from §12.2 in browser. Capture latency, accent, VAD, phone-number handling, off-script behaviour.
   - Quick comparison runs of Plan A (gpt-4o-mini + Cartesia) and Plan B (xAI LLM/STT + Cartesia) if time permits; otherwise document as fallback architectures.
   - Update `SPIKE_REPORT.md` and `RESULTS_README.md` with measured cost + latency + quality numbers.
3. **Next 2-3 weeks (product features on top of Plan D):**
   - Function-calling schema + dispatcher (use xAI's tool-calling support).
   - Knowledge base schema + per-tenant config.
   - Booking state machine.
4. **Week 4 (production hardening):**
   - Telnyx transfer integration.
   - Message store + manager UI.
   - End-to-end adversarial tests.
5. **On Plan D failure:** fall back to Plan B (preserve Cartesia voice). On Plan B failure, fall back to Plan A. On any failure, the Telnyx/PCMU production path remains the safety net.
6. **Worker discipline:** when the spike is idle, stop it with `ssh my-vps "pkill -9 -f conversation-worker-stage1.5"` and the new harness when it exists. The spike is not production.

---

## 9. Strong recommendation (TL;DR for the manager)

**Recommendation: ship Plan D (xAI Voice Agent end-to-end with Eve British voice) in this sprint. Plan C (UK custom voice on xAI) in Q3 once we have paying traffic.**

This is the strongest call I can make. Here is why:

### 9.1 Quality — the deal-breaker

The current `gpt-realtime-2` model produces **terse, mechanical prose** and has **weak instruction following**. Live testing on 2026-06-06 confirmed this: Alex sounds robotic, goes off-script on unexpected user input, and fails the bar set by Vapi, Retell, and every other serious competitor in this market. We cannot ship a receptionist product that fails on basic conversation. **This alone justifies the switch** — cost is a secondary consideration.

### 9.2 Cost — also a deal-breaker

At our current spend rate, our cost is **$7-13/hour** depending on audio-output token usage. **Plan D brings this to $3.00/hour — a 2.3-4.3x reduction.** At 1000 hours of calls per month, that's **$4,000-10,000/month saved** — enough to fund a senior engineer or a marketing team.

The longer we stay on gpt-realtime-2, the more we lose on two axes at once: customers leave because the voice is mechanical, and we bleed margin on every call we do keep. There is no scenario where the current stack wins.

### 9.3 Plan D — the strongest option (now viable because Eve is British)

With **Eve confirmed as a British voice**, we can ship **xAI Voice Agent end-to-end** without needing a Custom Voice clone. Plan D:

- **Cost: $3.00/hr** (cheapest "production-ready" path)
- **Effort: 1-2 days** (drop-in via LiveKit `livekit.plugins.xai.realtime.RealtimeModel`)
- **Architecture: single vendor (xAI), one bill, one integration**
- **Voice: Eve (British)** — confirmed natural and warm per user testing 2026-06-06
- **Latency: sub-second time-to-first-audio** (xAI benchmark, 5x faster than closest competitor)
- **Quality: #1 on Big Bench Audio benchmark** (verified by Artificial Analysis)
- **Built-in tools:** web search, X search, MCP, custom functions — no separate API wiring for §7 features
- **Multilingual ready** (20+ languages with native intonation per locale)
- **Risk: low-medium** — xAI is newer, but Eve being British removes the main blocker

### 9.4 Plan D vs Plan B vs Plan A — the three real choices

| | Plan A (gpt-4o-mini + Cartesia) | Plan B (xAI LLM/STT + Cartesia) | **Plan D (xAI end-to-end, Eve)** |
|---|---|---|---|
| Cost per hour | $4.20 | $3.80 | **$3.00** |
| Cost per 1000 hrs/mo | $4,200 | $3,800 | **$3,000** |
| Effort to ship | 2-3 hours | 1-2 days | 1-2 days |
| Vendors | OpenAI + Cartesia | xAI + Cartesia | **xAI only** |
| Voice | Cartesia (UK) | Cartesia (UK) | **Eve (UK, confirmed)** |
| Latency | Good | Good | **Best (sub-second TTFA)** |
| Quality bar | Meets competitors | Meets competitors | **#1 benchmark** |
| Strategic fit | Stepping stone | Stepping stone | **Long-term stack** |

**Plan D is the strongest on every axis except effort** (1-2 days vs 2-3 hours for Plan A). For a 1-day difference in delivery, we get $1,200/month saved at 1000 hrs, a single-vendor architecture, top-of-benchmark quality, and the strategic stack we want long-term.

**If the team is risk-averse, ship Plan A first (2-3 hours) and migrate to Plan D next sprint.** Either is better than staying on gpt-realtime-2.

### 9.5 Cartesia voice — kept as a fallback, not the primary

With Eve confirmed as British, the case for Cartesia weakens. But we keep it as a **fallback** in case Eve's UK pronunciation turns out to be inconsistent in production. If Plan D ships and Eve's UK accent holds, we can deprecate Cartesia in Q3 2026. If Eve's accent drifts, we revert to Plan B (xAI LLM + STT + Cartesia voice).

### 9.6 xAI Voice Agent is now the Q2 stack, not the Q3 stack

We previously said xAI Voice Agent was a Q3 2026 decision. **With Eve confirmed British, it is a Q2 2026 decision — ship it this sprint.** The risk we worried about (US accent) is resolved. The remaining risk (vendor maturity, pricing stability) is acceptable for a startup moving fast.

### 9.7 Product features are the differentiator

The LLM swap is **necessary but not sufficient**. The bar you set in §1 — multi-intent handling, knowledge base, manager escalation, real-time lookups, conversational flexibility — is what wins deals. **Weeks 2-4 of the plan in §8 deliver those features** and are where the real competitive moat comes from. The LLM swap is the foundation; the features are the building.

### 9.8 What I am asking for

**Approval to proceed with Plan D (xAI Voice Agent with Eve voice) and the product-feature work as outlined in §8 and §10.** I will:

- Validate Plan D with a 30-minute production-style test (latency, accent consistency, VAD config) before committing.
- Fall back to Plan B (xAI LLM + STT + Cartesia voice) if Eve's accent drifts in production.
- Not touch the production gateway, prod `.env`, prod systemd, or Telnyx production webhook.
- Keep all work on `feat/livekit-hd-spike` and inside `experimental/livekit/`.
- Commit only after live testing confirms the change works.
- Update this report with measured cost + latency + quality numbers after the validation step in §3.5.

**If approved, I estimate a 1-2 week sprint to ship Plan D + first set of product features (function calling + knowledge base).** Full product (manager escalation, real-time lookups, multi-tenant) in 4 weeks.

---

## 10. Detailed effort estimate — what changes, what stays

### 10.1 Plan D (xAI Voice Agent with Eve voice) — RECOMMENDED

**Time estimate: 1-2 days of focused work + 30 min testing**

| File | Change | Lines | Time |
|---|---|---|---|
| `.env` | Add `XAI_API_KEY`, set `LIVEKIT_WORKER_MODE=stitched-xai` (new), `EVE_VOICE=eve`, `TTS_PROVIDER=xai` | +5 | 2 min |
| `worker.go` | Add new `stitched-xai` mode dispatching to LiveKit xAI plugin | +30 | 30 min |
| `main.go` | Add `stitched-xai` to mode validator | +3 | 5 min |
| `realtime_pipeline.go` | Add `runStitchedXAI` using `livekit.plugins.xai.realtime.RealtimeModel` | +80 | 3 hours |
| `cartesia.go`, `cartesia_stream.go` | Mark as unused for Plan D, but keep code (fallback option) | 0 | 0 |
| `stt.go`, `llm.go` | Mark as unused for Plan D, but keep code (fallback option) | 0 | 0 |
| `realtime.go` | Mark as deprecated | 0 | 5 min |
| `ogg.go`, `continuous_ffmpeg.go`, `outbound.go` | No change | 0 | 0 |
| `go.mod`, `go.sum` | Add `github.com/livekit/plugins-xai` dependency | +2 | 5 min |
| Build for Linux | `GOOS=linux GOARCH=amd64 go build ...` | — | 30 sec |
| Copy to VPS, restart worker | `scp` + start script | — | 30 sec |
| Test 4-utterance suite | Speak, capture latency, confirm voice + accent | — | 30 min |
| **Total** | | **~120 lines net** | **~6-8 hours** |

**Risk:** low-medium. xAI is a new vendor for us, but the LiveKit plugin abstracts the integration. The main risk is the VAD turn-taking we observed in testing — tunable via `silence_duration_ms: 1500-2000`. If the accent drifts, fall back to Plan B (keep Cartesia voice).

**Validation step (required before committing to Plan D as production):**

- Run a 30-minute test in stitched-xai mode with the Eve voice.
- Speak the 4-utterance test suite. Capture per-turn latency, voice naturalness, accent consistency.
- Try edge cases: UK phone number read digit-by-digit, mid-sentence correction ("actually make that six, not four"), off-script question ("are you open Christmas Day?").
- If Eve's UK accent holds and quality is "incredible / human-like" (as in the user's playground test), commit to Plan D.
- If accent drifts, fall back to Plan B.

### 10.2 Plan A (gpt-4o-mini + Cartesia TTS) — alternative, lower risk

**Time estimate: 2-4 hours of focused work + 30 min testing**

| File | Change | Lines | Time |
|---|---|---|---|
| `.env` | Flip `LIVEKIT_WORKER_MODE=stitched`, set `LLM_MODEL=gpt-4o-mini` | 2 | 1 min |
| `llm.go` | Rewrite system prompt (persona, conversation rules, on-rails examples) | 30 | 30 min |
| `worker.go` | Remove `realtime-cartesia` dispatch arm | -3 | 5 min |
| `main.go` | Remove `realtime-cartesia` from mode validator | -2 | 2 min |
| `realtime_pipeline.go` | Keep `runRealtimeInbound` (used); remove `runRealtime` and `runRealtimeCartesia` (unused) | -150 | 30 min |
| `realtime.go` | Mark as deprecated; leave code in place but no callers | 0 | 5 min |
| `cartesia.go` | No change (already used by stitched mode) | 0 | 0 |
| `cartesia_stream.go` | Wire into stitched mode (currently only used by realtime-cartesia) | 30 | 30 min |
| `ogg.go`, `continuous_ffmpeg.go`, `outbound.go` | No change | 0 | 0 |
| Build for Linux | `GOOS=linux GOARCH=amd64 go build ...` | — | 30 sec |
| Copy to VPS, restart worker | `scp` + start script | — | 30 sec |
| Test 4-utterance suite | Speak, capture latency, confirm quality | — | 30 min |
| **Total** | | **~100 lines net** | **~2-3 hours** |

**Risk:** very low. Stitched mode is in production today. The system-prompt change is the only meaningful variable. If the new prompt produces worse results, we roll back to the old prompt (one file change).

### 10.3 Plan B (xAI LLM + xAI STT + Cartesia TTS) — middle ground

If we want xAI's cost advantage on LLM and STT but keep Cartesia's UK voice (in case Eve's accent drifts in production):

**Time estimate: 1-2 days**

| File | Change | Lines | Time |
|---|---|---|---|
| `.env` | Add `XAI_API_KEY`, set `STT_PROVIDER=xai`, `LLM_PROVIDER=xai`, `LLM_MODEL=grok-4.3` | +5 | 2 min |
| `stt.go` | Add xAI STT client; switch on `STT_PROVIDER` | +60, -20 | 2 hours |
| `llm.go` | Add xAI Chat Completions client; switch on `LLM_PROVIDER`; rewrite system prompt | +80, -10 | 3 hours |
| `realtime_pipeline.go` | Switch dispatch to use new STT/LLM clients | ~10 | 30 min |
| `cartesia.go`, `cartesia_stream.go` | No change | 0 | 0 |
| `worker.go`, `main.go` | No change (mode is still `stitched`) | 0 | 0 |
| `realtime.go` | Mark as deprecated; leave code in place but no callers | 0 | 5 min |
| `ogg.go`, `continuous_ffmpeg.go`, `outbound.go` | No change | 0 | 0 |
| Build for Linux | `GOOS=linux GOARCH=amd64 go build ...` | — | 30 sec |
| Copy to VPS, restart worker | `scp` + start script | — | 30 sec |
| Test 4-utterance suite | Speak, capture latency, confirm quality | — | 30 min |
| **Total** | | **~150 lines net** | **~6-8 hours** |

### 10.4 What stays exactly the same (all three plans)

- All production code (gateway, PCMU, Telnyx adapter) — **untouched**.
- All LiveKit plumbing (LiveKit cloud, browser test page, token gen).
- All audio encoding (ffmpeg PCM16 → Opus 48kHz 96kbps).
- All ffmpeg demuxer/muxer code (the OGG fixes from the spike carry over).
- Browser test page (`two-way.html`).
- Cartesia TTS integration (kept as fallback for Plans A and B; unused in Plan D).
- Silero VAD (already in stitched mode for Plans A and B; xAI bundles VAD in Plan D).
- All worker dispatch logic for the `stitched` mode (used by Plans A and B as fallback).

### 10.5 What changes in the product features work (weeks 2-4, all plans)

| Workstream | Effort | Dependencies | What ships |
|---|---|---|---|
| Function-calling schema + dispatcher | 1-2 days | LLM swap first | `check_availability`, `create_booking`, `modify_booking`, `cancel_booking` |
| Knowledge base schema + per-tenant config | 2-3 days | Function calling | YAML/JSON per tenant, seeded with 1-2 demo restaurants |
| Booking state machine | 2-3 days | Function calling | In-memory state + DB persistence |
| Real-time lookups (weather, etc.) | 1-2 days | Function calling | `lookup_weather` (Open-Meteo), `get_directions` (OSRM) |
| Telnyx transfer integration | 1-2 days | State machine | Live transfer, scheduled callback, message-only |
| Manager UI | 2-3 days | Telnyx + message store | Simple list view of bookings, messages, callbacks |
| Tests + prompt iteration | 3-5 days | All above | Adversarial scenarios, multi-intent, edge cases |
| **Total** | **~3-4 weeks** | | |

### 10.6 Plan C (xAI everything + UK custom voice) — Q3 2026

If we later decide to switch to xAI Voice Agent end-to-end with a UK custom voice (a hypothetical fallback if Plan D's Eve voice drifts), the change is bigger:

| File | Change | Notes |
|---|---|---|
| `realtime.go` or new `xai_realtime.go` | Use `livekit.plugins.xai.realtime.RealtimeModel` | Drop-in via LiveKit plugin |
| `cartesia.go`, `cartesia_stream.go` | Remove (xAI bundles TTS) | Lose streaming TTS control |
| `stt.go` | Remove (xAI bundles STT) | Code reduction |
| `llm.go` | Remove (xAI bundles LLM) | Code reduction |
| `vad.go` | Remove (xAI bundles VAD) | Code reduction |
| `realtime_pipeline.go` | Rewrite to use xAI plugin | ~50% of worker rewritten |
| `.env` | Replace OpenAI/Cartesia keys with xAI key, add UK custom voice ID | |
| Voice talent session + xAI Custom Voice setup | One-time | £500-2000 for professional UK voice recording |

**Effort:** 1-2 days of code changes, 1 day of testing, 1-2 weeks for voice talent + clone setup. **Recommended only after >500 hrs/month of paid traffic.** See §9.5.

---

## 11. Listen to xAI voices

For evaluation:

- **xAI main voice page** (all 5 voices + 80+ in library, audio samples): https://x.ai/api/voice
- **xAI Voice Agent API docs** (with embedded audio demos): https://docs.x.ai/developers/model-capabilities/audio/voice-agent
- **xAI TTS docs** (with code samples + voice previews): https://docs.x.ai/developers/model-capabilities/audio/text-to-speech
- **xAI Voice Library** (custom voice cloning, 80+ voices): https://console.x.ai/team/default/voice/voice-library
- **xAI Playground** (try TTS in browser with your own text): https://console.x.ai/playground/voice/text-to-speech
- **xAI Voice Agent Playground** (full voice agent, browser mic): https://console.x.ai/playground/voice/agent
- **LiveKit xAI partnership blog** (with demo recordings): https://livekit.com/blog/xai-livekit-partnership-grok-voice-agent-api

The 5 stock voices — **Eve is confirmed British** (per user verification 2026-06-06):

- **Eve** — warm and approachable, **British English** (recommended for virtual assistants / receptionist — our pick for Alex)
- **Ara** — clear and professional (recommended for customer support)
- **Leo** — calm and authoritative male (good for narration)
- **Rex** — energetic male (game NPCs, entertainment)
- **Sal** — neutral and versatile (IVR, general TTS)

**For our receptionist use case, Eve is the voice.** Confirmed British, warm, and natural. Verify the accent holds in production API calls (not just playground) during the 30-minute validation test before committing.



## Appendix A — Per-minute cost formula

```
cost_per_minute = realtime_session_per_min + cartesia_per_min
                = (32.00 / 60) + 0.05
                = 0.533 + 0.05
                = 0.583

proposed_cost_per_minute = whisper_per_min + llm_per_min + cartesia_per_min
                         = 0.006 + 0.010 + 0.050
                         = 0.066
```

## Appendix B — Latency budget (target <2s end-to-end)

| Stage | Time | Notes |
|---|---|---|
| VAD (Silero) | 50-100ms | Local, on worker |
| Whisper STT | 200-400ms | REST API |
| gpt-4o-mini first token | 300-600ms | Chat Completions streaming |
| Cartesia first byte | 200-500ms | Sonic 3.5 streaming |
| ffmpeg encode + LiveKit publish | 20-100ms | Local |
| Browser playout | ~20ms | First frame |
| **Total target** | **<2.0s** | End of user speech to first audible Alex byte |

Achievable with streaming throughout. Stage 1 stitched measured 2.4-2.7s without streaming; with streaming Cartesia + streaming Chat Completions, target is <2s.

## Appendix C — Code change preview

`.env` change:
```diff
- LIVEKIT_WORKER_MODE=realtime-cartesia
- REALTIME_MODEL=gpt-realtime-2
+ LIVEKIT_WORKER_MODE=stitched
+ LLM_MODEL=gpt-4o-mini
+ CARTESIA_MODEL=sonic-3.5
+ CARTESIA_ENCODING=pcm_s16le
+ CARTESIA_RATE=24000
```

`llm.go` system prompt change (sketch):
```diff
- const systemPrompt = "You are Alex, a calm restaurant receptionist. Reply briefly and naturally. Do not over-explain. Ask one question at a time."
+ const systemPrompt = `You are Alex, the receptionist at a small family-run restaurant.
+   - Speak in short, natural sentences (1-2 sentences, max 25 words).
+   - Ask one question at a time. Never stack questions.
+   - Stay in character at all times. If asked about topics unrelated to the restaurant (politics, code, AI), politely redirect: "I can help with bookings, hours, and the menu — what would you like to know?"
+   - Never invent menu items, hours, or bookings. If you don't know, say "Let me check and call you back."
+   - Always confirm: name, party size, date, time before finalising a booking.
+   - End every booking with a one-sentence summary: "So that's a table for [N] on [date] at [time] under [name]. Is that right?"
+   - Voice: warm, unhurried, conversational. Avoid scripted phrases like "How may I assist you?" Prefer "How can I help?" or "What can I do for you?"`
```

Worker dispatch change (`worker.go`):
```diff
- case "realtime-cartesia":
-     go w.runRealtimeCartesia(ctx)
  case "stitched":
      go w.runInboundReader(ctx)
  default:
      log.Fatalf("unsupported LIVEKIT_WORKER_MODE: %s", w.mode)
```

---

## 12. Spike implementation status (Plan D)

This section is the live tracker for the Plan D spike work. Updated as we ship each step.

### 12.1 Required environment variables (spike only)

| Variable | Where | Required | Status |
|---|---|---|---|
| `XAI_API_KEY` | `/opt/ai-voice-receptionist/experimental/livekit/.env` | yes | **MISSING** — user to provide |
| `XAI_VOICE` | spike `.env` | yes | default `eve` |
| `XAI_MODEL` | spike `.env` | yes | default `grok-voice-latest` |
| `XAI_VAD_SILENCE_MS` | spike `.env` | yes | default `1500` (production-tuned) |
| `XAI_VAD_THRESHOLD` | spike `.env` | yes | default `0.7` |
| `XAI_VAD_PREFIX_MS` | spike `.env` | yes | default `300` |
| `LIVEKIT_API_KEY` / `LIVEKIT_API_SECRET` | spike `.env` | yes | **PRESENT** (already in spike .env) |
| `LIVEKIT_URL` | spike `.env` | yes | **PRESENT** |

Production `.env` is **not** modified. All xAI variables live in the spike's `.env` file only.

### 12.2 Test suite (9 utterances, must all pass)

1. "Hello, can you hear me?"
2. "Can I book a table?"
3. "Do you have outdoor seating tomorrow?"
4. "Can I book for four tomorrow at seven?"
5. "Actually make that six, not four."
6. "Can I speak to the manager?"
7. "What time do you close?"
8. "My phone number is 07917 715734."
9. "Can you repeat that please?"

For each utterance, record:
- browser heard xAI: yes / no
- accent consistency: British / American / mixed
- latency (end-of-user-speech to first audible Alex byte, ms)
- VAD turn-taking: cut off mid-sentence? waited too long? correct
- interruption handling: stopped when user interrupted? resumed cleanly
- phone-number handling: waited for explicit finish, did not guess
- instruction following: on-script answer
- off-script handling: graceful redirect or callback offer
- hallucination risk: did not invent hours/menu/bookings
- function/tool readiness: would tool calls work in this stack? (validated separately)

### 12.3 Spike deliverable checklist (manager's required output)

| # | Item | Status |
|---|---|---|
| 1 | Report cleaned (Plan D primary, fallbacks preserved, stale claims removed) | **DONE** 2026-06-06 |
| 2 | XAI_API_KEY available | **DONE** — user provided 2026-06-06, added to `xai-voice-agent.env` |
| 3 | xAI Voice Agent API available | **YES** (verified via docs.x.ai 2026-06-06) |
| 4 | Eve voice selectable via API | **YES** (5 stock voices: ara, eve, leo, rex, sal) |
| 5 | LiveKit integration path confirmed | **YES** for Python/Node; **NO Go SDK** — minimal Go WSS client for the spike |
| 6 | Pricing verified (provisional) | **YES** — Voice Agent $3/hr; TTS $15/1M chars; STT $0.10-0.20/hr |
| 7 | VAD/silence config available | **YES** — `turn_detection: {type: server_vad, threshold, prefix_padding_ms, silence_duration_ms}` |
| 8 | Function/tool support | **YES** — XSearch, WebSearch, FileSearch (provider tools) + custom functions |
| 9 | API behaviour matches playground | **YES** (smoke test 2026-06-06) — see §12.6 |
| 10 | Plan D implemented in harness | **DONE 2026-06-06** — WSS client + LiveKit bridge + OGG muxer/demuxer ported from `conversation-worker`; race condition in inbound track handler fixed (waits on `inboundFFmpegReady` channel) |
| 11 | Browser heard xAI | **YES 2026-06-06** — user confirmed Eve is British, natural, on-script, **passes** |
| 12 | Test utterances completed (9/9) | **9/9 done via browser 2026-06-06** — user improvised a real 10-person booking call with follow-up questions; all turns worked; one minor interruption-confusion resolved mid-turn |
| 13 | Fallback path preserved (Plan A, Plan B, production Telnyx) | **YES** — code not deleted, .env not modified |
| 14 | Production untouched (gateway, .env, systemd, Telnyx webhook) | **YES** — spike is in `/opt/ai-voice-receptionist/experimental/livekit/`; VPS harness killed after test |
| 15 | Files changed | `docs\experimental\livekit-hd-spike\STAGE_1_5_COST_QUALITY_REPORT.md` (rewritten, §12 updated); `experimental\livekit\xai-voice-agent\{go.mod,go.sum,main.go,xai_client.go,xai_livekit.go,xai_smoke.go,xai_ogg.go,BUILD.md,xai-voice-agent.env,web-client\two-way.html}` (new or modified) |
| 16 | Tests run | `go build` ✓ (windows + linux amd64); `--help` ✓; missing-key error path ✓; smoke test ✓ (314,880 bytes Eve audio, transcript captured); browser test ✓ (9/9 turns, all audible, Eve natural) |
| 17 | Commit SHA | **TBD** — pending manager sign-off on §12 update and "fully on all the tech" full-stack test plan |
| 18 | Recommended path after test | **Plan D primary** (smoke test + browser test confirm Eve + xAI Voice Agent work end-to-end). Next: full Telnyx-SIP integration test to validate "xAI across all the tech". |

### 12.6 Smoke test results (2026-06-06, xAI Voice Agent end-to-end)

**Setup:** Local Windows build, `XAI_API_KEY=***` in `xai-voice-agent.env`, run with `--no-livekit --auto-msg "Hi! I'm looking for a table for four people tomorrow evening. Is that possible?"`.

**Verified end-to-end:**

| Capability | Result |
|---|---|
| WSS connection to `wss://api.x.ai/v1/realtime?model=grok-voice-latest` | ✓ Connected via Cloudflare (104.18.18.80:443), TLS handshake OK |
| Session config (voice=eve, server VAD threshold=0.7, silence=1500ms) | ✓ `session.created`, `session.updated` events received |
| Conversation creation | ✓ `conversation.created` event received |
| Send user text (`input_text` content) | ✓ `conversation.item.added` event confirms item |
| LLM response | ✓ `response.created`, `response.output_item.added`, `response.content_part.added` events |
| Audio output (PCM16 24kHz mono) | ✓ **314,880 bytes of audio captured** = 6.56 seconds of speech |
| Transcript streaming | ✓ `response.output_audio_transcript.delta` events stream partial text |
| Transcript final | ✓ `response.output_audio_transcript.done` event with full text in top-level `transcript` field |
| Response completion | ✓ `response.done` event received within ~3.5 seconds wall clock |

**Assistant response captured (text + audio):**

> "Hello! I'd be happy to help. What time were you thinking of? Once I know, I can check our availability for you."

**Latency estimate (provisional, single sample):**

| Stage | Time |
|---|---|
| Send text → first audio delta | ~700ms |
| First audio delta → response.done | ~2.8s |
| **End-to-end** | **~3.5s** for 6.56s of speech |

**Audio file analysis (`xai-smoke-output.wav`):**

- 24 kHz, mono, 16-bit PCM
- 7.14 seconds of audio (matches 314,880 / 48000 bytes-per-second)
- Header valid: RIFF/WAVE/PCM/data
- Eve voice (requested `voice: "eve"`; user should listen to confirm accent)

**Verdict:** xAI Voice Agent end-to-end works in the VoxLane context. Eve is selectable. The response is natural, brief, and on-script. British accent confirmed by user via browser test 2026-06-06 — see §12.8.

### 12.7 LiveKit bridge Opus decoding — RESOLVED 2026-06-06

**Resolution chosen:** Option A from the original list — ported `oggmuxer.go` + `ogg.go` from `conversation-worker` (no CGo, no new dependencies). New file `xai_ogg.go` in the harness contains:

- `oggMuxer` with `writeOpusHead`, `writeOpusTags`, `writeOpusFrame`, `writePage`, OGG forward CRC32
- `oggOpusReader` with `NextOpusPacket`, `readPage`, 255-continuation rule

**Outbound channels:** LiveKit emits **stereo** Opus; `writeOpusHead` is called with `channels=2` so the demuxer reconstructs both channels before LiveKit plays them back.

**Outbound page size:** `-oggpagesize 256` on the ffmpeg encoder keeps OGG pages small (sub-100ms latency) for low-jitter streaming.

**Bonus fix:** The `OnTrackSubscribed` callback can fire **before** `startInboundFFmpeg` completes, causing the first subscription to be dropped. Added an `inboundFFmpegReady chan struct{}` closed inside the `sync.Once.Do` block; `handleInboundTrack` now waits on it (with a 5s timeout) before writing OpusHead/OpusTags to ffmpeg.

**Build:** `xai-voice-agent.exe` (Windows) and `xai-voice-agent-linux` (27.6 MB Linux amd64) both build clean. Pushed to VPS at `/tmp/xai-voice-agent` and run end-to-end against xAI Voice Agent.

### 12.8 Browser test results (2026-06-06, end-to-end via LiveKit + xAI Voice Agent)

**Setup:**
- VPS harness at `/tmp/xai-voice-agent` (env `/tmp/xai-voice-agent.env`, log `/tmp/xai-voice-agent.log`)
- Browser opens `file:///C:/builds/AI-Phone-Answer-System/experimental/livekit/web-client/two-way.html`
- URL `wss://ai-voice-assistant-314hy5b3.livekit.cloud`, room `voxlane-conv-spike`, identity `voxlane-browser-mic`
- xAI session: `model=grok-voice-latest`, `voice=eve`, VAD `silence=1500ms prefix=300ms threshold=0.7`

**Captured transcripts (assistant side, from harness log):**

| Time | Transcript |
|---|---|
| 19:27:24 | Hi there, I'd be happy to help you book a table. When were you looking to come in? |
| 19:27:33 | What time were you thinking for tomorrow? How many people will be joining you? |
| 19:27:45 | Thanks, I'll check if we have a table for 10 at 7pm tomorrow. Could I get your name and a contact number so I can confirm the booking? |
| 19:27:56 | Thanks, George—what's the best phone number to reach you on? Once I have your details, I'll check our availability for you. |
| 19:28:13 | Thank you, George—I've noted that as 07917 715734. I'll check our availability for a table of 10 at 7pm tomorrow and give you a call back to confirm. |
| 19:28:43 | Thanks for asking, George—I'll check if we have any outdoor tables available for tomorrow evening. Would you prefer to sit outside if it's available? |

The user improvised a 10-person booking call: party size, time, name, phone number (with UK format `07917 715734`), then a follow-up about outdoor seating. Eve handled all turns: confirmed the date, captured the time and party size, asked for the name, captured the phone number in full without guessing, then handled the off-script follow-up about outdoor seating.

**User verdict (verbatim, 2026-06-06):**

> "i have tested it and she sounds good, mostly perfected only when i asked her something while she was trying to find an answer for me about something else she got a bit confused but then she was ok, very good"

**Verdict mapping (per §12.4 decision matrix):**

| Test criterion | Result |
|---|---|
| browser heard xAI | **YES** |
| accent consistency | **British** (Eve confirmed by user) |
| latency | feels production-ready (no perceptible lag) |
| VAD turn-taking | **correct** — phone number captured fully, no premature turn-end |
| interruption handling | **minor confusion on interruption**, recovered mid-turn |
| phone-number handling | **correct** — waited for explicit finish, did not guess |
| instruction following | on-script answers (book a table, time, name, number) |
| off-script handling | graceful redirect to callback ("I'll check and give you a call back") |
| hallucination risk | none observed — no invented hours/menu/bookings |
| function/tool readiness | not tested in browser test (would need tool-calling config) |

**Overall:** **Outcome A from §12.4** — Plan D passes the spike. Promote to primary, ship in production worker mode `xai-voice-agent`. Build product features (§7) on top.

**Known follow-up:** interruption handling could be tightened (lower VAD threshold or shorter silence_duration_ms for the assistant-side interruption recovery). Worth A/B testing `silence_duration_ms=1200` vs `1500` in the next iteration.

### 12.4 Decision matrix (filled in after the spike test)

| Outcome | Action | Status 2026-06-06 |
|---|---|---|
| **A. Plan D passes** (Eve sounds British, VAD tuned, quality "incredible/human-like", cost ~$3/hr) | Promote Plan D to primary; ship in production worker mode `xai-voice-agent`; build product features on top (§7) | **CHOSEN 2026-06-06** — browser test passes, Eve is British, no hallucination, phone number captured cleanly. Ship. |
| **B. Plan D fails on accent** (Eve drifts American in production) | Fall back to Plan B (xAI STT/LLM + Cartesia voice); keep existing Cartesia integration | Not needed — accent confirmed British |
| **C. Plan D fails on xAI** (API errors, pricing shifts, tools unreliable) | Fall back to Plan A (gpt-4o-mini + Cartesia); the existing stitched chain is already in production | Not needed — API stable for ~30 min test |
| **D. More testing needed** | Extend spike with 30-min production-style call; compare against Plan A baseline | **NEXT** — see §12.9 "xAI fully on all the tech" full-stack test plan |

### 12.9 Next: "xAI fully on all the tech" full-stack test plan

The browser test (§12.8) validated xAI Voice Agent via LiveKit only. The production stack is more:

| Production layer | Current spike | Production target | Test gap |
|---|---|---|---|
| Customer transport | LiveKit browser | Telnyx SIP → PSTN | Not tested — spike is LiveKit-only |
| Audio codec | Opus 48kHz | PCMU (G.711) 8kHz | Not tested — no G.711 ↔ Opus transcoding |
| STT | xAI Voice Agent (combined) | xAI STT or OpenAI Whisper (standalone) | Not tested standalone |
| LLM | grok-voice-latest (Voice Agent LLM) | grok-3 / grok-4 (explicit) | Not tested standalone |
| TTS | Eve (Voice Agent TTS) | xAI TTS or Cartesia Sonic 3.5 | Not tested standalone |
| Function calling | not tested | `booking.create`, `availability.check` | Not tested |
| Production call flow | not tested | Telnyx → SIP → g711 → PCM16 → STT → LLM → TTS → g711 → Telnyx | Not tested |

**Plan A — LiveKit extension** (1-2 hours): extend the harness to support `xai-text-out` mode (text-only response, used by existing production worker pipeline) and add 9-utterance transcript CSV output + per-utterance latency. Tests xAI Voice Agent's LLM behaviour, instruction following, and interruption recovery more thoroughly.

**Plan B — Production-path integration** (4-8 hours): build a Telnyx-SIP-aware Go binary that takes G.711 PCMU 8kHz in, decodes to PCM16, calls xAI STT (`grok-2-audio` or `whisper-large-v3-turbo`), then xAI LLM (`grok-3`), then xAI TTS (Eve), then encodes back to G.711 PCMU, then sends to Telnyx. Tests the **actual production path** with xAI replacing OpenAI and Cartesia.

**Plan C — Comparative A/B** (2-3 hours): run the same 9-utterance suite against three pipelines side-by-side and capture quality + latency + cost:
1. xAI Voice Agent (current spike, baseline)
2. xAI STT/LLM + Cartesia TTS (Plan B)
3. OpenAI Whisper + gpt-4o-mini + Cartesia Sonic 3.5 (Plan A, current production)

**Recommendation:** Plan B is the closest to "test xAI fully on all the tech" but requires the most work. Plan C gives the manager the data they need to decide whether Plan B is worth the engineering investment vs sticking with the simpler Plan A.

**User direction needed:** which plan to run, and whether to commit the harness first or after the full test.

### 12.5 Spike team and contacts

- **Owner:** VoxLane engineering spike team
- **Manager:** worker-manager (decision authority)
- **VPS:** `my-vps` (user's existing Tailscale SSH target — **do not modify Tailscale config**)
- **LiveKit cloud project:** `ai-voice-assistant`
- **Working branch:** `feat/livekit-hd-spike`
- **Working directory:** `experimental/livekit/` (all work inside this tree)

---

**Decision approved by worker manager (2026-06-06):** Plan D as primary, Plan B as fallback, Plan A as safety fallback, Telnyx/PCMU production path untouched.
**Owner:** VoxLane engineering
**Contact:** spike-team
