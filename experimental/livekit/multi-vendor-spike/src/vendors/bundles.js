// src/vendors/bundles.js — vendor bundle factory.
//
// A "bundle" is a complete voice pipeline configuration: STT + LLM + TTS
// + transport + memory. The user picks a bundle, the orchestrator wires
// them together. Swapping bundles is one config change.
//
// The user has approved building the following bundles for the spike:
//   - 'xai-bundle'        : xAI Voice Agent WSS for everything
//                            (the current spike; baseline for comparison)
//   - 'hybrid-deepgram'   : Deepgram STT + xAI Grok LLM + xAI TTS (or
//                            fallback to bundle) + Redis memory
//                            (biggest latency win)
//   - 'hybrid-elevenlabs' : Deepgram STT + xAI Grok LLM + ElevenLabs TTS
//                            (for the "curiosity" voice A/B test)
//   - 'matrix'            : per-call dynamic decision (best of all
//                            vendors per metric, with failover)
//
// STATUS: SKELETON. The factory functions throw until the vendor
// adapters are implemented and the API keys are set. The user has
// approved the scaffold; live use requires the go-ahead.

import type { VendorBundle } from './contracts.ts';

export type BundleName = 'xai-bundle' | 'hybrid-deepgram' | 'hybrid-elevenlabs' | 'matrix';

export interface BundleOptions {
  // API keys (read from .env by the adapters)
  xai_api_key?: string;
  deepgram_api_key?: string;
  elevenlabs_api_key?: string;
  cerebras_api_key?: string;
  redis_url?: string;
  livekit_url?: string;
  livekit_api_key?: string;
  livekit_api_secret?: string;

  // Tenant
  tenant_id?: string;

  // Rehearsal fixtures
  rehearsal_file?: string;
  rehearsal_format?: 'pcmu_8k' | 'pcm16_24k';
}

export async function makeBundle(name: BundleName, opts: BundleOptions = {}): Promise<VendorBundle> {
  switch (name) {
    case 'xai-bundle':
      return await makeXaiBundle(opts);
    case 'hybrid-deepgram':
      return await makeHybridDeepgramBundle(opts);
    case 'hybrid-elevenlabs':
      return await makeHybridElevenLabsBundle(opts);
    case 'matrix':
      return await makeMatrixBundle(opts);
    default:
      throw new Error(`Unknown bundle: ${name}`);
  }
}

// --- xAI bundle (current spike) ---

async function makeXaiBundle(opts: BundleOptions): Promise<VendorBundle> {
  const { XaiBundledStt } = await import('./stt/xai.js');
  const { XaiEveTts } = await import('./tts/xai-eve.js');
  const { FileLoopbackTransport } = await import('./transport/file-loopback.js');
  const { RedisMemMemory } = await import('./memory/redis-mem.js');

  // xAI bundle uses xAI's Voice Agent WSS for STT+LLM+TTS+VAD. The LLM
  // is the bundle's internal one; we represent it as a passthrough
  // vendor for the orchestrator's contract.
  return {
    name: 'xai-bundle',
    description: 'xAI Voice Agent WSS (grok-voice-latest + Eve). Baseline.',
    stt: new XaiBundledStt(),
    // The LLM in the xAI bundle is bound to the WSS session; we wrap
    // it as a no-op vendor here. The orchestrator's LLM calls go to
    // the WSS via response.create + function_call_output.
    llm: {
      name: 'xai-bundle-wss',
      complete: async () => { throw new Error('xAI bundle LLM is bound to the WSS; use xai-client.js'); },
      stream: async function* () { /* no-op */ },
      close: async () => {},
    } as any,
    tts: new XaiEveTts(),
    transport: new FileLoopbackTransport({
      file_path: opts.rehearsal_file || 'fixtures/rehearsal/t02-book.pcmu',
      format: opts.rehearsal_format || 'pcmu_8k',
    }),
    memory: new RedisMemMemory(),
    cost_per_min_usd: 0.05,  // $3.00/hr ÷ 60 min
    expected_first_audio_ms: { p50: 1800, p95: 2200 },
  };
}

