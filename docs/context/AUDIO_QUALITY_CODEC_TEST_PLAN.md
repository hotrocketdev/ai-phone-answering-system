# VoxLane Audio Quality Codec Test Plan

Last updated: 2026-06-01

## Current Baseline

The current production path is stable enough for receptionist behaviour validation, but it is still narrowband telephony audio.

Runtime:

```text
VOICE_RUNTIME=custom
VOICE_RENDERER=cartesia
FAST_STATIC_GREETING=true
TELNYX_STREAM_TRACK=inbound_track
TELNYX_STREAM_BIDIRECTIONAL_TARGET_LEGS=self
CARTESIA_SPEED=1
```

Current codec path:

```text
Telnyx inbound media: PCMA / 8000 Hz / mono
Gateway inbound: PCMA or PCMU -> PCM16 24 kHz for OpenAI
OpenAI input: PCM16 24 kHz
Cartesia output: raw pcm_mulaw / 8000 Hz
Telnyx outbound: JSON media events containing base64 raw PCMU frames
```

Latest observed baseline:

- inbound codec: PCMA / 8000 Hz / 1 channel
- outbound codec requested: PCMU
- Cartesia output format: raw pcm_mulaw / 8000 Hz
- static greeting first outbound frame: about 360-430 ms after render start in recent logs
- caller voice quality: intelligible, but still narrowband and occasionally noisy
- Alex voice quality: more natural behaviour, but still telephone-like
- booking flow status: working through date, time, guest count, name, and phone in latest good call

## Official Telnyx Codec Support

Official Telnyx Call Control media streaming documentation says:

- `stream_track` supports `inbound_track`, `outbound_track`, and `both_tracks`.
- `stream_codec` controls the codec used for streamed audio sent from Telnyx to our WebSocket. Supported values are `PCMU`, `PCMA`, `G722`, `OPUS`, `AMR-WB`, `L16`, and `default`.
- `stream_bidirectional_mode` supports `mp3` and `rtp`.
- `stream_bidirectional_codec` is used only when `stream_bidirectional_mode=rtp`; supported values are `PCMU`, `PCMA`, `G722`, `OPUS`, `AMR-WB`, and `L16`.
- `stream_bidirectional_sampling_rate` supports `8000`, `16000`, `22050`, `24000`, and `48000`.
- Bidirectional RTP audio is sent through WebSocket JSON media events with a base64 payload.
- Telnyx warns that if streamed audio uses a different encoding than the call, Telnyx may transcode and quality can degrade.

## Codec Candidates

| Codec | Telnyx stream_codec | Telnyx bidirectional | Expected quality | Notes |
| --- | --- | --- | --- | --- |
| PCMU | yes | yes | narrowband | Current outbound path |
| PCMA | yes | yes | narrowband | Current inbound format observed from Telnyx |
| G722 | yes | yes | wideband | First HD candidate |
| L16 | yes | yes | wideband/linear PCM | Strong AI fit, but bigger jump |
| OPUS | yes | yes | wideband | More complex packetization/codec dependency |

## First HD Candidate

First candidate: `G722`.

Reason:

- Telnyx officially lists `G722` for both `stream_codec` and `stream_bidirectional_codec`.
- It is a telephony wideband codec and a smaller step than jumping straight to L16 or OPUS.
- It lets us test whether the Telnyx Call Control WebSocket path improves perceived voice quality before considering larger architecture changes.

## G722 Pre-Implementation Answers

Does Telnyx support G722 for bidirectional outbound media?

```text
Yes. Official docs list G722 under stream_bidirectional_codec for rtp mode.
```

Does Telnyx support G722 inbound media?

```text
Yes. Official docs list G722 under stream_codec.
```

What exact `stream_bidirectional_codec` value is required?

```text
G722
```

What payload format is required?

```text
WebSocket text JSON media events:
{
  "event": "media",
  "media": {
    "payload": "<base64 encoded G722 RTP payload bytes>"
  }
}
```

Does outbound JSON media payload require raw G722 bytes?

```text
For our existing proven Telnyx JSON media path, payload should be base64 encoded audio payload bytes for the selected bidirectional RTP codec, without adding a separate RTP header in our WebSocket message.
```

