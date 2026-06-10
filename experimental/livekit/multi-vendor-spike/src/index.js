// src/index.js — multi-vendor worker entry point.
//
// Wires a vendor bundle into the orchestrator and starts the pipeline.
// The default is the xAI bundle (current spike). Pass --vendor-bundle
// to switch.
//
// Usage:
//   node src/index.js --vendor-bundle xai-bundle
//   node src/index.js --vendor-bundle hybrid-deepgram
//   node src/index.js --vendor-bundle hybrid-elevenlabs
//
// STATUS: SKELETON. The orchestrator + bundle factory are wired; the
// vendor adapters are TODO. To run, the user has to give the go-ahead
// for the vendor SDKs to be installed and the API keys set.

import 'dotenv/config';
import { makeBundle, type BundleName } from './vendors/bundles.js';
import { Orchestrator } from './orchestrator.js';

function parseArgs(): { bundle: BundleName } {
  const args = process.argv.slice(2);
  let bundle: BundleName = 'xai-bundle';
  for (let i = 0; i < args.length; i++) {
    if (args[i] === '--vendor-bundle' && args[i + 1]) {
      bundle = args[i + 1] as BundleName;
      i++;
    }
  }
  return { bundle };
}

async function main() {
  const args = parseArgs();
  console.log(`multi-vendor-spike starting with bundle: ${args.bundle}`);

  const bundle = await makeBundle(args.bundle, {
    xai_api_key: process.env.XAI_API_KEY,
    deepgram_api_key: process.env.DEEPGRAM_API_KEY,
    elevenlabs_api_key: process.env.ELEVENLABS_API_KEY,
    cerebras_api_key: process.env.CEREBRAS_API_KEY,
    redis_url: process.env.REDIS_URL,
    livekit_url: process.env.LIVEKIT_URL,
    livekit_api_key: process.env.LIVEKIT_API_KEY,
    livekit_api_secret: process.env.LIVEKIT_API_SECRET,
  });
  console.log(`Bundle built: ${bundle.name}`);
  console.log(`Expected first-audio: p50=${bundle.expected_first_audio_ms.p50}ms p95=${bundle.expected_first_audio_ms.p95}ms`);

  const orch = new Orchestrator(bundle);
  await orch.start();
  console.log('Orchestrator started. Waiting for transport frames.');

  process.on('SIGINT', async () => { console.log('shutdown'); await orch.stop(); process.exit(0); });
  process.on('SIGTERM', async () => { console.log('shutdown'); await orch.stop(); process.exit(0); });
}

main().catch((e) => { console.error('fatal:', e); process.exit(1); });
