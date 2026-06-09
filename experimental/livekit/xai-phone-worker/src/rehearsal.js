// src/rehearsal.js — full fake-provider rehearsal against the live
// xAI Voice Agent WSS.
//
// This is the manager's "controlled next phase" rehearsal: prove
// the full product flow end-to-end with the real xAI engine, the
// real Eve voice, and the real function-call bridge, but with
// FAKE ResDiary / Depos / manager-queue providers (no live API
// calls). When the real ResDiary API arrives, the only change
// needed is `USE_REAL_PROVIDERS=1` + the field-mapping TODOs.
//
// Run with: node src/rehearsal.js
//
// Output (in ./tmp/rehearsal/):
//   - rehearsal.log         : timestamped log of every event
//   - rehearsal-metrics.json: machine-readable metrics
//   - rehearsal-assistant.wav: concatenated assistant audio (24 kHz PCM16)

import 'dotenv/config';
import fs from 'node:fs';
import path from 'node:path';
import { performance } from 'node:perf_hooks';
import { XaiClient } from './xai-client.js';
import { dispatchToolCall } from './dispatcher.js';
import { getProviders, resetProviders } from './providers/index.js';
import { pcmuToPcm16, pcm8kToPcm24k, pcm24kToPcm8k, pcm16ToPcmu } from './pcmu-codec.js';
import { log } from './log.js';

const RESTAURANT_INSTRUCTIONS = `You are Alex, the receptionist at Porto Douro Restaurants. Greet the caller warmly and ask how you can help. For a new table booking, collect date, time, party size, name, and phone number. Use availability.check to see if a table is free, then booking.create to confirm. CRITICAL: if a caller asks something you cannot verify (menu, dietary, hours, anything restaurant-specific), call manager.escalate IMMEDIATELY in the same turn — do NOT ask the user 'would you like me to take a message' first, just call the tool. Speak in British English, briefly and warmly. Do not invent restaurant details.`;

const TOOLS = [
  {
    type: 'function',
    function: {
      name: 'availability.check',
      description: 'Check whether a table is available at a given date and time.',
      parameters: {
        type: 'object',
        properties: {
          date: { type: 'string', description: 'ISO date (YYYY-MM-DD) or natural language like "tomorrow".' },
          time: { type: 'string', description: '24-hour time HH:MM.' },
          party_size: { type: 'integer', description: 'Number of guests.' },
        },
        required: ['date', 'time', 'party_size'],
      },
    },
  },
  {
    type: 'function',
    function: {
      name: 'booking.create',
      description: 'Confirm a table booking with the caller on the line.',
      parameters: {
        type: 'object',
        properties: {
          date: { type: 'string' },
          time: { type: 'string' },
          party_size: { type: 'integer' },
          name: { type: 'string' },
          phone: { type: 'string' },
          notes: { type: 'string' },
        },
        required: ['date', 'time', 'party_size', 'name', 'phone'],
      },
    },
  },
  {
    type: 'function',
    function: {
      name: 'manager.escalate',
      description: 'Take a message for the manager to call back when a caller asks something you cannot verify.',
      parameters: {
        type: 'object',
        properties: {
          topic: { type: 'string' },
          message: { type: 'string' },
          caller_name: { type: 'string' },
          caller_phone: { type: 'string' },
        },
        required: ['message'],
      },
    },
  },
];

// 18-step rehearsal script. Each step is one of:
//   { side: 'caller', pcmu: 'fixtures/rehearsal/tNN-*.pcmu' }   -> feed audio to xAI
//   { side: 'wait', timeoutMs: 15000 }                         -> wait for assistant to respond
// 18-step rehearsal script. Each caller step references an audio
// fixture. The extension determines how it's decoded:
//   .pcmu       -> 8 kHz mu-law (telephony floor), decoded + 3x upsample
//   .pcm16_24k  -> 24 kHz PCM (no telephony floor), fed directly
// Set REHEARSAL_INPUT_FORMAT env to switch (default .pcmu).
const INPUT_EXT = process.env.REHEARSAL_INPUT_FORMAT || '.pcmu';
function fixt(name) { return `fixtures/rehearsal/${name}${INPUT_EXT}`; }

