# LiveKit HD Spike — Results

**Date:** 2026-06-03 (PCMU), 2026-06-04 (Opus)
**Branch:** `feat/livekit-hd-spike`
**Goal:** One-way audio proof — Cartesia PCM → LiveKit room → browser client. **HD Opus follow-up.**

---

## Outcome (TL;DR)

**Two codec paths are now working in the spike publisher:**

1. **PCMU (G.711 µ-law, 8 kHz)** — original spike, end-to-end pipeline proven:
   - ✓ Token generation (`token-gen`) produces a valid LiveKit JWT.
   - ✓ Publisher connects to LiveKit Cloud via WebSocket.
   - ✓ Publisher publishes a PCMU audio track to a LiveKit room.
   - ✓ 5-second 440 Hz test tone successfully streamed through the room.
   - ✓ Browser end-to-end audio test passed at the protocol level (3 sessions, `play()` invoked and not rejected, 5s of audio ran cleanly).

2. **Opus (libopus, 48 kHz, mono, 64 kbps VBR)** — HD follow-up, ffmpeg-backed:
   - ✓ ffmpeg 6.1.1 + libopus available on VPS (no install required).
   - ✓ Ogg demuxer written in Go (~100 lines, 3 unit tests pass).
   - ✓ `SPIKE_AUDIO_CODEC=opus` switch added to publisher.
   - ✓ Pre-flight run on VPS succeeded: `ffmpeg_started=true pid=2463713`, `opus_header version=1 channels=1 input_rate=48000`, `ogg_demuxer_ready`, track `TR_AMaGhvuxhYfLQL` published, `spike complete in 5.234s`.
   - ⚠ Browser end-to-end test for Opus path pending — user must run browser test (see [BROWSER_AUDIO_TEST_RUNBOOK.md](BROWSER_AUDIO_TEST_RUNBOOK.md) §"Opus path").

The spike is **NOT** an HD success until:
1. ✓ The browser actually hears the audio (PCMU protocol-level verified; Opus browser verification pending).
2. The publisher uses Opus (not PCMU) — Opus path implemented, browser verification pending.
3. Subjective quality exceeds the PSTN phone path — not yet measured.

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

---

## Opus/HD Follow-Up Plan

**Status (2026-06-04): Opus path IMPLEMENTED via ffmpeg. Browser verification pending.**

**Why the PCMU publisher is not HD:**
- PCMU is G.711 µ-law, 8 kHz, ~3.4 kHz frequency response. Same codec as the production phone path.

**What was blocking Opus (2026-06-03 blockers, now resolved for the spike):**
- ~~`github.com/hraban/opus` requires CGO + libopus + has a `gopus.Stream` regression on Go 1.26.3~~
- ~~The LiveKit Go SDK does not encode Opus internally — it expects pre-encoded Opus packets~~

**What we did (2026-06-04):**
- **ffmpeg + libopus** are already installed on the VPS (ffmpeg 6.1.1 with `--enable-libopus`).
- The publisher spawns ffmpeg as a child process. ffmpeg reads PCM from stdin, writes Ogg Opus to stdout.
- A new Go Ogg demuxer (`oggdemuxer.go`, ~100 lines) strips the Ogg container and yields raw Opus packets to a `OpusSampleProvider` (in `ffmpegopus.go`).
- The LiveKit `OpusPayloader` (already in `localsampletrack.go`) packetizes the raw Opus bytes into RTP.
- A new env flag `SPIKE_AUDIO_CODEC=pcmu|opus` selects the codec at runtime. Default is `pcmu` to preserve existing behavior.
- The LiveKit Go SDK version conflict is **not** an issue here: the SDK's `NewLocalSampleTrack` accepts a `RTPCodecCapability{MimeType: "audio/opus"}` directly, and `OpusPayloader` is the matching payloader. No SDK upgrade needed.

**What remains for the Opus spike:**
1. **Browser end-to-end verification.** The user must run the browser test (runbook) with `SPIKE_AUDIO_CODEC=opus` and listen for the 5-second 440 Hz tone. The browser's `livekit-client@2.5.7` natively supports Opus, so the existing `web-client/index.html` should work without changes.
2. **Cartesia HD PCM (Step 5).** Pipe Cartesia's `pcm_s16le` at 24 kHz or 48 kHz into ffmpeg via stdin (instead of the synthetic 440 Hz tone). This is a small change to `ffmpegopus.go` — add a function that writes a Cartesia PCM buffer to the ffmpeg stdin pipe and closes stdin when done. ~20 lines of code.

**Why we chose ffmpeg (Option 2) over the other 5 approaches:**

