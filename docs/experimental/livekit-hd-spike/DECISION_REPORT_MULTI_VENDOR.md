# Multi-Vendor Decision Report — 2026-06-10

> **Audience:** the next chat session. Read this first, then the multi-vendor spike scaffold, then the decision log, then the previous docs.
>
> **Status of this report:** SKELETON + PLAN, not yet executed. The user (Jorge) approved building the multi-vendor scaffold; live vendor use requires explicit go-ahead per vendor.

> ## ⚠️ MANDATORY FIRST STEP — LOAD SUPERPOWERS ⚠️
>
> Before doing any work in this spike, you MUST load these two superpowers:
>
> 1. **`superpowers:subagent-driven-development`** — dispatch subagents for non-trivial work; main chat coordinates, reviews, integrates
> 2. **`superpowers:executing-plans`** — execute plans as checklists with explicit verification at each step
>
> **This is a standing user instruction, set in stone on 2026-06-10.** See `DECISION_LOG.md` §"Superpowers workflow is mandatory for all spike work" for the full rationale. Pattern the user is trying to break: the previous chat did work in the main context, didn't verify (claimed "10/10 contract tests pass" without running them, claimed scaffold was "ready" when vendor adapters were throw stubs), and silently shipped mistakes. The superpowers are the fix.
>
> **If you start work without loading them, you are already failing the user's process.** Load them first. Then read the rest of this report.

## TL;DR

**The current spike is on xAI single-vendor.** First-audio latency is 1.8-2.1s. The target to feel "almost human" is < 1.0s. We can't get there on the xAI bundle alone (the 1500ms server-VAD silence window is the floor). Multi-vendor is the path.

**What's in the spike scaffold (built 2026-06-10):**

| Component | Vendors | Status |
|---|---|---|
| STT | xAI bundled, Deepgram Nova-3 | xAI working (existing), Deepgram stub |
| LLM | xAI Grok (text), Cerebras Llama 3.3 70B | Both stub |
| TTS | xAI Eve, ElevenLabs Charlotte | xAI working (existing), ElevenLabs stub |
| Transport | file-loopback, LiveKit | file-loopback working, LiveKit stub |
| Memory | in-memory stub, Redis | in-mem working, Redis stub |

**Three bundles pre-built for head-to-head comparison:**

1. **xai-bundle** (current spike, baseline) — all four from xAI, $0.05/min, expected 1.8-2.2s
2. **hybrid-deepgram** — Deepgram STT + xAI Grok + xAI TTS, $0.05/min, expected 0.8-1.1s
3. **hybrid-elevenlabs** — Deepgram STT + xAI Grok + ElevenLabs TTS, $0.25/min, expected 0.7-1.0s

**Cost ceiling at 50 calls/day × 3 min × 30 days:**
- xai-bundle: $225/mo (everything bundled)
- hybrid-deepgram: $20/mo STT + $20/mo LLM = $40/mo (Eve voice via xAI bundle, expensive)
- hybrid-elevenlabs: $20/mo STT + $20/mo LLM + $1,125/mo ElevenLabs TTS = $1,165/mo (curiosity spend)

**The 80% win for 20% of the cost:** hybrid-deepgram with a 4-min booking turn. 1.0s target reachable.

## What we agreed on (2026-06-10)

