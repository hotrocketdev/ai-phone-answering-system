# VOXLANE MASTER CONTEXT

Last updated: 2026-05-31

## 1. What VoxLane Is

VoxLane is a multi-tenant SaaS AI Voice Receptionist platform.

It is not a one-off AI receptionist for one restaurant.

The platform is called VoxLane.

Porto Douro Restaurants is the first tenant/customer.

The product must eventually support many different businesses, each with their own phone number, business name, voice, knowledge base, booking provider, opening hours, prompts, and call handling rules.

Example future tenants:

- Porto Douro Restaurants
- Other restaurants
- Dentists
- Hotels
- Salons
- Garages
- Estate agents
- Legal firms
- Medical clinics
- Trades businesses

The core product promise is:

A business gets a natural-sounding AI receptionist that answers calls, understands the caller, handles bookings or enquiries, and sounds professional enough to represent that business.

The voice experience is not a side feature. It is the product.

## 2. Product Goal

The goal is to build a production-quality AI receptionist SaaS with:

- Near-human voice quality
- Low conversational latency
- Natural interruption/barge-in
- Business-specific greetings
- Multi-tenant configuration
- Booking integrations
- Call logging
- Future dashboard for onboarding and settings
- SMS confirmations
- Call transfers
- Analytics
- Possible white-label support

The first commercial/use-case target is restaurants, starting with Porto Douro Restaurants.

## 2.1 Receptionist Prompt Architecture

VoxLane receptionist behaviour is assembled as layered prompt architecture:

```text
VoxLane Core Receptionist
+ Industry Behaviour Pack
+ Tenant Configuration
+ Current Conversation State
= Live System Prompt
```

For Porto Douro this currently means:

```text
VoxLane Core Receptionist
+ Restaurant Behaviour Pack
+ Porto Douro Tenant Configuration
+ Current Conversation State
= Live System Prompt
```

The core layer contains universal receptionist behaviour. The restaurant pack contains restaurant-specific workflows and enquiries. Tenant configuration contains facts such as business name, address, phone, opening hours, parking, live music, manager names, and staff names.

VoxLane must not be implemented as "Restaurant ChatGPT" or as a single Porto Douro prompt.

## 3. Critical Tenant Rule

Never treat Porto Douro as the platform.

Correct model:

```text
VoxLane = SaaS platform
Porto Douro Restaurants = first tenant/customer
```

Tenant-specific information must not be hardcoded into:

- voice gateway core runtime
- telephony provider code
- Cartesia renderer
- OpenAI session logic
- deployment infrastructure
- SaaS platform docs

Tenant-specific information belongs in tenant configuration.

Current MVP tenant configuration is env-based:

```env
TENANT_BUSINESS_NAME=Porto Douro Restaurants
```

Future tenant configuration should move to Redis/database keyed by phone number.

Expected future model:

```text
Phone number
→ tenant
→ business name
→ greeting
→ voice ID
→ booking provider
→ knowledge base
→ opening hours
→ call rules
```

## 4. Current High-Level Architecture

### Working fallback path

This path is confirmed working on the VPS:

```text
Twilio number
→ voice.voxlane.co.uk
→ nginx
→ Go voice gateway
→ OpenAI Realtime
→ Cartesia Sonic 3.5
→ Twilio
→ caller
```

Status:

- Working
- Caller hears Cartesia voice
- Porto Douro tenant name works
- ngrok removed
- VPS public endpoint works

Limitations:

- Twilio Media Streams uses narrowband audio
- Voice quality is not good enough long term
- Replies feel delayed
- This path is now fallback/demo only

### New primary target path

The active migration target is:

```text
Telnyx number
→ voice.voxlane.co.uk
→ nginx
→ Go voice gateway
→ OpenAI Realtime
→ Cartesia Sonic 3.5
→ Telnyx
→ caller
```

Goal:

