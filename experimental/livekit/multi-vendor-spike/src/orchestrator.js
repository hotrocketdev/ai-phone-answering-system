// src/orchestrator.js — multi-vendor orchestrator.
//
// Wires a VendorBundle's STT + LLM + TTS + transport + memory into
// the full voice pipeline. This is the multi-vendor equivalent of
// xai-phone-worker/src/index.js.
//
// The orchestrator is intentionally vendor-agnostic. The bundle
// config decides which adapters get used. The orchestrator's job:
//
//   1. Open the transport (LiveKit / file loopback / WSS)
//   2. Subscribe to inbound frames
//   3. Feed them to STT (Deepgram streaming / xAI bundled)
//   4. When STT declares end-of-utterance, send transcript to LLM
//   5. When LLM responds with text, send to TTS
//   6. Stream TTS audio back through transport
//   7. On every turn, append to memory (Redis / in-mem)
//
// STATUS: SKELETON. The orchestration logic is sound; the vendor
// adapters' internal calls are TODO. To run a real rehearsal, the
// user has to give the go-ahead for at least one vendor and install
// the corresponding SDK.

import { EventEmitter } from 'node:events';
import { performance } from 'node:perf_hooks';
import type { VendorBundle, SttPartial, LlmResult, TtsAudioChunk } from './vendors/contracts.ts';
import { dispatchToolCall } from '../../../xai-phone-worker/src/dispatcher.js';

const RESTAURANT_INSTRUCTIONS = `You are Alex, the receptionist at Porto Douro Restaurants. Greet the caller warmly and ask how you can help. For a new table booking, collect date, time, party size, name, and phone number. Use availability.check to see if a table is free, then booking.create to confirm. CRITICAL: if a caller asks something you cannot verify (menu, dietary, hours, anything restaurant-specific), call manager.escalate IMMEDIATELY in the same turn — do NOT ask the user 'would you like me to take a message' first, just call the tool. Speak in British English, briefly and warmly. Do not invent restaurant details.`;

const TOOLS = [
  { type: 'function', function: { name: 'availability.check', description: 'Check whether a table is available.' } },
  { type: 'function', function: { name: 'booking.create', description: 'Confirm a table booking.' } },
  { type: 'function', function: { name: 'manager.escalate', description: 'Take a message for the manager to call back.' } },
];

export interface OrchestratorMetrics {
  bundle_name: string;
  started_at_ms: number;
  first_audio_at_ms: number | null;
  first_audio_latency_ms: number | null;
  stt_first_byte_ms: number | null;
  llm_first_token_ms: number | null;
  tts_first_byte_ms: number | null;
  function_calls: number;
  errors: number;
  assistant_chars: number;
}

export class Orchestrator extends EventEmitter {
  private bundle: VendorBundle;
  private conversation: Array<{ role: string; content: string }> = [];
  private metrics: OrchestratorMetrics;
  private _ttsQueue: Buffer[] = [];
  private _running = false;

  constructor(bundle: VendorBundle) {
    super();
    this.bundle = bundle;
    this.metrics = {
      bundle_name: bundle.name,
      started_at_ms: performance.now(),
      first_audio_at_ms: null,
      first_audio_latency_ms: null,
      stt_first_byte_ms: null,
      llm_first_token_ms: null,
      tts_first_byte_ms: null,
      function_calls: 0,
      errors: 0,
      assistant_chars: 0,
    };
  }

  async start() {
    // 1. Open the transport.
    await this.bundle.transport.connect();
    this.emit('transport_connected');

    // 2. Subscribe to inbound frames.
    this.bundle.transport.onFrame(async (frame) => {
      await this._onInboundFrame(frame);
    });

    this._running = true;
  }

  async stop() {
    this._running = false;
    await this.bundle.transport.close();
    await this.bundle.stt.close();
    await this.bundle.llm.close();
    await this.bundle.tts.close();
    await this.bundle.memory.close();
  }

