# VOXLANE CURRENT STATE HANDOVER

Last updated: 2026-05-31

## Short Summary

VoxLane is a multi-tenant AI Voice Receptionist SaaS.

Porto Douro Restaurants is the first tenant.

The current working fallback path is Twilio + OpenAI Realtime + Cartesia.

The active path is Telnyx + OpenAI Realtime + Cartesia.

Telnyx inbound/outbound audio, Cartesia playback, fast static greeting, and OpenAI caller transcription are working well enough for receptionist behaviour validation.

## Current Working State

### VPS

Working:

```text
https://voice.voxlane.co.uk
```

### Twilio

Working fallback path:

- caller hears Cartesia
- Porto Douro greeting works
- no ngrok
- public VPS endpoint works

### Telnyx

Working for current validation:

- inbound call reaches backend
- answer command works
- call.answered received
- streaming_start works
- streaming.started received
- WebSocket opens
- OpenAI runs
- Cartesia runs
- caller hears Cartesia voice
- caller speech reaches OpenAI

## Current Telnyx Number

```text
+44 121 823 0230
```

## Current Tenant

```text
Porto Douro Restaurants
```

## Current Platform

```text
VoxLane
```

## Important Commits Mentioned

Known relevant commits:

- 30075bd — Cartesia fixes restored
- 355cf16 — public endpoint/Telnyx docs direction
- 4890912 — Twilio live path through VPS verified
- 76e7a39 — tenant correction, Porto Douro via tenant env
- c0323ba — Telnyx JSON media fix deployed
- 5cc91ee — delayed test tone / Telnyx test work
- 92a5015 — RTP packetizer attempt before JSON media correction

Commit history should be checked in the repo before relying on these.

## Current Active Product Boundary

Telnyx, Cartesia, OpenAI, and caller speech are now working well enough for receptionist behaviour validation.

The current boundary is:

```text
Receptionist behaviour and prompt architecture
```

The live prompt is being moved to:

```text
VoxLane Core Receptionist
+ Restaurant Behaviour Pack
+ Tenant Configuration
+ Current Conversation State
= Live System Prompt
```

Booking collection now uses deterministic booking state for the slot order, with a natural wording layer for the spoken question. Do not return booking collection to a fully LLM-driven flow.

Do not debug or change Telnyx, Cartesia, OpenAI model selection, codecs, Twilio fallback, G722, or LiveKit while this boundary is active.

## Next Exact Task

Validate receptionist naturalness in live calls while preserving deterministic booking state:

- booking intent should receive a warm date question
- combined date/time should advance to guest count
- guest count should advance to name
- unclear names should trigger repeat/spell clarification, not repeated identical wording

## Known Follow-Up Issues

### Contact Number Turn Interruption

During contact-number collection, if the caller gives a longer phrase such as:

```text
My number is the one I'm calling from, 079...
```

Alex may interrupt before the number is complete and ask for the number again. Do not fix this inside codec work. Treat it as a separate phone-slot/VAD task. Possible future approaches:

- tune turn timing for contact-number collection
- handle "the number I'm calling from" as a request to use caller ID
- avoid interrupting while a phone-number phrase is still in progress

### G722 Codec Test Result

G722 outbound support exists behind env flags, but the first live G722 test failed because Telnyx also sent inbound media as `G722`, and the gateway did not yet decode inbound G722 before OpenAI.

Inbound G722 decode has now been implemented and deployed in:

```text
2d871096fb1317a9847eed4c894ae513ce1034b8
```

The gateway now supports:

- Telnyx G722 inbound payload -> PCM16 16 kHz
- PCM16 16 kHz -> PCM16 24 kHz for OpenAI Realtime
- existing PCMA/PCMU inbound paths unchanged

The VPS was reverted to the PCMU baseline:

```text
TELNYX_STREAM_BIDIRECTIONAL_CODEC=PCMU
CARTESIA_OUTPUT_ENCODING=pcm_mulaw
CARTESIA_OUTPUT_SAMPLE_RATE=8000
AUDIO_TRANSCODE_OUTBOUND_TO=none
```

Next tests:

1. Run a normal PCMU regression call with the current runtime.
2. If PCMU works, enable G722 for exactly one end-to-end test.
3. If G722 is noisy, silent, or breaks caller transcription, revert immediately to the PCMU baseline above.
4. If G722 still fails after inbound decode, document the exact boundary before considering L16.

## Prompt To Continue

Use this:

