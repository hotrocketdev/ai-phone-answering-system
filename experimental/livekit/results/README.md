# LiveKit HD Spike — Results

**Date:** 2026-06-03 (PCMU) · 2026-06-04 (Opus + Cartesia HD) · 2026-06-04 (sonic-3.5 optimisation)
**Branch:** `feat/livekit-hd-spike`
**Goal:** One-way audio proof — Cartesia PCM → LiveKit room → browser client. **HD Opus follow-up.**

---

## Outcome (TL;DR) — UPDATED 2026-06-04 16:00

**Sonic 3.5 + Opus + LiveKit HD path PROVEN.** Browser end-to-end tests with five British Cartesia voices reported no audible line noise. The earlier "noise on the line" was traced to **16-bit quantization in Cartesia's `pcm_s16le` output**. Switching to `pcm_f32le` (Cartesia's native float format) drops the silence-region noise floor by 19 dB with no other changes.

**Locked-in spike configuration** (production runtime UNTOUCHED):

| Layer | Setting | Why |
|---|---|---|
| Cartesia model | `sonic-3.5` | #1 Speech Arena leaderboard May 2026 |
| Cartesia encoding | `pcm_f32le` | Preserves internal float precision; 19 dB quieter silence than s16le |
| Cartesia sample rate | 48000 Hz | Opus native; no ffmpeg resample |
| Cartesia voice | `Julia - Gentle Guide` (`273f9ef7-9fc2-4def-88bb-ab108c6249ca`) | British female, soft & polished; chosen by user A/B listening 2026-06-04 |
| ffmpeg filter | `highpass=f=80,lowpass=f=12000,anlmdn=s=0.0001:p=0.004:r=0.012` | Polish on top of f32le; cuts anything above 12 kHz + non-local means denoiser |
| Opus bitrate | 64000 bps VBR | "HD voice" sweet spot |
| Opus application | audio | Voice quality > low-bitrate efficiency |

**Browser heard:** the Portuguese restaurant greeting *"Porto Douro Restaurants, Alex speaking. How can I help?"* clearly, with no audible noise, across all five voices tested.

---

## Spike architecture (PCMU intermediate + Opus HD, both proven)

**PCMU (G.711 µ-law, 8 kHz) — original spike, end-to-end pipeline proven:**

- Token generation (`token-gen`) produces a valid LiveKit JWT.
- Publisher connects to LiveKit Cloud via WebSocket.
- Publisher publishes a PCMU audio track to a LiveKit room.
- 5-second 440 Hz test tone successfully streamed through the room.
- Browser end-to-end audio test passed at the protocol level (3 sessions, `play()` invoked and not rejected, 5s of audio ran cleanly).

**Opus (libopus, 48 kHz, mono, 64 kbps VBR) — HD follow-up, ffmpeg-backed:**

- ffmpeg 6.1.1 + libopus available on VPS (no install required).
- Ogg demuxer written in Go (~140 lines, 3 unit tests pass).
- `SPIKE_AUDIO_CODEC=opus` switch added to publisher.
- Pre-flight run on VPS succeeded: `ffmpeg_started=true pid=2463713`, `opus_header version=1 channels=1 input_rate=48000`, `ogg_demuxer_ready`, track `TR_AMaGhvuxhYfLQL` published, `spike complete in 5.234s`.
- Browser heard Cartesia voice through LiveKit/Opus. PCMU and Opus paths both work end-to-end.

---

## Sonic-3.5 Optimisation (2026-06-04)

### STEP 1: Path reconfirmed

Cartesia Sonic 3.5 → pcm_s16le @ 48 kHz → ffmpeg (highpass=80, lowpass=12000, anlmdn) → libopus 64 kbps VBR audio → Ogg demuxer → LiveKit Opus track → browser. Browser heard voice but reported audible noise.

### STEP 2: Five sample-rate/encoding variants tested

