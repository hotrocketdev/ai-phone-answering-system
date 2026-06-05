# VoxLane — Future-Proof HD Voice Architecture

**Author:** AI Engineering Assistant
**Date:** 2026-06-05
**Status:** Architecture proposal, awaiting implementation
**Audience:** VoxLane engineering, project owner
**Supersedes:** `docs/experimental/livekit-hd-spike/PRODUCTION_MIGRATION_RECOMMENDATION.md` (kept for history; see § 13 below)

---

## 1. Product goal

> **VoxLane is a premium AI voice receptionist.**
> Near-human voice quality. Natural real-time conversation. Future-proof architecture. HD voice where technically possible. PCMU/Telnyx is MVP fallback only — not the final product direction.

We are **not** building a basic old-telephony voice bot. The product experience must feel like talking to a person, not a phone tree. That requires:

- **Fullband audio** (≥ 7 kHz, ideally 16-20 kHz) so callers hear consonants, breath, and natural prosody
- **Bidirectional real-time conversation** (sub-500 ms end-to-end latency) so callers can interrupt naturally
- **TTS that sounds human** — natural prosody, breathing, intonation, regional accent support
- **STT that handles UK accents and ambient noise** in restaurants and offices
- **A conversation engine** (intent + memory + tool use) that can run a real back-and-forth, not a single request/response

PCMU at 8 kHz can deliver none of these. It is the safety net, not the product.

---

## 2. Why PCMU is not the final product

| Property | PCMU (G.711 µ-law, 8 kHz) | LiveKit/WebRTC/Opus (48 kHz fullband) |
|---|---|---|
| Frequency response | ~3.4 kHz (telephone) | up to 20 kHz (fullband) |
| Latency floor (codec frame) | 20 ms | 20 ms |
| Typical PSTN end-to-end | 4-12 s first ring + answer | <2 s (browser to worker, no PSTN) |
| Bidirectional feel | Half-duplex-ish (CNG silence on idle) | True full-duplex, AEC-ready |
| Comfort noise | Telnyx CNG at **-34.6 dB** constant | Opus DTX silence (much more natural) |
| Mobile carrier effect | Bandwidth limit, G.711 transcodes, audio degradation | End-to-end Opus, no PSTN transcodes |
| Sound quality perceived | "old telephone" | "near-human, indistinguishable from a real person" |

A caller on a UK mobile in a restaurant can hear a person across the table on a LiveKit call. They cannot on PCMU. For a premium receptionist, that gap is the product.

PCMU *was* the right choice for the MVP. It is no longer the right choice for the product. We keep it for callers who do not have a better path, and we make sure the architecture supports both.

---

## 3. What PCMU remains useful for

- **Stable MVP fallback.** Today's production runs on Telnyx/PCMU and has done so reliably. We do not destabilise it.
- **Regulatory/legacy compliance.** Some call recording laws, emergency services, and certain enterprise contracts expect PSTN-anchored numbers. PCMU gives us that anchor.
- **Number portability.** Telnyx DID numbers are portable; switching vendors later is much easier if PCMU remains the legacy anchor.
- **Sales continuity.** Restaurants calling us through a normal phone number must still work, even if LiveKit has an incident.
- **No-app-required callers.** Some customers will only ever call on their phone. We do not exclude them.

So PCMU is a **first-class fallback path**, with all the operational maturity that comes from 12+ months of production telemetry. The architecture treats it as a peer to LiveKit, just optimised for the narrowband case.

---

## 4. Why LiveKit/WebRTC/Opus is the premium path

LiveKit gives us, in a single managed platform:

- **Real-time WebRTC routing** — sub-100 ms browser-to-server, sub-200 ms server-to-server
- **Opus codec at 48 kHz fullband** — preserves all the audio quality Cartesia generates
- **Bidirectional browser/server SDKs** — same library for browser mic in, server worker out
- **Built-in TURN, ICE, codec negotiation** — no firewall pain for browser clients
- **TURN/SIP integration for PSTN** — when we eventually want to terminate UK PSTN directly
- **Recording, transcriptions, analytics** — first-class observability
- **Edge SFUs** — low latency from anywhere in the world

