// index.js — Telnyx/PCMU <-> xAI Voice Agent WSS harness (entry point).
//
// Usage:
//   1. Set XAI_API_KEY in .env (do NOT commit)
//   2. node src/index.js [--input path/to/input.pcmu | --tone-ms 5000]
//      --output path/to/output.wav
//
// With --tone-ms 5000, the harness sends 5 seconds of 250 Hz tone at
// 8 kHz PCMU. The model has no idea what to make of a tone and will
// typically ask for clarification. This is enough to verify the WSS
// path is alive end-to-end. For realistic speech, supply a real PCMU
// file (e.g. an actual Telnyx recording converted via ffmpeg) via
// --input.
//
// --output writes the assistant's PCM16 24 kHz response to a WAV file
// for verification. Default: ./output.wav.
//
// This is the smallest possible harness to validate that the
// production phone path (PCMU in, PCM out, function-call bridge) is
// viable — per Opus's feasibility note, this is the highest-value
// next step because the browser failure does not test the phone
// path at all.

import 'dotenv/config';
import fs from 'node:fs';
import path from 'node:path';
import { argv } from 'node:process';
import { XaiClient } from './xai-client.js';
import { dispatchToolCall } from './tools.js';
import { tonePcmu, pcm16ToPcmu, pcmuToPcm16, pcm8kToPcm24k, pcm24kToPcm8k } from './pcmu-codec.js';
import { log } from './log.js';

function parseArgs() {
  const args = {
    input: null,
    output: 'output.wav',
    toneMs: 0,
    model: 'grok-voice-latest',
    voice: 'eve',
    tools: null,
    temperature: 0.7,
  };
  for (let i = 2; i < argv.length; i++) {
    const a = argv[i];
    if (a === '--input') args.input = argv[++i];
    else if (a === '--output') args.output = argv[++i];
    else if (a === '--tone-ms') args.toneMs = parseInt(argv[++i], 10);
    else if (a === '--model') args.model = argv[++i];
    else if (a === '--voice') args.voice = argv[++i];
    else if (a === '--tools') args.tools = argv[++i];
    else if (a === '--temperature') args.temperature = parseFloat(argv[++i]);
  }
  return args;
}

const RESTAURANT_INSTRUCTIONS = `You are Alex, the receptionist at Porto Douro Restaurants. Greet the caller warmly and ask how you can help. For a new table booking, collect date, time, party size, name, and phone number. Use availability.check to see if a table is free, then booking.create to confirm. If anything is unclear or you don't know the answer, offer to take a message via manager.escalate. Speak in British English, briefly and warmly.`;