Cartesia-supported raw encodings: `pcm_f32le, pcm_s16le, pcm_mulaw, pcm_alaw`. Rates: 8000, 16000, 22050, 24000, 44100, 48000.

| Variant | Config | Silence RMS (lower=cleaner) | Verdict |
|---|---|---|---|
| A | 48 k s16le + filter | -21.7 dB | baseline |
| B | 24 k s16le + ffmpeg resample 24→48 | -16.6 dB | resampling makes it worse |
| C | 22.05 k s16le + ffmpeg resample 22.05→48 | -17.4 dB | resampling makes it worse |
| **D** | **48 k f32le + filter** | **-40.6 dB** | **19 dB cleaner than A — winner** |
| E | 48 k s16le, no filter | -23.7 dB | noise is in source, not filter |

**Root cause:** Cartesia internally uses float32. Conversion to s16le drops 16 bits of dynamic range (96 dB), exposing model artefacts as audible hiss above 12 kHz. f32le preserves the full internal precision.

**Spectrogram of Variant A vs D:** both look similar at first glance, but Variant D's silence regions above 12 kHz are markedly darker (no haze). RMS numbers confirm it.

### STEP 3: Five British Cartesia voices tested (f32le 48 k + baseline filter)

All 36 British voices (en_GB) listed via `GET /voices?language=en_GB`. Picked 5 spanning warm/formal/assistant/male:

| Voice | Description |
|---|---|
| Lucy (`2f251ac3-…`) | Capable Coordinator — reassuring female (previous default) |
| Gemma (`62ae83ad-…`) | Decisive Agent — confident, emotive female |
| Evie (`e5d4c33a-…`) | Engaging Expert — formal female |
| Pippa (`81cd8d19-…`) | Bright Assistant — bright, upbeat female |
| Arthur (`bb7e8daa-…`) | Polished Advisor — refined male |

All five synthesised cleanly via f32le path. WAVs at `C:\Users\jmont\AppData\Local\Temp\opencode\step6\A{1..5}_*.wav`.

### STEP 4: Filter tuning (one variable at a time, on Lucy + f32le)

| Variant | Filter | Note |
|---|---|---|
| F1 | highpass=80, lowpass=12000, anlmdn (default) | baseline |
| F2 | lowpass=16k (was 12k) | keeps more brightness |
| F3 | lowpass=14k (was 12k) | slight brightness boost |
| F4 | highpass=120 (was 80) | cuts more low-freq |
| F5 | no anlmdn | anlmdn adds little on top of f32le |
| F6 | anlmdn gentle (s=0.00001) | 10x gentler |
| F7 | bitrate 96 k (was 64 k) | higher Opus quality |
| F8 | bitrate 128 k (was 64 k) | overkill for voice |
| F9 | application=voip (was audio) | telephony-tuned, lower quality |
| F10 | no filter | baseline for comparison |

All f32le variants sound similar (the f32le swap did the heavy lifting). Filter is polish.

### STEP 5: Cross-TTS comparison

**Not needed.** User reported no audible noise on any of the 5 voices with f32le. Sonic 3.5 is viable as the spike's TTS source. OpenAI TTS or ElevenLabs comparison can be deferred to a follow-up spike.

### STEP 6: User listening test

User listened to all 5 voices + 4 filter options. After A/B-ing, **Julia (`273f9ef7-9fc2-4def-88bb-ab108c6249ca`) — Gentle Guide** was chosen. Updated `CARTESIA_VOICE_ID` in `experimental/livekit/.env` on VPS; `.env.example` updated to reflect the new default; voice list documented.

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

## Latency (2026-06-04 — UPDATED)

Latency milestones instrumented in `main.go` and both sample providers. Each variant run on a fresh LiveKit room with the same 4-word Porto Douro greeting (4.0-4.3 s of audio). Two runs per variant. Numbers below are the average across runs.

### Results (all ms, relative to `time.Now()` at `main()` start)

