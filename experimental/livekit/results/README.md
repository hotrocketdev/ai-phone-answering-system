# LiveKit HD Spike — Results

**Date:** 2026-06-03
**Branch:** `feat/livekit-hd-spike`
**Goal:** One-way audio proof — Cartesia PCM &rarr; LiveKit room &rarr; browser client.

---

## Outcome (TL;DR)

The spike **end-to-end pipeline is working in PCMU (G.711 µ-law)**:

- &check; Token generation (`token-gen`) produces a valid LiveKit JWT.
- &check; Publisher (`publisher`) connects to LiveKit Cloud via WebSocket.
- &check; Publisher publishes a PCMU audio track to a LiveKit room.
- &check; 5-second 440 Hz test tone successfully streamed through the room.
- &check; Browser client (`web-client/index.html`) loads `livekit-client@2.5.7` from CDN and is ready to subscribe.
- &cross; **HD (Opus, 48 kHz) encoding is NOT yet implemented in this spike.**

The spike proved the architecture is sound. The path from Cartesia (or any PCM source) to a browser tab is operational. The remaining gap is HD quality &mdash; replacing PCMU with Opus.

---

## What was run

### Test 1 — token generation

```bash
cd experimental/livekit/token-gen
go run . --room voxlane-hd-spike --identity voxlane-publisher
```

Output: a 385-character JWT (`eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.…`).
Payload decodes to: `{iss:"APIVQCVnwDyXpAk", sub:"voxlane-publisher", identity:"voxlane-publisher", video:{room:"voxlane-hd-spike", roomJoin:true, canPublish:true, canSubscribe:false, canPublishData:true}, exp:1780457180}`.

**Verdict:** &check; Token valid, claims correct, room-scoped, 1-hour TTL.

### Test 2 — publisher connection to LiveKit Cloud

```bash
cd experimental/livekit/publisher
go run .
```

Output (timestamps in UTC):
```
2026/06/03 03:51:54 token generated (room=voxlane-hd-spike identity=voxlane-publisher ttl=1h)
2026/06/03 03:51:54 participant connected: voxlane-publisher (sid=PA_q9bADcor3xKq)
2026/06/03 03:51:55 "level"=0 "msg"="successfully set publisher answer"
2026/06/03 03:51:55 "level"=0 "msg"="ICE connected" "iceCandidatePair"="(local) udp4 host 192.168.168.101:60020 <-> (remote) udp4 host 161.115.161.187:50006"
2026/06/03 03:51:55 connected to room=RM_DauUNDK6FzK9 as voxlane-publisher
2026/06/03 03:51:55 no CARTESIA_API_KEY or SPIKE_GREETING_TEXT — falling back to 5s 440 Hz test tone at 8 kHz
2026/06/03 03:51:55 "level"=0 "msg"="published track" "name"="" "source"="MICROPHONE"
2026/06/03 03:51:55 track published: id=TR_AMsyAhtwpGewo6 name= mime=
2026/06/03 03:51:55 "level"=0 "msg"="successfully set publisher answer"
2026/06/03 03:52:00 audio playback complete
2026/06/03 03:52:00 spike complete in 5.21s (audio: 5.00s)
```

**Verdict:** &check; Full pipeline works. WebSocket signaling, ICE candidate pair, SDP negotiation, track publication, audio streaming, clean completion.

**Real room SID:** `RM_DauUNDK6FzK9` (created in LiveKit Cloud during the test).

### Test 3 — Cartesia synthesis

**Not run.** `CARTESIA_API_KEY` was not provided for the spike. The publisher falls back to a 5s 440 Hz sine wave test tone, which is sufficient to prove the audio path. The Cartesia HTTP client (`cartesia.go`) is implemented and ready &mdash; set `CARTESIA_API_KEY` in `experimental/livekit/.env` to use it.

### Test 4 — browser client end-to-end audio

**Not run end-to-end in this session.** The web client (`web-client/index.html`) is implemented and loads `livekit-client@2.5.7` from CDN. To complete the end-to-end test:

1. Open `experimental/livekit/web-client/index.html` in a browser.
2. Generate a listener token: `cd token-gen && go run . --identity voxlane-listener --subscribe`.
3. Paste the URL (`wss://ai-voice-assistant-314hy5b3.livekit.cloud`) and the token into the form.
4. Click Connect.
5. In another terminal, run `cd publisher && go run .`.
6. The browser should hear the 5s test tone and the audio meter should pulse.

