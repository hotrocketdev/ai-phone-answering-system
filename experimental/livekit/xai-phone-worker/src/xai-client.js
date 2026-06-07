// xai-client.js — xAI Voice Agent WSS client (production worker version).
//
// Same protocol as the spike (xai-phone-harness/src/xai-client.js).
// Differences for the production worker:
//   - audio is fed live from a Telnyx media stream (WebSocket, JSON
//     frames with base64 PCMU payloads) — see TelnyxMediaSource in
//     src/telnyx-source.js (TODO: implement when ResDiary lands)
//   - audio is emitted live to a Telnyx media stream — see
//     TelnyxMediaSink in src/telnyx-sink.js (TODO: implement)
//   - the dispatcher (dispatchToolCall) hits real ResDiary + Depos
//     APIs instead of returning synthetic stubs
//   - METRIC logs are written to /var/log/voxlane/xai-worker.log
//     (or equivalent) for the production log pipeline, not stdout
//
// This file is the WSS layer only. The dispatcher and the I/O
// adapters are separate modules so they can be unit-tested in
// isolation against a fake xAI WSS server.

import { WebSocket } from 'ws';
import { EventEmitter } from 'node:events';
import { performance } from 'node:perf_hooks';
import { log } from './log.js';

const XAI_WSS_URL = 'wss://api.x.ai/v1/realtime';

export class XaiClient extends EventEmitter {
  constructor({
    apiKey,
    model = 'grok-voice-latest',
    voice = 'eve',
    tools = [],
    instructions = '',
    temperature = 0.7,
    onTranscript = null,
    onError = null,
  }) {
    super();
    this.apiKey = apiKey;
    this.model = model;
    this.voice = voice;
    this.tools = tools;
    this.instructions = instructions;
    this.temperature = temperature;
    this.onTranscript = onTranscript;
    this.onError = onError;
    this.ws = null;
    this.turnCounter = 0;
    this.latestCallID = null;
    this.functionCallCount = 0;
    this.transcriptCount = 0;
    this.errorCount = 0;
    this._responseStart = 0;
    this._pcmChunks = [];
    this._currentTranscript = null;
    this._fcArgs = null;
    this._sessionReady = false;
  }

  async connect() {
    return new Promise((resolve, reject) => {
      this.ws = new WebSocket(XAI_WSS_URL, {
        headers: { Authorization: `Bearer ${this.apiKey}` },
      });
      this.ws.on('open', () => {
        log('METRIC session_connect turnCounter=0 audio_bytes=0',
            `url=${XAI_WSS_URL} model=${this.model}`);
        this._sendSessionUpdate();
        resolve();
      });
      this.ws.on('message', (data) => this._handleEvent(data.toString()));
      this.ws.on('close', (code, reason) => {
        log('METRIC session_end',
            `turns=${this.turnCounter} function_calls=${this.functionCallCount} ` +
            `transcripts=${this.transcriptCount} errors=${this.errorCount} ` +
            `code=${code} reason=${reason?.toString() || ''}`);
        this.emit('close');
      });
      this.ws.on('error', (err) => {
        log('xai error:', err.message);
        this.errorCount++;
        if (this.onError) this.onError(err);
        this.emit('error', err);
        reject(err);
      });
    });
  }

  _sendSessionUpdate() {
    const session = {
      voice: this.voice,
      model: this.model,
      instructions: this.instructions,
      turn_detection: {
        type: 'server_vad',
        threshold: 0.7,
        prefix_padding_ms: 300,
        silence_duration_ms: 1500,
      },
      tools: this.tools.length ? this.tools : undefined,
    };
    if (typeof this.temperature === 'number') {
      session.temperature = this.temperature;
    }
    this._send({ type: 'session.update', session });
  }

  _send(ev) {
    this.ws.send(JSON.stringify(ev));
  }