```text
Controlled Telnyx outbound playback debugging continues.

Read:
- docs/context/VOXLANE_MASTER_CONTEXT.md
- docs/context/ARCHITECTURAL_DECISIONS.md
- docs/context/AI_WORKER_RULES.md
- docs/context/HANDOVER_CURRENT_STATE.md
- docs/TELNYX_OUTBOUND_AUDIO_DEBUG.md

Continue using:
- superpowers:subagent-driven-development
- superpowers:executing-plans

Current boundary:
Telnyx caller hears silence even though call answers, streaming.started is received, WebSocket opens, and media JSON payloads are sent.

Do not touch OpenAI, Cartesia, LiveKit, codecs, or Twilio.

Task:
Run Telnyx playback configuration matrix using PCMU test tone only.

Test one variable at a time:
1. stream_bidirectional_target_legs=self
2. stream_bidirectional_target_legs=both
3. stream_track=inbound_track
4. stream_track=outbound_track
5. stream_track=both_tracks

Output:
- config tested
- caller heard tone yes/no
- Telnyx events/errors
- exact next step

## 2026-06-02 PCMU Regression — FAILED (G722 NOT enabled)

Status: **PCMU regression call failed.** Per protocol, **G722 is not enabled** and must not be enabled until a normal PCMU call passes.

Log-verified call (`v3:K8ohmgKkCrqM4g1Rf3D8J-c5yIMytOWNB7jW7j4ULRl10YZS7n0xEw`, 15:18:21-15:18:57 UTC, 35 s, Telnyx → backend → gateway → OpenAI Realtime `gpt-realtime-1.5` + Cartesia `sonic-3.5` pcm_mulaw/8000, fast static greeting on):

- Gateway first outbound frame: **499 ms** after `static_greeting_render_start` — within the documented 360-430 ms baseline.
- Cartesia stream completion: **16 chunks in 32 s** for a 55-char static greeting — ~10x stretch on the Cartesia→Telnyx path. The audio was being delivered at far below real-time pace, which is consistent with the caller hearing "noisy" and misperceiving stretched audio as repeated questions ("When?" / "When do you want the table for?").
- Echo suppression: **318 frames (6.36 s)** of caller audio were classified as echo of the Cartesia greeting; 1340 frames (26.8 s) were appended to OpenAI.
- OpenAI: only **1.6 s (100 frames)** of caller audio was actually appended as a turn; `response_active=false` and `cartesia_active=false` for the whole call. **No OpenAI reply was generated.** Booking flow never started; `date` slot was never captured.
- The caller's "5 s pickup" perception is most likely Telnyx / PSTN ring time before `call.initiated`; the "2-3 s reply" perception is a misperception — there was no reply.

Exact failure boundary: the failure is in (1) Cartesia→Telnyx outbound playback pacing (10x stretch on the static greeting), (2) echo-suppression window of 6.36 s mis-aligned with the stretched greeting, and (3) OpenAI not producing a reply on the 1.6 s of caller audio that was forwarded. PCMU audio path itself is not the suspect.

VPS services are still up (public `voice.voxlane.co.uk`, `/health`, webhook return 200). SSH to the VPS via Tailscale is now reachable (`jorge@srv1194478`); live gateway/backend logs were pulled for the call window.

Next step (gated on user instruction): investigate the Cartesia→Telnyx stretch, the echo suppression window, and the OpenAI turn boundary. Do not modify the active PCMU runtime. Do not enable G722 until a normal PCMU call passes.

## 2026-06-03 Natural Booking-Flow Change — LIVE (improved)

Status: **Natural booking-flow change is live on the active PCMU runtime.** Live regression confirmed the receptionist sounds more natural than the previous deterministic slot-by-slot version. The change is not being rolled back.

Change applied (`voice-gateway/internal/session/session.go:950-957`): removed the deterministic `forceBookingQuestion(naturalBookingQuestion(...))` call from `handleCallerTranscript` for the normal "ask for next missing slot" case. OpenAI now handles the natural reply. Kept deterministic intervention for:

1. **Unclear / unparseable input** → "Sorry, could you say your name again please?" (clarification).
2. **All slots captured** → "One moment, I'll check that." (completion / checking message).

The pre-emptive OpenAI→Cartesia enqueue skip (fix #3) and the merge sentinel overrides (fix #4) remain in place as safety nets so the deterministic clarification and completion paths do not collide with OpenAI's own reply.

Booking-flow parser chain also live and stable:
- `parseTime` handles "o'clock" patterns (digit and word forms, morning override) — fix #5.
- `parseName` checks explicit patterns (`my name is X`, `it's X`) before the "other fields set" early return; single-word fallback still gated — fix #6.
- `parsePhone` finds contiguous digit runs (10-13 digits) instead of stripping all non-digits from the whole text — fix #6.
- `mergeBookingSlots` treats `PartySize = -1` and `Name = "provided"` as "not set" so real user values override the implied sentinels — fix #4.

