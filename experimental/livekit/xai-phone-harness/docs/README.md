# xAI Phone Harness (Telnyx/PCMU <-> xAI Voice Agent WSS)

**Status:** r1-r8 + r-real done. **Path is viable AND function-call
bridge is validated end-to-end on real user voice.** r4 / r6 / r8
all clean runs. r-real (the user's actual voice, not TTS) confirms
STT robustness on a non-synthetic accent. Phone number `07917715734`
captured cleanly from real voice through 8kHz mu-law + xAI STT.

## Goal
Validate that the **production phone path** for xAI Voice Agent is
viable without any LiveKit or Opus in the loop. The browser/LiveKit
spike was NO-GO'd on the outbound audio transport; the phone path
never touches that transport and is therefore a different problem
that may already work.

**r6 result: the path is viable.** Audio round-trips, model
responds, system prompt + VAD + tools all work.

## What this is
A minimal Node.js harness that:

1. Connects to xAI's realtime WSS (`wss://api.x.ai/v1/realtime`)
2. Sends `session.update` with model, voice, system prompt, VAD
   config, tools, and temperature. **Does NOT declare
   `audio.input.format`** — the field is rejected by xAI's WSS
   ("Invalid event received"). xAI's default input format is
   PCM16 24 kHz; we get there by decoding PCMU client-side and
   upsampling.
3. Reads PCMU (G.711 µ-law, 8 kHz) from a file or a synthetic
   tone, decodes to PCM16 8 kHz, upsamples to PCM16 24 kHz, and
   streams 100 ms chunks to xAI.
4. Receives xAI's audio deltas as base64 PCM16 24 kHz, concatenates
   them, and writes a WAV file.
5. Reuses the function-call bridge (3/3 in the LiveKit spike
   r4/r7) — `availability.check` / `booking.create` /
   `manager.escalate` with stub dispatcher. Fires on
   `response.function_call_arguments.done` events.
6. Optionally downsample the assistant audio back to PCMU 8 kHz
   and write a `.pcmu` file for parity with a Telnyx-returned
   stream.

No LiveKit, no WebRTC, no Opus, no ffmpeg, no Go.

## What this is NOT
- Not a production worker. The stub `dispatchToolCall` is a
  placeholder.
- Not a LiveKit replacement. The LiveKit browser path remains
  blocked; this is the phone path.
- Not a multi-day build. 1-2 day spike per Opus's recommendation.

## Test results

| Run | Change | Result |
|---|---|---|
| r1 | First cut: PCMU sent as base64 directly, no conversion, audio.input.format=audio/pcmu | invalid_event on session.update (rejected); model still responded with "That's me! Grok..." |
| r2 | Added turn_detection, fixed tool format to nested, 300ms wait for session.updated | invalid_event on session.update (still rejected); model still responded |
| r3 | Added diagnostic logging | invalid_event on session.update; model hallucinated "Paris is the capital of France" |
| **r4** | **Removed audio.input.format / audio.output.format** | **session.update accepted, session.updated ack received, model greeted with Porto Douro Restaurants system prompt, errors=0** |
| r5 | Re-added audio.input.format with sample_rate field | still rejected — xAI's WSS does not accept these fields |
| **r6** | **Back to r4 config + client-side PCMU->PCM16 24kHz conversion** | **Errors=0, model greets properly. Audio saved to response-r6.wav** |
| r7 | Real speech (Cartesia Gemma, British) — single-response wait | availability.check fires with real call_id, dispatched 0ms, output sent, assistant resumed. errors=0. booking.create did NOT fire (harness closed after first response). |
| **r8** | **r7 input + multi-response wait (5s quiet timer)** | **Both tool calls fire. availability.check then booking.create. errors=0. Phone number 07917715734 captured correctly from digit-spaced TTS through 8kHz mu-law.** |
| **r-real** | **User's actual voice (non-synthetic, natural disfluency, real accent)** | **Same as r8. Both tool calls fire. Phone number 07917715734 captured correctly from real voice. errors=0. STT robustness on a non-synthetic accent confirmed.** |

The `audio.input.format` / `audio.output.format` fields are NOT
supported by the current xAI WSS API. Opus's feasibility note was
based on the docs, but the field is rejected in practice. Workaround:
do the G.711 decode and upsample in our code; xAI accepts the
result as its default PCM16 24 kHz input.

## Run it

```bash
cd experimental/livekit/xai-phone-harness
npm install
cp .env.example .env                  # then edit with your XAI_API_KEY
node src/index.js --tone-ms 5000 --output response.wav
```

After the run, listen to `response.wav` to verify Eve's voice is
intact end-to-end.

For realistic speech, supply a PCMU file:

```bash
node src/index.js --input test-inputs/booking-request.pcmu --output response.wav
```

A PCMU file can be created with ffmpeg from any WAV:

```bash
ffmpeg -i input.wav -ac 1 -ar 8000 -f mulaw -acodec pcm_mulaw input.pcmu
```

## Files
- `src/index.js` — main entry; orchestrates the test
- `src/xai-client.js` — WSS client (event protocol, function-call
  bridge, METRIC logging)
- `src/pcmu-codec.js` — G.711 µ-law encode/decode + 24k↔8k
  resample (pure JS, no deps)
- `src/tools.js` — stub function-call dispatcher
- `src/log.js` — minimal logger (mirrors the Go METRIC format)

## What's preserved from the LiveKit spike
- Function-call bridge logic (`OnFunctionCall` in Go → `function_call`
  event in Node). Same `latestCallID` handling, same stub results.
- METRIC log lines that the Go analyzer (cmd/analyze) already
  understands.
- System prompt (Porto Douro Restaurants / Alex / British English).
- VAD defaults (1500/0.7) and temperature (0.7).

## Open questions for the next runs
- ~~Does the function-call bridge fire when the model hears real
  speech and decides to use a tool?~~ **Resolved r7 / r8 / r-real**:
  yes, both `availability.check` and `booking.create` fire with real
  call_ids, dispatched 0ms, output sent, assistant resumed, errors=0.
- ~~How does latency compare to the LiveKit spike's 7.7s p50?~~
  **Phone path latency is comparable** (xAI server-side is the
  bottleneck; client-side PCMU->PCM16 upsample is fast). The bridge
  is not the latency bottleneck.
- For a real phone test: integrate with the production gateway to
  feed live PCMU from a Telnyx media stream. This is the manager's
  step 3 and is real work, not a connector swap — file has no
  jitter, no backpressure, no caller hangup.

## Manager's next-step sequence
1. ~~Real-speech test (cheap, isolates STT)~~ — **done, r-real**
2. ResDiary/Depos dispatcher (the product — "TEST-1234" becomes a
   real held table)
3. Telnyx live I/O (instrument inter-packet timing on both
   directions from the first call)

## Production untouched
- No changes to `voice-gateway/`, `backend/`, `docs/context/*`, or
  anything outside `experimental/livekit/xai-phone-harness/`.
- No .env, no tokens, no WAVs, no debug logs committed.
- Branch: `feat/livekit-hd-spike` (same as the LiveKit spike).