  _handleEvent(raw) {
    let ev;
    try {
      ev = JSON.parse(raw);
    } catch (e) {
      log('xai: bad JSON event', raw.slice(0, 200));
      return;
    }
    const t = ev.type;
    switch (t) {
      case 'session.created':
      case 'conversation.created':
      case 'session.updated':
        if (t === 'session.updated') this._sessionReady = true;
        log('xai: event', t);
        break;
      case 'ping':
        break;
      case 'input_audio_buffer.speech_started':
        this.turnCounter++;
        log('METRIC turn_start', `turn_id=${this.turnCounter} event=speech_started`);
        break;
      case 'input_audio_buffer.speech_stopped':
        log('METRIC turn_event', `turn_id=${this.turnCounter} event=speech_stopped`);
        break;
      case 'input_audio_buffer.committed':
        break;
      case 'conversation.item.input_audio_transcription.completed':
        if (ev.item && typeof ev.item.transcript === 'string') {
          this.transcriptCount++;
          log('METRIC transcript', `turn_id=${this.turnCounter} role=user bytes=${ev.item.transcript.length}`);
          this.emit('user_transcript', ev.item.transcript);
        }
        break;
      case 'response.created':
        this._pcmChunks = [];
        this._responseStart = performance.now();
        break;
      case 'response.output_audio.delta':
        if (ev.delta) {
          const buf = Buffer.from(ev.delta, 'base64');
          this._pcmChunks.push(buf);
          this.emit('audio_delta', buf);
        }
        break;
      case 'response.output_audio_transcript.delta':
        if (ev.delta) this._currentTranscript = (this._currentTranscript || '') + ev.delta;
        break;
      case 'response.output_audio_transcript.done':
        this.transcriptCount++;
        const text = ev.transcript || this._currentTranscript || '';
        this._currentTranscript = null;
        log('METRIC transcript', `turn_id=${this.turnCounter} role=assistant bytes=${text.length}`);
        log('xai transcript [assistant]:', text);
        if (this.onTranscript) this.onTranscript('assistant', text);
        this.emit('assistant_transcript', text);
        break;
      case 'response.function_call_arguments.delta':
        if (this._fcArgs == null) this._fcArgs = '';
        if (ev.delta) this._fcArgs += ev.delta;
        break;
      case 'response.function_call_arguments.done':
        const name = ev.name || '';
        const argsJson = ev.arguments || this._fcArgs || '{}';
        const callID = ev.call_id || '';
        this._fcArgs = null;
        this.latestCallID = callID;
        this.functionCallCount++;
        log('METRIC function_call', `turn_id=${this.turnCounter} name=${name} args=${argsJson}`);
        this.emit('function_call', { name, argsJson, callID });
        break;
      case 'response.output_item.done':
        break;
      case 'response.done':
        if (this._pcmChunks.length) {
          const total = Buffer.concat(this._pcmChunks);
          this._pcmChunks = [];
          log('METRIC audio_done', `turn_id=${this.turnCounter} bytes=${total.length}`);
          this.emit('audio_done', total);
        }
        this.emit('response_done');
        break;
      case 'error':
        this.errorCount++;
        log('METRIC error', `turn_id=${this.turnCounter} code=${ev.error?.code || 'unknown'} type=${ev.error?.type || 'unknown'} msg="${ev.error?.message || ''}"`);
        this.emit('xai_error', ev.error);
        break;
      default:
        log('xai: unhandled event', t);
    }
  }

  // appendAudio: feed a PCM16 24 kHz mono buffer chunk to xAI as
  // base64 in input_audio_buffer.append. The caller is responsible
  // for ensuring the audio is in the right format (PCM16 24 kHz
  // mono, since xAI's WSS rejects the audio.input.format field).
  appendAudio(pcm24kBytes) {
    if (!this._sessionReady) return;
    this._send({
      type: 'input_audio_buffer.append',
      audio: pcm24kBytes.toString('base64'),
    });
  }

  commitAudio() {
    this._send({ type: 'input_audio_buffer.commit' });
    this._send({ type: 'response.create' });
  }

  sendFunctionResult(callName, args, output) {
    if (!this.latestCallID) {
      throw new Error('sendFunctionResult: no call_id available');
    }
    this._send({
      type: 'conversation.item.create',
      item: {
        type: 'function_call_output',
        call_id: this.latestCallID,
        output: JSON.stringify(output),
      },
    });
    this._send({ type: 'response.create' });
  }

  close() {
    if (this.ws) {
      try { this.ws.close(); } catch (e) { /* ignore */ }
    }
  }
}