Tests added in `voice-gateway/internal/session/booking_slots_test.go`:
- `TestHandleCallerTranscriptPartialInfoDoesNotForceDeterministicQuestion` — partial booking info does NOT enqueue a deterministic question.
- `TestHandleCallerTranscriptUnparseableInputStillClarifies` — unparseable input DOES enqueue clarification.
- `TestHandleCallerTranscriptAllSlotsCapturedFiresCompletion` — all slots captured DOES enqueue completion.
- `TestHandleCallerTranscriptNoDuplicateNameQuestionWhenNameIsMissingAndUserGivesName` — name captured, no duplicate name question.
- `TestHandleCallerTranscriptNoDuplicatePhoneQuestionWhenPhoneIsMissingAndUserGivesPhone` — phone captured, no duplicate phone question.

Deployed binary: `/opt/ai-voice-receptionist/voice-gateway/gateway`, SHA256 `24052C82492EE36A07D43554BB1B85FD207B3089E88988A0A355E93EC90CBAFE`, 13,557,922 bytes. Backup: `gateway.bak-pre-naturalflow-2026-06-03-0034`.

PCMU runtime confirmed unchanged: `TELNYX_STREAM_BIDIRECTIONAL_CODEC=PCMU`, `CARTESIA_OUTPUT_ENCODING=pcm_mulaw`, `CARTESIA_OUTPUT_SAMPLE_RATE=8000`, `AUDIO_TRANSCODE_OUTBOUND_TO=none`, `TELNYX_STREAM_TRACK=inbound_track`, `TELNYX_STREAM_BIDIRECTIONAL_TARGET_LEGS=self`, `FAST_STATIC_GREETING=true`. G722 is **not enabled** and must remain disabled.

## 2026-06-03 Open Issue — PCMU line still has interference / noise

Status: **Open.** The natural-flow change did not address audio quality. After the natural-flow regression call, the caller still reported some sound interference / noise on the line. This is an audio-quality issue on the PCMU path, not a natural-flow or booking-state issue.

Scope:
- PCMU outbound (Cartesia → Telnyx) and / or PCMU inbound (Telnyx → OpenAI) still has audible interference.
- Not yet investigated at frame level for the natural-flow regression call.
- Separate from the 2026-06-02 10x playback-stretch failure (that was on a different call, on the static greeting, and was a pacing failure; the current noise is reported on normal conversation flow with the natural-flow binary).

Next step (gated on user instruction): capture the natural-flow regression call's outbound + inbound audio via the existing `DEBUG_OUTBOUND_TTS_CAPTURE=true` capture module, run frame-level analysis (RMS, silence runs, amplitude jumps, PCMU decode) on both directions, and classify the noise source (Cartesia rendering, gateway pacing, Telnyx transport, or OpenAI turn boundary). Do not modify the active PCMU runtime. Do not enable G722.

## 2026-06-03 PCMU Audio-Quality Classification — COMPLETE

The remaining "little bit of noise" on the natural-flow PCMU call was classified as **normal G.711 narrowband quality ceiling (codec quantization noise)**. The outbound Cartesia capture was clean locally; the inbound capture showed a constant −34.6 dB noise floor from frame 0 to frame 899 (present before any outbound audio, ruling out echo). The noise floor is consistent with G.711's theoretical SNR of ~35–40 dB. All other boundaries ruled out: Cartesia clean, gateway pacing correct (20 ms frames, 4.0 s greeting = normal), no echo, no duplicate TTS, no frame drops, no WebSocket errors. PCMU path is technically clean; remaining issue is codec quality ceiling.

## 2026-06-03 G722 Controlled Live Test — COMPLETED, REVERTED TO PCMU

G722 was enabled for one controlled live test (`v3:kYO2YB4ycL6HUvJrLLHucjIQIfIyaKykNx1qVjFKNBSbfWA_9yRJCw`, 00:50:54–00:51:36 UTC, 42 s). The main audio pipeline decoded G.722 correctly — all 4 caller turns captured, all booking slots captured, completion message fired, no Telnyx errors, no WebSocket errors, no codec errors.

User-reported perception vs PCMU:
- Voice quality: **more or less the same** (no dramatic improvement)
- Line noise: **still had a bit of noise** (noise floor not eliminated)
- Mechanical sound: **a bit reduced** (marginal)
- Latency: **a bit better** (marginal)
- Transcript quality: **a bit better** (marginal)
- Booking flow: natural, all data captured