- Replace Twilio as primary telephony layer
- Improve routing and future codec options
- Keep Twilio only as fallback
- Eventually test better codecs such as G.722 / L16 / Opus

### Future HD path

LiveKit remains future evaluation, not current implementation:

```text
Telnyx/SIP/HD provider
→ LiveKit
→ agent/gateway
→ OpenAI or Grok
→ Cartesia
→ caller
```

LiveKit is not the immediate next step. It is a later spike if Telnyx direct WebSocket does not achieve the required quality.

## 5. Current Infrastructure

Production/dev staging endpoint:

```text
https://voice.voxlane.co.uk
```

The app has been deployed to a VPS.

Known VPS details from recent work:

- VPS provider: Hostinger
- Public IP: 72.62.5.240
- App path: /opt/ai-voice-receptionist
- Reverse proxy: nginx, not Caddy
- Gateway systemd service: voxlane-gateway.service
- Backend systemd service: voxlane-backend.service
- Redis is running
- nginx handles HTTPS and WebSocket upgrade

Important:

ngrok was removed from the live path because it repeatedly caused 502 / WebSocket instability.

Do not reintroduce ngrok as a stable test path.

## 6. Current Code/Stack

### Voice gateway

Language: Go

Role:

- Telephony WebSocket handling
- Provider adapters
- Realtime session orchestration
- OpenAI Realtime integration
- Cartesia renderer integration
- Audio frame routing
- Interruption/barge-in support
- Telnyx/Twilio provider boundary

### Backend

Framework: NestJS / Node.js

Role:

- Webhooks
- Telnyx Call Control REST commands
- Twilio webhook handling
- Tenant config boundary
- Future booking/tool APIs

### Voice renderer

Cartesia Sonic 3.5

Current selected voice is configured by `CARTESIA_VOICE_ID`.

Cartesia is the desired voice provider. Do not randomly switch voice providers.

### Conversation engine

OpenAI Realtime

Current model used in the project:

```text
gpt-realtime-1.5
```

OpenAI is used as the conversation brain, not the final voice.

When Cartesia is active, OpenAI audio must be dropped/ignored.

### Current important env concepts

```env
VOICE_RUNTIME=custom
VOICE_RENDERER=cartesia
TELEPHONY_PROVIDER=twilio|telnyx
TENANT_BUSINESS_NAME=Porto Douro Restaurants
CARTESIA_MODEL=sonic-3.5
OPENAI_REALTIME_MODEL=gpt-realtime-1.5
```

Secrets must never be committed or printed.

## 7. Why We Moved Away From Twilio As Primary

Twilio was useful for proving the concept, but it became clear that Twilio Media Streams is not the long-term quality path.

Problems encountered:

- 8kHz µ-law audio ceiling
- Voice sounds more mechanical/telephone-like than Cartesia playground
- Latency feels high
- Many regressions while trying to optimise within Twilio constraints
- ngrok caused repeated WebSocket failures before VPS deployment

Decision:

```text
Twilio = fallback/demo path
Telnyx = primary migration path
LiveKit = future HD evaluation
```

Do not keep trying to make Twilio the premium production path.

## 8. Why Telnyx Is Current Priority

The user now has a Telnyx number approved:

```text
+44 121 823 0230
```

Telnyx test quality sounded better than the Twilio path.

Telnyx direct WebSocket was chosen before LiveKit because:

- Smaller migration than LiveKit
- Adapter swap is more surgical
- Keeps Go gateway architecture mostly intact
- Lets us test better telephony routing/codecs sooner
- LiveKit remains available later if direct WebSocket is not good enough

## 9. Current Telnyx Status

### Confirmed working

Telnyx inbound call path now reaches the system.

Confirmed:

- `call.initiated` webhook received
- backend parses `call_control_id`
- backend sends Telnyx REST `answer` command
- `call.answered` webhook received
- backend sends `streaming_start` command
- `streaming.started` webhook received
- Telnyx opens WebSocket to gateway
- gateway session starts
- OpenAI runs
- Cartesia runs
- outbound media is generated and written to Telnyx WebSocket