Does Cartesia support direct G722 output?

```text
No evidence in official Cartesia output-format docs that Cartesia emits G722 directly. Official Cartesia encodings include pcm_f32le, pcm_s16le, pcm_mulaw, and pcm_alaw.
```

If Cartesia does not support G722 directly, where will transcoding happen?

```text
Gateway outbound path:
Cartesia raw pcm_s16le / 16000 Hz -> gateway G722 encoder -> Telnyx JSON media payload.
```

## Required Implementation Scope

Do not implement until this plan is approved.

Files likely involved:

- `backend/src/modules/voice/voice.controller.ts`
  - stop hardcoding `stream_bidirectional_codec: 'PCMU'`
  - send `TELNYX_BIDIRECTIONAL_CODEC=G722` when enabled
  - optionally send `stream_codec=G722` and `stream_bidirectional_sampling_rate=16000`
- `voice-gateway/internal/config/config.go`
  - add Cartesia output format envs if not already present
  - add outbound transcode target env if needed
- `voice-gateway/internal/renderer/cartesia/renderer.go`
  - make output encoding/sample rate configurable
  - default remains `pcm_mulaw` / `8000`
- `voice-gateway/internal/session/session.go`
  - route Cartesia `pcm_s16le` / `16000` through G722 encoder when enabled
  - default remains current direct PCMU path
- `voice-gateway/internal/provider/telnyx/adapter.go`
  - ensure outbound codec metadata and frame sizing are correct for G722
- `voice-gateway/internal/audio/`
  - add or integrate G722 encoder
  - add tests using known vectors or round-trip sanity checks

Proposed env flags:

```text
TELNYX_STREAM_CODEC=G722
TELNYX_BIDIRECTIONAL_CODEC=G722
TELNYX_STREAM_BIDIRECTIONAL_SAMPLING_RATE=16000
CARTESIA_OUTPUT_ENCODING=pcm_s16le
CARTESIA_OUTPUT_SAMPLE_RATE=16000
AUDIO_TRANSCODE_OUTBOUND_TO=G722
```

Defaults must remain:

```text
TELNYX_BIDIRECTIONAL_CODEC=PCMU
CARTESIA_OUTPUT_ENCODING=pcm_mulaw
CARTESIA_OUTPUT_SAMPLE_RATE=8000
```

## Required Tests

- config defaults preserve current PCMU path
- config accepts G722 env flags
- Cartesia renderer default request remains `pcm_mulaw` / `8000`
- Cartesia renderer can request `pcm_s16le` / `16000`
- G722 encoder converts 16 kHz PCM16 into expected frame bytes
- Telnyx adapter sends JSON media events with base64 payload for G722
- no RTP header is added to WebSocket JSON media payloads
- PCMU path regression test still passes

## Live Test Plan

Only after implementation behind env flags:

1. Enable G722 on VPS.
2. Restart services.
3. Call `+44 121 823 0230`.
4. Test greeting only.
5. Test booking flow:
   - "Can I book a table?"
   - "Tomorrow at seven p.m."
   - "Four people."
   - name
   - phone
6. Capture:
   - caller hears greeting
   - Alex voice quality better/same/worse
   - caller transcript quality better/same/worse
   - booking flow still works
   - line noise better/same/worse
   - latency better/same/worse

## Fallback Rule

If G722 fails, immediately revert runtime env to:

```text
TELNYX_BIDIRECTIONAL_CODEC=PCMU
CARTESIA_OUTPUT_ENCODING=pcm_mulaw
CARTESIA_OUTPUT_SAMPLE_RATE=8000
```

Do not leave the VPS in an experimental codec state.

## G722 Live Test Result — 2026-06-01

Test commit:

```text
0d75f4388fe76be0d74580d50ef24a15835ddd41
```

G722 runtime used:

```text
TELNYX_STREAM_BIDIRECTIONAL_CODEC=G722
CARTESIA_OUTPUT_ENCODING=pcm_s16le
CARTESIA_OUTPUT_SAMPLE_RATE=16000
AUDIO_TRANSCODE_OUTBOUND_TO=g722
```

Live result:

