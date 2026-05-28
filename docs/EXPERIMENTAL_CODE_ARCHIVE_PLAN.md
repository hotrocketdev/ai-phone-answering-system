# Experimental Code Archive Plan

**Date**: 2026-05-28  
**Status**: Identification only — no deletions yet per CODEBASE-CLEANUP-AND-FOCUS-PLAN.md

---

## Archive Candidates (Delete AFTER HD path proven)

### Deepgram Runtime
| File | Reason |
|------|--------|
| `internal/runtime/deepgram/agent.go` | Experimental runtime, not production |
| `internal/runtime/runtime.go` | Deepgram-specific interface |
| `cmd/gateway/main.go` `runDeepgramRelay()` | Deepgram relay function |
| `DEEPGRAM_API_KEY` references | Config/env |

### OpenAI Native Audio Playback
| File | Reason |
|------|--------|
| `internal/session/session.go` `runOpenAILoop` audio path | Suppressed when Cartesia active; unused in production |
| `internal/openai/client.go` `handleAudioDelta()` | No longer used in Cartesia mode |

### Stale Prompts / Configs
| File | Reason |
|------|--------|
| `internal/session/sm/state_machine.go` "Bella Roma" references | Default placeholder, not production |
| `internal/session/session.go` `Voice: "marin"` hardcode | Not used in Cartesia mode |
| `.env.example` Deepgram, Telnyx sections | Experimental configs |

### Debug / Test Paths
| File | Reason |
|------|--------|
| `cmd/gateway/main.go` `sendTestTone()` | DEBUG_TWILIO_TEST_TONE diagnostic |
| `cmd/gateway/main.go` `runCartesiaDirectGreeting()` | DEBUG_CARTESIA_DIRECT_GREETING diagnostic |
| `internal/openai/client.go` verbose event logging | Diagnostic logging |
| `internal/renderer/cartesia/renderer.go` firstChunk logging | Diagnostic logging |

### Duplicate / Legacy
| File | Reason |
|------|--------|
| `internal/twilio/handler.go` | Superseded by `internal/provider/twilio/adapter.go` |
| `internal/llm/` Grok scaffold | Not implemented |
| `internal/renderer/` ElevenLabs scaffold | Not implemented |
| `internal/renderer/openai/` native voice | Not used in Cartesia production path |

### Audio Experiments
| File | Reason |
|------|--------|
| `internal/audio/pipeline.go` `ProcessOutboundBytes` buffering | Replaced by remainder buffer in session.go |
| `internal/renderer/cartesia/renderer.go` `ConvertPCM16ToMulaw()` | No longer used; Cartesia outputs native u-law |

## Keep (Production)
| File | Reason |
|------|--------|
| `internal/session/sm/state_machine.go` | Core conversation logic |
| `internal/renderer/cartesia/renderer.go` | Voice rendering |
| `internal/openai/client.go` | Conversation brain |
| `internal/provider/twilio/adapter.go` | Twilio transport |
| `internal/config/config.go` | Runtime configuration |
| `internal/session/session.go` | Session orchestration |
| `backend/src/modules/tools/` | Booking tools |
| `backend/src/modules/voice/` | Twilio webhook |

## Archive Process

1. Tag current commit as `pre-cleanup`
2. Create archive branch `archive/experimental-code`
3. Remove files from main
4. Document removals in CHANGELOG
5. Run tests on main
6. Verify production path still works

**Timing**: Per PRODUCTION-ROADMAP.md Stage 2 — after MVP stabilised, before HD spike implementation.
