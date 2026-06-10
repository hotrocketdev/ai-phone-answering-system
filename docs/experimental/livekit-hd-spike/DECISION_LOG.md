# Decision Log — VoxLane voice pipeline

> **Status:** Living document. Every strategic decision the user has made or approved is recorded here. Updated by the assistant after every user-approved change.
>
> **Process:** the user makes the call. The assistant writes it down. No silent decisions.

## 2026-06-10 (this session)

### Decision: Build the multi-vendor spike scaffold
- **What:** Create `experimental/livekit/multi-vendor-spike/` with vendor adapters for Deepgram (STT), xAI Grok (LLM text), ElevenLabs (TTS), Cerebras (LLM), LiveKit (transport), Redis (memory). Bundle factory. Vendor-agnostic orchestrator. Contract tests.
- **Why:** the user wants to A/B test vendors to find the cheapest combo that hits < 1.0s first-audio. Multi-vendor is the future architecture.
- **Cost approved:** $0 (scaffold only). SDK install + live API calls are pending per-vendor approval.
- **Risk:** none to production. All work on `feat/livekit-hd-spike` in `experimental/livekit/`.
- **Status:** COMPLETE. 11 files created. Contract test scaffold ready.

### Decision: Keep xAI Eve voice for first ship
- **What:** the receptionist voice stays as Eve (xAI bundled) for the first production release.
- **Why:** user said "keep xAI voice for now". Eve is good quality, included in the xAI bundle (no separate cost).
- **When to revisit:** if ElevenLabs A/B test shows a clear quality win.
- **Cost:** included in xAI $3/hr bundle.
- **Status:** DECIDED. Implementation in `xai-phone-worker/src/index.js` (existing).

### Decision: Try ElevenLabs for curiosity (A/B test only)
- **What:** include ElevenLabs in the spike as an A/B candidate, not as the production TTS.
- **Why:** user said "I'm curious to try ElevenLabs as well just for curiosity". They want to hear the voice quality, not commit.
- **Cost ceiling:** $0 for the spike (no SDK install yet). If we proceed to A/B test, $5 in trial credits.
- **Status:** PENDING user approval for the $5 trial. Spike scaffold includes the stub.

### Decision: Multi-vendor is the future architecture
- **What:** even if we ship on xAI single-vendor for the first release, the architecture must be multi-vendor swappable.
- **Why:** "it will be easy to change" (user, 2026-06-10). We don't want to rewrite when a vendor changes pricing or a new one comes along.
- **Implementation:** `src/vendors/contracts.ts` is the single source of truth. Adding/swapping vendors is an interface implementation, not a rewrite.
- **Status:** DECIDED. Implementation in spike scaffold.

### Decision: I do not make strategic decisions on my own
- **What:** the user is the decision-maker. The assistant writes things down, presents options, and waits for approval.
- **Why:** the assistant made a call earlier in the week (committing the codec swap rehearsal) without explicit approval. The user clarified that day that we decide together.
- **Status:** PROCESS DECISION. Applies to every future decision.

### Decision: Spike scaffold is built; vendor SDKs are NOT installed
- **What:** the spike has stubs for all 5 vendors, but the npm packages (@deepgram/sdk, elevenlabs, @livekit/agents, @cerebras/cerebras-cloud-sdk, ioredis) are NOT installed.
- **Why:** each install enables a live API call path. The user has to explicitly approve each one.
- **Status:** DECIDED. List of pending approvals below.

### Pending user approval (from 2026-06-10)
1. `npm install @deepgram/sdk` + set `DEEPGRAM_API_KEY` — enables Deepgram Nova-3 STT live test. **Cost:** $0 (free $200 trial for new accounts).
2. `npm install elevenlabs` + set `ELEVENLABS_API_KEY` — enables ElevenLabs TTS live test. **Cost:** $0 (free 10K chars/month tier). **NOTE:** production usage of ElevenLabs is $1,100+/mo, the A/B test is just for the voice.
3. `npm install @cerebras/cerebras-cloud-sdk` + set `CEREBRAS_API_KEY` — enables Cerebras LLM live test. **Cost:** $0 (free trial available, otherwise pay-as-you-go).
4. `npm install @livekit/agents` + set LiveKit env — enables LiveKit transport test. **Cost:** $0 (1K participant-minutes free, then $0.004/min).
5. `npm install ioredis` + set `REDIS_URL` — enables Redis memory on the production VPS. **Cost:** $0 (VPS already has Redis).
6. **Add booking.cancel and booking.update tools to the dispatcher** (design limitation flagged in the 18-step rehearsal; not vendor-related but is a blocker for production).

## 2026-06-09 (yesterday)

