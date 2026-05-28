# Next-Generation HD Audio Plan

Date: 2026-05-28  
Project: AI Voice Receptionist / VoxLane

## Objective

Move the AI receptionist from narrowband phone audio to a modern HD voice architecture.

## Current Limitation

The current system forces Cartesia Sonic 3.5 into:

```json
{
  "container": "raw",
  "encoding": "pcm_mulaw",
  "sample_rate": 8000
}
```

This is required for Twilio Media Streams, but it reduces voice quality heavily.

## Target Quality

We want Cartesia closer to playground quality, where audio can be generated as higher-quality PCM/WAV formats.

## Proposed Target Stack

```text
UK Caller
→ SIP provider with wideband support
→ LiveKit SIP / LiveKit Agents
→ AI conversation brain
→ Cartesia Sonic 3.5
→ HD voice back to caller
```

## Candidate Telephony Providers

### Telnyx

Pros:
- HD voice direction
- G.722 and Opus support
- modern AI voice positioning
- good long-term economics

Cons:
- UK number approval friction
- onboarding may slow MVP

### Simwood

Pros:
- UK-native carrier
- SIP-first
- likely good fit for UK local-business telephony

Cons:
- needs technical validation
- integration docs may be less polished than Telnyx

### Vonage / Sinch / DIDWW

Pros:
- possible SIP trunk alternatives
- may be easier to provision UK numbers

Cons:
- needs codec and LiveKit validation
- may be more enterprise-heavy

## LiveKit Spike Goals

Create branch:

```text
spike/livekit-hd-audio
```

### Phase 1 — Research

Confirm:
- inbound SIP setup
- UK number options
- G.722 support
- Opus support
- LiveKit dispatch rules
- Cartesia plugin path
- tool/webhook calling from agent worker

### Phase 2 — Minimal Prototype

Build:
- one inbound call route
- one LiveKit room
- one voice agent
- Cartesia TTS
- simple fixed greeting

Do not integrate bookings yet.

### Phase 3 — Compare Against Current Stack

Compare:
- voice quality
- latency
- interruption handling
- caller experience
- stability
- cost per minute
- complexity

### Phase 4 — Decide

If LiveKit path is clearly better:
- promote to production roadmap
- keep Twilio as fallback
- plan migration by modules

## Success Criteria

The HD audio spike succeeds if:
- caller hears noticeably better voice quality
- latency remains acceptable
- interruption remains natural
- UK number path is commercially viable
- business tools can still be called reliably

## Failure Criteria

The spike fails if:
- UK number provisioning is blocked
- latency is worse than current Twilio path
- complexity becomes too high
- audio still downgrades to narrowband in normal calls

## Recommendation

Do not delay all SaaS work for this, but do not go to production relying only on Twilio Media Streams if premium voice quality is the main differentiator.