  private async _onInboundFrame(frame: { pcm16: Buffer; sample_rate: number; delta_ms: number; emitted_at_ms: number }) {
    // The orchestrator's per-frame work is vendor-specific. For the
    // hybrid-deepgram bundle, we'd feed the PCM chunk to Deepgram's
    // streaming STT and let it declare end-of-utterance. For the
    // xAI-bundle, the WSS handles everything in one event.
    //
    // For now, we route to the bundle's STT startStream().
    const sttStart = performance.now();
    try {
      for await (const partial of this.bundle.stt.startStream({
        sample_rate: 24000 as 24000,  // 24 kHz canonical
        language: 'en-GB',
        streaming: true,
        endpointing_ms: 250,
      })) {
        if (partial.is_final) {
          if (this.metrics.stt_first_byte_ms === null) {
            this.metrics.stt_first_byte_ms = performance.now() - sttStart;
          }
          this.emit('user_transcript', partial.text);
          await this._handleUserTurn(partial.text);
        } else {
          this.emit('user_transcript_partial', partial.text);
        }
      }
    } catch (e: any) {
      this.metrics.errors++;
      this.emit('error', e);
    }
  }

  private async _handleUserTurn(transcript: string) {
    this.conversation.push({ role: 'user', content: transcript });
    const llmStart = performance.now();
    let firstTokenSeen = false;
    let accumulatedText = '';
    let toolCalls: any[] = [];
    let finishReason = 'stop';

    try {
      const stream = this.bundle.llm.stream({
        model: 'grok-4-fast-non-reasoning',
        messages: [
          { role: 'system', content: RESTAURANT_INSTRUCTIONS },
          ...this.conversation,
        ],
        tools: TOOLS as any,
        tool_choice: 'auto',
        temperature: 0.7,
        stream: true,
      });
      for await (const tok of stream) {
        if (!firstTokenSeen && tok.text) {
          this.metrics.llm_first_token_ms = performance.now() - llmStart;
          firstTokenSeen = true;
        }
        if (tok.text) accumulatedText += tok.text;
        if (tok.tool_call) toolCalls.push(tok.tool_call);
        if (tok.finish_reason) finishReason = tok.finish_reason;
      }
    } catch (e: any) {
      this.metrics.errors++;
      this.emit('error', e);
      return;
    }

    // Handle tool calls.
    for (const tc of toolCalls) {
      this.metrics.function_calls++;
      const args = JSON.parse(tc.function.arguments || '{}');
      try {
        const result = await dispatchToolCall(tc.function.name, args, tc.id);
        this.conversation.push({
          role: 'tool',
          content: JSON.stringify(result),
          tool_call_id: tc.id,
        } as any);
      } catch (e: any) {
        this.metrics.errors++;
        this.conversation.push({
          role: 'tool',
          content: JSON.stringify({ error: e.message }),
          tool_call_id: tc.id,
        } as any);
      }
    }

    // Send text to TTS, stream audio back.
    if (accumulatedText) {
      this.metrics.assistant_chars += accumulatedText.length;
      this.conversation.push({ role: 'assistant', content: accumulatedText });
      const ttsStart = performance.now();
      let firstAudioSeen = false;
      try {
        for await (const chunk of this.bundle.tts.stream({
          text: accumulatedText,
          voice_id: 'eve',
          output_format: 'pcm16_24k',
          streaming: true,
        })) {
          if (!firstAudioSeen) {
            this.metrics.tts_first_byte_ms = performance.now() - ttsStart;
            firstAudioSeen = true;
            if (this.metrics.first_audio_at_ms === null) {
              this.metrics.first_audio_at_ms = performance.now();
              this.metrics.first_audio_latency_ms = performance.now() - this.metrics.started_at_ms;
            }
          }
          this.bundle.transport.write(chunk.pcm, chunk.sample_rate);
        }
      } catch (e: any) {
        this.metrics.errors++;
        this.emit('error', e);
      }
    }
  }

  getMetrics(): OrchestratorMetrics {
    return { ...this.metrics };
  }
}