### Decision: Stay on xAI single-vendor
- **What:** ship the production worker on xAI single-vendor (Eve voice, grok-voice-latest).
- **Why:** user heard xAI playground voice, said "quite good". Cost was the deciding factor.
- **Status:** SUPERSEDED 2026-06-10 by multi-vendor decision. The xAI bundle is still in the spike as the baseline.

### Decision: opus 24 kHz input
- **What:** generate fixtures and test the worker with PCM16 24 kHz audio (not just PCMU 8 kHz).
- **Why:** the worker already accepts PCM16 24 kHz via `appendAudio`; the codec floor on the worker side is the same. Validating the end-to-end with 24k audio gives the realistic production experience.
- **Result:** first-audio latency dropped 32% (1810-2058ms vs 2990ms). Confirmed in rehearsal 3.
- **Status:** DONE. Commit `a376192`.

### Decision: Strengthen the manager.escalate prompt
- **What:** change "offer to take a message" to "call manager.escalate IMMEDIATELY in the same turn".
- **Why:** the model interpreted the original as "ask the user first" and didn't fire the tool. The off-script vegan menu question didn't escalate.
- **Status:** DONE. Commit `ea48d18`. Production worker has the new prompt.

### Decision: Apply production worker prompt fix
- **What:** apply the strengthened manager.escalate prompt to `src/index.js` (the production worker), not just `src/rehearsal.js`.
- **Status:** DONE. Commit `ea48d18`. 20/20 tests still pass.

## 2026-06-08

### Decision: Build the dispatcher + provider contracts + fakes + Telnyx scaffold
- **What:** design the dispatcher as the single source of orchestration truth. Freeze 4 TS-style provider interfaces. Build in-process fakes for ResDiary/Depos/manager queue. Build file-based Telnyx I/O scaffold.
- **Why:** the manager's directive was "prove the full product flow before live API access arrives." Fake providers let us rehearse the orchestration without waiting for ResDiary.
- **Status:** DONE. Commit `020d631`. 15/15 contract tests + 5/5 Telnyx tests pass.

### Decision: Fake ResDiary date/time validators accept natural language
- **What:** the fake ResDiary provider's `validateDate`/`validateTime` now accept "tomorrow", "seven", weekday names, etc.
- **Why:** the model passes natural language (per the system prompt rule), and the fake validator was rejecting it. Bug found in the first rehearsal run.
- **Status:** DONE.

## 2026-06-07

### Decision: LiveKit browser path is NO-GO
- **What:** abandon the LiveKit browser-spike path.
- **Why:** r8 L16 SDP failure. LiveKit Go SDK outbound Opus transport + browser SDP doesn't support L16.
- **Status:** DECIDED. Commit `0e5d259` archives the spike.

### Decision: Phone path is the path
- **What:** ship via Telnyx media stream (PCM16 24 kHz). Don't try to put the receptionist in a browser.
- **Why:** phone path validated end-to-end with real user voice. The user's locked-in persona is "receptionist on the phone", not a web widget.
- **Status:** DECIDED.

### Decision: xAI Voice Agent + Eve is the locked-in stack
- **What:** STT+LLM+TTS+VAD come from xAI's Voice Agent WSS (grok-voice-latest + Eve British).
- **Why:** manager directive. Most human-like voice, conversational, handles unexpected, not robotic.
- **Status:** SUPERSEDED 2026-06-10 by multi-vendor decision. xAI is still in the spike as the baseline.

### Decision: VAD silence 1500ms (VAD A)
- **What:** xAI's server-VAD silence window is 1500ms (VAD A). Don't change it to 2000ms unless both A and B cut off the user.
- **Status:** DECIDED. This is the lever that the multi-vendor spike attacks.

## Earlier (before 2026-06-07)

- **VoxLane scope:** restaurant receptionist for Porto Douro Restaurants, additive worker mode to existing 3 modes.
- **Behaviour pack:** `docs/context/tenant-behaviours/RESTAURANT_BEHAVIOUR_PACK.md` defines the receptionist rules.
- **Locked tenants:** Porto Douro Restaurants, with a per-tenant behaviour pack.
- **Production constraints:** don't touch production PCMU runtime, .env, systemd, Telnyx production webhook, production gateway.
- **Reversibility:** low (xAI bundle means swapping TTS requires another spike).

---

## How to use this log

- **When a decision is made:** the assistant adds an entry with date, what, why, status, and pending user approvals.
- **When a decision is superseded:** mark "SUPERSEDED" with the date and link to the new decision.
- **When the user asks "what did we decide about X":** search this log first.
- **When a new vendor is added:** the assistant adds an entry under "Pending user approval" until the user gives the go-ahead.
