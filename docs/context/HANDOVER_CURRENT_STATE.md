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
```
