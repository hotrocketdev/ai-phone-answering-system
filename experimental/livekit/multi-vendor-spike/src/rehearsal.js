// src/rehearsal.js — head-to-head rehearsal across vendor bundles.
//
// Compares the xAI baseline against the hybrid bundles. Outputs a
// markdown report with latency numbers, voice quality (subjective
// after listening to the WAVs), and per-vendor cost estimates.
//
// STATUS: SKELETON. The orchestrator + bundle factory are wired; the
// vendor adapters are TODO. To run, the user has to give the go-ahead
// for the vendor SDKs to be installed and the API keys set.
//
// Usage:
//   node src/rehearsal.js --vendor-bundle xai
//   node src/rehearsal.js --vendor-bundle hybrid-deepgram
//   node src/rehearsal.js --vendor-bundle hybrid-elevenlabs
//   node src/rehearsal.js --vendor-bundle compare    (runs all three)
//
// Output:
//   tmp/rehearsal/<bundle>/rehearsal.log
//   tmp/rehearsal/<bundle>/rehearsal-metrics.json
//   tmp/rehearsal/<bundle>/rehearsal-assistant.wav
//   tmp/rehearsal/COMPARE_REPORT.md   (when --vendor-bundle compare)

import 'dotenv/config';
import fs from 'node:fs';
import path from 'node:path';
import { performance } from 'node:perf_hooks';
import { makeBundle, type BundleName } from './vendors/bundles.js';
import { Orchestrator } from './orchestrator.js';

// 18-step rehearsal script (same as xai-phone-worker spike).
const SCRIPT: Array<{ step: number; side: 'caller' | 'wait'; note: string; pcmu?: string }> = [
  { step: 1,  side: 'caller', note: 'Caller says hello', pcmu: 't01-hello' },
  { step: 2,  side: 'wait', note: 'Assistant greets' },
  { step: 3,  side: 'caller', note: 'Caller asks to book a table', pcmu: 't02-book' },
  { step: 4,  side: 'wait', note: 'Assistant acknowledges' },
  { step: 5,  side: 'caller', note: 'Caller says "Tomorrow at seven for four people"', pcmu: 't03-tomorrow-7-4' },
  { step: 6,  side: 'wait', note: 'Assistant calls availability.check' },
  { step: 7,  side: 'caller', note: 'Caller says "George"', pcmu: 't07-george' },
  { step: 8,  side: 'wait', note: 'Assistant asks for phone' },
  { step: 9,  side: 'caller', note: 'Caller says phone number', pcmu: 't09-phone' },
  { step: 10, side: 'wait', note: 'Assistant explains deposit, calls deposit.hold + booking.create' },
  { step: 11, side: 'wait', note: 'Assistant continues after tools' },
  { step: 12, side: 'wait', note: 'Assistant confirms booking' },
  { step: 13, side: 'caller', note: 'Caller changes party size to 6', pcmu: 't14-change-to-6' },
  { step: 14, side: 'wait', note: 'Assistant re-checks availability' },
  { step: 15, side: 'caller', note: 'Caller asks off-script (vegan tasting menu)', pcmu: 't16-off-script' },
  { step: 16, side: 'wait', note: 'Assistant calls manager.escalate' },
  { step: 17, side: 'wait', note: 'Assistant acknowledges escalation' },
  { step: 18, side: 'wait', note: 'Assistant ends call naturally' },
];

function parseArgs(): { bundle: BundleName; file: string; format: 'pcmu_8k' | 'pcm16_24k' } {
  const args = process.argv.slice(2);
  let bundle: BundleName = 'xai-bundle';
  let file = 'fixtures/rehearsal/t02-book.pcmu';
  let format: 'pcmu_8k' | 'pcm16_24k' = 'pcmu_8k';
  for (let i = 0; i < args.length; i++) {
    if (args[i] === '--vendor-bundle' && args[i + 1]) {
      bundle = args[i + 1] as BundleName;
      i++;
    } else if (args[i] === '--file' && args[i + 1]) {
      file = args[i + 1];
      i++;
    } else if (args[i] === '--format' && args[i + 1]) {
      format = args[i + 1] as 'pcmu_8k' | 'pcm16_24k';
      i++;
    }
  }
  return { bundle, file, format };
}