### Still broken

Caller hears silence over Telnyx.

This is the current active blocker.

### Current failure boundary

The failure is now very narrow:

```text
Telnyx accepts call
Telnyx starts streaming
WebSocket opens
Gateway writes outbound media
Caller hears nothing
```

OpenAI and Cartesia are not currently suspected.

The remaining suspected issue is Telnyx outbound playback configuration/contract.

## 10. Telnyx Outbound Audio History

The team tried several Telnyx outbound media formats.

### Attempt 1

Raw binary PCMU frames sent to WebSocket.

Result:

No sound.

### Attempt 2

12-byte RTP header + PCMU payload sent as binary WebSocket frames.

Result:

No sound.

### Attempt 3

Codex verified official Telnyx docs and found the adapter should use WebSocket text JSON media events:

```json
{
  "event": "media",
  "media": {
    "payload": "base64-encoded raw PCMU payload"
  }
}
```

Adapter was changed to send JSON `media.payload` base64 raw PCMU, no RTP header.

Result:

Still no sound.

### Attempt 4

Test tone path added.

OpenAI and Cartesia bypassed.

PCMU tone sent through same JSON media path.

Result:

Still no sound.

### Attempt 5

Tone was initially sent too early, before `streaming.started`.

Worker fixed timing so tone was sent after stream readiness.

Result:

Still no sound.

## 11. Current Most Likely Telnyx Causes

The most likely remaining causes are now:

1. Wrong `stream_bidirectional_target_legs`
2. Wrong `stream_track`
3. Subtle mismatch in Telnyx JSON outbound media contract
4. Telnyx silently discarding media due to stream config
5. Wrong Call Control leg being targeted
6. Need to test `self`, `opposite`, `both`
7. Need to test `inbound_track`, `outbound_track`, `both_tracks`
8. Possibly need to test `mp3` bidirectional mode if RTP mode still silent

Do not move to G.722/L16/Opus until a simple PCMU test tone is audible.

## 12. Current Immediate Next Task

The next task is NOT OpenAI, Cartesia, LiveKit, or codec upgrade.

The next task is:

Run a controlled Telnyx playback configuration matrix.

Test one variable at a time:

### Target legs

Current was likely:

```text
stream_bidirectional_target_legs=opposite
```

Test:

```text
self
both
```

### Stream track

Current:

```text
stream_track=both_tracks
```

Test one at a time:

```text
inbound_track
outbound_track
both_tracks
```

Use only the PCMU test tone until audio is heard.

Success means:

```text
caller hears simple test tone
```

Only after that:

- disable test tone
- test Cartesia over Telnyx
- then test HD codec ladder

## 13. Codec Strategy

Do not jump directly to HD.

Correct ladder:

1. PCMU / G711U / 8kHz
   - compatibility-only
   - prove the pipe
   - not final quality target

2. G722 / 16kHz
   - first real HD target

3. L16 / 16000
   - possible next quality test if supported cleanly

4. Opus
   - later if supported cleanly

5. LiveKit
   - future if direct WebSocket remains limited

Important:

G711U/PCMU is not the final production quality target. It is only a first working proof.

## 14. Files A New AI Must Read First

When starting a new chat, read these in order if present:

```text
docs/context/VOXLANE_MASTER_CONTEXT.md
docs/context/ARCHITECTURAL_DECISIONS.md
docs/context/AI_WORKER_RULES.md
docs/context/HANDOVER_CURRENT_STATE.md
docs/PROJECT_STATUS.md
docs/CURRENT_RUNTIME_LOCK.md
docs/PRODUCTION_RUNTIME_BOUNDARIES.md
docs/TENANT_CONFIGURATION_MVP.md
docs/MULTI_TENANCY_ARCHITECTURE_NOTE.md
docs/TELNYX_DIRECT_WEBSOCKET_ADAPTER.md
docs/TELNYX_OUTBOUND_AUDIO_DEBUG.md
experimental/telnyx/TELNYX_ADAPTER_IMPLEMENTATION_PLAN.md
```