const SCRIPT = [
  { step: 1,  side: 'caller', note: 'Caller says hello',                                      pcmu: fixt('t01-hello') },
  { step: 2,  side: 'wait',   note: 'Assistant greets' },
  { step: 3,  side: 'caller', note: 'Caller asks to book a table',                            pcmu: fixt('t02-book') },
  { step: 4,  side: 'wait',   note: 'Assistant acknowledges' },
  { step: 5,  side: 'caller', note: 'Caller says "Tomorrow at seven for four people"',      pcmu: fixt('t03-tomorrow-7-4') },
  { step: 6,  side: 'wait',   note: 'Assistant checks availability.check' },
  { step: 7,  side: 'caller', note: 'Caller says "George"',                                   pcmu: fixt('t07-george') },
  { step: 8,  side: 'wait',   note: 'Assistant asks for phone' },
  { step: 9,  side: 'caller', note: 'Caller says phone number',                               pcmu: fixt('t09-phone') },
  { step: 10, side: 'wait',   note: 'Assistant explains deposit, calls deposit.hold + booking.create' },
  { step: 11, side: 'wait',   note: 'Assistant continues after tools' },
  { step: 12, side: 'wait',   note: 'Assistant confirms booking' },
  { step: 13, side: 'caller', note: 'Caller changes party size to 6',                          pcmu: fixt('t14-change-to-6') },
  { step: 14, side: 'wait',   note: 'Assistant re-checks availability and updates booking' },
  { step: 15, side: 'caller', note: 'Caller asks off-script (vegan tasting menu)',           pcmu: fixt('t16-off-script') },
  { step: 16, side: 'wait',   note: 'Assistant calls manager.escalate' },
  { step: 17, side: 'wait',   note: 'Assistant acknowledges escalation' },
  { step: 18, side: 'wait',   note: 'Assistant ends call naturally' },
];

// --- Logging ---

const OUT_DIR = path.resolve('./tmp/rehearsal');
fs.mkdirSync(OUT_DIR, { recursive: true });
const LOG_PATH = path.join(OUT_DIR, 'rehearsal.log');
const METRICS_PATH = path.join(OUT_DIR, 'rehearsal-metrics.json');
const ASSISTANT_WAV = path.join(OUT_DIR, 'rehearsal-assistant.wav');

fs.writeFileSync(LOG_PATH, ''); // truncate
function flog(...args) {
  const line = `${new Date().toISOString()} ${args.map((a) => typeof a === 'string' ? a : JSON.stringify(a)).join(' ')}`;
  console.log(line);
  fs.appendFileSync(LOG_PATH, line + '\n');
}

const metrics = {
  started_at: new Date().toISOString(),
  scenario: 'fake-provider rehearsal (18 steps)',
  turns: [],
  function_calls: [],
  transcripts: [],
  errors: [],
  audio: {
    assistant_pcm16_24k: 0,
    assistant_wav_bytes: 0,
    dropped_frames: 0,
  },
  latency: {
    first_assistant_audio_ms: null,
    turn_latencies_ms: [],
  },
};

function recordTurn(turn) {
  metrics.turns.push(turn);
  fs.writeFileSync(METRICS_PATH, JSON.stringify(metrics, null, 2));
}

function writeWav(filePath, pcm16LE, sampleRate) {
  const dataLen = pcm16LE.length;
  const buf = Buffer.alloc(44 + dataLen);
  buf.write('RIFF', 0);
  buf.writeUInt32LE(36 + dataLen, 4);
  buf.write('WAVE', 8);
  buf.write('fmt ', 12);
  buf.writeUInt32LE(16, 16);
  buf.writeUInt16LE(1, 20);
  buf.writeUInt16LE(1, 22);
  buf.writeUInt32LE(sampleRate, 24);
  buf.writeUInt32LE(sampleRate * 2, 28);
  buf.writeUInt16LE(2, 32);
  buf.writeUInt16LE(16, 34);
  buf.write('data', 36);
  buf.writeUInt32LE(dataLen, 40);
  pcm16LE.copy(buf, 44);
  fs.writeFileSync(filePath, buf);
}

// --- Turn execution ---

async function waitForResponseDone(xai, timeoutMs = 20000) {
  // Use a one-shot listener that resets on each response. After the
  // last response_done, wait 2.5s of quiet to be sure no follow-up
  // audio is coming. Avoid leaking listeners across turns by
  // removing ours on resolve.
  return new Promise((resolve) => {
    let done = false;
    let quietTimer = null;
    const finish = (reason) => {
      if (done) return;
      done = true;
      xai.removeListener('response_done', onResponse);
      if (quietTimer) clearTimeout(quietTimer);
      flog(`  -> turn wait finished (${reason})`);
      resolve();
    };
    const onResponse = () => {
      if (quietTimer) clearTimeout(quietTimer);
      quietTimer = setTimeout(() => finish('quiet'), 2500);
    };
    xai.on('response_done', onResponse);
    setTimeout(() => finish('timeout'), timeoutMs);
  });
}