The spike proved all of this on a small budget. The next step is to make it the *default* path for browser- and app-originated callers, and keep Telnyx/PCMU as the narrowband fallback for plain phone callers.

---

## 5. Dual-path architecture

```
                                 VoxLane Platform
                                       │
                ┌──────────────────────┴──────────────────────┐
                │                                             │
          PATH A: PSTN fallback                       PATH B: HD premium
                │                                             │
   ┌────────────┴────────────┐              ┌──────────────────┴──────────────────┐
   │ Telnyx DID number       │              │ Web/app caller                     │
   │  PSTN/SS7 → Telnyx SIP  │              │ (LiveKit JS SDK, mic + speaker)    │
   │  → Telnyx media stream  │              │  → wss://livekit.voxlane.cloud      │
   │  → VoxLane gateway      │              │  → LiveKit Cloud SFU               │
   │  → Cartesia TTS         │              │  → VoxLane LiveKit worker          │
   │  → PCMU/G.711 back      │              │  → Cartesia Sonic 3.5              │
   │  → PSTN                 │              │  → Opus 48 kHz fullband            │
   │  → caller phone         │              │  → LiveKit Cloud SFU               │
   │                         │              │  → browser speaker                 │
   │ Codec: G.711 8 kHz      │              │                                     │
   │ Latency: 4-5 s answer   │              │ Codec: Opus 48 kHz                  │
   │ Quality: telephone      │              │ Latency: ~1.5-2.1 s first-audio    │
   │ Use: fallback only      │              │ Quality: near-human                │
   └─────────────────────────┘              └─────────────────────────────────────┘
```

### Path A: legacy phone/PSTN fallback

- Telnyx DID number → Telnyx media stream → VoxLane gateway
- Conversation engine: existing PCMU-compatible pipeline (OpenAI intent + Cartesia TTS)
- Codec: PCMU/G.711 8 kHz back to caller
- Purpose: reliable fallback for normal telephone callers, MVP production continuity

### Path B: premium HD voice path

- Browser/app/WebRTC client → LiveKit room → VoxLane LiveKit worker
- Conversation engine: **OpenAI Realtime** (or Deepgram STT + OpenAI + Cartesia TTS for cheaper)
- TTS: Cartesia Sonic 3.5 (Julia voice, pcm_f32le, 48 kHz) — or whichever TTS wins the comparison in § 7
- Codec: Opus 48 kHz fullband back to client
- Purpose: premium near-human demo, future mobile/web app calls, enterprise-quality voice, sales demo, no PSTN 8 kHz ceiling

### Routing logic

A caller dials a VoxLane number (Telnyx DID). VoxLane can:

1. **Auto-detect** if the caller is a web/app client (LiveKit token in the URL/header) → route to Path B
2. **Default to Path A** for any caller where we cannot prove Path B is available
3. **Manually select** via a number mapping (some DID ranges are "HD-only" for sales demos, others are "PSTN only" for failover)

For browser-originated calls (no DID involved), Path B is the only option. Path A is irrelevant.

---

## 6. STT / reasoning / TTS options for the HD path

### STT (speech-to-text)

| Option | Pros | Cons | Spike result |
|---|---|---|---|
| **OpenAI Whisper** | Already in the stack, decent UK accent | 1-2 s latency, batch-mode | Not used in spike |
| **Deepgram Nova-2** | <300 ms streaming, UK accent support, good with noise | New vendor, ~$0.0043/min | Not used in spike |
| **OpenAI Realtime (gpt-4o-realtime)** | Sub-200 ms, native STT+LLM+TTS pipeline, full duplex | $0.06/min in + $0.024/min out, 5x Cartesia TTS cost | **Recommended for HD path** — see Stage 2 below |
| **AssemblyAI Universal-Streaming** | Low latency, good accuracy | New vendor | Optional |

**Decision (provisional):** OpenAI Realtime is the easiest path to "real conversation" but is expensive. For the production-grade HD path, Deepgram + OpenAI + Cartesia is cheaper and more flexible. Spike the Realtime option first, optimise later.

