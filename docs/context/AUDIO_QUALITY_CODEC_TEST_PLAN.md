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

G722 outbound code is implemented behind flags, but G722 cannot become a candidate default until the inbound G722 decode path is implemented and validated. The next codec task should be either:

1. implement inbound G722 decode to PCM16 24 kHz for OpenAI, then retry G722 end-to-end, or
2. test L16 as a separate controlled path if Telnyx can provide inbound/outbound linear PCM without G722 ADPCM decode complexity.
