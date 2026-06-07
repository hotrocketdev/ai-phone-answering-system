# xAI Phone Harness (Telnyx/PCMU <-> xAI Voice Agent WSS)

**Status:** New spike, pre-validation. r1 will follow this README.

## Goal
Validate that the **production phone path** for xAI Voice Agent is
viable without any LiveKit or Opus in the loop. The browser/LiveKit
spike was NO-GO'd on the outbound audio transport; the phone path
never touches that transport and is therefore a different problem
that may already work.

## What this is
A minimal Node.js harness that:

1. Connects to xAI's realtime WSS (`wss://api.x.ai/v1/realtime`)
2. Declares input format = `audio/pcmu` (G.711 µ-law, 8 kHz — the
   Telnyx codec) and output format = `audio/pcm` (PCM16 24 kHz)
3. Streams PCMU bytes (from a file, a synthetic tone, or silence)
4. Receives xAI's audio deltas as base64 PCM16
5. Reuses the function-call bridge (3/3 in the LiveKit spike
   r4/r7) — `availability.check` / `booking.create` /
   `manager.escalate` with stub dispatcher
6. Writes the assistant's response to a WAV file for verification

No LiveKit, no WebRTC, no Opus, no ffmpeg, no Go.

## What this is NOT
- Not a production worker. The stub `dispatchToolCall` is a
  placeholder.
- Not a LiveKit replacement. The LiveKit browser path remains
  blocked; this is the phone path.
- Not a multi-day build. 1-2 day spike per Opus's recommendation.

## Run it

```bash
cd experimental/livekit/xai-phone-harness
npm install
echo "XAI_API_KEY=sk-..." > .env      # do NOT commit
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
- `src/pcmu-codec.js` — G.711 µ-law encode/decode + 24k↔8k downsample
- `src/tools.js` — stub function-call dispatcher
- `src/log.js` — minimal logger (mirrors the Go METRIC format)

## What's preserved from the LiveKit spike
- Function-call bridge logic (`OnFunctionCall` in Go → `function_call`
  event in Node). Same `latestCallID` handling, same stub results.
- METRIC log lines that the analyzer (cmd/analyze) already understands.
- System prompt (Porto Douro Restaurants / Alex / British English).
- VAD defaults (1500/0.6). The xAI server's internal VAD is
  configured the same way; we don't have to do anything different
  for the phone path because the audio format doesn't change
  xAI's VAD behavior.

## What's NEW for the phone path
- `session.update` declares `audio.input.format = audio/pcmu` and
  `audio.output.format = audio/pcm` (the LiveKit spike sent
  `audio.input.format = audio/pcm` and relied on internal
  resampling — we tested that path; this is the documented native
  G.711 path).
- No samplebuilder, no OGG mux, no ffmpeg. PCMU goes in, PCM comes
  out, that's it.
- Output WAV is saved for offline listening. The LiveKit spike
  streamed output to the browser in real time.

## Open questions for the next run
- Does xAI correctly accept `audio/pcmu` as input? (Opus's
  feasibility note says yes, but we haven't tested.)
- Does the model respond appropriately to a tone or to silence?
  (Expected: ask for clarification, not silently fail.)
- Does the function-call bridge fire when the model decides to
  use a tool? (Should — same protocol as the LiveKit spike.)
- Latency: how does p50/p95 compare to the LiveKit spike's
  7.7s p50? (Phone path skips the encoder/OGG layer, so should
  be lower, but xAI server-side latency is unchanged.)

## Production untouched
- No changes to `voice-gateway/`, `backend/`, `docs/context/*`, or
  anything outside `experimental/livekit/xai-phone-harness/`.
- No .env, no tokens, no WAVs, no debug logs committed.
- Branch: `feat/livekit-hd-spike` (same as the LiveKit spike).