### Reasoning (LLM)

| Option | Notes |
|---|---|
| **OpenAI gpt-4o / gpt-4o-mini** | Already in the stack. Handles restaurant booking, intent, multi-turn. |
| **OpenAI Realtime (gpt-4o-realtime)** | Same model, but with native audio I/O. Use if we go single-vendor for the conversation loop. |

### TTS (text-to-speech)

| Option | Notes |
|---|---|
| **Cartesia Sonic 3.5 + Julia (pcm_f32le, 48 kHz)** | Spike winner. 19 dB SNR improvement. ~$0.05/min. British voice. |
| **Other Cartesia voices / models** | Worth A/B testing — see § 7 |
| **OpenAI TTS (gpt-4o-tts, tts-1-hd)** | Newer, native integration with OpenAI Realtime. Quality unknown vs Cartesia. |
| **ElevenLabs** | Premium voice quality, expensive (~$0.30/min for Creator tier). Known for natural voices. |
| **PlayHT** | Similar to ElevenLabs. |

**Decision (provisional):** Cartesia Sonic 3.5 + Julia is the spike's TTS. Real decision deferred to the § 7 comparison plan.

---

## 7. TTS provider comparison plan (HD path)

The spike validated Cartesia Sonic 3.5 + Julia. Before locking it in for production, we run a structured comparison through the same LiveKit/Opus path.

### Candidates

| Provider | Voice | Model | Encoding | Rate | Notes |
|---|---|---|---|---|---|
| **Cartesia** | Julia (`273f9ef7-...`) | sonic-3.5 | pcm_f32le | 48000 | Spike default |
| Cartesia | Lucy (`2f251ac3-...`) | sonic-3.5 | pcm_f32le | 48000 | Previous default |
| Cartesia | Best of 36 en_GB voices | sonic-3.5 | pcm_f32le | 48000 | A/B winner |
| **OpenAI TTS** | alloy / nova / shimmer / onyx / fable / echo | gpt-4o-tts-2025-01 | pcm (s16le) | 24000 | Native OpenAI integration |
| **ElevenLabs** | Best British voice | eleven_turbo_v2_5 | pcm (s16le) | 22050 or 44100 | Premium quality, expensive |
| **PlayHT** | Best British voice | playht-2.0 | pcm | 24000 | Optional |

### Metrics

- **Noise floor (silence RMS, dB)** — same measurement as spike Variant A vs D (-21.7 dB → -40.6 dB)
- **Naturalness (MOS — Mean Opinion Score, 1-5)** — 5-person listening panel, blind A/B
- **British accent suitability** — qualitative, 3-person panel (1 native UK, 1 neutral, 1 not-UK)
- **Latency (first-audio-byte, ms)** — measured at `time.Now()` after TTS request
- **Cost (per minute of audio, USD)**
- **Streaming support** — can we get the first byte before the full TTS is ready? (key for conversational latency)
- **Production API reliability** — error rate over 100 calls, rate limits, regional availability

### Method

For each candidate, run the spike's `publishPCMAsOpus` pipeline (TTS → pcm → ffmpeg Opus → LiveKit) and measure:

1. Save the raw TTS output to a WAV file at `docs/experimental/livekit-hd-spike/tts-comparison/<provider>-<voice>.wav`
2. Play through the existing browser test (commit `477b0b5` two-way loop), capture VU meter
3. Score against the 5-person panel
4. Capture `first_audio_byte` from the worker log
5. Capture cost from the provider's pricing page
6. Document in `docs/experimental/livekit-hd-spike/TTS_COMPARISON.md`

**Decision deadline:** 2 weeks from start of comparison. After that, lock in one provider for the HD path.

---

## 8. Two-way LiveKit HD conversation loop plan

The spike **already proved this end-to-end** (commits `5fed0b5` + `477b0b5` + `39a86c2`). What remains is the *real* conversation engine on the worker side, not the sync.Once fixed reply.

### What the spike proved (already done)