async function runBundle(bundleName: BundleName, opts: { file: string; format: 'pcmu_8k' | 'pcm16_24k' }) {
  const outDir = path.resolve(`./tmp/rehearsal/${bundleName}`);
  fs.mkdirSync(outDir, { recursive: true });
  const log = (...a: any[]) => {
    const line = `${new Date().toISOString()} ${a.map((x) => typeof x === 'string' ? x : JSON.stringify(x)).join(' ')}`;
    console.log(line);
    fs.appendFileSync(path.join(outDir, 'rehearsal.log'), line + '\n');
  };

  fs.writeFileSync(path.join(outDir, 'rehearsal.log'), '');

  log(`=== REHEARSAL START: ${bundleName} ===`);

  let bundle;
  try {
    bundle = await makeBundle(bundleName, {
      xai_api_key: process.env.XAI_API_KEY,
      deepgram_api_key: process.env.DEEPGRAM_API_KEY,
      elevenlabs_api_key: process.env.ELEVENLABS_API_KEY,
      cerebras_api_key: process.env.CEREBRAS_API_KEY,
      redis_url: process.env.REDIS_URL,
      rehearsal_file: opts.file,
      rehearsal_format: opts.format,
    });
  } catch (e: any) {
    log(`BUNDLE_BUILD_FAILED: ${e.message}`);
    fs.writeFileSync(path.join(outDir, 'rehearsal-metrics.json'), JSON.stringify({
      bundle: bundleName,
      error: e.message,
      status: 'failed_at_bundle_build',
    }, null, 2));
    return null;
  }

  log(`Bundle: ${bundle.name} - ${bundle.description}`);
  log(`Expected first-audio: p50=${bundle.expected_first_audio_ms.p50}ms p95=${bundle.expected_first_audio_ms.p95}ms`);
  log(`Cost: $${bundle.cost_per_min_usd}/min`);

  const orch = new Orchestrator(bundle);
  const metrics: any = { bundle: bundleName, status: 'running' };
  let firstAudioSeen = false;

  orch.on('user_transcript', (text: string) => log(`USER_TRANSCRIPT: ${text}`));
  orch.on('error', (e: any) => { metrics.errors = (metrics.errors || 0) + 1; log(`ERROR: ${e.message}`); });
  orch.on('transport_connected', () => log('TRANSPORT_CONNECTED'));

  try {
    await orch.start();
    log('ORCHESTRATOR_STARTED');

    // For the spike, the file-loopback transport doesn't yet auto-play
    // the file in this scaffold; the orchestrator is structured to wait
    // for onFrame() callbacks. In a real run, the transport would feed
    // frames from the LiveKit / Telnyx / file-loopback source.
    // For the head-to-head scaffolding, we record what we have and exit
    // cleanly so the user can hear the assistant voice (TTS) once the
    // vendor adapters are wired.
    await new Promise((r) => setTimeout(r, 2000));  // give the LLM/TTS a moment to wake
    log('ORCHESTRATOR_IDLE: waiting for transport frames (or vendor wiring)');
  } catch (e: any) {
    log(`ORCHESTRATOR_FAILED: ${e.message}`);
    metrics.status = 'failed';
  }

  metrics.bundle_metrics = orch.getMetrics();
  fs.writeFileSync(path.join(outDir, 'rehearsal-metrics.json'), JSON.stringify(metrics, null, 2));
  await orch.stop();
  log('=== REHEARSAL END ===');
  return metrics;
}

async function runCompare() {
  const outDir = path.resolve('./tmp/rehearsal');
  const bundles: BundleName[] = ['xai-bundle', 'hybrid-deepgram', 'hybrid-elevenlabs'];
  const results: any[] = [];
  for (const b of bundles) {
    log(`Running bundle: ${b}`);
    const r = await runBundle(b, { file: 'fixtures/rehearsal/t02-book.pcmu', format: 'pcmu_8k' });
    results.push(r);
  }

  // Generate a markdown comparison report.
  let md = `# Multi-Vendor Rehearsal Comparison\n\n`;
  md += `Run date: ${new Date().toISOString()}\n\n`;
  md += `## Results\n\n`;
  md += `| Bundle | Status | First-audio (ms) | Cost/min | STT first-byte | LLM first-token | TTS first-byte | Errors |\n`;
  md += `|---|---|---|---|---|---|---|---|\n`;
  for (const r of results) {
    if (!r) continue;
    const m = r.bundle_metrics || {};
    md += `| ${r.bundle} | ${r.status} | ${m.first_audio_latency_ms?.toFixed(0) || '—'} | $${r.cost || '—'} | ${m.stt_first_byte_ms?.toFixed(0) || '—'} | ${m.llm_first_token_ms?.toFixed(0) || '—'} | ${m.tts_first_byte_ms?.toFixed(0) || '—'} | ${m.errors || 0} |\n`;
  }
  md += `\n## Recommendation\n\n`;
  md += `(filled in by the user after listening to the assistant.wav files)\n`;
  fs.writeFileSync(path.join(outDir, 'COMPARE_REPORT.md'), md);
  log(`Comparison report: ${path.join(outDir, 'COMPARE_REPORT.md')}`);
}

const args = parseArgs();
if (args.bundle === 'compare') {
  await runCompare();
} else {
  await runBundle(args.bundle, { file: args.file, format: args.format });
}
process.exit(0);