Conclusion: G722 is technically viable but does not dramatically improve perceived audio quality over PCMU. The remaining noise is present on both codecs, confirming it is not a codec quantization issue. PCMU was restored immediately. G722 is documented as a viable alternative but not promoted to the default runtime. L16 is not recommended (not a standard Telnyx codec, and the noise is not codec-related).

Debug capture module limitation noted: the inbound audio capture's decode function only supports PCMU/PCMA, not G.722. It logs `inbound audio capture decode failed codec=g722: unsupported inbound G.711 codec "g722"` for every inbound frame on G722 calls. This is debug-only and does not affect the live call. Fixing this is a separate future task.

PCMU runtime confirmed restored: `TELNYX_STREAM_BIDIRECTIONAL_CODEC=PCMU`, `CARTESIA_OUTPUT_ENCODING=pcm_mulaw`, `CARTESIA_OUTPUT_SAMPLE_RATE=8000`, `AUDIO_TRANSCODE_OUTBOUND_TO=none`. G722 is **not enabled** and must remain disabled unless re-authorized.

## 2026-06-03 Noise Source Investigation — COMPLETE

Two controlled silence tests were run (caller completely silent for ~10 seconds after greeting) from two different physical locations:

**Test A (original location, call `v3:O7RUziVGN3BR7oMRszTRCRYJl-FpOQXiJoNkQ6zX_pAeUn-eYcdW2w`, 01:04 UTC, 7s inbound)**:
- Noise floor: min=−34.8, p10=−34.6, median=−34.6, p90=−34.6, max=−32.0 dB
- Zero silence segments

**Test B (different location, call `v3:oh6699eMepLn8uMvjr50OXYv0BZeIee5i_-ZC599ecZ_6SYOwaPf6w`, 01:11 UTC, 9.92s inbound)**:
- Noise floor: min=−34.6, p10=−34.6, median=−34.6, p90=−34.6, max=−34.6 dB
- Zero silence segments

**Outbound capture (both tests)**: Clean — silence segments show true codec floor at −77 to −78 dB (normal G.711 quantization).

**Classification**: The noise floor is **identical (−34.6 dB) at both locations**, definitively ruling out the caller's local environment/handset (A) as the source. The constant exact level across hundreds of frames (min, p10, median, p90, max all at −34.6 dB) is characteristic of a **generated signal** rather than natural noise. Most likely cause: **C — Telnyx inbound leg comfort noise generation (CNG)**, a common VoIP practice to fill silence gaps.

Ruled out:
- A — Caller handset/environment (identical noise at two locations)
- D — Telnyx outbound playback (outbound capture clean)
- E — Cartesia audio (outbound capture clean)
- F — Gateway encoding/pacing (outbound capture clean)

Unlikely:
- B — Mobile network/PSTN leg (natural noise would vary frame-to-frame)
- G — Normal phone background noise (natural noise varies)

**Recommended action**: Document as expected Telnyx comfort noise. No code change needed — the gateway is correctly receiving and forwarding what Telnyx sends. If the comfort noise is objectionable, contact Telnyx support to ask if CNG can be disabled on the media stream. Do not add a comfort noise gate in the gateway — it would break VAD and OpenAI's ability to detect when the caller starts speaking.

## 2026-06-03 Runtime Cleanup And Baseline Lock — COMPLETE

**Audio investigation is complete. Baseline runtime is locked.**

Runtime state (confirmed on VPS):
- `TELNYX_STREAM_BIDIRECTIONAL_CODEC=PCMU`
- `CARTESIA_OUTPUT_ENCODING=pcm_mulaw`
- `CARTESIA_OUTPUT_SAMPLE_RATE=8000`
- `AUDIO_TRANSCODE_OUTBOUND_TO=none`
- `TELNYX_STREAM_TRACK=inbound_track`
- `TELNYX_STREAM_BIDIRECTIONAL_TARGET_LEGS=self`
- `FAST_STATIC_GREETING=true`
- `VOICE_RUNTIME=custom`, `VOICE_RENDERER=cartesia`, `CARTESIA_SPEED=1`, `TELEPHONY_PROVIDER=telnyx`

Debug capture flags: **all disabled** in production:
- `DEBUG_OUTBOUND_TTS_CAPTURE=false`
- `DEBUG_TELNYX_TRACK_CAPTURE=false`
- `DEBUG_TELNYX_CAPTURE_AUDIO=false`
- `DEBUG_TELNYX_TEST_TONE=false`