- Browser mic → LiveKit Cloud → worker subscribes to mic track
- Worker fires first-frame reply (3 s 440 Hz tone, or 5 s Cartesia greeting)
- Browser hears reply, VU meter confirms audio
- Real human in the loop confirmed it works

### What needs to be built next (Stage 1 of the HD path)

Replace the sync.Once fixed-reply with a real conversation loop:

```
browser mic → LiveKit Cloud
  → worker.OnTrackSubscribed
    → VAD (silence detection on inbound frames)
    → STT (Deepgram streaming or OpenAI Realtime)
    → LLM (OpenAI gpt-4o with restaurant prompt)
    → TTS (Cartesia Sonic 3.5)
    → Opus frames → outboundProvider → LiveKit → browser
```

**Success criteria:**

- [ ] User speaks in browser
- [ ] Worker detects voice activity (VAD)
- [ ] STT transcribes with <500 ms latency
- [ ] LLM responds in <1 s
- [ ] TTS streams first byte in <500 ms
- [ ] User hears Alex reply in HD
- [ ] User can interrupt Alex (barge-in)
- [ ] Latency end-to-end: <2 s (well under PCMU's 4-5 s)
- [ ] Quality clearly better than PCMU (subjective listening panel)
- [ ] Production PCMU path untouched

### Implementation estimate

- OpenAI Realtime integration: **2-3 days** (easiest, but expensive)
- OR Deepgram + OpenAI + Cartesia: **5-7 days** (cheaper, more control, more code)
- Barge-in: **+1-2 days** for either option
- Quality validation + iteration: **+2-3 days**

**Recommended start:** OpenAI Realtime path first (faster, proves the loop). Optimise to Deepgram+Cartesia later if cost demands it.

---

## 9. Migration stages

### Stage 0 — Today (already done)

- LiveKit HD spike proven on `feat/livekit-hd-spike`
- Two-way audio loop proven with real human in the loop
- Production PCMU runtime unchanged and serving real customers
- Spike branch isolated, not merged

### Stage 1 — HD conversation loop (next, ~1 week)

- Replace `sync.Once` fixed reply with real conversation engine
- OpenAI Realtime OR Deepgram + OpenAI + Cartesia
- Tested in spike environment only — production PCMU still untouched
- Browser test: 5-minute back-and-forth conversation with Alex
- **Gate to Stage 2:** HD quality clearly better than PCMU, latency under 2 s end-to-end

### Stage 2 — TTS comparison + lock-in (1-2 weeks)

- Run the § 7 comparison plan
- Lock in the chosen TTS provider for HD path
- Document in `docs/experimental/livekit-hd-spike/TTS_COMPARISON.md`
- **Gate to Stage 3:** cost-quality decision made, vendor locked in

### Stage 3 — Production dual-path (2-3 weeks, gated on Stage 1+2)

- Stand up LiveKit Cloud production project (separate from spike)
- Build the routing logic in § 5 (auto-detect browser vs PSTN)
- Add a LiveKit token issuer service (in `voice-gateway/`)
- Canary: 1 restaurant routes through LiveKit if their DID is "HD-enabled"
- Keep PCMU path as default for all existing customers
- **Gate to Stage 4:** canary restaurant happy with HD for 2 weeks straight

### Stage 4 — HD-default for new tenants (next quarter, gated)

- New tenant onboarding defaults to HD path
- Existing tenants stay on PCMU unless they opt in
- Telnyx/PCMU remains available indefinitely as fallback
- **Gate to Stage 5:** >50% of new tenants on HD, quality + cost validated

### Stage 5 — Optional future (gated on business case)

- LiveKit SIP trunk for direct UK PSTN termination
- Drop Telnyx dependency for customers who don't need PSTN
- Native iOS/Android app with embedded LiveKit SDK
- Multi-language support (Opus makes this much easier than PCMU)

### What we do NOT do (per critical rules)

- **Do not** break production PCMU runtime
- **Do not** remove PCMU fallback
- **Do not** remove Twilio fallback
- **Do not** modify production Telnyx webhook
- **Do not** merge `feat/livekit-hd-spike` to main until manager approves a Stage 1+ plan
- **Do not** print or commit secrets
- **Do not** commit debug audio, binaries, `.env`, or build artifacts
- **Do not** treat old 8 kHz PCMU as the final architecture

---

## 10. Rollback / safety

Every stage of the migration is reversible:

| Stage | What we change | Rollback |
|---|---|---|
| Stage 1 | Spike code only, no production | Delete `feat/livekit-hd-spike` branch |
| Stage 2 | Spike code only, TTS provider picked | Switch env var, no code change |
| Stage 3 | Production adds LiveKit token issuer as new code path | Disable feature flag, PCMU path serves all traffic |
| Stage 4 | New tenants default to HD | Change default in tenant onboarding |
| Stage 5 | LiveKit SIP replaces Telnyx for some numbers | Route those numbers back to Telnyx |

**Operational guarantees:**

- Production PCMU runtime, Telnyx webhook, and Twilio fallback **never change** in any stage above.
- Spike work stays on `feat/livekit-hd-spike` until the manager reviews and approves a merge plan.
- Every new HD feature has a feature flag that defaults to OFF in production.
- Every stage has a documented rollback that takes <1 hour.

---

## 11. Success criteria

The HD architecture is "done" when:

- [ ] **Quality**: HD voice is subjectively rated > 4.0/5.0 on a 5-person panel, vs 2.5/5.0 for PCMU.
- [ ] **Latency**: end-to-end first-audio-byte < 2.0 s for browser callers, vs 4-5 s for PCMU PSTN.
- [ ] **Reliability**: HD path uptime ≥ 99.5% measured over 30 days.
- [ ] **Cost**: HD path ≤ 2x the cost of PCMU per minute (target: cost-parity with Telnyx).
- [ ] **Coverage**: at least 2 production tenants successfully using HD for 30+ days with no PCMU fallback incidents.
- [ ] **Barge-in**: caller can interrupt Alex mid-sentence.
- [ ] **TTS lock-in**: TTS provider chosen, documented, contractually committed.
- [ ] **Operational runbook**: on-call team has a written runbook for HD incidents.

---

## 12. What not to do

A non-exhaustive list of things this architecture must **not** become:

- ❌ A pure LiveKit-only product that abandons PSTN callers
- ❌ A pure PCMU-only product that abandons the HD opportunity
- ❌ A single-vendor lock-in (no LiveKit, no Cartesia, no OpenAI, no Telnyx) without a documented exit plan
- ❌ A "ship it and iterate" attitude on production voice quality — quality regressions are a P0 incident
- ❌ A premature merge of `feat/livekit-hd-spike` to main without a migration plan
- ❌ A "two years from now" roadmap item — this is the next 1-2 quarters
- ❌ A browser-only product (must still accept PSTN for normal phone callers)
- ❌ A path that requires end users to install anything (browser-native only)
- ❌ An architecture that locks us out of multi-language, multi-tenant, or multi-region expansion

---

## 13. Relationship to the previous production migration recommendation

The previous document (`docs/experimental/livekit-hd-spike/PRODUCTION_MIGRATION_RECOMMENDATION.md`, commits `7e43594` + `ab1df82`) recommended shipping the Cartesia TTS upgrade on PCMU as the only production change, with LiveKit deferred indefinitely. **The project owner has corrected that recommendation.**

This document supersedes it. The corrected stance:

> PCMU is MVP fallback only. The future product direction is HD voice through LiveKit/WebRTC/Opus or an equivalent modern media path. Do not treat old 8 kHz telephony as the final architecture.

The Cartesia TTS upgrade on PCMU (sonic-3.5 + Julia + pcm_f32le) is still a worthwhile 1-day ship — it improves audio quality on the fallback path while we build the HD path. But it is no longer the *only* production change to consider. The HD path is now an active roadmap priority, not a "we'll get to it eventually" item.

The previous recommendation is preserved in the docs for history. It is not deleted. This document is the new North Star.

---

*End of architecture document. Branch: `feat/livekit-hd-spike`. Implementation pending manager approval of Stage 1.*