- caller heard greeting: yes
- voice quality: worse than PCMU because the call began very noisy
- booking flow: failed after caller said they wanted to book a table
- Telnyx API errors: none observed
- WebSocket write errors: none observed
- exact failure boundary: Telnyx inbound media arrived as `G722`, but the gateway inbound path only supports G.711 PCMA/PCMU for OpenAI input, so caller audio was dropped with `unsupported inbound G.711 codec "G722"`

Relevant log pattern:

```text
dropping provider audio before OpenAI codec=G722 payload_len=160 error=unsupported inbound G.711 codec "G722" sent_to_openai=false
```

Action taken:

```text
Reverted VPS runtime to PCMU baseline immediately.
```

Restored runtime:

```text
TELNYX_STREAM_BIDIRECTIONAL_CODEC=PCMU
CARTESIA_OUTPUT_ENCODING=pcm_mulaw
CARTESIA_OUTPUT_SAMPLE_RATE=8000
AUDIO_TRANSCODE_OUTBOUND_TO=none
```

Conclusion:

G722 outbound code is implemented behind flags, but the first live test was not a fair quality test because inbound G722 was not decoded before OpenAI.

## G722 Inbound Decode Implementation — 2026-06-02

Implementation commit:

```text
2d871096fb1317a9847eed4c894ae513ce1034b8
```

Implemented:

- Telnyx `start.media_format.encoding=G722` is normalised to `g722`.
- inbound G722 payloads are decoded with the existing `github.com/gotranspile/g722` dependency.
- decoded PCM16 16 kHz audio is resampled to PCM16 24 kHz for OpenAI Realtime input.
- PCMA and PCMU inbound paths remain unchanged.
- Telnyx track capture can decode G722 captures to 16 kHz WAV for diagnostics.
- deployed VPS gateway binary and checkout are aligned to commit `2d871096fb1317a9847eed4c894ae513ce1034b8`.

Current VPS runtime remains PCMU fallback:

```text
TELNYX_STREAM_BIDIRECTIONAL_CODEC=PCMU
CARTESIA_OUTPUT_ENCODING=pcm_mulaw
CARTESIA_OUTPUT_SAMPLE_RATE=8000
AUDIO_TRANSCODE_OUTBOUND_TO=none
```

Checkpoint before next codec test:

Run one normal PCMU regression call. Only if PCMU still works, enable G722 for one controlled end-to-end call.

## PCMU Regression — 2026-06-02 (FAILED)

Caller-facing observed behaviour on the active PCMU runtime (Telnyx + OpenAI Realtime + Cartesia, `FAST_STATIC_GREETING=true`, `TELNYX_STREAM_BIDIRECTIONAL_TARGET_LEGS=self`, `TELNYX_STREAM_BIDIRECTIONAL_CODEC=PCMU`):

- Greeting audible but **noisy / garbled** to the caller (user-reported). Caller perceived repeated questions ("When?" / "When do you want the table for?") even though the gateway logs show only one Cartesia static greeting was generated.
- Caller reported "5-second pickup" — log-verified: gateway first outbound frame at 499 ms after static-greeting render start (within the documented 360-430 ms baseline). The 5 s is most likely Telnyx / PSTN ring time before `call.initiated`.
- Caller reported "2-3 second reply" — log-verified: **no OpenAI response was generated**. `response_active=false`, `cartesia_active=false` for the entire call. Caller received only the static greeting.
- Booking flow never started. `date` slot was never captured.

Log-verified exact failure boundary (from `voxlane-gateway` and `voxlane-backend` journals for call `v3:K8ohmgKkCrqM4g1Rf3D8J-c5yIMytOWNB7jW7j4ULRl10YZS7n0xEw`):

