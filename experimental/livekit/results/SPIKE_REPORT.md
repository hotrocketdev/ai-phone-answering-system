# LiveKit HD Audio Spike — Final Report

**Date:** 2026-06-04
**Branch:** `feat/livekit-hd-spike` (not merged to main)
**Production impact:** NONE. Production PCMU runtime on VPS is untouched.
**Report status:** All spike work complete. Awaiting product-level decision on production migration.

---

## 1. TL;DR

The LiveKit HD audio spike is **done and proven**. We can deliver near-human voice quality (Opus, 48 kHz, ~20 kHz audio bandwidth) from Cartesia Sonic 3.5 to a browser over LiveKit Cloud, with **no audible line noise** and **~2.1 s first-audio-byte latency** — well under the typical 4-5 s PSTN answer delay.

- **End-to-end audio path proven:** Cartesia → Go publisher → ffmpeg (Opus encode + filter) → LiveKit Cloud → browser. Voice heard cleanly on a `livekit-client@2.5.7` web client.
- **Noise issue root-caused and fixed:** the earlier "noise on the line" was 16-bit quantization noise from Cartesia's `pcm_s16le` output. Switching to `pcm_f32le` (Cartesia's native float format) drops the silence-region noise floor by **19 dB**.
- **Voice selection complete:** 5 British Cartesia voices (Lucy, Gemma, Evie, Pippa, Arthur) tested clean via the f32le path. **Julia (`273f9ef7-9fc2-4def-88bb-ab108c6249ca`) — "Gentle Guide"** chosen by user A/B listening.
- **Latency measured:** first-audio-byte = 2.1 s for the production-target Opus/f32le path; spike complete ≈ 4.9 s for 4.0-4.3 s of greeting audio. Both acceptable.
- **Production runtime is fully untouched** — all work on the `feat/livekit-hd-spike` feature branch, all on a non-production VPS env. Stop conditions still in force.

---

## 2. What was built (deliverables on the spike branch)

| Component | File | Notes |
|---|---|---|
| Token-generation CLI | `experimental/livekit/token-gen/main.go` | Generates LiveKit JWTs (1-hour TTL, room-scoped). Tested. |
| Audio publisher | `experimental/livekit/publisher/main.go` | PCMU (8 kHz) + Opus (48 kHz) paths. `SPIKE_AUDIO_CODEC` switch. Latency milestones. |
| Cartesia HTTP TTS client | `experimental/livekit/publisher/cartesia.go` | Decodes pcm_s16le, pcm_f32le, pcm_mulaw, pcm_alaw. |
| PCMU sample provider | `experimental/livekit/publisher/pcmsampleprovider.go` | Pure Go µ-law encoder. 20 ms frames, 5 unit tests pass. |
| ffmpeg-backed Opus provider | `experimental/livekit/publisher/ffmpegopus.go` | ffmpeg subprocess + Ogg demux + libopus 64 kbps VBR audio mode. |
| Ogg/Opus demuxer | `experimental/livekit/publisher/oggdemuxer.go` | Ogg page demuxer, 3 unit tests pass. |
| Browser web client | `experimental/livekit/web-client/index.html` | ESM, `livekit-client@2.5.7` from CDN, audio meter, log area. |
| Runbook | `experimental/livekit/results/BROWSER_AUDIO_TEST_RUNBOOK.md` | 7-step browser test procedure. |
| Spike results | `experimental/livekit/results/README.md` | Full results incl. sonic-3.5 + latency sections. |
| Spike design | `docs/context/LIVEKIT_HD_SPIKE_PLAN.md` | 16-section design. |
| Voice-quality strategy | `docs/context/VOICE_QUALITY_STACK_STRATEGY.md` | Why LiveKit is the only path to HD. |
| HANDOVER | `docs/context/HANDOVER_CURRENT_STATE.md` | Sections dated 2026-06-03 through 2026-06-04 covering PCMU, G722, noise source, LiveKit design + impl + latency. |

