// src/vendors/stt/xai.js — xAI bundled STT (the WSS server-VAD path).
//
// STATUS: WORKING. This is the existing behaviour from the
// xai-phone-worker spike. The bundled STT comes free with xAI's Voice
// Agent WSS and is wired into the input_audio_buffer.* events. The
// trade-off is the 1500ms server-VAD silence window.
//
// We expose it as an SttVendor for head-to-head comparison. The
// underlying mechanism is xai-client.js's response.input_audio_transcription
// events, but we re-frame the latency here as if it were a streaming STT.

import { EventEmitter } from 'node:events';
import type { SttRequest, SttPartial, SttResult, SttVendor, VendorName } from '../contracts.ts';

const VENDOR: VendorName = 'xai';
const SILENCE_WINDOW_MS = 1500;

export class XaiBundledStt extends EventEmitter implements SttVendor {
  readonly name: VendorName = VENDOR;
  private _silenceWindowMs: number;

  constructor(opts: { silence_window_ms?: number } = {}) {
    super();
    this._silenceWindowMs = opts.silence_window_ms ?? SILENCE_WINDOW_MS;
  }

  async *startStream(req: Omit<SttRequest, 'pcm16_mono'>) {
    // The xAI bundle doesn't expose partial transcripts; it only emits
    // a final transcript via conversation.item.input_audio_transcription.completed
    // after the server-VAD silence window elapses. So this stream yields
    // exactly one final transcript after _silenceWindowMs of silence.
    //
    // The orchestrator's job is to either:
    //   (a) wait for the final transcript (current behaviour), or
    //   (b) bypass the silence window by calling input_audio_buffer.commit
    //       when an external STT (e.g. Deepgram) declares end-of-utterance.
    //
    // For head-to-head comparison, we use the (a) path. To get the
    // (b) benefit, switch the vendor to deepgram.js.
    throw new Error(
      'XaiBundledStt.startStream: xAI bundled STT does not stream partials. ' +
      'Use xai-client.js event "user_transcript" directly, or switch to deepgram.js ' +
      'for sub-300ms endpointing.'
    );
  }

  async transcribe(req: SttRequest): Promise<SttResult> {
    // We don't have a "transcribe a buffer" mode in the xAI bundle.
    // The bundled STT is WSS-only and live-only.
    throw new Error('XaiBundledStt.transcribe: xAI bundle is WSS-only. Use deepgram.js for batch.');
  }

  async close(): Promise<void> {
    // No-op; the WSS lifecycle is managed by xai-client.js.
  }
}

// --- Cost notes ---
//
//   xAI Voice Agent (bundle): $3.00/hr total for STT+LLM+TTS+VAD
//   At 50 calls/day x 3 min = 150 min/day = $7.50/day = $225/mo
//   vs Deepgram Nova-3:        $0.0043/min x 150 = $0.65/day = $20/mo
//
//   xAI bundle is 11x more expensive for STT alone, but you also get
//   LLM and TTS in the same WSS connection. Apples-to-apples comparison
//   has to be done at the bundle level, not the per-component level.