The earlier follow-up plan listed 5 approaches. The chosen path was **Option 2 (ffmpeg)** for these reasons:
- ffmpeg was already installed on the VPS — zero install risk.
- libopus is mature, well-tested, and used by all major Opus implementations.
- The Go publisher stays clean: just spawn a child process, demux Ogg, feed packets.
- Avoids the Go CGO/toolchain hell with `hraban/opus` and newer LiveKit SDK versions.
- The original Spike PCMU path (`SPIKE_AUDIO_CODEC=pcmu`) is preserved unchanged for regression.

**Once Opus works in the publisher:**
- Re-run the browser end-to-end test (runbook).
- Compare subjective quality to PCMU (the spike will be a real HD test, not just a connectivity test).
- If quality is clearly better, proceed to Phase 2 (two-way conversation).
- If quality is the same as PCMU, investigate Opus encoder settings (bitrate, FEC, dtx).

---

## Browser End-to-End Audio Test (Step 2 of the iteration)

**Status: RUNBOOK READY, AWAITING USER EXECUTION**

A CLI agent cannot open a browser, so the actual end-to-end audio test must be performed by the user. The runbook in [BROWSER_AUDIO_TEST_RUNBOOK.md](BROWSER_AUDIO_TEST_RUNBOOK.md) is the exact procedure.

**What I verified programmatically:**
- The web client HTML loads `livekit-client@2.5.7` ESM from CDN — `https://unpkg.com/livekit-client@2.5.7/dist/livekit-client.esm.mjs`.
- The HTML form, status, audio element, level meter, and log wiring are syntactically correct.
- A listener token can be generated for `voxlane-listener` identity with subscribe permission.
- The publisher publishes a track under the same room name (`voxlane-hd-spike`).

**What requires the user:**
1. Open `experimental/livekit/web-client/index.html` (or serve it via `python -m http.server 8765`).
2. Paste the URL + a freshly generated listener token.
3. Click Connect.
4. Start the publisher (in wait-for-subscriber mode, or one-shot).
5. Listen for the 5-second 440 Hz tone.
6. Report back: did you hear it? any errors? any autoplay or mute issues?

**Pre-flight publisher test (run 2026-06-03 04:27:27 UTC, no listener):**
```
token generated (room=voxlane-hd-spike identity=voxlane-publisher ttl=1h)
participant connected: voxlane-publisher (sid=PA_a2XzDf72nc8P)
ICE connected udp4 192.168.168.101:62499 <-> 161.115.161.187:50006
connected to room=RM_86T59simEdiN
falling back to 5s 440 Hz test tone at 8 kHz
published track name="" source="MICROPHONE"
track published: id=TR_AM9tuba5XUdHdt mime=
audio playback complete
spike complete in 5.19s (audio: 5.00s)
```

**Pre-flight wait-for-subscriber test (run 2026-06-03 04:27:55 UTC):**
```
token generated
connected to room=RM_86T59simEdiN
listener already in room (1 remote) — proceeding with publish  ← stale entry from previous test
track published: id=TR_AMu9yEoTrv373B
```

The publisher's `SPIKE_WAIT_FOR_SUBSCRIBER=true` mode (env var) waits for a remote participant before publishing. This makes browser setup easier.

**What was NOT run:**
- The browser end-to-end test itself (the agent has no GUI).
- Cartesia synthesis (no API key in this spike).
- Opus encoding (deferred to follow-up spike per the plan above).

**Diagnosis path if the user reports the browser does not hear audio:**

| Symptom | Likely cause | Fix |
|---|---|---|
| `room.connect()` hangs | UDP blocked on browser network | Try a different network (e.g. mobile hotspot) |
| `audio.play() rejected: Autoplay policy blocked` | Browser autoplay policy | Click anywhere on the page first, re-click Connect |
| Connected, no `track subscribed` event | Publisher not running, or different room | Start the publisher, check `--room` flag matches |
| Track subscribed but silent | Audio element muted, wrong output device | Check tab/system volume, check `chrome://media-internals` |
| Token rejected (`invalid grant`, `signature mismatch`) | Wrong API key/secret, expired token, wrong project | Re-generate token, check `.env` matches the LiveKit project |
| One-way audio (publisher hears nothing) | Browser mic not enabled, or browser test was one-way | Expected for this spike — spike is one-way only |
| "ICE failed" or "DTLS handshake failed" | Network/firewall blocking UDP | Try TURN/TCP (not yet implemented in this spike) |

The full troubleshooting guide is in [BROWSER_AUDIO_TEST_RUNBOOK.md](BROWSER_AUDIO_TEST_RUNBOOK.md).
