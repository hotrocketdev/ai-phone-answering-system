# VoxLane Voice Quality Stack Strategy

**Date:** 2026-06-03
**Status:** Strategy review. No runtime changes.
**Author:** VoxLane engineering (controlled review)

---

## 1. Current Voice Stack Reality

### What is working
- Telnyx direct WebSocket media streaming is operational.
- PCMU 8 kHz narrowband is the production baseline.
- G722 16 kHz wideband is technically viable (tested 2026-06-03) and available behind env flags.
- Cartesia TTS (`aura-2-pandora-en` / `sonic-3.5` / speed 1.0) is working and produces high-quality audio at the source.
- OpenAI Realtime (`gpt-realtime-1.5`) is working as the reasoning engine.
- Natural booking flow is live (commit `1bf8422`).
- All 5 booking slots capture reliably (date, time, guest count, name, phone).
- Echo suppression, VAD, and frame pacing are correct.

### What is not solved
- Caller-heard audio quality is still "old telephone" — narrowband frequency response, constant Telnyx comfort noise floor (~−34.6 dB), no HD voice.
- G722 did not materially improve perceived quality (user-reported: "more or less the same"). The marginal gains in latency, mechanical sound, and transcript quality do not justify switching.
- The Telnyx comfort noise is a Telnyx-side behavior (likely CNG) and cannot be fixed at the VoxLane layer without breaking VAD.
- The conversation still feels "a bit automated" — natural-flow helped but the narrowband audio quality limits the "near-human" perception.

### Why G722 did not solve it
- G722 widens the frequency response from ~3.4 kHz (G.711) to ~7 kHz, but it is still narrowband compared to HD voice (Opus at 32-48 kHz, ~20 kHz frequency response).
- Cartesia generates high-quality audio (24 kHz PCM) but it gets downsampled to 8 kHz (PCMU) or 16 kHz (G722) for the PSTN leg. The caller hears the downsampled version, not the HD source.
- The Telnyx comfort noise is independent of codec and is present at the same level on both PCMU and G722.
- G722 is a codec improvement, not a quality paradigm shift. The "old telephone" feel is a PSTN limitation, not a codec limitation.

### What is acceptable in current MVP
- Stable calls with no drops, no errors, no WebSocket failures.
- Natural booking flow that captures all fields reliably.
- Cartesia voice quality at the source (high fidelity before downsampling).
- Echo suppression that prevents feedback loops.
- Frame pacing that prevents burst-send artifacts.

### What is not acceptable for premium product quality
- Caller-heard audio that sounds like a 1990s phone call.
- Constant low-level noise floor on the line.
- Narrowband frequency response that makes Cartesia's natural voice sound compressed and tinny.
- No HD voice path for callers who could receive it (web app, mobile app, SIP clients).

---

## 2. Can Telnyx Direct WebSocket Reach Target Quality?

### Answer: No — not for PSTN callers.

The fundamental constraint is the PSTN last mile. For any caller dialing a phone number:

| Layer | Sample Rate | Frequency Response | Quality Tier |
|-------|-------------|-------------------|--------------|
| Cartesia source | 24 kHz | ~12 kHz | HD |
| G.711 PCMU (PSTN) | 8 kHz | ~3.4 kHz | Narrowband (PSTN standard) |
| G.722 | 16 kHz | ~7 kHz | Wideband (ISDN/VoIP) |
| Opus (WebRTC) | 48 kHz | ~20 kHz | HD (near-human) |
| EVS / AMR-WB | 32 kHz | ~14 kHz | HD (mobile carrier) |

**PSTN is narrowband by design.** The copper/fiber last mile to the caller's phone, and the caller's phone handset, limit the frequency response to ~3.4 kHz (G.711) regardless of what codec the VoIP provider uses internally. G.722 extends this to ~7 kHz if the caller's carrier and handset support it, but this is not universal in UK PSTN.

