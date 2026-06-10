// src/vendors/tts/elevenlabs.js — ElevenLabs TTS adapter.
//
// STATUS: SKELETON. The user approved including ElevenLabs in the
// spike for "curiosity" — they want to A/B test Eve against an
// ElevenLabs British voice to decide which one feels more human.
//
// ElevenLabs offers:
//   - Streaming TTS with ~200-300ms first-byte latency
//   - Voice library (Bella, Rachel, Charlotte, etc.) + voice cloning
//   - Multilingual v2 model (best quality)
//   - Eleven Turbo v2.5 (fastest, lower cost)
//
// Reference: https://elevenlabs.io/docs/api-reference/text-to-speech
//
// When the user gives the go-ahead:
//   1. npm install elevenlabs (or use fetch() directly)
//   2. Set ELEVENLABS_API_KEY + a voice_id in .env
//   3. Implement synthesize() (one-shot) and stream() (chunked)
//   4. Test with a sample prompt; compare voice quality to Eve
//
// Voice candidates for a British restaurant receptionist:
//   - Charlotte (British, warm, professional)        voice_id: XB0fDUnXU5powFXDhCwa
//   - Alice (British, conversational, British accent) voice_id: Xb7hH8MSUJpSbSDYk0k2
//   - Bella (American, soft, intimate)              voice_id: EXAVITQu4vr4xnSDxMaL
//   - Custom voice clone of Eve (out of scope for this spike)

import type { TtsRequest, TtsAudioChunk, TtsVendor, VendorName } from '../contracts.ts';

const VENDOR: VendorName = 'elevenlabs';
const ELEVENLABS_API_URL = 'https://api.elevenlabs.io/v1/text-to-speech';
const DEFAULT_MODEL = 'eleven_turbo_v2_5';

interface ElevenLabsConfig {
  apiKey: string;
  voiceId?: string;       // default to Charlotte (British, professional)
  model?: string;
  timeout_ms?: number;
  /** Voice settings forwarded to the API. */
  voice_settings?: {
    stability?: number;          // 0-1, default 0.5
    similarity_boost?: number;   // 0-1, default 0.75
    style?: number;              // 0-1, default 0.0
    use_speaker_boost?: boolean; // default true
  };
}

export class ElevenLabsTts implements TtsVendor {
  readonly name: VendorName = VENDOR;
  private cfg: Required<ElevenLabsConfig>;

  constructor(cfg: ElevenLabsConfig) {
    this.cfg = {
      apiKey: cfg.apiKey,
      voiceId: cfg.voiceId || 'XB0fDUnXU5powFXDhCwa',  // Charlotte
      model: cfg.model || DEFAULT_MODEL,
      timeout_ms: cfg.timeout_ms ?? 15000,
      voice_settings: {
        stability: cfg.voice_settings?.stability ?? 0.5,
        similarity_boost: cfg.voice_settings?.similarity_boost ?? 0.75,
        style: cfg.voice_settings?.style ?? 0.0,
        use_speaker_boost: cfg.voice_settings?.use_speaker_boost ?? true,
      },
    };
    if (!this.cfg.apiKey) {
      throw new Error('ElevenLabsTts: apiKey is required');
    }
  }

  async synthesize(req: TtsRequest): Promise<Buffer> {
    // TODO: implement when user gives go-ahead.
    // Sketch:
    //   const url = `${ELEVENLABS_API_URL}/${this.cfg.voiceId}`;
    //   const body = {
    //     text: req.text,
    //     model_id: this.cfg.model,
    //     voice_settings: this.cfg.voice_settings,
    //   };
    //   const res = await fetch(url, {
    //     method: 'POST',
    //     headers: {
    //       'xi-api-key': this.cfg.apiKey,
    //       'Content-Type': 'application/json',
    //       'Accept': 'audio/mpeg',
    //     },
    //     body: JSON.stringify(body),
    //   });
    //   const arrayBuf = await res.arrayBuffer();
    //   return Buffer.from(arrayBuf);
    // For pcm16_24k output, set ?output_format=pcm_24000 query param.
    throw new Error('ElevenLabsTts.synthesize: not yet implemented. Set ELEVENLABS_API_KEY and fill in.');
  }

  async *stream(req: TtsRequest): AsyncIterable<TtsAudioChunk> {
    // TODO: implement streaming TTS.
    // Use /v1/text-to-speech/{voice_id}/stream with Accept: text/event-stream.
    // Parse SSE chunks; each chunk is a base64-encoded audio delta.
    // For pcm16_24k: ?output_format=pcm_24000
    throw new Error('ElevenLabsTts.stream: not yet implemented.');
  }

  async close(): Promise<void> {
    // No-op; HTTP-only.
  }
}

// --- Cost notes (verified at ElevenLabs pricing as of 2026-06) ---
//
//   Free tier: 10,000 chars/month, attribution required
//   Starter:   $5/mo, 30,000 chars
//   Creator:   $22/mo, 100,000 chars
//   Pro:       $99/mo, 500,000 chars
//   Scale:     $330/mo, 2,000,000 chars
//
//   Per 1K characters (pay-as-you-go on top of subscription):
//     Multilingual v2: $0.18
//     Turbo v2.5:      $0.15
//     Flash v2.5:      $0.10
//
//   A 4-min booking call uses roughly 5K chars (Eve's a chatty one):
//     Turbo v2.5:      $0.75/call   =  ~$1,125/mo at 50 calls/day
//     Multilingual v2: $0.90/call   =  ~$1,350/mo
//
//   This is MUCH more expensive than xAI Eve. We're including it for
//   "curiosity" — the user wants to A/B test the voice quality. If
//   ElevenLabs's voice isn't dramatically better than Eve, we should
//   stay on the xAI bundle.