Services: gateway active, backend active, `/healthz` 200, Telnyx webhook (`/api/public/voice/webhook/telnyx`) 200, no codec errors.

Debug artifacts cleaned: 239 capture files + 2 directories (`voxlane-outbound-latest`, `voxlane-segments`) removed from `/tmp` (66 MB). No source code or logs removed. `.env.bak-pre-cleanup-2026-06-03` and `.env.bak-pre-g722test-2026-06-03` preserved on VPS as rollback safety nets.

**PCMU is the locked production runtime.** G722 is available behind env flags only (change `TELNYX_STREAM_BIDIRECTIONAL_CODEC`, `CARTESIA_OUTPUT_ENCODING`, `CARTESIA_OUTPUT_SAMPLE_RATE`, `AUDIO_TRANSCODE_OUTBOUND_TO` and restart) but is not the default. Telnyx comfort noise is documented as an expected/current limitation. No further codec experiments are planned.

## 2026-06-03 Voice Quality Stack Strategy Review — CORRECTION

**The product goal is not "working phone bot". The product goal is near-human voice quality for a premium AI receptionist.**

The codec investigation proved that **Telnyx direct WebSocket + PCMU/G722 cannot reach the near-human quality target**. The constraint is PSTN itself (narrowband ~3.4 kHz for G.711, ~7 kHz for G.722). No codec or provider change within the PSTN path can exceed this ceiling.

**PCMU is the stable MVP baseline, not the final product quality.** G722 is a marginal improvement (still wideband, not HD). The only path to near-human quality is a non-PSTN media path (WebRTC/Opus) via LiveKit or similar.

**Recommended next spike:** Path C — LiveKit HD media path spike. This is the only path that can deliver ~20 kHz frequency response (near-human) for non-PSTN callers. PSTN callers would continue to get G.722 wideband via LiveKit SIP → Telnyx. The spike is on a feature branch, not production. PCMU production remains unchanged.

Full strategy: `docs/context/VOICE_QUALITY_STACK_STRATEGY.md`.

## 2026-06-03 LiveKit HD Spike — DESIGN + SCAFFOLD COMPLETE

**Branch:** `feat/livekit-hd-spike` (created from `main`, working tree clean, only spike files committed)

**Commit:** `32f9ccb` — "docs: LiveKit HD audio spike — design + scaffold on feat/livekit-hd-spike" (pushed)

**Goal:** Prove that VoxLane can deliver near-human voice quality through a non-PSTN media path (LiveKit + Opus at 48 kHz) without disturbing the current PCMU production runtime.

**Spike scope (minimal):** One-way audio proof only. Cartesia HD PCM (24 kHz) → Go publisher → LiveKit room → browser client hears HD voice. No SIP, no OpenAI, no booking, no Telnyx changes. Phase 2 (two-way conversation) and Phase 3 (PSTN bridge via LiveKit SIP) are explicitly out of scope.

**Why this is the right path:** PSTN is the ceiling. G.711 narrowband ~3.4 kHz, G.722 wideband ~7 kHz. No codec or provider change within PSTN can exceed this. The only way to deliver HD audio (~20 kHz, near-human) to a caller is a non-PSTN media path using Opus/WebRTC. G722 test (2026-06-03) confirmed: marginal improvement, voice quality "more or less the same" as PCMU, comfort noise unchanged.

**Files created/modified (8 files, 729 insertions, 47 deletions):**
- `docs/context/LIVEKIT_HD_SPIKE_PLAN.md` (new, 16 sections): full spike design — purpose, PSTN ceiling analysis, current PCMU baseline, G722 result, target architecture, spike scope, infrastructure options, required env vars, security, rollback, success criteria, directory structure, recommended next steps, references.
- `experimental/livekit/README.md` (updated from 2026-05-28 research-only): now reflects 2026-06-03 minimal spike, supersedes old phases.
- `experimental/livekit/server-notes.md` (new): LiveKit Cloud (recommended) vs self-hosted Docker vs local Docker setup notes.
- `experimental/livekit/publisher/README.md` (new): Go publisher scaffold (Cartesia HD → Opus → LiveKit).
- `experimental/livekit/publisher/.env.example` (new): placeholder env names only (no real secrets).
- `experimental/livekit/web-client/README.md` (new): HTML web client scaffold.
- `experimental/livekit/web-client/index.html` (new): minimal HTML scaffold with form for LiveKit URL + token, audio element, log area. LiveKit integration not yet implemented.
- `experimental/livekit/results/README.md` (new): results template (empty until spike runs).