async function feedCallerTurn(xai, audioPath) {
  const t0 = performance.now();
  let pcm16_24k;
  let label;
  if (audioPath.endsWith('.pcm16_24k')) {
    // Already 24 kHz PCM16 mono — feed directly to xAI.
    pcm16_24k = fs.readFileSync(audioPath);
    label = `${pcm16_24k.length} bytes PCM16 24k`;
  } else {
    // PCMU 8 kHz: decode mu-law, upsample 3x to PCM16 24 kHz.
    // This is the telephony floor; useful for measuring the
    // "real production" experience.
    const pcmu = fs.readFileSync(audioPath);
    const pcm16_8k = pcmuToPcm16(pcmu);
    pcm16_24k = pcm8kToPcm24k(pcm16_8k);
    label = `${pcmu.length} bytes PCMU -> ${pcm16_24k.length} bytes PCM16 24k`;
  }

  // Stream in 100ms chunks (4800 bytes at 24 kHz).
  const CHUNK = 4800;
  for (let i = 0; i < pcm16_24k.length; i += CHUNK) {
    xai.appendAudio(pcm16_24k.subarray(i, Math.min(i + CHUNK, pcm16_24k.length)));
    await new Promise((r) => setTimeout(r, 100));
  }
  // Force end-of-turn: xAI's VAD will eventually fire on silence but
  // committing explicitly is faster in a test.
  xai.commitAudio();
  flog(`  -> fed ${label} in ${(performance.now() - t0).toFixed(0)}ms`);
}

