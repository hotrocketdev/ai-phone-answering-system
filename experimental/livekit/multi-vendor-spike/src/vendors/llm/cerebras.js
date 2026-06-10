// src/vendors/llm/cerebras.js — Cerebras inference LLM adapter.
//
// STATUS: SKELETON. Cerebras provides some of the fastest LLM inference
// in the market (~2000 tokens/s on Llama 3.1 70B), which is interesting
// for the latency target but introduces a second vendor for the LLM
// alone. The trade-off is the user is locked into Llama-class models
// (Cerebras hosts Llama 3.1 8B/70B, Qwen 2.5, etc., not Grok).
//
// Why it's in the spike: the user mentioned "axion" (they may have meant
// "Cerebras" or another ultra-fast inference vendor). We're including
// it as a candidate so we can decide whether the latency win justifies
// swapping xAI Grok for a non-xAI model.
//
// Reference: https://inference-docs.cerebras.ai/
//
// When the user gives the go-ahead:
//   1. npm install @cerebras/cerebras-cloud-sdk
//   2. Set CEREBRAS_API_KEY in .env
//   3. Implement complete() and stream() below
//   4. Test with the same 18-step rehearsal

import type { LlmRequest, LlmResult, LlmToken, LlmVendor, VendorName } from '../contracts.ts';

const VENDOR: VendorName = 'cerebras';
const CEREBRAS_API_URL = 'https://api.cerebras.ai/v1/chat/completions';
const DEFAULT_MODEL = 'llama-3.3-70b';  // fastest + smartest Cerebras model

interface CerebrasConfig {
  apiKey: string;
  model?: string;
  timeout_ms?: number;
}

export class CerebrasLlm implements LlmVendor {
  readonly name: VendorName = VENDOR;
  private cfg: Required<CerebrasConfig>;

  constructor(cfg: CerebrasConfig) {
    this.cfg = {
      apiKey: cfg.apiKey,
      model: cfg.model || DEFAULT_MODEL,
      timeout_ms: cfg.timeout_ms ?? 15000,
    };
    if (!this.cfg.apiKey) {
      throw new Error('CerebrasLlm: apiKey is required');
    }
  }

  async complete(req: LlmRequest): Promise<LlmResult> {
    // TODO: implement when user gives go-ahead.
    // Cerebras uses an OpenAI-compatible API surface, so the body shape
    // matches xai-grok.js's complete() almost exactly.
    throw new Error('CerebrasLlm.complete: not yet implemented. Install @cerebras/cerebras-cloud-sdk or use fetch().');
  }

  async *stream(req: LlmRequest): AsyncIterable<LlmToken> {
    throw new Error('CerebrasLlm.stream: not yet implemented.');
  }

  async close(): Promise<void> {
    // No-op; HTTP-only.
  }
}

// --- Cost notes (verified at Cerebras docs as of 2026-06) ---
//
//   Llama 3.3 70B:    $0.60/M input, $0.60/M output
//   Llama 3.1 8B:     $0.10/M input, $0.10/M output
//   Qwen 2.5 32B:     $0.40/M input, $0.40/M output
//
//   Speed: 2000+ tokens/s for 70B (industry-leading)
//
//   For a 4-min booking call (~3000 in, ~1500 out tokens):
//     Llama 3.3 70B:  $0.0018 + $0.0009 = $0.0027/call  =  ~$20/mo
//     Llama 3.1 8B:   $0.0003 + $0.00015 = $0.00045/call =  ~$3/mo
//
//   Trade-off: 70B is on par with grok-4 quality (subjective), but
//   using Cerebras means a non-xAI model handles the reasoning. The
//   voice still comes from xAI TTS or ElevenLabs, so the assistant
//   persona is independent of the LLM vendor.