All tests pass: 5 PCMU + 3 Ogg demuxer = **8/8**.

---

## 3. The "noise on the line" investigation

A 5-step investigation traced the issue from "Cartesia model noise floor" hypothesis to the actual cause: **16-bit quantization in Cartesia's `pcm_s16le` output**.

| Variant | Config | Silence RMS (1.6-2.0s) | Δ vs A |
|---|---|---|---|
| A | 48 k s16le + filter | -21.7 dB | baseline |
| B | 24 k s16le + ffmpeg resample | -16.6 dB | worse (resampling adds artefacts) |
| C | 22.05 k s16le + ffmpeg resample | -17.4 dB | worse |
| **D** | **48 k f32le + filter** | **-40.6 dB** | **-19 dB (winner)** |
| E | 48 k s16le, no filter | -23.7 dB | -2 dB (filter can't fix source noise) |

**Root cause:** Cartesia's synthesis engine uses float32 internally. Converting to signed 16-bit PCM drops 16 bits of dynamic range (96 dB) and surfaces high-frequency model artefacts as audible hiss above 12 kHz. `pcm_f32le` preserves the internal precision; ffmpeg accepts it directly via `-f f32le`.

**Spectrograms confirmed:** Variant A's silence regions above 12 kHz show a blue/purple haze; Variant D is dark.

A round of ffmpeg denoisers (`afftdn`, `anlmdn` with 10x default strength) was tried first but couldn't fix the noise because the noise was baked into the source samples themselves. The fix is in the source, not the filter.

---

## 4. Locked-in spike configuration (production-target)

| Layer | Setting | Rationale |
|---|---|---|
| Cartesia model | `sonic-3.5` | #1 Speech Arena leaderboard (May 2026). |
| Cartesia encoding | **`pcm_f32le`** | Preserves internal float precision; 19 dB quieter silence. |
| Cartesia sample rate | 48000 Hz | Opus native clock; no ffmpeg resample. |
| Cartesia voice | `Julia - Gentle Guide` (`273f9ef7-9fc2-4def-88bb-ab108c6249ca`) | British female, soft & polished. User A/B winner. |
| ffmpeg filter | `highpass=f=80,lowpass=f=12000,anlmdn=s=0.0001:p=0.004:r=0.012` | Polish on top of f32le. |
| Opus bitrate | 64000 bps VBR | "HD voice" sweet spot. |
| Opus application | `audio` | Voice quality > low-bitrate efficiency. |

All configurable via env vars (`SPIKE_CARTESIA_RATE`, `SPIKE_CARTESIA_ENCODING`, `SPIKE_FILTER_CHAIN`, `SPIKE_OPUS_BITRATE`, `SPIKE_OPUS_APPLICATION`, `SPIKE_NO_FILTER`); see `experimental/livekit/publisher/.env.example`.

---

## 5. Latency test (2026-06-04)

Three variants run on fresh LiveKit rooms, twice each, with the same 4-word Porto Douro greeting (4.0-4.3 s of audio). Wall-clock times measured from `time.Now()` at `main()` start.

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

**Numbers are in milliseconds.** All variants are below the typical 4-5 s PSTN answer delay. The Opus path adds ~600 ms over PCMU, of which ~700 ms is fixed-cost (ffmpeg subprocess + OpusHead/OpusTags parsing).

**Bug fixed during this round:** `ffmpegInputFormat` was being passed as `pcm_s16le` / `pcm_f32le` to ffmpeg's `-f` flag; ffmpeg wants `s16le` / `f32le` (no prefix). Now stripped via `strings.TrimPrefix` in `main.go` and `streamCartesiaPCM`.

---

## 6. Safety guarantees — verified

- ✅ Production PCMU runtime on VPS is **untouched** (binary, env, systemd, nginx all unchanged).
- ✅ Production Telnyx webhook, OpenAI Realtime config, Cartesia production config all **unchanged**.
- ✅ All spike work on `feat/livekit-hd-spike` feature branch. Tracked files clean, no dirty state on production main.
- ✅ LiveKit Cloud credentials stored only in `experimental/livekit/.env` on VPS (gitignored, 0600 perms, jorge-only).
- ✅ Cartesia API key in spike `.env` only (gitignored). Production env on the user's local dev machine and on the VPS is unchanged.
- ✅ Rollback is `git checkout main && git branch -D feat/livekit-hd-spike`. Spike `.env` and the 73.6 MB Go install in `$HOME/go` are not tracked by git and would be removed manually.
- ✅ No PSTN changes. No LiveKit SIP trunk added. No two-way conversation built. No production credentials committed.

---

## 7. Open decisions (gated on user / product owner)

| Decision | Options | Recommendation | Impact if chosen |
|---|---|---|---|
| **Production migration** | (a) Keep PCMU only. (b) Add LiveKit as a second path for non-PSTN callers. (c) Full migration to LiveKit + SIP trunk to Telnyx. | (b) — adds WebRTC/Opus path for browser/mobile callers while keeping PSTN on PCMU. Lowest risk. | (a) spike becomes reference doc. (b) requires adding LiveKit room-creation to the session, ~3-5 days. (c) requires LiveKit SIP service, 2-4 weeks. |
| **Latency optimization** | (a) Accept 2.1 s. (b) Cartesia streaming TTS endpoint (if available). (c) Pre-render greeting at call setup. (d) Chunked first-byte streaming. | (a) — 2.1 s is acceptable for a receptionist. | (b) is the right next step if <1 s is required; (c)/(d) are quick wins (~1 day). |
| **Sonic 3.5 vs production aura-2-pandora-en** | (a) Keep production Cartesia model unchanged. (b) Migrate production to sonic-3.5 + pcm_f32le + Julia. | (a) until live regression on production calls confirms sonic-3.5 vs aura-2-pandora-en has no regression. (b) requires Cartesia plan + budget check + live regression call. | (b) requires prod binary rebuild and live test call. |
| **First-frame denoiser** | (a) Keep `anlmdn` filter. (b) Replace with RNNoise (`arnndn`) once a working `.nn` model URL is found. | (a) — f32le made the denoiser mostly redundant. | Cosmetic at this point. |

See `NEXT_STEP_DECISION.md` for the full decision matrix (options A-E) and the recommended next step.

---

## 8. Stop conditions (still in force — do NOT do without explicit approval)

- Do NOT wire LiveKit into production VoxLane.
- Do NOT replace Telnyx.
- Do NOT change Cartesia production config (model, voice, encoding, sample rate).
- Do NOT remove PCMU/Twilio fallbacks.
- Do NOT add LiveKit SIP trunk to Telnyx.
- Do NOT build two-way conversation (Phase 2).
- Do NOT deploy LiveKit to production VPS.
- Do NOT modify production systemd services, nginx, or production `.env`.
- Do NOT merge `feat/livekit-hd-spike` to main.

---

## 9. Key file locations (in this repo and on the VPS)

| File | Purpose |
|---|---|
| `docs/context/HANDOVER_CURRENT_STATE.md` | Project handover, sections dated 2026-06-03 through 2026-06-04 |
| `docs/context/VOICE_QUALITY_STACK_STRATEGY.md` | Why LiveKit is the only path to near-human quality |
| `docs/context/LIVEKIT_HD_SPIKE_PLAN.md` | 16-section spike design |
| `experimental/livekit/README.md` | Spike top-level |
| `experimental/livekit/publisher/main.go` | Publisher (PCMU + Opus, latency-milestone-logged) |
| `experimental/livekit/publisher/cartesia.go` | Cartesia TTS client (4 encodings) |
| `experimental/livekit/publisher/ffmpegopus.go` | Opus encode path (ffmpeg + Ogg demux) |
| `experimental/livekit/publisher/pcmsampleprovider.go` | PCMU encode path (pure Go) |
| `experimental/livekit/publisher/oggdemuxer.go` | Ogg page demuxer |
| `experimental/livekit/publisher/.env.example` | All env vars documented with comments |
| `experimental/livekit/web-client/index.html` | Browser test client |
| `experimental/livekit/results/README.md` | Full spike results |
| `experimental/livekit/results/BROWSER_AUDIO_TEST_RUNBOOK.md` | 7-step browser test procedure |
| `experimental/livekit/results/SPIKE_REPORT.md` | This report |
| `experimental/livekit/results/NEXT_STEP_DECISION.md` | Next-step decision matrix |
| `/opt/ai-voice-receptionist/experimental/livekit/.env` (VPS, 0600, gitignored) | Julia voice, sonic-3.5, pcm_f32le, LiveKit Cloud creds, Cartesia key |
| `/opt/ai-voice-receptionist/experimental/livekit/publisher/publisher-codec.bin` (VPS) | 26.6 MB spike binary (production target = this config) |

---

## 10. Commit log (spike branch, most recent first)

- `8095f9b` feat(spike): latency instrumentation + Opus f32le first-byte ~2.1s
- `d0dfed4` docs(spike): sonic-3.5 optimisation outcome (pcm_f32le, Julia, 19 dB improvement)
- `340c7e4` sonic-3.5 optimisation (pcm_f32le, voice/filter variants)
- `fb5bdd7` docs handover Step 5 noise isolated
- `be4ea70` feat anlmdn denoiser + WAV save
- `c61587f` fix Cartesia HD 48kHz
- `e4084a3` feat wire Cartesia HD PCM
- `f8a0c78` docs Opus path
- `36085c9` feat ffmpeg-backed Opus HD
- `912fce1` docs browser audio test result
- `b588782` docs SESSION_RESTORE_GUIDE E: backup
- `b8eafb2` docs SESSION_RESTORE_GUIDE
- `d638289` docs Go 1.23.4 VPS
- `e7f3c53` docs VPS sync
- `8ff2f3c` docs browser test runbook
- `89e5c91` feat wait-for-subscriber
- `3da7dca` feat PCMU one-way proof
- `e0be779` docs spike design
- `32f9ccb` docs LiveKit spike design
- `d081cce` docs voice quality stack
- `8c31bc6` docs runtime cleanup
- `17866d8` docs noise source
- `f48b869` docs G722 test
- `1bf8422` feat natural booking flow (production; do not change)

All commits on `feat/livekit-hd-spike`, all pushed to `origin`. Working tree on spike files is clean. Production main is at `1bf8422`.

---

## 11. Risk register

| Risk | Likelihood | Impact | Mitigation |
|---|---|---|---|
| Sonic 3.5 misbehaves on production Cartesia plan | Low | Medium | Live regression call on production before any migration. |
| LiveKit Cloud free tier throttles | Medium | Low | Free tier is enough for spike. For production: self-host or pay tier. |
| Two-way conversation introduces echo / mic feedback | High (if attempted) | High | Out of scope for spike. Address in a separate spike with acoustic echo cancellation. |
| PCMU fallback broken after spike work | Very low | Critical | Spike `.env` is gitignored; production env on VPS is untouched. Verified `sha256 24052C82…0CBAFE` matches before spike. |
| Browser-side audio broken after LiveKit SDK upgrades | Low | Medium | Pin `livekit-client@2.5.7` via CDN. Don't auto-upgrade. |
| Cartesia API rate limit on production calls | Low | High | Out of scope; production already uses Cartesia. |
| `pcm_f32le` adds noticeable CPU on Cartesia | Very low | Very low | Cartesia-side, not our side. |

---

## 12. What I would do next (if asked)

1. **Live regression call on production** to confirm sonic-3.5 + pcm_f32le + Julia does not regress PCMU call quality. This is the only thing missing before recommending (b) "add LiveKit as a second path".
2. **Browser-side two-way conversation spike** — see "Status update 2026-06-04" below; this spike is **code-complete on the worker side and awaiting a manual browser user test** to close out Stage 1 + Stage 2.
3. **LiveKit SIP trunk** evaluation (out of scope, gated on product decision to migrate PSTN callers).

See `NEXT_STEP_DECISION.md` for the full decision matrix and the recommended single next step.

---

## Status update 2026-06-04 — Two-way pipeline code-complete

After the spike consolidation commits, the two-way conversation worker and browser client have been built and the pipeline has been **proven end-to-end on the VPS** (commit `5fed0b5`).

### What was built

`experimental/livekit/conversation-worker/` (new module, pinned to the same SDK versions as the publisher):
- `worker.go` — `worker` struct, `run()` connects + publishes a pre-empty outbound Opus track, callbacks for `OnTrackSubscribed` / `OnTrackUnsubscribed` / `OnParticipantConnected` / `OnParticipantDisconnected`, `runInboundReader` goroutine per subscribed track, `maybeFireReply` (sync.Once) + `fireReply` switch, `publishOutboundTrack`, `publishTone` (3 s 440 Hz), `publishCartesia` (falls back to tone if `CARTESIA_API_KEY` empty), `publishPCMAsOpus` (ffmpeg → Ogg → provider), `mintToken` (with `Identity` claim so LiveKit Cloud accepts the token).
- `main.go` — `spikeStartTime` + `latencyLog`, env loader (looks in `experimental/livekit/.env`).
- `inbound.go` — `opusSampleBuilder` wrapping `pion/webrtc/v3/pkg/media/samplebuilder.New(200, &codecs.OpusPacket{}, 48000)`; `push` calls `sb.Push(pkt)` then `sb.Pop()`.
- `outbound.go` — `outboundProvider` channel-backed `SampleProvider` (push / close / `NextSample` / `OnBind` / `OnUnbind` / `Close` / `CurrentAudioLevel`).
- `ffmpegopus.go` — `ffmpegProcess` + `startFfmpegOpus` (strips `pcm_` prefix from input format; args: `-hide_banner -loglevel error -f <fmt> -ar <rate> -ac 1 -i pipe:0 [-af <chain>] -c:a libopus -application <app> -b:a <br> -vbr on -compression_level 10 -f opus pipe:1`); `streamPCM` + `writePCMInt16` + `writePCMFloat32`; `kill()`. Stderr is drained in a goroutine.
- `ogg.go` — `oggOpusReader` (OggS page demux + lacing table) + `opusHead` struct + `ParseOpusHead`.
- `cartesia.go` — `Synthesize` (POST `https://api.cartesia.ai/tts/bytes` with `X-API-Key` + `Cartesia-Version: 2024-06-01`); `decodeCartesiaPCM` for `pcm_s16le` / `pcm_f32le` / `pcm_mulaw` / `pcm_alaw`; `mulawToLinear` + `alawToLinear`.
- `tone.go` — `generateTone(sampleRate, channels, freq, seconds)` returns mono int16 PCM (amplitude 0.3 × 32767).
- `go.mod` — `livekit/server-sdk-go v1.0.16`, `livekit/protocol v1.9.5`, `pion/webrtc/v3 v3.2.44`, `pion/rtp v1.8.5`, `joho/godotenv v1.5.1`.
- `.env.example` — LiveKit creds, Cartesia creds (Julia voice, `sonic-3.5`, `pcm_f32le`, 48 kHz), Opus encoder (64 kbit/s, `audio` application), `FILTER_CHAIN`, `REPLY_MODE` (`none` / `tone_on_first_frame` / `fixed_on_first_frame`), `REPLY_TEXT`, `WORKER_ROOM` (`voxlane-conv-spike`), `WORKER_IDENTITY` (`voxlane-conv-worker`).
- `.gitignore` — excludes binaries and `.env`.

`experimental/livekit/web-client/two-way.html` (new; the one-way `index.html` is untouched):
- Uses `livekit-client@2.5.7` from a CDN.
- 4-section UI: Connect (URL + token), Microphone (`createLocalAudioTrack` + `publishTrack` with `echoCancellation: false`, `noiseSuppression: false`, `autoGainControl: false`), Alex remote audio (`<audio autoplay playsinline>` + 32-bar `AnalyserNode` VU meter), Log (event log).
- 4 state pills: mic on/off, published / not, alex track / no, alex audio level.

### VPS proof (commit `5fed0b5`)

Test script: `run_twoway_test.sh` — runs the conversation-worker in the background while the spike publisher simulates a "browser mic" by publishing one 5 s Cartesia greeting into the same room.

Key events from the run (2026-06-04 18:16:34 → 18:16:52):

| t (worker-relative) | event | meaning |
|---|---|---|
| +  302 ms | `room_connected` | worker joined `voxlane-conv-test-shared` |
| +  322 ms | `outbound_track_published` | worker's empty reply track is now visible to browsers / other participants |
| +   ~4 s | `participant connected: voxlane-conv-test-mic` | publisher (mic sim) joined |
| + 6814 ms | `inbound_track_subscribed` | LiveKit told the worker about the publisher's track |
| + 6837 ms | `first_inbound_frame` (255 B Opus) | the first decoded 20 ms frame from the publisher arrived |
| + 6837 ms | `outbound_reply_start: mode=tone_on_first_frame` | `sync.Once` fired the reply |
| + 6837 ms | `outbound_tone: freq=440Hz duration=3s` | 3 s 440 Hz test tone scheduled |
| + 6838 ms | `ffmpeg_started=true pid=2590523` | PCM → Ogg Opus encoder running |
| +   ~+1 s | `opus_header` parsed | OggOpusReader consumed `OpusHead` |
| + 6.8 s → 12.0 s | 300+ `inbound_metric` frames (50 frames/s × 20 ms) | publisher's track was streaming cleanly at the expected rate |
| + 12.0 s | `track unsubscribed` + `participant disconnected` | publisher finished its 5 s greeting and disconnected |
| + 12.0 s | `inbound_read_rtp: err=EOF` + `inbound_reader_exit` | worker handled the disconnect cleanly |
| + ~18 s | `worker: signal terminated` | SIGTERM from the test script; clean shutdown |

The reply tone was successfully published to the room; in this run there was no third participant to hear it, but the publish pipeline (ffmpeg → OggOpusReader → outboundProvider → LiveKit writer) ran to completion with no errors.

### What still needs to happen

- ~~**Manual browser user test**~~ — **DONE** on 2026-06-04 23:38. A real user ran the two-way browser test from a Chrome tab and heard the 3 s 440 Hz test tone (Cartesia test-account credits were exhausted at test time, so the worker fell back to tone as designed). VU meter on the page lit up in sync with the tone. Pipeline: Chrome `getUserMedia` → LiveKit Cloud → server SDK `OnTrackSubscribed` → samplebuilder → first-frame trigger → ffmpeg → OggOpusReader → outboundProvider → LiveKit writer → Chrome `<audio>` element. **Two-way loop proven with a real human in the loop.**

  Two bugs had to be fixed before this worked (commit `477b0b5`):
  1. `publishOutboundTrack` called `StartWrite` before `PublishTrack`. The LiveKit server SDK (`localsampletrack.go:240`) only spawns the `writeWorker` goroutine when the track is already bound to a peer connection, which only happens once `PublishTrack` has run. Calling `StartWrite` first set `s.provider = provider` but never spawned the consumer, so every Opus frame was silently dropped on the way to the RTP egress. The fix swaps the two calls (matches the order the spike publisher already uses).
  2. `ogg.go` `NextOpusPacket` had an infinite loop. After returning the first packet from a page, the next call found `packet == nil` and `pageBuf != nil`, so the for-loop's two guards were both false and the function spun forever. Replaced with a copy of the publisher's `oggdemuxer.go` (one Opus packet per segment, `segIdx`-driven).

- **Stage 3 (OpenAI Realtime)** is still a separate follow-up spike and was not touched in this work.

---

*End of report.*