async function main() {
  if (!process.env.XAI_API_KEY) {
    console.error('XAI_API_KEY not set');
    process.exit(1);
  }

  flog('=== REHEARSAL START ===');
  flog(`Provider kind: ${getProviders().kind}`);
  flog(`Tools loaded: ${TOOLS.length}`);
  flog(`Script steps: ${SCRIPT.length}`);
  flog(`XAI model: grok-voice-latest, voice: eve`);

  // Session-wide audio buffer (for the final WAV).
  const assistantPcm = [];

  const xai = new XaiClient({
    apiKey: process.env.XAI_API_KEY,
    model: 'grok-voice-latest',
    voice: 'eve',
    tools: TOOLS,
    instructions: RESTAURANT_INSTRUCTIONS,
    temperature: 0.7,
  });

  // Function-call bridge: dispatch to the (fake) providers via the
  // dispatcher. The dispatcher enforces the
  // availability -> hold -> book -> compensate flow. This is the
  // single piece of code that knows the product logic.
  let expectResumedFor = null;
  xai.on('function_call', ({ name, argsJson, callID }) => {
    let args = {};
    try { args = JSON.parse(argsJson); } catch (_) { /* keep {} */ }
    const dispatchStart = performance.now();
    flog(`FUNCTION_CALL name=${name} call_id=${callID} args=${JSON.stringify(args)}`);
    expectResumedFor = name;

    // We need a synchronous-looking return to xAI.sendFunctionResult,
    // but the dispatcher is async. Capture the call.
    dispatchToolCall(name, args, callID).then((result) => {
      const dispatchMs = performance.now() - dispatchStart;
      flog(`FUNCTION_CALL_DISPATCHED name=${name} call_id=${callID} dispatch_ms=${dispatchMs.toFixed(0)} result=${JSON.stringify(result)}`);
      metrics.function_calls.push({
        name,
        call_id: callID,
        args,
        result,
        dispatch_ms: dispatchMs,
      });
      try {
        xai.sendFunctionResult(name, args, result);
        flog(`FUNCTION_CALL_OUTPUT_SENT name=${name} call_id=${callID}`);
      } catch (e) {
        flog(`FUNCTION_CALL_OUTPUT_FAILED name=${name} call_id=${callID} err=${e.message}`);
        metrics.errors.push({ where: 'sendFunctionResult', name, call_id: callID, message: e.message });
      }
    }).catch((e) => {
      flog(`FUNCTION_CALL_ERROR name=${name} call_id=${callID} err=${e.message}`);
      metrics.errors.push({ where: 'dispatcher', name, call_id: callID, message: e.message });
      try { xai.sendFunctionResult(name, args, { error: 'tool_failed', detail: e.message }); } catch (_) { /* */ }
    });
  });
  xai.on('response_done', () => {
    if (expectResumedFor) {
      flog(`ASSISTANT_RESUMED_AFTER_TOOL name=${expectResumedFor}`);
      expectResumedFor = null;
    }
  });
  xai.on('audio_delta', (chunk) => {
    if (metrics.latency.first_assistant_audio_ms === null) {
      metrics.latency.first_assistant_audio_ms = performance.now() - metrics._firstCallerAudioAt;
      flog(`FIRST_ASSISTANT_AUDIO_LATENCY_MS=${metrics.latency.first_assistant_audio_ms.toFixed(0)}`);
    }
    assistantPcm.push(chunk);
    metrics.audio.assistant_pcm16_24k += chunk.length;
  });
  xai.on('assistant_transcript', (text) => {
    flog(`ASSISTANT_TRANSCRIPT ${text}`);
    metrics.transcripts.push({ role: 'assistant', text });
  });
  xai.on('xai_error', (err) => {
    flog(`XAI_ERROR ${JSON.stringify(err)}`);
    metrics.errors.push({ where: 'xai', err });
  });

  await xai.connect();
  flog('xai: connected');

  // Run the script.
  let callerTurnIdx = 0;
  for (let i = 0; i < SCRIPT.length; i++) {
    const step = SCRIPT[i];
    flog(`STEP ${step.step}: ${step.note}`);

    if (step.side === 'caller') {
      callerTurnIdx++;
      const turnStart = performance.now();
      metrics._firstCallerAudioAt = turnStart;
      recordTurn({ step: step.step, side: 'caller', note: step.note, started_at: new Date().toISOString() });
      await feedCallerTurn(xai, step.pcmu);
      const turnLatency = performance.now() - turnStart;
      recordTurn({ step: step.step, side: 'caller', latency_ms: turnLatency });
    } else if (step.side === 'wait') {
      const waitStart = performance.now();
      await waitForResponseDone(xai, step.timeoutMs ?? 20000);
      const waitLatency = performance.now() - waitStart;
      metrics.latency.turn_latencies_ms.push(waitLatency);
      recordTurn({ step: step.step, side: 'wait', note: step.note, latency_ms: waitLatency });
    }
  }

  // Final close.
  flog('=== REHEARSAL END ===');
  metrics.ended_at = new Date().toISOString();
  metrics.latency.avg = metrics.latency.turn_latencies_ms.length
    ? (metrics.latency.turn_latencies_ms.reduce((a, b) => a + b, 0) / metrics.latency.turn_latencies_ms.length)
    : 0;
  metrics.latency.p95 = (() => {
    const arr = [...metrics.latency.turn_latencies_ms].sort((a, b) => a - b);
    if (!arr.length) return 0;
    return arr[Math.floor(arr.length * 0.95)];
  })();

  // Write the assistant WAV.
  if (assistantPcm.length) {
    const pcm = Buffer.concat(assistantPcm);
    writeWav(ASSISTANT_WAV, pcm, 24000);
    metrics.audio.assistant_wav_bytes = fs.statSync(ASSISTANT_WAV).size;
    // Also write downsampled PCMU 8 kHz for parity.
    const pcm8k = pcm24kToPcm8k(pcm);
    const pcmuOut = pcm16ToPcmu(pcm8k);
    const pcmuPath = ASSISTANT_WAV.replace(/\.wav$/, '.pcmu');
    fs.writeFileSync(pcmuPath, pcmuOut);
    flog(`Assistant audio: ${metrics.audio.assistant_wav_bytes} bytes WAV + ${pcmuOut.length} bytes PCMU`);
  }

  fs.writeFileSync(METRICS_PATH, JSON.stringify(metrics, null, 2));
  flog(`Metrics written: ${METRICS_PATH}`);
  flog(`WAV: ${ASSISTANT_WAV}`);
  flog(`Summary: ${SCRIPT.length} steps, ${metrics.function_calls.length} function calls, ${metrics.transcripts.length} assistant transcripts, ${metrics.errors.length} errors`);

  xai.close();
  process.exit(metrics.errors.length === 0 ? 0 : 1);
}

main().catch((e) => {
  flog('fatal:', e.message ?? String(e));
  process.exit(1);
});