### Specific limitations of Telnyx direct WebSocket + PCMU/G722
1. **PSTN ceiling**: G.711 PCMU is the PSTN standard. No codec upgrade within this path will exceed ~3.4 kHz frequency response for the caller.
2. **Telnyx media streaming**: Telnyx's WebSocket delivery uses the codec negotiated with the caller's carrier. We can request G.722 but the caller's carrier may negotiate down to PCMU.
3. **Codec negotiation**: Even if we set `TELNYX_STREAM_BIDIRECTIONAL_CODEC=G722`, the actual codec used depends on the caller's carrier and phone. We cannot force G.722 end-to-end through PSTN.
4. **Cartesia transcoding**: Cartesia's 24 kHz output is downsampled to 8 kHz (PCMU) or 16 kHz (G722). The HD audio is lost in the downsampling.
5. **Phone number vs WebRTC/SIP**: A phone number means PSTN. The only way to get HD audio to a caller is if they connect via WebRTC, SIP, or a carrier that supports wideband (AMR-WB / EVS).

### What this means
- For PSTN callers (dialing +44 121 823 0230), the maximum achievable quality with Telnyx direct WebSocket is G.722 wideband (~7 kHz), and only if the caller's carrier and handset support it. In practice, most UK PSTN callers will get G.711 narrowband (~3.4 kHz).
- For non-PSTN callers (web app, mobile app, SIP clients), we can deliver HD audio by using a media path that supports Opus/WebRTC.
- The "near-human voice quality" target requires escaping the PSTN constraint, which means offering a non-PSTN access point.

---

## 3. Stack Path Evaluation

### Path A — Stay on Telnyx direct WebSocket + PCMU/G722

**Pros:**
- Already working, stable, production-tested.
- Simplest production path.
- Easiest to ship MVP.
- PCMU is the universal fallback.
- G722 is available as a marginal upgrade.

**Cons:**
- Hard quality ceiling at ~3.4 kHz (PCMU) or ~7 kHz (G722) for PSTN callers.
- Telnyx comfort noise is present and cannot be removed without breaking VAD.
- "Old telephone" feel limits premium product positioning.
- No HD audio path for any caller.

**Verdict:** Acceptable for MVP. Not acceptable for premium product quality target.

---

### Path B — Telnyx direct WebSocket + improved settings/support

**Investigate:**
- Telnyx support ticket about CNG / comfort noise removal.
- Whether Telnyx supports wideband codecs (AMR-WB, Opus) on UK PSTN numbers.
- Whether inbound/outbound codecs can be separated (e.g., Opus from Cartesia → G.722 to PSTN).
- Whether Telnyx UK routing can be improved to reduce noise/jitter.
- Whether Telnyx has an HD voice product (some carriers offer AMR-WB on 4G/5G).

**Pros:**
- Stays within the existing Telnyx relationship and infrastructure.
- Low implementation cost if Telnyx has a solution.
- May resolve comfort noise (if Telnyx can disable CNG on our account).

**Cons:**
- Limited by PSTN carrier and handset — cannot exceed G.722 wideband for most callers.
- AMR-WB/EVS requires carrier and handset support (not universal in UK).
- Depends on Telnyx support response (unknown timeline).
- Still no HD audio path for non-PSTN callers.

**Verdict:** Worth a support ticket (low cost, potential comfort noise fix), but will not achieve the "near-human" target for PSTN callers.

---

### Path C — LiveKit HD media path

**Evaluate:**
- LiveKit provides WebRTC-based media with Opus codec (48 kHz, ~20 kHz frequency response — near-human quality).
- LiveKit SIP can bridge to PSTN (still narrowband for phone callers, but LiveKit gives us the HD path for non-PSTN callers).
- Cartesia → LiveKit → Opus 48 kHz → web/mobile app caller = near-human quality.
- Cartesia → LiveKit → SIP → Telnyx → PSTN = still narrowband for phone callers, but we can still use G.722 to maximize what's available.
- OpenAI Realtime stays as the reasoning engine (LiveKit handles media, not reasoning).
- Implementation: LiveKit server (self-hosted or cloud), LiveKit SDK in voice-gateway, LiveKit SIP trunk to Telnyx.

**Pros:**
- HD audio path for web app, mobile app, and SIP clients.
- Eliminates codec quality ceiling for non-PSTN callers.
- Future-proof: as more callers move to web/mobile, the HD path becomes the default.
- LiveKit is open-source and self-hostable.
- Cartesia's HD output is preserved (no downsampling to PSTN levels).
- OpenAI Realtime integration stays the same (LiveKit handles media transport, not AI reasoning).

