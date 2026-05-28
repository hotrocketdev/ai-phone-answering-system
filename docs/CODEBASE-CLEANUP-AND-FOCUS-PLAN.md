# Codebase Cleanup And Focus Plan

Date: 2026-05-28  
Project: AI Voice Receptionist / VoxLane

## Problem

The codebase accumulated too many experimental paths during live debugging:

- Twilio Media Streams
- OpenAI native voice
- OpenAI text + Cartesia
- Deepgram runtime
- Cartesia direct greeting
- test tone paths
- multiple audio conversion paths
- old prompt/config examples

This caused workers to become confused and introduced regressions.

## Principle

Keep one stable production path and one experimental branch at a time.

## Current Stable Path

```text
VOICE_RUNTIME=custom
VOICE_RENDERER=cartesia
```

Meaning:

```text
Twilio
→ Go Gateway
→ OpenAI Realtime conversation
→ Cartesia Sonic 3.5 voice
→ Twilio
```

## Keep For Now

- Twilio path
- OpenAI Realtime brain
- Cartesia Sonic 3.5 renderer
- direct Cartesia debug greeting
- safe config logging
- frame pacing diagnostics

## Archive / Remove Later

Only after current MVP is stable:

- Deepgram runtime
- OpenAI native audio playback
- unused renderer branches
- duplicate manual turn detection
- old conversion pipelines
- stale `.env.example` values
- old “Bella Roma” prompt
- dead debug scripts

## Do Not Remove Yet

Do not remove anything until:
- current Cartesia path is documented
- tests pass
- a fallback path exists
- HD audio spike branch is separate

## Cleanup Phases

### Phase 1 — Lock Current Runtime

Document and enforce:

```env
VOICE_RUNTIME=custom
VOICE_RENDERER=cartesia
CARTESIA_MODEL=sonic-3.5
```

### Phase 2 — Test Coverage

Add tests for:
- Cartesia JSON `data` parsing
- µ-law direct output
- 160-byte frame splitting
- no OpenAI audio leakage
- no duplicate manual turn response
- env loading from `.env`

### Phase 3 — Archive Experiments

Move experimental provider code behind clear build/runtime boundaries.

### Phase 4 — Create HD Audio Spike

Create:

```text
spike/livekit-hd-audio
```

Do not mix it into current MVP branch until proven.

## Worker Instruction

Future workers must not reopen provider debates unless explicitly assigned.

Default instruction:

```text
Do not change provider architecture.
Current production path is custom + Cartesia.
Fix only the assigned boundary.
```