function loadTools(pathOrNull) {
  if (!pathOrNull) {
    // Tool format mirrors the LiveKit spike (xai-voice-agent/tools-booking.json):
    // { type: "function", function: { name, description, parameters } } — NESTED.
    return [
      {
        type: 'function',
        function: {
          name: 'availability.check',
          description: 'Check whether a table is available at a given date and time.',
          parameters: {
            type: 'object',
            properties: {
              date: { type: 'string', description: 'ISO date (YYYY-MM-DD) or natural-language date like "tomorrow".' },
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
          description: 'Take a message for the manager to call back.',
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
  }
  return JSON.parse(fs.readFileSync(pathOrNull, 'utf-8'));
}

function writeWav(filePath, pcm16LE, sampleRate) {
  // 16-bit PCM WAV. 44-byte header, then interleaved little-endian samples.
  const dataLen = pcm16LE.length;
  const buf = Buffer.alloc(44 + dataLen);
  buf.write('RIFF', 0);
  buf.writeUInt32LE(36 + dataLen, 4);
  buf.write('WAVE', 8);
  buf.write('fmt ', 12);
  buf.writeUInt32LE(16, 16);
  buf.writeUInt16LE(1, 20); // PCM
  buf.writeUInt16LE(1, 22); // mono
  buf.writeUInt32LE(sampleRate, 24);
  buf.writeUInt32LE(sampleRate * 2, 28); // byte rate
  buf.writeUInt16LE(2, 32); // block align
  buf.writeUInt16LE(16, 34); // bits per sample
  buf.write('data', 36);
  buf.writeUInt32LE(dataLen, 40);
  pcm16LE.copy(buf, 44);
  fs.writeFileSync(filePath, buf);
  log('wrote', filePath, `${dataLen} bytes PCM16 @ ${sampleRate} Hz`);
}

async function main() {
  const args = parseArgs();
  const apiKey = process.env.XAI_API_KEY;
  if (!apiKey) {
    console.error('XAI_API_KEY not set in env (.env or process env)');
    process.exit(1);
  }

  log('xai-phone-harness starting');
  log(`  model=${args.model} voice=${args.voice} temperature=${args.temperature}`);
  log(`  input: PCMU 8kHz (G.711 mu-law) -> decode -> upsample -> PCM16 24kHz to xAI`);
  log(`  output: PCM16 24kHz from xAI -> write WAV + downsample to PCMU 8kHz`);

  const tools = loadTools(args.tools);
  log(`  loaded ${tools.length} tools`);

  const xai = new XaiClient({
    apiKey,
    model: args.model,
    voice: args.voice,
    tools,
    instructions: RESTAURANT_INSTRUCTIONS,
    temperature: args.temperature,
    // xAI does not accept audio.input.format / audio.output.format
    // in session.update (r3-r5 of the spike). The default is
    // PCM16 24 kHz, which we get by decoding + upsampling the PCMU
    // 8 kHz input client-side.
  });

  // Function-call bridge: receive the model's tool call, dispatch to
  // the stub, send the function_call_output back. Same flow as the
  // LiveKit spike (xai_livekit.go OnFunctionCall), adapted to Node.
  let expectResumedFor = null;
  xai.on('function_call', ({ name, argsJson, callID }) => {
    const args = (() => { try { return JSON.parse(argsJson); } catch { return {}; } })();
    const start = performance.now();
    const result = dispatchToolCall(name, args);
    log('METRIC function_call_dispatched', `name=${name} call_id=${callID} result=${JSON.stringify(result)} dispatch_ms=${(performance.now() - start).toFixed(0)}`);
    expectResumedFor = name;
    try {
      xai.sendFunctionResult(name, args, result);
      log('METRIC function_call_output_sent', `name=${name} call_id=${callID}`);
    } catch (e) {
      log('METRIC function_call_output_failed', `name=${name} call_id=${callID} err=${e.message}`);
    }
  });
  xai.on('response_done', () => {
    if (expectResumedFor) {
      log('METRIC assistant_resumed_after_tool', `name=${expectResumedFor}`);
      expectResumedFor = null;
    }
  });

  // Collect the assistant's response audio. _pcmChunks is per-response;
  // we keep a flat list across the session for the WAV output.
  const sessionPcm = [];
  xai.on('audio_delta', (chunk) => sessionPcm.push(chunk));
  xai.on('audio_done', (total) => {
    log('METRIC audio_captured', `bytes=${total.length}`);
  });

  await xai.connect();

  // Wait for session.updated before sending audio. The xai client
  // exposes this via the `_sessionReady` flag through the next-tick
  // promise; we just sleep 300ms which is enough for xAI to ack the
  // session.update and accept subsequent input_audio_buffer.append.
  log('waiting 300ms for session.updated ack from xAI');
  await new Promise((r) => setTimeout(r, 300));

  // Decide the input.
  let pcmu;
  if (args.input) {
    log('reading PCMU input from', args.input);
    pcmu = fs.readFileSync(args.input);
    log('  loaded', pcmu.length, 'bytes PCMU');
  } else if (args.toneMs > 0) {
    log('generating', args.toneMs, 'ms of 250 Hz tone at 8 kHz PCMU');
    pcmu = tonePcmu(250, args.toneMs, 8000, 8000);
  } else {
    log('no --input or --tone-ms; sending 1 second of PCMU silence');
    pcmu = Buffer.alloc(8000, 0xff);
  }

  // Decode PCMU -> PCM16 8 kHz -> upsample to PCM16 24 kHz. xAI's
  // WSS expects PCM16 audio at 24 kHz (the default). We do the
  // G.711 mu-law decode + 3x upsample client-side; the alternative
  // of declaring audio.input.format = audio/pcmu was rejected by
  // the server (r3-r5 of the spike).
  log('decoding PCMU -> PCM16 8kHz -> upsampling to 24kHz');
  const pcm8k = pcmuToPcm16(pcmu);
  const pcm24k = pcm8kToPcm24k(pcm8k);
  log('converted', pcmu.length, 'bytes PCMU ->', pcm24k.length, 'bytes PCM16 24 kHz');

  // Stream PCM16 24 kHz in 100 ms chunks (4800 bytes each at 24 kHz).
  const chunk = 4800;
  for (let i = 0; i < pcm24k.length; i += chunk) {
    xai.appendAudio(pcm24k.subarray(i, i + chunk));
    // Pace ourselves at real-time so xAI's VAD has time to detect.
    await new Promise((r) => setTimeout(r, 100));
  }
  log('input flushed; committing and triggering response');

  // Force end-of-turn: xAI's VAD will eventually fire on silence
  // anyway, but committing explicitly gets a faster response in
  // testing.
  xai.commitAudio();

  // Wait for the response to complete. The WSS session stays open
  // for further turns if more audio arrives; for the harness we
  // close after one full round-trip.
  await new Promise((resolve) => {
    let done = false;
    const finish = () => { if (!done) { done = true; resolve(); } };
    xai.once('response_done', finish);
    // Safety timeout: 30s for a single response.
    setTimeout(() => { log('METRIC response_timeout'); finish(); }, 30000);
  });

  // Save the assistant audio to WAV.
  if (sessionPcm.length) {
    const pcm24k = Buffer.concat(sessionPcm);
    writeWav(args.output, pcm24k, 24000);
    // Also write a downsampled PCMU 8 kHz version for parity with
    // a Telnyx-returned stream.
    const pcm8k = pcm24kToPcm8k(pcm24k);
    const pcmu8k = pcm16ToPcmu(pcm8k);
    const pcmuPath = args.output.replace(/\.wav$/i, '.pcmu');
    fs.writeFileSync(pcmuPath, pcmu8k);
    log('wrote', pcmuPath, `${pcmu8k.length} bytes PCMU 8 kHz (downsampled)`);
  } else {
    log('no assistant audio captured');
  }

  log('closing session');
  xai.close();

  // Brief wait for the close event to flush.
  await new Promise((r) => setTimeout(r, 500));
  log('done');
  process.exit(0);
}

main().catch((e) => {
  console.error('fatal:', e);
  process.exit(1);
});