**Cons:**
- Significant implementation effort (LiveKit server, SDK integration, SIP trunk, web/mobile client).
- New infrastructure component to maintain and monitor.
- PSTN callers still get narrowband (but we can still use G.722 to maximize).
- LiveKit SIP to Telnyx may have its own audio quality issues (needs testing).
- Two media paths to maintain (LiveKit for web/mobile, Telnyx direct for PSTN) until PSTN callers migrate.

**Verdict:** The correct path for the "near-human voice quality" target. High implementation cost but enables the quality tier that justifies the premium product positioning.

---

### Path D — Alternative telephony/media provider

**Evaluate:**
- SignalWire: similar to Telnyx (PSTN + WebRTC). Same PSTN limitation.
- Daily.co: WebRTC-first, no PSTN. Would require a separate PSTN bridge.
- Twilio: similar to Telnyx. Already configured in env but not actively used. Same PSTN limitation.
- Other SIP providers: same PSTN limitation.

**Pros:**
- May have better UK routing, different comfort noise behavior, or HD voice support.
- SignalWire has good WebRTC support and competitive pricing.

**Cons:**
- PSTN limitation applies to all providers for phone callers.
- Switching providers is high-risk (rebuild webhook integration, test carrier interop, migrate phone number).
- No provider can escape the PSTN narrowband ceiling for phone callers.
- The marginal benefit (possibly less comfort noise, possibly G.722 support) does not justify the migration cost.

**Verdict:** Not recommended. The PSTN ceiling is universal. Switching providers does not solve the fundamental constraint.

---

## 4. Voice Quality Target

### Measurable targets for near-human caller experience

| Metric | Target | Current (PCMU) | Gap |
|--------|--------|----------------|-----|
| Greeting start time | < 1.0s after stream ready | 0.27s (within target) | ✓ |
| User-turn response latency | < 1.5s (OpenAI first token) | ~0.9s (within target) | ✓ |
| Caller speech recognition reliability | > 95% (OpenAI transcript accuracy) | ~95% (within target) | ✓ |
| TTS perceived naturalness | Clearly better than standard IVR | Narrowband, "old telephone" | ✗ |
| Line noise tolerance | < −60 dBFS silence floor | −34.6 dB (Telnyx CNG) | ✗ |
| Caller-heard frequency response | > 7 kHz (wideband minimum) | ~3.4 kHz (G.711 narrowband) | ✗ |
| Acceptable codec (PSTN) | G.722 wideband (if carrier supports) | G.711 PCMU (universal) | ✗ |
| Preferred audio sample rate | 48 kHz (Opus/WebRTC) | 8 kHz (PCMU) | ✗ |
| Production fallback | PCMU (safe, universal) | PCMU (current) | ✓ |

### What "near-human voice quality" means in practice
- The caller should not be able to tell they are talking to an AI from the audio quality alone.
- The voice should sound like a natural human receptionist on a high-quality phone or headset.
- No audible compression, no narrowband "tinniness", no constant background noise.
- Natural prosody, natural pacing, natural breathing — all of which Cartesia provides at the source, but are lost in the PSTN downsampling.

### How to measure perceived quality
- MOS (Mean Opinion Score) testing with a panel of callers.
- A/B comparison: PCMU vs G722 vs LiveKit HD, blind test, rate 1-5.
- Word recognition rate at the caller's end (not OpenAI's transcript, but what the caller actually hears and understands).
- Subjective "does this sound like a human?" rating.

---

## 5. Recommended Next Spike

### Recommendation: **Path C — LiveKit HD media path spike**

This is the only path that can deliver the "near-human voice quality" target. The PSTN constraint is universal and cannot be solved by codec or provider changes. The only way to deliver HD audio is to offer a non-PSTN access point (web app, mobile app, SIP client) using a media path that supports Opus/WebRTC.

The spike should be a **proof-of-concept on a feature branch**, not a production change. It should validate the architecture without disrupting the current PCMU production runtime.

### Why this is the best next step
- It is the only path that can achieve the quality target.
- It preserves the existing PCMU production runtime as the PSTN fallback.
- It enables a future product offering (web/mobile app with HD voice) that justifies premium pricing.
- Cartesia's HD output is preserved end-to-end (no downsampling to PSTN levels).
- OpenAI Realtime stays as the reasoning engine — no change to the AI layer.

### Expected benefit
- HD audio for non-PSTN callers (web app, mobile app, SIP clients): 48 kHz Opus, ~20 kHz frequency response, near-human quality.
- PSTN callers continue to get G.722 wideband (best available through Telnyx SIP trunk).
- The comfort noise issue is reduced on the HD path (no PSTN carrier involved).
- Future-proof: as PSTN usage declines, the HD path becomes the default.

