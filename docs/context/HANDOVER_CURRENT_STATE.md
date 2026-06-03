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
```