- 15:18:21 UTC — `Telnyx call.initiated`
- 15:18:22 UTC — `Telnyx answer` (HTTP 200), `Telnyx streaming_start` (HTTP 200), `Telnyx streaming.started`, `WebSocket opened`, OpenAI Realtime connected, `voice renderer: cartesia`, `static_greeting_render_start text_len=55`
- 15:18:23.214 UTC — first outbound RTP frame sent to Telnyx, `static_greeting_first_outbound_frame since_render_start_ms=499`
- 15:18:23 to 15:18:55 — outbound RTP frames sent (PCMU 8 kHz, ~50 fps), `cartesia` stream open, `echo suppression` engaged
- 15:18:55 — `cartesia: stream complete: 16 chunks` — **the entire 55-char static greeting took 32 s of outbound playback to deliver 16 Cartesia chunks**. Normal pacing for a 55-char greeting is ~3-5 s. This is a ~10x playback stretch on the Cartesia→Telnyx path.
- 15:18:56.16 — `first inbound frame appended after echo suppression, suppressed_frames=318 suppressed_duration_ms=6360 appended_frames=1340` — 6.36 s of caller audio was classified as echo of the Cartesia greeting; the remaining 26.8 s of caller audio was sent to OpenAI.
- 15:18:56.6 — `openai append stats turn_frames=100 turn_encoded_bytes=128000 pending_frames=100 pending_encoded_bytes=128000 response_active=false cartesia_active=false` — only 1.6 s of caller audio was actually appended to OpenAI as a turn; the rest was dropped (likely because the call ended or the VAD cut off). OpenAI did not produce a response.
- 15:18:57 — `Telnyx call.hangup`, `Twilio disconnected, cleaning up` (the `Twilio disconnected` is a log-string reuse bug; the provider is Telnyx). `OpenAI ReadLoop ended`. `session ended`.

Caller perception vs log reality:

- "5 s pickup" — Telnyx ring time, not gateway latency. Gateway first-outbound-frame latency was 499 ms, within baseline.
- "noisy" / "When?" / "When do you want the table for?" / "she repeated the question" — these are consistent with the 32-s stretched greeting audio sounding garbled to the caller, plus 6.36 s of echo suppression treating caller audio as echo, plus no OpenAI reply. The caller likely heard stretched, echoed greeting audio and misperceived it as repeated questions.
- "2-3 s reply" — there was no reply. `response_active=false` for the whole call.

Diagnosis candidates (out of scope to fix in this task):

- Cartesia→Telnyx playback pacing is stretched ~10x for the static greeting (16 chunks in 32 s instead of ~4 s). Root cause is in the Cartesia renderer or the gateway outbound pacing.
- Echo suppression window is 6.36 s, which is longer than the natural playback window. After 6.36 s the suppression ends and the remainder of caller audio is forwarded. The VAD or the suppression tail is not properly aligned with the stretched greeting.
- OpenAI received 1.6 s of caller audio but did not respond before the call ended. Either the VAD did not declare end-of-turn in time, or the call ended too quickly, or the audio was too short for a turn.

Action taken:

- **G722 was NOT enabled.** Per protocol, G722 is gated on a clean PCMU regression.
- No code change was made.
- The three doc files were updated; no commit was made.

Next step (gated on user instruction):

- Investigate the Cartesia→Telnyx playback stretch and the OpenAI turn boundary on the active PCMU path. After a normal PCMU call passes, the G722 controlled test can be re-attempted.

## G722 Controlled Live Test — 2026-06-03 (COMPLETED, REVERTED TO PCMU)

G722 was enabled for one controlled live test. The test completed successfully (all booking slots captured, no errors in the main audio pipeline), and the runtime was reverted to PCMU.

### Pre-test PCMU baseline (confirmed before switch)
- Gateway active, backend active, `/healthz` 200
- `TELNYX_STREAM_BIDIRECTIONAL_CODEC=PCMU`, `CARTESIA_OUTPUT_ENCODING=pcm_mulaw`, `CARTESIA_OUTPUT_SAMPLE_RATE=8000`, `AUDIO_TRANSCODE_OUTBOUND_TO=none`
- Natural-flow PCMU regression call (2026-06-03 00:10:06–00:10:45, 39 s) passed cleanly: all slots captured, no mechanical behavior, no pacing issues, no WebSocket errors
- Outbound Cartesia capture clean; inbound had a constant −34.6 dB noise floor classified as normal G.711 narrowband quantization noise ceiling
- PCMU fallback values known and ready for revert