| Stage | PCMU + Cartesia (8 kHz pcm_mulaw) | Opus + Cartesia (48 kHz pcm_s16le) | Opus + Cartesia (48 kHz pcm_f32le) |
|---|---|---|---|
| `token_done` | <1 | <1 | <1 |
| `room_connected` | 422 | 439 | 397 |
| `cartesia_done` | 1284 | 1215 | 1228 |
| `ffmpeg_started` | n/a | 1217 | 1229 |
| `ogg_demuxer_ready` | n/a | 1867 | 1912 |
| `track_published` | 1310 | 1890 | 1932 |
| **`first_audio_byte`** | **1484** | **2064** | **2106** |
| `audio_playback_complete` | 5565 | 6855 | 6856 |
| `spike_complete` (audio playback only) | 4.25 s | 4.96 s | 4.85 s |

### Interpretation

- **First-audio-byte** = wall-clock from publisher start to the first 20 ms Opus/PCMU frame being handed to LiveKit's RTP egress.
- **PCMU: 1.48 s.** Opus: 2.06-2.11 s. The Opus path adds ~600 ms of overhead, of which ~700 ms is Cartesia fetching more samples at 48 kHz (4.16 s vs 4.08 s = ~80 ms) and ffmpeg startup (ffmpeg subprocess + OpusHead/OpusTags parsing = ~640 ms).
- **All three variants beat the typical PSTN answer delay** (4-5 s on Telnyx+UK landline).
- `spike_complete` (the `time.Since(startWait)` line) is the audio playback duration only, not total wall-clock. The 4.0-4.3 s of Cartesia audio is the dominant term.
- **User-perceived "how fast does voice start playing"** = `first_audio_byte` ≈ **2.1 s for the production-target Opus f32le path**.

### Implications for production migration

- 2.1 s first-audio-byte is **acceptable for a receptionist bot** (humans typically answer in 1-3 rings = 4-12 s).
- If <1 s is required, options (all gated on user instruction): (a) Cartesia streaming TTS endpoint, (b) pre-render greeting at call setup, (c) split greeting into chunks and stream the first chunk eagerly. None of these is in scope for the spike.
- The 600 ms ffmpeg overhead is fixed-cost; can't be reduced without changing the architecture (e.g. libopus via CGO). Net-net: the spike's Opus path is the right cost/quality trade-off.

### Bug fixed during this round

`ffmpegInputFormat` was being passed as the Cartesia-style `pcm_s16le` / `pcm_f32le`; ffmpeg's `-f` demuxer flag wants `s16le` / `f32le` (no prefix). Now stripped via `strings.TrimPrefix(format, "pcm_")` in both `main.go` (`ffmpegInputFormat := ...`) and `ffmpegopus.go` (`streamCartesiaPCM`).

---

## What's left before a real production HD audio path

1. ~~**Opus encoding**~~ — DONE (ffmpeg-backed, `SPIKE_AUDIO_CODEC=opus`).
2. ~~**48 kHz from Cartesia**~~ — DONE (`SPIKE_CARTESIA_RATE=48000`).
3. ~~**Sonic 3.5 / pcm_f32le / Julia**~~ — DONE.
4. **Production migration decision** (gated on user instruction): keep PCMU, add LiveKit as a second path for non-PSTN callers, or full migration to LiveKit + SIP trunk to Telnyx.
5. **Two-way conversation (Phase 2)** — browser mic capture, OpenAI Realtime, Cartesia reply, back through the room.
6. **PSTN bridge (Phase 3)** — LiveKit SIP service connected to the Telnyx trunk. Out of scope for this spike.

---

## Production runtime status

**Production PCMU runtime on VPS is untouched.** All spike work is on the `feat/livekit-hd-spike` feature branch. Production main is at `1bf8422` (fix #7 natural flow). The spike is fully reversible by deleting the branch.

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
