// src/vendors/tts/xai-eve.js — xAI TTS with Eve (British) voice.
//
// STATUS: PARTIAL. xAI's Voice Agent bundles TTS with the WSS — we
// don't have a separate TTS API endpoint from xAI as of 2026-06.
//
// This adapter represents the "use the Eve voice from the xAI bundle"
// path. To actually use Eve TTS in a multi-vendor pipeline, we have
// two options:
//
//   (a) Use the xAI Voice Agent WSS for the whole pipeline
//       (STT+LLM+TTS+VAD bundled). This is the current spike.
//   (b) Use xAI's TTS through some other surface, if/when xAI exposes one.
//       As of 2026-06, xAI's standalone TTS API is not generally available.
//
// For the multi-vendor spike, the user has approved keeping Eve as the
// voice. The two ways to do that with multi-vendor are:
//
//   1. Use xAI's full bundle (option a). All four components come from xAI.
//      Easy, but doesn't help latency (we keep the 1500ms silence window).
//
//   2. Use Deepgram STT + xAI Grok LLM, then call xAI TTS via the
//      Voice Agent WSS just for the audio output. This is awkward
//      because the bundle wants to handle STT and LLM too — you'd be
//      fighting the API.
//
//   3. Switch the voice to ElevenLabs. Different voice, but 200-300ms
//      streaming TTS, so the latency win is real. The user said "I'm
//      curious to try ElevenLabs just for curiosity" — so we include
//      it as a candidate.

import type { TtsRequest, TtsAudioChunk, TtsVendor, VendorName } from '../contracts.ts';

const VENDOR: VendorName = 'xai-eve';

export class XaiEveTts implements TtsVendor {
  readonly name: VendorName = VENDOR;
  private _voiceId: string;

  constructor(opts: { voice_id?: string } = {}) {
    // xAI's Eve voice identifier. Same as used in the Voice Agent WSS.
    this._voiceId = opts.voice_id || 'eve';
  }

  async synthesize(req: TtsRequest): Promise<Buffer> {
    // xAI does not expose a standalone TTS API as of 2026-06. To use
    // Eve TTS in a multi-vendor pipeline, the only path is through
    // the Voice Agent WSS, which requires also using xAI's STT and LLM.
    //
    // For a head-to-head spike, this means: if you want to keep Eve,
    // you stay on the xAI bundle. The latency win from Deepgram STT
    // cannot be realized without also giving up Eve.
    throw new Error(
      'XaiEveTts.synthesize: xAI does not expose standalone TTS. To use Eve, ' +
      'use the Voice Agent bundle (xai-client.js). To get the Deepgram latency ' +
      'win, switch the voice to ElevenLabs (see elevenlabs.js).'
    );
  }

  async *stream(req: TtsRequest): AsyncIterable<TtsAudioChunk> {
    throw new Error('XaiEveTts.stream: not available standalone. Use Voice Agent bundle.');
  }

  async close(): Promise<void> {
    // No-op.
  }
}

// --- Cost notes ---
//
//   xAI Voice Agent bundle: $3.00/hr (includes Eve TTS)
//   xAI TTS standalone:     not available as of 2026-06
//
//   The cost of "Eve's voice" is the cost of the xAI bundle, not a
//   separate TTS line item. Switching to ElevenLabs means paying for
//   ElevenLabs TTS ($0.18-0.30 per 1K chars) instead.
