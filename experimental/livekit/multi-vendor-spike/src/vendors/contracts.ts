// src/vendors/contracts.ts — frozen vendor contracts.
//
// Single source of truth for the multi-vendor spike. The vendor adapters
// (src/vendors/stt/*, llm/*, tts/*, transport/*, memory/*) all implement
// these interfaces. Swapping vendors is a config change.
//
// DO NOT BREAK COMPATIBILITY WITH THIS FILE WITHOUT USER APPROVAL.
// If a vendor doesn't fit, add a new interface, don't change this one.

export type VendorName = string;  // 'deepgram' | 'xai' | 'elevenlabs' | 'cerebras' | 'livekit' | 'redis' | etc.

export interface VendorTiming {
  /** Time from request start to first useful byte (ms). */
  first_byte_ms: number;
  /** Total request duration (ms). */
  total_ms: number;
  /** Provider's own reported latency if available (ms). */
  provider_self_reported_ms?: number;
}

export interface VendorCallStats {
  calls: number;
  errors: number;
  timeouts: number;
  first_byte_p50_ms: number;
  first_byte_p95_ms: number;
  first_byte_p99_ms: number;
  total_p50_ms: number;
}

// --- STT (Speech-to-Text) -----------------------------------------------

export interface SttRequest {
  /** PCM16 mono audio. The transport layer decides the sample rate; the
   *  STT vendor resamples internally if needed. */
  pcm16_mono: Buffer;
  sample_rate: 8000 | 16000 | 24000 | 48000;
  language: 'en-GB' | 'en-US' | string;
  /** Streaming mode: emit partial transcripts as audio arrives. */
  streaming: boolean;
  /** Endpointing: ms of silence before declaring end-of-utterance. */
  endpointing_ms: number;
}

export interface SttPartial {
  text: string;
  is_final: boolean;
  /** Confidence 0-1, if vendor reports it. */
  confidence?: number;
}

export interface SttResult {
  text: string;
  language_detected?: string;
  duration_ms: number;
  vendor_self_latency_ms?: number;
}

export interface SttVendor {
  readonly name: VendorName;
  /** Streaming mode: emits SttPartial as audio is fed. */
  startStream(req: Omit<SttRequest, 'pcm16_mono'>): AsyncIterable<SttPartial>;
  /** One-shot mode: returns the final transcript for the whole audio. */
  transcribe(req: SttRequest): Promise<SttResult>;
  close(): Promise<void>;
}

// --- LLM (text reasoning + tool calling) --------------------------------

export interface LlmMessage {
  role: 'system' | 'user' | 'assistant' | 'tool';
  content: string;
  /** For tool messages: the tool call id. */
  tool_call_id?: string;
  /** For assistant messages: tool calls to make. */
  tool_calls?: LlmToolCall[];
}

export interface LlmTool {
  type: 'function';
  function: {
    name: string;
    description: string;
    parameters: Record<string, any>;  // JSON schema
  };
}

export interface LlmToolCall {
  id: string;
  type: 'function';
  function: {
    name: string;
    arguments: string;  // JSON string
  };
}

export interface LlmRequest {
  model: string;
  messages: LlmMessage[];
  tools?: LlmTool[];
  tool_choice?: 'auto' | 'none' | { type: 'function'; function: { name: string } };
  temperature?: number;
  max_tokens?: number;
  /** If true, stream partial tokens. */
  stream: boolean;
}

export interface LlmToken {
  type: 'text' | 'tool_call' | 'done';
  text?: string;
  tool_call?: LlmToolCall;
  finish_reason?: string;
}

export interface LlmResult {
  text: string;
  tool_calls: LlmToolCall[];
  finish_reason: 'stop' | 'tool_calls' | 'length' | 'content_filter';
  usage?: {
    prompt_tokens: number;
    completion_tokens: number;
    total_tokens: number;
  };
}

export interface LlmVendor {
  readonly name: VendorName;
  complete(req: LlmRequest): Promise<LlmResult>;
  stream(req: LlmRequest): AsyncIterable<LlmToken>;
  close(): Promise<void>;
}