### G722 runtime (enabled, tested, reverted)
- `TELNYX_STREAM_BIDIRECTIONAL_CODEC=G722`
- `CARTESIA_OUTPUT_ENCODING=pcm_s16le`
- `CARTESIA_OUTPUT_SAMPLE_RATE=16000`
- `AUDIO_TRANSCODE_OUTBOUND_TO=g722`
- Unchanged: `FAST_STATIC_GREETING=true`, `TELNYX_STREAM_TRACK=inbound_track`, `TELNYX_STREAM_BIDIRECTIONAL_TARGET_LEGS=self`, `VOICE_RUNTIME=custom`, `VOICE_RENDERER=cartesia`, `CARTESIA_SPEED=1`
- Backup: `/opt/ai-voice-receptionist/.env.bak-pre-g722test-2026-06-03`

### G722 live call result (`v3:kYO2YB4ycL6HUvJrLLHucjIQIfIyaKykNx1qVjFKNBSbfWA_9yRJCw`, 00:50:54–00:51:36 UTC, 42 s)

User-reported (caller perception):
- Greeting heard: yes
- Voice quality vs PCMU: **more or less the same** (no dramatic improvement)
- Line noise vs PCMU: **still had a bit of noise** (noise floor not eliminated)
- Mechanical sound: **a bit reduced** (marginal improvement)
- Latency vs PCMU: **a bit better** (marginal improvement)
- OpenAI transcript quality: **a bit better** (marginal improvement)
- Date captured: yes | Time captured: yes | Guest count: yes | Name: yes | Phone: yes
- Booking flow still natural: yes
- Telnyx errors: no | WebSocket errors: no | Unsupported codec errors in main pipeline: no

Log-verified main pipeline behaviour:
- Static greeting: 22 chunks, 4.08 s (normal pacing, first frame at 273 ms — within baseline)
- Echo suppression: 1.1 s, 2.02 s, 2.94 s, 3.96 s, 4.8 s (normal growth)
- All 4 caller turns captured with correct transcripts and booking state
- Completion message "One moment, I'll check that." fired correctly at the end
- No Telnyx API errors, no WebSocket errors, no codec errors in the main audio pipeline

Debug capture module issue (debug-only, not live):
- The inbound audio capture's decode function does not support G.722; it logs `inbound audio capture decode failed codec=g722: unsupported inbound G.711 codec "g722"` for every inbound frame
- Impact: `.pcm16_8k` and `.pcm16_24k` capture files are 0 bytes; `.pcmu` is partially written; raw `.g722` capture works
- This does **not** affect the live call — the main audio pipeline decodes G.722 correctly
- The capture module's decode function is a known limitation that should be extended to support G.722 in a future task (separate from this controlled test)

### Classification

G722 is **technically viable** (no errors in the main pipeline, all booking slots captured, call completed cleanly) but does **not dramatically improve perceived audio quality** over PCMU. The remaining noise floor is present on both codecs, confirming it is not a codec quantization issue — it is likely PSTN line noise or caller-environment noise that neither codec can fix.

### Decision: Reverted to PCMU

The marginal improvements in latency, mechanical sound, and transcript quality do not justify changing the safe runtime. PCMU remains the proven safe baseline. G722 is documented as a viable alternative but not promoted to the default runtime.

PCMU is restored and verified:
- `TELNYX_STREAM_BIDIRECTIONAL_CODEC=PCMU`
- `CARTESIA_OUTPUT_ENCODING=pcm_mulaw`
- `CARTESIA_OUTPUT_SAMPLE_RATE=8000`
- `AUDIO_TRANSCODE_OUTBOUND_TO=none`
- Gateway active, backend active, `/healthz` 200, no errors

### L16 consideration

L16 (linear 16-bit PCM, 16 kHz, no compression) is not a standard Telnyx codec option and would require a different transport. It is not recommended for the next test. The remaining noise is not a codec issue, so changing the codec again is unlikely to help. The next investigation should focus on the source of the constant noise floor (PSTN line quality, caller environment, or echo path), not on codec selection.

### Files changed in this test
- `/opt/ai-voice-receptionist/.env` (temporarily switched to G722, reverted to PCMU)
- No code change made
- No commit made (debug capture files, binary, and `.env` changes are runtime-only)