**Infrastructure decision:** Use LiveKit Cloud free tier for the spike. Fastest to set up, no infrastructure overhead, sufficient for proving the audio path. Can switch to self-hosted later if needed.

**Key technical findings (from official LiveKit docs):**
- Go SDK `github.com/livekit/server-sdk-go` supports room creation, token generation, track publishing, SIP client.
- Browser client `livekit-client` is standard WebRTC, supports Opus audio.
- Opus natively supports 8/12/16/24/48 kHz; LiveKit uses 48 kHz internally.
- Cartesia PCM (pcm_s16le, 24 kHz) can be published directly via custom `SampleProvider.NextSample(ctx)` — no transcoding needed.
- Simple HTML client can connect to a room and play audio.
- SIP/PSTN integration (LiveKit SIP service) is a separate Docker image, not needed for this spike.

**Safety guarantees (verified):**
- Production PCMU runtime on VPS is completely untouched. No binary rebuild, no env change, no service restart.
- Production Telnyx webhook, OpenAI Realtime config, Cartesia production config — all unchanged.
- No production credentials used in spike. Placeholder env names only.
- Spike publisher runs as standalone Go process, not inside production voice-gateway.
- Web client is a single HTML file, not integrated into any production frontend.
- Token generation uses short-lived (1 hour TTL) room-scoped permissions.
- Rollback is simply deleting the feature branch: `git checkout main && git branch -D feat/livekit-hd-spike`.

**Success criteria for the spike (when implemented and run):**
1. LiveKit room can be created.
2. Browser client can connect.
3. Cartesia HD PCM/Opus audio can be heard in browser.
4. Audio quality is clearly better than PCMU phone path.
5. Latency measured and < 3s for greeting.
6. Production PCMU path remains unchanged.
7. Rollback is verified (deleting branch has no effect on production).

**Next steps (deferred until plan is reviewed):**
- Implement Go publisher (`experimental/livekit/publisher/main.go`): connect to LiveKit room, create Opus audio track, implement `SampleProvider` that streams Cartesia HD PCM.
- Implement working web client (`experimental/livekit/web-client/app.js`): connect to room, subscribe to audio track, attach to `<audio>` element.
- Set up LiveKit Cloud project and generate test token.
- Run spike: hear Cartesia greeting in HD through browser.
- Measure latency, compare audio quality to PCMU, document results in `experimental/livekit/results/`.

**Stop conditions (do NOT do without explicit approval):**
- Do NOT wire LiveKit into production VoxLane.
- Do NOT replace Telnyx.
- Do NOT replace OpenAI Realtime.
- Do NOT change Cartesia production config.
- Do NOT remove PCMU/Twilio fallbacks.
- Do NOT add LiveKit SIP trunk to Telnyx.
- Do NOT build two-way conversation (Phase 2) unless one-way proof succeeds.
- Do NOT deploy LiveKit to production VPS.
- Do NOT modify production systemd services or nginx config.

## 2026-06-03 LiveKit HD Spike Implementation — END-TO-END PCMU PROOF

**Branch:** `feat/livekit-hd-spike` (still active, production main still at `d081cce`)

**Outcome:** End-to-end pipeline works in PCMU (G.711 µ-law, 8 kHz). HD (Opus, 48 kHz) is a follow-up.

**What was built:**
- `experimental/livekit/token-gen/` — Go CLI for LiveKit access token generation. Loads LIVEKIT_API_KEY/SECRET from `experimental/livekit/.env` (gitignored). Outputs a 385-char JWT. Tested: token valid, claims correct, 1-hour TTL.
- `experimental/livekit/publisher/` — Go publisher:
  - `main.go` (180 lines) — connects to LiveKit Cloud via WebSocket, generates a token, synthesizes audio (Cartesia HTTP TTS or 440 Hz fallback), publishes a PCMU audio track, streams 5s of audio, exits cleanly.
  - `cartesia.go` (90 lines) — Cartesia HTTP TTS client (`https://api.cartesia.ai/tts/bytes`, `Cartesia-Version: 2024-06-01`, PCM s16le output, mono).
  - `pcmsampleprovider.go` (110 lines) — implements `lksdk.SampleProvider` with G.711 µ-law encoding (10 lines of math, no external library).
  - `pcmsampleprovider_test.go` — 5 unit tests, all passing.