// --- TTS (Text-to-Speech) ------------------------------------------------

export interface TtsRequest {
  text: string;
  voice_id: string;       // 'eve' (xAI) | 'EXAVITQu4vr4xnSDxMaL' (ElevenLabs Bella) | etc.
  model?: string;         // 'sonic-3.5' | 'eleven_turbo_v2_5' | etc.
  output_format: 'pcm16_24k' | 'pcm16_16k' | 'pcm_f32le_24k';
  /** Voice characteristics (vendor-specific). */
  voice_settings?: Record<string, any>;
  /** Streaming: emit audio chunks as they're generated. */
  streaming: boolean;
}

export interface TtsAudioChunk {
  pcm: Buffer;
  sample_rate: 16000 | 24000 | 48000;
  is_final: boolean;
}

export interface TtsVendor {
  readonly name: VendorName;
  synthesize(req: TtsRequest): Promise<Buffer>;
  stream(req: TtsRequest): AsyncIterable<TtsAudioChunk>;
  close(): Promise<void>;
}

// --- Transport (caller <-> worker) --------------------------------------

export interface TransportFrame {
  /** Inbound: PCM16 from the caller. Outbound: PCM16 to the caller. */
  pcm16: Buffer;
  sample_rate: 8000 | 16000 | 24000;
  /** Time since the previous frame (ms). */
  delta_ms: number;
  /** Wall-clock time when the frame was emitted (ms since epoch). */
  emitted_at_ms: number;
}

export interface TransportVendor {
  readonly name: VendorName;
  /** Open the transport. Resolves when the caller has connected. */
  connect(): Promise<void>;
  /** Inbound: caller audio as it arrives. */
  onFrame(cb: (frame: TransportFrame) => void): void;
  /** Outbound: write PCM16 to the caller. */
  write(pcm16: Buffer, sample_rate: 16000 | 24000): void;
  /** Close the transport. */
  close(): Promise<void>;
  /** Stats: emit a snapshot of transport-level metrics. */
  snapshot(): {
    frames_in: number;
    frames_out: number;
    dropped_in: number;
    dropped_out: number;
    p50_delta_ms: number;
    p95_delta_ms: number;
    jitter_ms: number;
  };
}

// --- Memory (caller context, booking history) ----------------------------

export interface CallerContext {
  /** Phone number, E.164 format. */
  phone: string;
  /** Last-known customer name (if any). */
  name?: string;
  /** Recent bookings. */
  recent_bookings?: Array<{
    date: string;
    time: string;
    party_size: number;
    confirmation_code: string;
  }>;
  /** Tenant preferences. */
  preferences?: Record<string, any>;
  /** Last interaction summary. */
  last_call_summary?: string;
}

export interface MemoryVendor {
  readonly name: VendorName;
  get(phone: string): Promise<CallerContext | null>;
  set(phone: string, ctx: CallerContext, ttl_seconds?: number): Promise<void>;
  /** Append a transcript or summary to the caller's history. */
  append(phone: string, entry: { ts_ms: number; summary: string }): Promise<void>;
  close(): Promise<void>;
}

// --- Bundle (a complete voice pipeline configuration) ---------------------

export interface VendorBundle {
  name: string;            // 'xai' | 'hybrid-deepgram-xai-eve' | 'matrix' | ...
  description: string;
  stt: SttVendor;
  llm: LlmVendor;
  tts: TtsVendor;
  transport: TransportVendor;
  memory: MemoryVendor;
  /** Cost estimate per 1-minute call. */
  cost_per_min_usd: number;
  /** Expected first-audio latency window (ms). */
  expected_first_audio_ms: { p50: number; p95: number };
}

// --- Result type (for fallible vendor calls) -----------------------------

export type Result<T, E = VendorError> =
  | { ok: true; value: T; timing?: VendorTiming }
  | { ok: false; error: E };

export interface VendorError {
  code: 'timeout' | 'rate_limit' | 'auth' | 'bad_request' | 'server_error' | 'network' | 'unknown';
  message: string;
  retryable: boolean;
  vendor: VendorName;
}