// --- Hybrid: Deepgram STT + xAI Grok LLM + xAI TTS (via bundle) ---

async function makeHybridDeepgramBundle(opts: BundleOptions): Promise<VendorBundle> {
  if (!opts.deepgram_api_key) throw new Error('hybrid-deepgram: deepgram_api_key required');
  if (!opts.xai_api_key) throw new Error('hybrid-deepgram: xai_api_key required');

  const { DeepgramStt } = await import('./stt/deepgram.js');
  const { XaiGrokLlm } = await import('./llm/xai-grok.js');
  const { XaiEveTts } = await import('./tts/xai-eve.js');
  const { FileLoopbackTransport } = await import('./transport/file-loopback.js');
  const { RedisMemMemory } = await import('./memory/redis-mem.js');

  return {
    name: 'hybrid-deepgram',
    description: 'Deepgram STT (250ms endpointing) + xAI Grok LLM + xAI TTS (best effort) + Redis. Targets < 1.0s first-audio.',
    stt: new DeepgramStt({ apiKey: opts.deepgram_api_key, endpointing_ms: 250 }),
    llm: new XaiGrokLlm({ apiKey: opts.xai_api_key }),
    tts: new XaiEveTts(),
    transport: new FileLoopbackTransport({
      file_path: opts.rehearsal_file || 'fixtures/rehearsal/t02-book.pcmu',
      format: opts.rehearsal_format || 'pcmu_8k',
    }),
    memory: new RedisMemMemory(),
    cost_per_min_usd: 0.05,  // Deepgram + Grok; TTS via xAI bundle still bundled
    expected_first_audio_ms: { p50: 800, p95: 1100 },
  };
}

// --- Hybrid: Deepgram STT + xAI Grok LLM + ElevenLabs TTS ---

async function makeHybridElevenLabsBundle(opts: BundleOptions): Promise<VendorBundle> {
  if (!opts.deepgram_api_key) throw new Error('hybrid-elevenlabs: deepgram_api_key required');
  if (!opts.xai_api_key) throw new Error('hybrid-elevenlabs: xai_api_key required');
  if (!opts.elevenlabs_api_key) throw new Error('hybrid-elevenlabs: elevenlabs_api_key required');

  const { DeepgramStt } = await import('./stt/deepgram.js');
  const { XaiGrokLlm } = await import('./llm/xai-grok.js');
  const { ElevenLabsTts } = await import('./tts/elevenlabs.js');
  const { FileLoopbackTransport } = await import('./transport/file-loopback.js');
  const { RedisMemMemory } = await import('./memory/redis-mem.js');

  return {
    name: 'hybrid-elevenlabs',
    description: 'Deepgram STT + xAI Grok LLM + ElevenLabs TTS (Charlotte) + Redis. Curiosity A/B vs Eve.',
    stt: new DeepgramStt({ apiKey: opts.deepgram_api_key, endpointing_ms: 250 }),
    llm: new XaiGrokLlm({ apiKey: opts.xai_api_key }),
    tts: new ElevenLabsTts({ apiKey: opts.elevenlabs_api_key }),
    transport: new FileLoopbackTransport({
      file_path: opts.rehearsal_file || 'fixtures/rehearsal/t02-book.pcmu',
      format: opts.rehearsal_format || 'pcmu_8k',
    }),
    memory: new RedisMemMemory(),
    cost_per_min_usd: 0.25,  // 5x more expensive than xAI bundle
    expected_first_audio_ms: { p50: 700, p95: 1000 },
  };
}

// --- Matrix: per-call dynamic decision ---

async function makeMatrixBundle(opts: BundleOptions): Promise<VendorBundle> {
  // Picks the cheapest viable bundle for the current call based on
  // historical latency, error rate, and current vendor health.
  // For the spike, this is a thin wrapper that does nothing extra
  // over the hybrid-deepgram bundle.
  return await makeHybridDeepgramBundle(opts);
}