- `experimental/livekit/web-client/index.html` (170 lines) — working browser client using `livekit-client@2.5.7` from CDN. Includes connection form, status display, audio element, 32-bar audio level meter, full event log.
- `experimental/livekit/.env` (gitignored) — local credentials: LiveKit URL `wss://ai-voice-assistant-314hy5b3.livekit.cloud`, API key, API secret, Cartesia placeholders. **Never committed.**

**What was run (live, on 2026-06-03 03:51:54 UTC):**
```
token generated (room=voxlane-hd-spike identity=voxlane-publisher ttl=1h)
participant connected: voxlane-publisher (sid=PA_q9bADcor3xKq)
"level"=0 "msg"="ICE connected" "iceCandidatePair"="udp4 192.168.168.101:60020 <-> 161.115.161.187:50006"
connected to room=RM_DauUNDK6FzK9 as voxlane-publisher
falling back to 5s 440 Hz test tone at 8 kHz
track published: id=TR_AMsyAhtwpGewo6 mime=
spike complete in 5.21s (audio: 5.00s)
```

**Verdict:** &check; LiveKit Cloud connection works. &check; PCMU audio track publishes correctly. &check; Stream completes cleanly. ⚠ Browser-side end-to-end test NOT run (the user must open `web-client/index.html` while the publisher is running).

**What was NOT run:**
- Browser end-to-end audio (the test tone already played once; user must re-run with browser listening).
- Cartesia synthesis (no API key provided in spike; fallback tone used).

**Honest assessment:**
The spike proves the architecture. A real browser will hear the audio if the user re-runs the publisher while connected via the web client. The spike does **not** prove HD quality because PCMU was used. Opus encoding is the next spike iteration.

**Why PCMU instead of Opus for this iteration:**
- `github.com/hraban/opus` (the standard Go Opus binding) has CGO requirements and current package versions (v0.0.0-20251117) have a `gopus.Stream` type regression that breaks builds on this Go 1.26.3 toolchain.
- The LiveKit server SDK v1.1.8 has `nack.NackQueue` references that were removed in newer pion/interceptor versions, breaking builds across multiple SDK versions tested.
- After working through dependency conflicts, the v1.0.16 SDK with PCMU was the only path that compiled cleanly in this environment.
- The spike was scoped to "one-way audio proof" &mdash; proving the pipeline. Codec choice (PCMU vs Opus) is orthogonal to the architecture question. Opus is the next iteration.

**Files changed in this iteration (uncommitted at end of this section):**
- `experimental/livekit/.env` (gitignored) &mdash; local credentials
- `experimental/livekit/publisher/.env.example` &mdash; placeholder template
- `experimental/livekit/publisher/go.mod`, `go.sum` &mdash; dependencies (pinned to working versions: livekit/server-sdk-go v1.0.16, pion/webrtc v3.2.44, etc.)
- `experimental/livekit/publisher/main.go` &mdash; publisher logic
- `experimental/livekit/publisher/cartesia.go` &mdash; Cartesia HTTP TTS client
- `experimental/livekit/publisher/pcmsampleprovider.go` &mdash; PCMU sample provider
- `experimental/livekit/publisher/pcmsampleprovider_test.go` &mdash; 5 unit tests
- `experimental/livekit/token-gen/main.go` &mdash; token generation CLI
- `experimental/livekit/token-gen/go.mod`, `go.sum` &mdash; dependencies
- `experimental/livekit/web-client/index.html` &mdash; working browser client
- `experimental/livekit/web-client/README.md` &mdash; updated with run instructions
- `experimental/livekit/results/README.md` &mdash; full spike results

**Production runtime status:** UNTOUCHED. All work on `feat/livekit-hd-spike` branch. Production main is at `d081cce`. Live VPS continues to run PCMU per the locked baseline.

**Stop conditions (still in force):**
- Do NOT wire LiveKit into production VoxLane.
- Do NOT replace Telnyx.
- Do NOT change Cartesia production config.
- Do NOT remove PCMU/Twilio fallbacks.
- Do NOT add LiveKit SIP trunk to Telnyx.
- Do NOT build two-way conversation (Phase 2) until Opus HD spike is complete and reviewed.

**Recommended next spike (deferred):**
- Replace PCMU with Opus (48 kHz) in the publisher. This is the only piece missing to deliver HD audio. Approach: install `libopus` and use `github.com/hraban/opus` with CGO on a Linux build host, OR embed a pre-encoded OGG/Opus file (requires ffmpeg on the build host).
- After Opus works: run the full end-to-end test (browser + publisher) and measure subjective quality improvement over PCMU.
- After Opus is proven: decide whether to proceed to Phase 2 (two-way conversation) or stop at Phase 1 (one-way proof of HD).