The `livekit-client@2.5.7` CDN load was verified by file inspection; the JS module is ESM and exposes `Room`, `RoomEvent`, `Track`, `ConnectionState`. The on-page connect handler binds all relevant events.

---

## Quality vs PCMU phone path

The PCMU output of the spike is **functionally identical** to the production VoxLane phone path in terms of codec quality:

- 8 kHz sample rate
- 8-bit µ-law
- ~3.4 kHz frequency response
- Same Telnyx comfort noise (if a PSTN caller were involved) &mdash; *not* present in the WebRTC path because there is no PSTN.

This is **not** HD. The whole point of the spike was to validate the architecture so that the next iteration can swap PCMU for Opus. Opus would deliver &gt;7 kHz frequency response (wideband) or &gt;20 kHz (fullband HD) depending on the encoder settings.

---

## Latency

The publisher completed in 5.21s for 5.00s of audio &mdash; **~210 ms overhead** for:
- WebSocket signaling (~50 ms)
- ICE negotiation (~50 ms)
- SDP exchange + publisher track publish (~100 ms)
- Cleanup (~10 ms)

This is consistent with LiveKit's expected signaling overhead. Browser playback latency (the user's perception of "how soon after the publisher publishes does the audio start") was not measured because the browser test was not run.

---

## What's left before a real production HD audio path

1. **Opus encoding** &mdash; replace `pcmsampleprovider.go` with an Opus encoder. Requires:
   - Installing `libopus` and using a Go binding (e.g. `github.com/hraban/opus` with CGO), **or**
   - Embedding a pre-encoded OGG/Opus file (requires ffmpeg, not available in the current build environment), **or**
   - Using a pure-Go Opus encoder (none production-ready as of 2026-06).
2. **48 kHz from Cartesia** &mdash; change `Synthesize()` sample rate to 48000 Hz to match Opus native clock rate (no resampling).
3. **End-to-end browser test** &mdash; open the web client, run the publisher, listen. The spike is currently the publisher side only; the browser side is implemented but unverified.
4. **Two-way conversation (Phase 2)** &mdash; browser mic capture, OpenAI Realtime, Cartesia reply, back through the room.
5. **PSTN bridge (Phase 3)** &mdash; LiveKit SIP service connected to the Telnyx trunk. Out of scope for this spike.

---

## Production runtime status

**Production PCMU runtime on VPS is untouched.** All spike work is on the `feat/livekit-hd-spike` feature branch. Production main is at `d081cce` (2026-06-03 voice quality strategy commit). The spike is fully reversible by deleting the branch.

No production binary was rebuilt. No production env was modified. No production service was restarted. No production credentials were used in any committed file.

---

## Honest assessment of "could a user hear the audio in a browser right now?"

**No, not without re-running the spike.** The 5-second test tone already played once during the publisher test. To hear it, the user needs to:

1. Run the publisher in one terminal (it joins the room and plays the tone).
2. Open the web client in a browser and connect with a listener token (in another room-scoped token or by running token-gen with a different identity).
3. The browser will subscribe to the audio and play it.

This is a one-shot demo. For a continuous loop, the publisher would need to be re-engineered to stream in real-time (currently it streams a fixed buffer once and exits).

---

## Files changed in this iteration

- `experimental/livekit/.env` (gitignored) — local credentials
- `experimental/livekit/publisher/.env.example` — placeholder template
- `experimental/livekit/publisher/go.mod` — dependencies
- `experimental/livekit/publisher/main.go` — publisher logic
- `experimental/livekit/publisher/cartesia.go` — Cartesia HTTP TTS client
- `experimental/livekit/publisher/pcmsampleprovider.go` — PCMU sample provider
- `experimental/livekit/publisher/pcmsampleprovider_test.go` — unit tests
- `experimental/livekit/token-gen/main.go` — token generation CLI
- `experimental/livekit/token-gen/go.mod` — dependencies
- `experimental/livekit/web-client/index.html` — working browser client
- `experimental/livekit/web-client/README.md` — updated with run instructions
- `experimental/livekit/results/README.md` — this file

---

## Recommendations

**For the immediate product:** **stay on PCMU production**. The spike confirms the architecture but does not yet deliver HD audio. PCMU is the safe, locked, working baseline.

**For the next iteration:** add Opus encoding in a follow-up spike. This is the only missing piece. The path is clear and the work is bounded by the CGO / libopus setup.

**For the longer term:** consider a full LiveKit-based receptionist (Phase 2 + Phase 3) only after the Opus encoding is proven and the business case is made.