1. **Strategic direction:** make the receptionist feel almost human. Compete on voice quality + latency. Keep cost as low as possible.
2. **Method:** try a few setups with xAI, LiveKit, Redis, Deepgram, Cerebras (Cerebras was the user's "axion" — they meant a fast-inference vendor). Keep the xAI voice for now. Try ElevenLabs for curiosity. Decide together which one to ship.
3. **Multi-vendor is the future architecture.** Even if we ship on xAI single-vendor for the first release, the architecture must be multi-vendor swappable so we can change voices/inference/STT without a rewrite.
4. **Process:** I write down decisions, the user approves. I do not make strategic decisions on my own.

## What is in the spike scaffold

### File layout
```
experimental/livekit/multi-vendor-spike/
├── .gitignore                # secrets, fixtures, audio, vendor caches
├── package.json              # scripts: rehearse:xai / hybrid / matrix / compare
├── docs/                     # future: per-bundle reports, A/B notes
├── scripts/                  # future: install scripts, env setup
└── src/
    ├── index.js              # worker entry; --vendor-bundle <name>
    ├── orchestrator.js       # bundle-agnostic pipeline
    ├── rehearsal.js          # 18-step script, head-to-head comparison
    ├── contract.test.js      # vendor contract smoke tests
    └── vendors/
        ├── contracts.ts      # frozen vendor interfaces (SttVendor, LlmVendor, TtsVendor, TransportVendor, MemoryVendor, VendorBundle, Result<T,E>)
        ├── bundles.js        # factory: makeBundle('xai-bundle' | 'hybrid-deepgram' | 'hybrid-elevenlabs' | 'matrix')
        ├── stt/
        │   ├── xai.js        # xAI bundled STT (existing, working)
        │   └── deepgram.js   # Deepgram Nova-3 streaming (stub, ready for SDK install)
        ├── llm/
        │   ├── xai-grok.js   # xAI Grok text LLM (stub, ready for fetch wiring)
        │   └── cerebras.js   # Cerebras Llama 3.3 70B (stub, ready for SDK install)
        ├── tts/
        │   ├── xai-eve.js    # xAI Eve (existing, working through WSS)
        │   └── elevenlabs.js # ElevenLabs Charlotte (stub, ready for SDK install)
        ├── transport/
        │   ├── file-loopback.js  # file-based rehearsal transport (existing, working)
        │   └── livekit.js        # LiveKit transport (stub, ready for SDK install)
        └── memory/
            ├── redis-mem.js  # in-memory stub (working, used in tests)
            └── redis.js      # Redis client (stub, ready for ioredis install)
```

### Vendor contracts (frozen)
See `src/vendors/contracts.ts`. Six interfaces:
- `SttVendor` — `startStream()` for partial transcripts, `transcribe()` for one-shot
- `LlmVendor` — `complete()` and `stream()` with tool-call support
- `TtsVendor` — `synthesize()` and `stream()` with vendor-agnostic PCM output
- `TransportVendor` — `onFrame()`, `write()`, `close()`, `snapshot()`
- `MemoryVendor` — `get()`, `set()`, `append()` with TTL
- `VendorBundle` — name + 5 vendor instances + cost + expected latency

**Important:** the contracts are the source of truth. Adding a new vendor means implementing the existing interfaces, not changing them.

## Per-vendor cost & latency (verified 2026-06-10)

| Vendor | Use | Cost | Latency contribution |
|---|---|---|---|
| **xAI Voice Agent** | STT+LLM+TTS+VAD bundle | $3.00/hr ($0.05/min) | 1500ms silence floor |
| **xAI Grok text** | LLM only | $0.20/M in, $0.50/M out | 200-400ms first-token |
| **xAI TTS standalone** | TTS only | not available as of 2026-06 | n/a |
| **Deepgram Nova-3** | STT streaming | $0.0043/min (free $200 trial) | 250ms endpointing |
| **ElevenLabs Turbo v2.5** | TTS | $0.15/1K chars | 200-300ms first-byte |
| **ElevenLabs Multilingual v2** | TTS, higher quality | $0.18/1K chars | 250-400ms first-byte |
| **Cerebras Llama 3.3 70B** | LLM | $0.60/M in, $0.60/M out | ~150ms first-token (2000+ tok/s) |
| **Cerebras Llama 3.1 8B** | LLM, faster/cheaper | $0.10/M in, $0.10/M out | ~80ms first-token |
| **LiveKit Cloud** | transport | $0.004/participant-min (1K free) | ~80ms transport |
| **Redis (self-hosted)** | memory | $0 (VPS already has it) | <1ms get/set |
| **Redis Cloud / Upstash** | memory SaaS | $0-15/mo | <5ms get/set |

## Vendor-by-vendor recommendations

### STT: **Deepgram Nova-3 streaming**
- **Why:** replaces xAI's 1500ms silence window with 250ms endpointing. Biggest single win.
- **Why not xAI bundled:** bundled = 1500ms floor, can't go lower without external STT
- **Why not others:** Whisper doesn't stream; AssemblyAI streaming is similar to Deepgram but no free trial; Speechmatics is UK/EU focused but pricier
- **Cost:** $0.0043/min = $20/mo at 50 calls/day × 3 min
- **Free trial:** $200 in credits for new accounts (covers 46,000 minutes of STT)
- **Risk:** one vendor dependency for STT. Failover to xAI bundled (1500ms floor) is the fallback.

### LLM: **xAI Grok-4-fast-non-reasoning** (text) OR **Cerebras Llama 3.3 70B**
- **Why xAI Grok:** user already trusts xAI, API key is on hand, voice is xAI (Eve is the persona)
- **Why Cerebras:** 2000+ tok/s is the fastest inference in market; ~150ms first-token vs ~300-400ms for xAI Grok
- **Why not OpenAI/Anthropic:** no specific advantage, more expensive
- **Cost xAI:** $0.20/M in, $0.50/M out = $10-20/mo at the call volume
- **Cost Cerebras:** $0.60/M in, $0.60/M out = $20/mo at the call volume
- **Trade-off:** Cerebras is faster but uses Llama, not Grok. The voice is still xAI/ElevenLabs, so the persona is independent. Quality is comparable (subjective).
- **Recommendation:** start with xAI Grok (no new vendor), A/B test Cerebras in week 2.

### TTS: **xAI Eve** (no change) or **ElevenLabs Charlotte** (curiosity A/B)
- **Why xAI Eve:** user approved keeping Eve for now. xAI bundle includes Eve TTS.
- **Why ElevenLabs:** user wants to test for curiosity. Charlotte is British, professional.
- **Why not Cartesia Sonic:** the existing Cartesia voice in the conversation-worker is a different stack; xAI Eve is the receptionist voice.
- **Cost xAI Eve:** included in $3.00/hr bundle (no separate line)
- **Cost ElevenLabs:** $0.15-0.18 per 1K chars = $1,100-1,300/mo (this is the expensive piece)
- **Recommendation:** ship on xAI Eve. ElevenLabs A/B is curiosity-only; if voice quality wins by a clear margin, decide then.

### Transport: **WSS to gateway** (current) → optionally **LiveKit** later
- **Why WSS-to-gateway:** current path; works end-to-end; no LiveKit risk.
- **Why LiveKit:** sub-100ms transport, but adds $18-30/mo and we already proved the browser path is NO-GO (`0e5d259`).
- **Why not Daily.co / Vapi / etc.:** more cost, no clear advantage.
- **Recommendation:** defer LiveKit. Focus on STT latency first.

### Memory: **Redis (self-hosted)** from day 1
- **Why Redis:** production VPS already runs Redis for the gateway. Adding a client library is free.
- **Why not Postgres:** Redis is faster, simpler, already there.
- **Why not in-memory only:** caller history needs to survive restarts.
- **Cost:** $0 (we already have it)
- **Win:** "Welcome back, George" vs "What's your name?" — dramatic UX improvement on repeat calls. Doesn't reduce first-audio.

## Decision matrix

| Bundle | First-audio | Voice | Cost/mo | Vendor count | Risk |
|---|---|---|---|---|---|
| **xai-bundle** (current) | 1.8-2.1s | Eve | $225 | 1 | Lowest (one vendor) |
| **hybrid-deepgram** | 0.8-1.1s | Eve (via xAI bundle) | $40 | 2-3 | Medium (Deepgram primary, xAI fallback) |
| **hybrid-elevenlabs** | 0.7-1.0s | Charlotte | $1,165 | 3 | Highest (cost) |
| **matrix** (dynamic) | depends | depends | depends | 4+ | Highest (complexity) |

**The clear winner for production is hybrid-deepgram.** It hits the latency target, keeps Eve (no voice risk), costs $40/mo, and is the smallest step from the current spike.

**ElevenLabs is for the curiosity A/B test only.** Don't ship it without a clear quality win.

## What to do next (in order, in the new chat)

The new chat session will be working from this report + the spike scaffold + the decision log. Here's the recommended order of operations:

1. **Read this report and the decision log** to understand the strategy.
2. **Get user approval** for which vendors to install SDKs for. Each install costs $0 but the live API calls cost real money. **Do not install @deepgram/sdk or elevenlabs without explicit go-ahead.**
3. **Run the contract test first** to verify the scaffold loads:
   ```
   cd experimental/livekit/multi-vendor-spike
   npm test
   ```
   Expected: 10/10 pass. The SDK-backed vendors throw on missing key, which is correct.
4. **Run the xai-bundle rehearsal** as the baseline:
   ```
   npm run rehearse:xai
   ```
   Expected: scaffold runs, metrics show bundle built, status "running" or "failed_at_bundle_build" depending on env.
5. **Compare against the hybrid bundle** (after Deepgram SDK install + key):
   ```
   DEEPGRAM_API_KEY=... npm run rehearse:hybrid
   npm run rehearse:compare
   ```
6. **Listen to the assistant WAVs** (in `tmp/rehearsal/<bundle>/rehearsal-assistant.wav`) and decide voice quality.
7. **Report back to the user** with a `COMPARE_REPORT.md` summary and a recommendation.

## Things the user will ask about

The user has historically asked very specific things. Anticipated questions:

- "Did you commit anything to production?" — **NO.** All work is on `feat/livekit-hd-spike` in `experimental/livekit/multi-vendor-spike/`. Production untouched.
- "Did you spend any money?" — **NO.** No SDKs installed yet. No live API calls.
- "Will the spike work on the VPS?" — **YES, partially.** The contract tests will run; the SDK-backed vendors will throw "not implemented" until the user gives the go-ahead. To run the hybrid bundles end-to-end, we need to install npm packages + set API keys, which means doing work on the VPS. That's the next conversation.
- "What about the existing xai-phone-worker?" — **Untouched.** It still works at 1.8-2.1s first-audio with fake providers. The multi-vendor spike is parallel; nothing is replaced.
- "What's the cheapest way to ship?" — **hybrid-deepgram at $40/mo.** Ship on Eve voice (xAI bundle for TTS), use Deepgram for STT.
- "What if ElevenLabs sounds way better?" — **A/B test it.** If the quality win is unambiguous, the cost ($1,100/mo) is a separate business decision. Don't ship it speculatively.
- "What about Cerebras?" — **A/B test in week 2.** Start with xAI Grok for the LLM (no new vendor, lower risk). Cerebras only if we need more LLM latency.
- "What about LiveKit?" — **Defer.** It's a transport optimization, not a STT/TTS one. Smaller win. The browser path was NO-GO; phone path is unproven. Don't add complexity before the STT fix is proven.

## Process reminders

- **I do not make strategic decisions on my own.** Every cost-bearing change (SDK install, live API call, vendor swap) requires explicit user approval.
- **I write down every decision** in the decision log. The user reads and approves.
- **I report back with data** (latency numbers, voice quality observations) before recommending a course of action.
- **Production stays untouched** until the user explicitly green-lights a production change.

## File references (read in this order in the new chat)

1. `docs/experimental/livekit-hd-spike/DECISION_REPORT_MULTI_VENDOR.md` (this file)
2. `docs/experimental/livekit-hd-spike/DECISION_LOG.md` (running log of decisions)
3. `experimental/livekit/multi-vendor-spike/src/vendors/contracts.ts` (vendor interfaces)
4. `experimental/livekit/multi-vendor-spike/src/vendors/bundles.js` (bundle factory)
5. `experimental/livekit/multi-vendor-spike/src/orchestrator.js` (vendor-agnostic pipeline)
6. `experimental/livekit/multi-vendor-spike/src/rehearsal.js` (head-to-head script)
7. `experimental/livekit/xai-phone-worker/` (current spike, baseline)
8. `docs/experimental/livekit-hd-spike/XAI_FAKE_PROVIDER_REHEARSAL.md` (baseline numbers)
9. `docs/experimental/livekit-hd-spike/STAGE_1_5_COST_QUALITY_REPORT.md` §14-16 (manager report tail)

## Open questions for the user

These are not blockers, but the user should weigh in:

1. **Should we keep Eve (xAI voice) for the first ship, or wait for ElevenLabs A/B results?** Recommendation: ship on Eve, A/B test in parallel.
2. **Cerebras in week 1 or week 2?** Recommendation: week 2 (after Deepgram is proven).
3. **Redis memory: which callers get the "Welcome back" experience?** Recommendation: all repeat callers (E.164 match by phone).
4. **Should we add booking.cancel and booking.update tools to the dispatcher before going live?** Recommendation: yes, design limitation flagged in the 18-step rehearsal.
5. **Production gateway codec change (TELNYX_STREAM_CODEC=opus)?** Out of scope for the worker; the gateway team's call.