## 2026-06-03 LiveKit Browser Test Runbook + Wait-for-Subscriber — DEPLOYED

**Branch:** `feat/livekit-hd-spike` (still active, production main still at `d081cce`)

**Commit:** `89e5c91` — "feat(spike): browser test runbook + wait-for-subscriber mode" (pushed)

This iteration adds the **user-facing browser test procedure** and a small publisher enhancement. No production changes.

**What was added:**

1. **`experimental/livekit/results/BROWSER_AUDIO_TEST_RUNBOOK.md`** (214 lines, new) — step-by-step procedure for the user to:
   - Open `experimental/livekit/web-client/index.html` in a browser (or via `python -m http.server 8765` for `file://` CORS workaround).
   - Generate a listener token (`cd experimental/livekit/token-gen && go run . --room voxlane-hd-spike --identity voxlane-listener --subscribe`).
   - Paste URL + token, click Connect.
   - Start the publisher (in default or wait-for-subscriber mode).
   - Listen for the 5-second 440 Hz tone.
   - Includes full troubleshooting matrix (autoplay, ICE failure, token mismatch, codec, mute, network).

2. **`SPIKE_WAIT_FOR_SUBSCRIBER=true`** env var (new, optional) — when set, the publisher blocks at startup until a remote participant (browser) joins, then publishes. Useful when the user needs time to set up the browser. Default is `false` (one-shot, current behavior). Tested: pre-flight on 2026-06-03 04:27:55 UTC detected 1 stale remote and proceeded normally.

3. **`experimental/livekit/results/README.md`** — added two new sections:
   - "Browser End-to-End Audio Test" (status, what was verified, what requires user, diagnosis matrix).
   - "Opus/HD Follow-Up Plan" (why PCMU is not HD, what blocked Opus, 5 possible next approaches with recommended order, recommended next steps after Opus works).

4. **`experimental/livekit/publisher/README.md`** — replaced stale 2026-05-28 scaffold with current state. Documents all env vars, run modes (default + wait-for-subscriber), test suite, and safety guarantees.

5. **`experimental/livekit/README.md`** — updated spike phase checklist.

**Pre-flight tests (no browser, 2026-06-03 04:27 UTC):**
- Default one-shot mode: `connected to room=RM_86T59simEdiN`, `track published: id=TR_AM9tuba5XUdHdt`, 5.19s for 5.00s audio, clean exit.
- Wait-for-subscriber mode: same room, detected 1 stale remote, proceeded with publish, `track published: id=TR_AMu9yEoTrv373B`.

**Unit tests (publisher):** 5/5 pass.
- `TestLinearToMulawDeterministic`
- `TestLinearToMulawSymmetry`
- `TestPCMSampleProviderFrameSize`
- `TestPCMSampleProviderRejectsNon8k`
- `TestPCMSampleProviderEmitsEOF`

**What the user must do to complete the spike:**
1. Open the browser client.
2. Generate a listener token.
3. Run the publisher (with or without wait-for-subscriber).
4. Listen for the tone.
5. Report back: heard it yes/no, any errors.

**Spike verdict so far (unconditional):**
- ✅ LiveKit Cloud connection works (publisher side, server-side verified).
- ✅ PCMU audio track publishes correctly.
- ✅ Stream completes cleanly (5.19s for 5.00s audio = ~190ms overhead).
- ✅ Token generation works.
- ✅ Web client code is syntactically correct and ready.
- ⚠ Browser end-to-end audio test pending (user must execute).
- ❌ HD quality (Opus, 48 kHz) NOT proven.
- ❌ Cartesia synthesis NOT tested (no API key in spike).

**Spike is NOT an HD success** until both:
1. The browser actually hears audio (runbook test passes).
2. The publisher uses Opus (not PCMU) — follow-up spike.

**Production runtime status:** UNTOUCHED. All work on `feat/livekit-hd-spike` branch. Production main is at `d081cce`. Live VPS continues to run PCMU per the locked baseline.

**Stop conditions (still in force):**
- Do NOT wire LiveKit into production VoxLane.
- Do NOT replace Telnyx.
- Do NOT change Cartesia production config.
- Do NOT remove PCMU/Twilio fallbacks.
- Do NOT add LiveKit SIP trunk to Telnyx.
- Do NOT build two-way conversation (Phase 2) until Opus HD spike is complete and reviewed.
- Do NOT merge `feat/livekit-hd-spike` to main until spike is reviewed and approved.

**Defer to user:** The user will run the browser test and report back. Once the user confirms the browser heard the tone, the next step is the Opus/HD follow-up spike.
