// xai-client.js — xAI Voice Agent WSS client (Node, no LiveKit, no Opus).
//
// Adapted from the Go xai_client.go in the LiveKit spike. Same event
// protocol, same METRIC logging format, same function-call bridge. The
// only differences from the spike version:
//   - no samplebuilder / OGG mux / ffmpeg — input is raw PCMU bytes
//     fed directly as base64 in input_audio_buffer.append
//   - audio output is written to a Buffer (or WAV file) instead of
//     piped to a SampleProvider
//   - session.update declares audio.input.format = audio/pcmu so xAI
//     expects G.711 µ-law from us (no server-side resample on the
//     way in)
//
// Audio format: xAI returns audio.delta events with base64 PCM16 at
// the rate configured in session.update (default 24 kHz). The harness
// can either keep it as PCM16 24 kHz or downsample to PCMU 8 kHz for
// Telnyx playback.

import { WebSocket } from 'ws';
import { EventEmitter } from 'node:events';
import { performance } from 'node:perf_hooks';
import { log } from './log.js';

const XAI_WSS_URL = 'wss://api.x.ai/v1/realtime';

export class XaiClient extends EventEmitter {
  constructor({ apiKey, model = 'grok-voice-latest', voice = 'eve', tools = [], instructions = '', temperature = 0.7, inputFormat = { type: 'audio/pcmu' }, outputFormat = { type: 'audio/pcm' } }) {
    super();
    this.apiKey = apiKey;
    this.model = model;
    this.voice = voice;
    this.tools = tools;
    this.instructions = instructions;
    this.temperature = temperature;
    this.inputFormat = inputFormat;
    this.outputFormat = outputFormat;
    this.ws = null;
    this.turnCounter = 0;
    this.latestCallID = null;
    this.functionCallCount = 0;
    this.transcriptCount = 0;
    this.errorCount = 0;
    this._pcmChunks = []; // collected audio deltas for the current response
    this._transcripts = []; // collected assistant transcripts
    this._sessionReady = false;
  }

  async connect() {
    return new Promise((resolve, reject) => {
      this.ws = new WebSocket(XAI_WSS_URL, {
        headers: { Authorization: `Bearer ${this.apiKey}` },
      });
      this.ws.on('open', () => {
        log('METRIC session_connect turnCounter=0 audio_bytes=0', `url=${XAI_WSS_URL} model=${this.model}`);
        this._sendSessionUpdate();
        resolve();
      });
      this.ws.on('message', (data) => this._handleEvent(data.toString()));
      this.ws.on('close', (code, reason) => {
        log('METRIC session_end', `turns=${this.turnCounter} function_calls=${this.functionCallCount} transcripts=${this.transcriptCount} errors=${this.errorCount} code=${code} reason=${reason?.toString() || ''}`);
        this.emit('close');
      });
      this.ws.on('error', (err) => {
        log('xai error:', err.message);
        this.errorCount++;
        this.emit('error', err);
        reject(err);
      });
    });
  }

  _sendSessionUpdate() {
    const session = {
      voice: this.voice,
      model: this.model,
      audio: {
        input: { format: this.inputFormat },
        output: { format: this.outputFormat },
      },
      instructions: this.instructions,
      tools: this.tools.length ? this.tools : undefined,
      tool_choice: this.tools.length ? 'auto' : undefined,
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
        // log but don't act
        log('xai: event', t);
        break;
      case 'ping':
        // no-op; xAI pings every 10s
        break;
      case 'input_audio_buffer.speech_started':
        this.turnCounter++;
        log('METRIC turn_start', `turn_id=${this.turnCounter} event=speech_started`);
        break;
      case 'input_audio_buffer.speech_stopped':
        log('METRIC turn_event', `turn_id=${this.turnCounter} event=speech_stopped`);
        break;
      case 'input_audio_buffer.committed':
        // xAI committed our PCMU; STT happens server-side.
        break;
      case 'conversation.item.input_audio_transcription.completed':
        // xAI sometimes emits this with the user transcript in
        // item.transcript. Capture for our analyzer.
        if (ev.item && typeof ev.item.transcript === 'string') {
          this.transcriptCount++;
          log('METRIC transcript', `turn_id=${this.turnCounter} role=user bytes=${ev.item.transcript.length}`);
          this.emit('user_transcript', ev.item.transcript);
        }
        break;
      case 'response.created':
        // response has begun; reset per-response audio buffer
        this._pcmChunks = [];
        this._responseStart = performance.now();
        break;
      case 'response.output_audio.delta':
        // base64 PCM16 (rate = outputFormat.sample_rate or default 24k)
        if (ev.delta) {
          const buf = Buffer.from(ev.delta, 'base64');
          this._pcmChunks.push(buf);
          this.emit('audio_delta', buf);
        }
        break;
      case 'response.output_audio_transcript.delta':
        // streaming text transcript; accumulate
        if (ev.delta && this._currentTranscript == null) this._currentTranscript = '';
        if (ev.delta) {
          this._currentTranscript = (this._currentTranscript || '') + ev.delta;
        }
        break;
      case 'response.output_audio_transcript.done':
        this.transcriptCount++;
        const text = ev.transcript || this._currentTranscript || '';
        this._currentTranscript = null;
        log('METRIC transcript', `turn_id=${this.turnCounter} role=assistant bytes=${text.length}`);
        log('xai transcript [assistant]:', text);
        this._transcripts.push({ role: 'assistant', text });
        this.emit('assistant_transcript', text);
        break;
      case 'response.function_call_arguments.delta':
        // accumulating
        if (this._fcArgs == null) this._fcArgs = '';
        if (ev.delta) this._fcArgs += ev.delta;
        break;
      case 'response.function_call_arguments.done':
        // xAI emits at the TOP LEVEL of the event
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
        // end of an output item (audio, function_call, etc.)
        break;
      case 'response.done':
        // entire response is complete; flush the audio buffer
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

  // appendAudio: feed a PCMU buffer chunk to xAI as base64 in
  // input_audio_buffer.append. Returns once written.
  appendAudio(pcmuBytes) {
    if (!this._sessionReady) return;
    this._send({
      type: 'input_audio_buffer.append',
      audio: pcmuBytes.toString('base64'),
    });
  }

  // commitAudio: tell xAI to finalize the buffer and produce a
  // response. Used when we want to force turn end without waiting
  // for VAD silence detection.
  commitAudio() {
    this._send({ type: 'input_audio_buffer.commit' });
    this._send({ type: 'response.create' });
  }

  // sendFunctionResult: respond to a function_call with a synthetic
  // result. Uses the latest call_id captured from
  // function_call_arguments.done.
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