If the context files do not exist, create them from this pack.

## 15. Important Existing Docs

Existing docs created during the project include:

- docs/PUBLIC_ENDPOINT_MIGRATION_PLAN.md
- docs/VPS_CADDY_DEPLOYMENT_CHECKLIST.md
- docs/VPS_DEPLOYMENT_RUNBOOK.md
- docs/WINDOWS_TO_VPS_DEPLOY_NOTES.md
- docs/CURRENT_MVP_STABILITY_CHECKLIST.md
- docs/CURRENT_MVP_LIMITATIONS.md
- docs/CURRENT_RUNTIME_LOCK.md
- docs/TENANT_CONFIGURATION_MVP.md
- docs/MULTI_TENANCY_ARCHITECTURE_NOTE.md
- docs/TELNYX_DIRECT_WEBSOCKET_ADAPTER.md
- docs/TELNYX_OUTBOUND_AUDIO_DEBUG.md
- docs/PRODUCTION_RUNTIME_BOUNDARIES.md
- docs/AGENT_RUNTIME_EVALUATION.md
- docs/CODEBASE-CLEANUP-AND-FOCUS-PLAN.md
- docs/VOICE-STACK-DECISION-MATRIX.md
- docs/NEXTGEN-LIVEKIT-HD-AUDIO-PLAN.md
- docs/PRODUCTION-ROADMAP-VOICE-QUALITY-FIRST.md

## 16. Worker Operating Model

The user prefers worker prompts in the controlled MVP style.

Always include:

```text
Continue using:
- superpowers:subagent-driven-development
- superpowers:executing-plans
```

Typical prompt structure:

```text
Controlled [task] continues.

Current confirmed state:
...

Critical rules:
- do NOT ...
- do NOT ...
- do NOT print secrets

Task:
...

Steps:
1.
2.
3.

Output required:
1.
2.
3.
```

The user wants strong prompts that prevent LLM workers from redesigning the architecture or debugging five things at once.

## 17. Lessons Learned

Important lessons from the project:

- Change one variable at a time.
- Do not assume docs marketing means WebSocket contract works the same way.
- Do not move codecs until the basic audio pipe is proven.
- Do not treat WebSocket writes as success; success is caller hears audio.
- Do not commit secrets.
- Do not trust ngrok for stable voice testing.
- Do not hardcode tenants.
- Do not optimise latency before audio works.
- Do not keep debugging Cartesia when the boundary is Telnyx.
- Do not keep debugging OpenAI when Cartesia/test tone are bypassed.
- Do not remove Twilio fallback until Telnyx is stable.
- Do not implement LiveKit until direct Telnyx has been properly evaluated.
- Always document the exact boundary.

## 18. Current Definition Of Done For Telnyx Stage

The Telnyx stage is not complete until:

- Caller dials +44 121 823 0230
- Call answers
- Telnyx streaming starts
- Gateway WebSocket opens
- Caller hears PCMU test tone
- Caller hears Cartesia voice
- Caller can ask a question
- AI replies naturally
- Voice quality is compared against Twilio
- Then G722/L16/Opus testing begins

## 19. What Not To Do Next

Do not:

- Implement LiveKit
- Change Cartesia model
- Change OpenAI model
- Re-optimise Twilio
- Start ResDiary integration
- Build dashboard
- Add billing
- Change tenants
- Move to G722/L16 before PCMU audio is heard

## 20. Best Next Prompt

The next prompt should say:

Test Telnyx bidirectional playback configuration matrix using PCMU test tone only. Try target legs self and both. Then try stream_track values one at a time. Do not touch OpenAI, Cartesia, codecs, or LiveKit. Success is caller hears tone.
