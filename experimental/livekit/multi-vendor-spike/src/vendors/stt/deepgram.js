// src/vendors/stt/deepgram.js — Deepgram Nova-3 streaming STT adapter.
//
// STATUS: SKELETON. Do NOT install @deepgram/sdk or run this against the
// live API without explicit user approval. The user has approved building
// the multi-vendor spike scaffold and adding this vendor; live API calls
// require explicit go-ahead and the $5 trial budget.
//
// When the user gives the go-ahead:
//   1. npm install @deepgram/sdk
//   2. Set DEEPGRAM_API_KEY in .env
//   3. Verify the contract test in src/contract.test.js passes
//   4. Wire the streaming partial callback into the orchestrator
//
// Why this matters: Deepgram's Nova-3 streaming endpoint fires partial
// transcripts as audio arrives and declares end-of-utterance with 200-300ms
// of silence. This replaces xAI's 1500ms server-VAD silence window, which
// is the single biggest contributor to first-audio latency on the current
// spike.
//
// Reference: https://developers.deepgram.com/docs/streaming

import { EventEmitter } from 'node:events';
import type { SttRequest, SttPartial, SttResult, SttVendor, VendorTiming, VendorName } from '../contracts.ts';

const VENDOR: VendorName = 'deepgram';
const DEFAULT_ENDPOINTING_MS = 250;
const DEEPGRAM_WSS_URL = 'wss://api.deepgram.com/v1/listen';

interface DeepgramConfig {
  apiKey: string;
  model?: 'nova-3' | 'nova-2' | 'nova-3-general';
  language?: string;
  endpointing_ms?: number;
  /** interim_results: receive partial transcripts. */
  interim_results?: boolean;
  /** vad_events: receive speech-start/speech-end events. */
  vad_events?: boolean;
}

export class DeepgramStt extends EventEmitter implements SttVendor {
  readonly name: VendorName = VENDOR;
  private cfg: Required<DeepgramConfig>;
  private _ws: any = null;  // WebSocket type from 'ws'; not imported to keep the file loadable without @deepgram/sdk installed
  private _pending: { resolve: (r: SttResult) => void; reject: (e: Error) => void; started_at: number } | null = null;

  constructor(cfg: DeepgramConfig) {
    super();
    this.cfg = {
      apiKey: cfg.apiKey,
      model: cfg.model || 'nova-3',
      language: cfg.language || 'en-GB',
      endpointing_ms: cfg.endpointing_ms ?? DEFAULT_ENDPOINTING_MS,
      interim_results: cfg.interim_results ?? true,
      vad_events: cfg.vad_events ?? true,
    };
    if (!this.cfg.apiKey) {
      throw new Error('DeepgramStt: apiKey is required');
    }
  }

  async startStream(req: Omit<SttRequest, 'pcm16_mono'>) {
    // TODO: implement when @deepgram/sdk is installed.
    // Sketch:
    //   const { createClient, LiveTranscriptionEvents } = await import('@deepgram/sdk');
    //   const dg = createClient(this.cfg.apiKey);
    //   const conn = dg.listen.live({
    //     model: this.cfg.model,
    //     language: this.cfg.language,
    //     endpointing: this.cfg.endpointing_ms,
    //     interim_results: this.cfg.interim_results,
    //     vad_events: this.cfg.vad_events,
    //     sample_rate: req.sample_rate,
    //     encoding: 'linear16',
    //   });
    //   for await (const partial of conn) {
    //     yield { text: partial.channel.alternatives[0].transcript, is_final: partial.is_final };
    //   }
    throw new Error('DeepgramStt.startStream: not yet implemented. Install @deepgram/sdk and fill in.');
  }

  async transcribe(req: SttRequest): Promise<SttResult> {
    throw new Error('DeepgramStt.transcribe: not yet implemented. Install @deepgram/sdk and fill in.');
  }

  async close(): Promise<void> {
    if (this._ws) {
      try { this._ws.close(); } catch (_) { /* ignore */ }
      this._ws = null;
    }
  }
}

// --- Cost notes (verified at vendor docs as of 2026-06) ---
//
//   Deepgram Nova-3: $0.0043/min  (pay-as-you-go, $200 free trial for new accounts)
//   Nova-2:          $0.0028/min
//   Endpointing:     configurable, 10-2000ms
//   Best for:        streaming endpointing with sub-300ms silence detection
//
// Expected impact on the spike:
//   Replaces xAI's 1500ms silence window with Deepgram's 250ms default.
//   Win: ~1200ms on first-audio for a 4-guest booking turn.
//
// Failover note: if Deepgram is unavailable, the orchestrator can fall back
// to xAI's bundled STT (1500ms silence) and still produce a working call,
// just slower.