### Risk
- High implementation effort (LiveKit server, SDK, SIP trunk, web/mobile client).
- Two media paths to maintain during transition.
- LiveKit SIP to Telnyx quality is untested — may have its own issues.
- Production risk if the spike is not properly isolated.

### Files/branches involved
- New feature branch: `feat/livekit-hd-spike`
- New component: `voice-gateway/internal/livekit/` (LiveKit client)
- New config: `LIVEKIT_URL`, `LIVEKIT_API_KEY`, `LIVEKIT_API_SECRET` in `.env`
- SIP trunk: LiveKit SIP → Telnyx (new Telnyx SIP configuration)
- Web/mobile client: new frontend component (out of scope for spike, but architecture must support it)
- No changes to: `session.go` AI logic, booking flow, Cartesia renderer, OpenAI Realtime integration, PCMU production path.

### Success criteria
1. LiveKit server deployed (self-hosted or LiveKit Cloud free tier).
2. Voice-gateway can publish/subscribe to LiveKit rooms.
3. Cartesia audio is published to LiveKit at 48 kHz Opus (no downsampling).
4. Web client (simple HTML/JS test page) can connect to LiveKit room and hear HD audio.
5. Latency: greeting < 1s, user-turn < 1.5s (same as current PCMU).
6. Audio quality: caller reports "clearly better than PCMU" in A/B test.
7. PSTN path still works (PCMU production unchanged, Telnyx webhook still active).
8. Rollback plan verified: can disable LiveKit env flags and revert to PCMU-only in < 5 minutes.

### Rollback plan
- LiveKit integration is behind env flags (`LIVEKIT_ENABLED=false` by default).
- PCMU production path is unchanged.
- If LiveKit spike fails, disable the env flag, remove the LiveKit code, and the system reverts to current PCMU behavior.
- No production data is at risk (LiveKit spike is a new media path, not a replacement).
- The PCMU binary on VPS remains the production binary until the spike is validated and a new production binary is deployed.

### How to keep current PCMU production safe
- Spike is on a feature branch, not main.
- Production binary on VPS is the current PCMU binary (SHA256 `24052c82…0cbafe` or later).
- LiveKit env flags are not set in production `.env`.
- LiveKit server is a separate infrastructure component (not on the same VPS as production).
- No changes to the Telnyx webhook, the PCMU env, or the production services.
- The spike validates the architecture before any production migration is considered.

---

## 6. What NOT to do

- Do not change the PCMU production runtime.
- Do not enable G722 as default.
- Do not implement LiveKit in production without a validated spike.
- Do not switch telephony providers (PSTN ceiling is universal).
- Do not change the receptionist prompt, booking state, Cartesia voice/model/speed, or OpenAI model.
- Do not commit debug audio, binaries, `.env`, or build artifacts.
- Do not treat the current PCMU baseline as the final product quality — it is the stable MVP baseline only.

---

## 7. Next Steps

1. **Immediate (this week):** Document strategy (this file). Commit to main.
2. **Short-term (next 1-2 weeks):** LiveKit HD spike on `feat/livekit-hd-spike` branch. Validate architecture. A/B test against PCMU.
3. **Medium-term (next month):** If spike succeeds, design production migration plan. If spike fails, document why and evaluate Path B (Telnyx support ticket) as a fallback.
4. **Long-term (next quarter):** If LiveKit is production-ready, build web/mobile app with HD voice. Keep PCMU as PSTN fallback.

---

## 8. References

- PCMU regression result: commit `17866d8`, `docs/PROJECT_STATUS.md` §9
- G722 controlled test: commit `f48b869`, `docs/context/AUDIO_QUALITY_CODEC_TEST_PLAN.md`
- Noise source investigation: commit `17866d8`, `docs/context/AUDIO_QUALITY_CODEC_TEST_PLAN.md`
- Runtime cleanup and baseline lock: commit `8c31bc6`, `docs/PROJECT_STATUS.md` §15
- Natural booking flow: commit `1bf8422`
- Telnyx media streaming docs: https://developers.telnyx.com/docs/voice/programmable-voice/media-streaming
- LiveKit docs: https://docs.livekit.io
- Opus codec: https://opus-codec.org
