// index.js — production worker entry point.
//
// Telnyx media stream <-> xAI Voice Agent WSS. This is the
// production deployment of the spike (xai-phone-harness).
//
// What's wired:
//   - XaiClient (xai-client.js) — the WSS protocol layer
//   - Dispatcher (dispatcher.js) — ResDiary + Depos + manager queue
//   - Telephony I/O (telnyx-source.js, telnyx-sink.js) — TODO
//
// What's NOT wired yet:
//   - The Telnyx WebSocket source. We need to either (a) install
//     the @telnyx/sdk and stream from a Telnyx media call, or (b)
//     inherit the production gateway's media stream. The latter
//     is the integration path; the former is a separate worker.
//   - The Telnyx media sink. xAI's PCM16 24 kHz response audio
//     needs to be encoded to opus (or whatever the gateway
//     expects) and written back to the stream.
//   - Production logging. The log() function writes to stdout;
//     production needs to redirect to a log file and ship to
//     the observability stack.

import 'dotenv/config';
import { XaiClient } from './xai-client.js';
import { dispatchToolCall } from './dispatcher.js';
import { log } from './log.js';

const RESTAURANT_INSTRUCTIONS = `You are Alex, the receptionist at Porto Douro Restaurants. Greet the caller warmly and ask how you can help. For a new table booking, collect date, time, party size, name, and phone number. Use availability.check to see if a table is free, then booking.create to confirm. If anything is unclear or you don't know the answer, offer to take a message via manager.escalate. Speak in British English, briefly and warmly.`;

const TOOLS = [
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

async function main() {
  const apiKey = process.env.XAI_API_KEY;
  if (!apiKey) {
    console.error('XAI_API_KEY not set');
    process.exit(1);
  }
  if (!process.env.RESDIARY_API_KEY) {
    log('WARN: RESDIARY_API_KEY not set; availability.check and booking.create will fail at runtime');
  }
  if (!process.env.DEPOS_API_KEY) {
    log('WARN: DEPOS_API_KEY not set; booking.create deposit hold will fail at runtime');
  }

  log('xai-phone-worker starting');
  log('  model=grok-voice-latest voice=eve temperature=0.7');
  log('  codec: opus 24 kHz preferred (production gateway should request from Telnyx)');

  const xai = new XaiClient({
    apiKey,
    model: 'grok-voice-latest',
    voice: 'eve',
    tools: TOOLS,
    instructions: RESTAURANT_INSTRUCTIONS,
    temperature: 0.7,
  });

  // Function-call bridge: dispatch the model's tool call, send the
  // result back to xAI, log everything. Identical pattern to the
  // spike but with the real ResDiary + Depos dispatcher.
  let expectResumedFor = null;
  xai.on('function_call', async ({ name, argsJson, callID }) => {
    let args = {};
    try { args = JSON.parse(argsJson); } catch (_) { /* keep {} */ }
    const dispatchStart = Date.now();
    log('METRIC function_call_dispatched',
        `name=${name} call_id=${callID} args=${JSON.stringify(args)}`);
    expectResumedFor = name;
    try {
      const result = await dispatchToolCall(name, args);
      log('METRIC function_call_dispatch_done',
          `name=${name} call_id=${callID} result=${JSON.stringify(result)} dispatch_ms=${Date.now() - dispatchStart}`);
      xai.sendFunctionResult(name, args, result);
      log('METRIC function_call_output_sent', `name=${name} call_id=${callID}`);
    } catch (e) {
      log('METRIC function_call_dispatch_error',
          `name=${name} call_id=${callID} err=${e.message}`);
      // Tell the model the tool failed so it can recover.
      xai.sendFunctionResult(name, args, { error: 'tool_failed', detail: e.message });
    }
  });
  xai.on('response_done', () => {
    if (expectResumedFor) {
      log('METRIC assistant_resumed_after_tool', `name=${expectResumedFor}`);
      expectResumedFor = null;
    }
  });

  // TODO: wire to Telnyx media stream (telnyx-source.js)
  //   const telnyxSource = new TelnyxMediaSource({ url, token });
  //   telnyxSource.on('audio', (pcm16_24k) => xai.appendAudio(pcm16_24k));
  //   telnyxSource.on('hangup', () => xai.close());
  //   await telnyxSource.connect();

  // TODO: wire to Telnyx media sink
  //   const telnyxSink = new TelnyxMediaSink({ url, token, codec: 'opus' });
  //   xai.on('audio_delta', (pcm16_24k) => telnyxSink.write(pcm16_24k));

  await xai.connect();
  log('xai-phone-worker LIVE (xAI session). Waiting for audio source.');

  // Keep the process alive.
  process.on('SIGINT', () => { log('shutdown'); xai.close(); process.exit(0); });
  process.on('SIGTERM', () => { log('shutdown'); xai.close(); process.exit(0); });
}

main().catch((e) => {
  console.error('fatal:', e);
  process.exit(1);
});
