// src/vendors/llm/xai-grok.js — xAI Grok (text) LLM adapter.
//
// STATUS: SKELETON. xAI's Voice Agent bundles the LLM with STT/TTS/VAD
// in a single WSS. To use xAI as a text-only LLM in a multi-vendor
// pipeline, we'd call the xAI REST API (api.x.ai/v1/chat/completions)
// with the conversation text. This is the same model the bundle uses
// but invoked directly.
//
// Reference: https://docs.x.ai/docs/overview
//
// When the user gives the go-ahead for the multi-vendor spike:
//   1. No new SDK needed; use fetch() against api.x.ai/v1/chat/completions
//   2. Set XAI_API_KEY in .env
//   3. The LLM vendor becomes a thin wrapper over fetch()

const VENDOR = 'xai-grok';
const XAI_API_URL = 'https://api.x.ai/v1/chat/completions';
const DEFAULT_MODEL = 'grok-4-fast-non-reasoning';  // fastest text model; bundle uses grok-voice-latest
// Alternative: grok-4-1-fast-non-reasoning (faster, less context), grok-3-mini (cheap fallback)

/**
 * @typedef {Object} XaiGrokConfig
 * @property {string} apiKey
 * @property {string} [model]
 * @property {number} [timeout_ms]
 */

export class XaiGrokLlm {
  /** @type {string} */
  name = VENDOR;
  /** @type {Required<XaiGrokConfig>} */
  cfg;

  /** @param {XaiGrokConfig} cfg */
  constructor(cfg) {
    this.cfg = {
      apiKey: cfg.apiKey,
      model: cfg.model || DEFAULT_MODEL,
      timeout_ms: cfg.timeout_ms ?? 15000,
    };
    if (!this.cfg.apiKey) {
      throw new Error('XaiGrokLlm: apiKey is required');
    }
  }

  /** @param {import('../contracts.ts').LlmRequest} req */
  async complete(req) {
    // TODO: implement when user gives go-ahead.
    // Sketch:
    //   const body = {
    //     model: this.cfg.model,
    //     messages: req.messages,
    //     tools: req.tools,
    //     tool_choice: req.tool_choice,
    //     temperature: req.temperature,
    //     max_tokens: req.max_tokens,
    //     stream: false,
    //   };
    //   const t0 = performance.now();
    //   const ctrl = new AbortController();
    //   const timer = setTimeout(() => ctrl.abort(), this.cfg.timeout_ms);
    //   const res = await fetch(XAI_API_URL, {
    //     method: 'POST',
    //     headers: { 'Authorization': `Bearer ${this.cfg.apiKey}`, 'Content-Type': 'application/json' },
    //     body: JSON.stringify(body),
    //     signal: ctrl.signal,
    //   });
    //   clearTimeout(timer);
    //   const data = await res.json();
    //   const choice = data.choices[0];
    //   return {
    //     text: choice.message.content || '',
    //     tool_calls: choice.message.tool_calls || [],
    //     finish_reason: choice.finish_reason,
    //     usage: data.usage,
    //   };
    throw new Error('XaiGrokLlm.complete: not yet implemented. Set XAI_API_KEY and fill in.');
  }

  /** @param {import('../contracts.ts').LlmRequest} req */
  async *stream(req) {
    // TODO: SSE streaming. Use fetch with stream:true + ReadableStream.
    // Yields { type: 'text', text: '...' } tokens, then { type: 'tool_call', tool_call: {...} }.
    throw new Error('XaiGrokLlm.stream: not yet implemented.');
  }

  async close() {
    // No-op; HTTP-only.
  }
}

// --- Cost notes (verified at xAI docs as of 2026-06) ---
//
//   grok-4-fast-non-reasoning:  $0.20/M input, $0.50/M output tokens
//   grok-4-mini-fast:           $0.20/M input, $0.50/M output
//   grok-3-mini:                $0.30/M input, $0.50/M output
//
//   Voice Agent bundle:         $3.00/hr (covers LLM + STT + TTS + VAD)
//
//   A 4-min booking call uses roughly 3,000 LLM tokens in, 1,500 out:
//     grok-4-fast:    $0.0006 + $0.00075 = $0.00135/call  =  ~$10/mo at 50 calls/day
//     grok-3-mini:    $0.0009 + $0.00075 = $0.00165/call  =  ~$12/mo
//     Voice Agent:    ~$0.20/call                            =  ~$225/mo (everything bundled)
//
//   Per-call LLM cost is small either way. The bundle charges for STT and
//   TTS, which is where the cost difference shows up.
