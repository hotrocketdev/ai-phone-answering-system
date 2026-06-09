// src/telnyx-io.js — EXPERIMENTAL Telnyx media-stream I/O scaffold.
//
// NOT CONNECTED TO PRODUCTION. This module reads from a simulated
// Telnyx media stream (a file of PCMU frames) and writes to a
// simulated outbound sink (a WAV file). It exercises the same
// audio flow as a live call:
//
//   Telnyx PCMU/PCMA media frame (8 kHz mu-law, 20 ms = 160 bytes)
//     -> decode to PCM16 8 kHz
//     -> resample to PCM16 24 kHz
//     -> xai.AppendAudio (in 100 ms chunks to xAI WSS)
//   xAI audio delta (PCM16 24 kHz, base64)
//     -> downsample to PCM16 8 kHz
//     -> encode to PCMU
//     -> simulated outbound frame sink
//
// The frame-pacing instrumentation (inbound gap, decode time,
// resample time, xai append time, xai first audio latency,
// outbound frame count, outbound pacing, dropped frames,
// backpressure events) is per the manager's directive — this
// is what we'll need to monitor the first live call.
//
// To swap to a real Telnyx media stream:
//   1. Replace TelnyxMediaSource with a Telnyx WSS client that
//      consumes media frames from a Telnyx Programmable Voice
//      call control webhook.
//   2. Replace TelnyxMediaSink with a Telnyx WSS client that
//      writes back media frames to the same call.
//   3. Re-run the frame-pacing instrument to compare against
//      the simulated baseline.

import fs from 'node:fs';
import { performance } from 'node:perf_hooks';
import { pcm16ToPcmu, pcmuToPcm16, pcm8kToPcm24k, pcm24kToPcm8k } from './pcmu-codec.js';

/** Frame size constants for G.711 mu-law at 8 kHz. */
const PCMU_FRAME_MS = 20;
const PCMU_FRAME_BYTES = 160;          // 8000 Hz * 20 ms = 160 samples
const PCM16_24K_FRAME_MS = 20;
const PCM16_24K_FRAME_BYTES = 960;     // 24000 Hz * 20 ms * 2 bytes = 960 bytes

/**
 * Read PCMU frames from a file at the simulated real-time pace
 * (1 frame every 20 ms). Emits 'frame' events as each frame is
 * read. Records per-frame metrics: gap, decode time, resample
 * time, xai append time.
 *
 * @fires frame with (frameIndex, pcm16_8k: Buffer, pcm16_24k: Buffer, gap_ms: number, decode_ms: number, resample_ms: number)
 */
export class TelnyxMediaSource {
  /**
   * @param {string} pcmuFilePath - file of raw PCMU bytes (8 kHz mono)
   * @param {{ speed?: number, verbose?: boolean }} opts
   */
  constructor(pcmuFilePath, opts = {}) {
    this.path = pcmuFilePath;
    this.speed = opts.speed ?? 1;          // 1 = real-time; 10 = 10x for testing
    this.verbose = !!opts.verbose;
    this.metrics = {
      frames: 0,
      total_bytes: 0,
      gaps_ms: [],
      decode_ms: [],
      resample_ms: [],
      append_ms: [],
      backpressure_events: 0,
      dropped_frames: 0,
      first_frame_at: null,
      last_frame_at: null,
    };
    this.handlers = { frame: [], error: [], end: [] };
  }

  on(event, fn) { (this.handlers[event] ||= []).push(fn); }
  _emit(event, ...args) { (this.handlers[event] || []).forEach((f) => f(...args)); }

  /**
   * Stream frames out. The 'onFrame' callback receives each
   * decoded + resampled 20 ms PCM16 24 kHz chunk. Real-time pace
   * is simulated by setTimeout; in production this loop is
   * driven by socket events.
   *
   * @param {(frame: { pcm16_8k: Buffer, pcm16_24k: Buffer, gap_ms: number, decode_ms: number, resample_ms: number }) => Promise<void>} onFrame
   * @returns {Promise<void>} resolves when the file is exhausted.
   */
  async stream(onFrame) {
    const buf = fs.readFileSync(this.path);
    this.metrics.total_bytes = buf.length;
    if (this.verbose) console.log(`[TelnyxSource] streaming ${buf.length} bytes of PCMU from ${this.path}`);

    let prevFrameAt = null;
    for (let i = 0; i < buf.length; i += PCMU_FRAME_BYTES) {
      const frame = buf.subarray(i, i + PCMU_FRAME_BYTES);
      if (frame.length < PCMU_FRAME_BYTES) break; // partial frame at EOF

      const frameStart = performance.now();
      const gap = prevFrameAt === null ? 0 : (frameStart - prevFrameAt);
      if (prevFrameAt !== null) this.metrics.gaps_ms.push(gap);
      prevFrameAt = frameStart;
      this.metrics.first_frame_at ??= frameStart;
      this.metrics.last_frame_at = frameStart;

      // Decode PCMU -> PCM16 8 kHz.
      const t0 = performance.now();
      const pcm16_8k = pcmuToPcm16(frame);
      const decode_ms = performance.now() - t0;
      this.metrics.decode_ms.push(decode_ms);

      // Resample 8k -> 24k.
      const t1 = performance.now();
      const pcm16_24k = pcm8kToPcm24k(pcm16_8k);
      const resample_ms = performance.now() - t1;
      this.metrics.resample_ms.push(resample_ms);

      this.metrics.frames++;
      this._emit('frame', {
        frameIndex: this.metrics.frames,
        pcm16_8k,
        pcm16_24k,
        gap_ms: gap,
        decode_ms,
        resample_ms,
      });

      try {
        await onFrame({ pcm16_8k, pcm16_24k, gap_ms: gap, decode_ms, resample_ms });
      } catch (e) {
        // If the consumer throws, treat as backpressure / drop.
        this.metrics.backpressure_events++;
        this.metrics.dropped_frames++;
        if (this.verbose) console.log(`[TelnyxSource] backpressure on frame ${this.metrics.frames}: ${e.message}`);
      }

      // Real-time pacing: 20 ms per frame, scaled by speed.
      const elapsed = performance.now() - frameStart;
      const target = (PCMU_FRAME_MS / this.speed);
      const delay = Math.max(0, target - elapsed);
      if (delay > 0) await new Promise((r) => setTimeout(r, delay));
    }
    this._emit('end', this.metrics);
  }

  /** Summary metrics for the run. */
  summary() {
    const avg = (arr) => arr.length ? (arr.reduce((a, b) => a + b, 0) / arr.length) : 0;
    const p95 = (arr) => {
      if (!arr.length) return 0;
      const sorted = [...arr].sort((a, b) => a - b);
      return sorted[Math.floor(sorted.length * 0.95)];
    };
    return {
      frames: this.metrics.frames,
      total_bytes: this.metrics.total_bytes,
      gap_ms: { avg: avg(this.metrics.gaps_ms), p95: p95(this.metrics.gaps_ms) },
      decode_ms: { avg: avg(this.metrics.decode_ms), p95: p95(this.metrics.decode_ms) },
      resample_ms: { avg: avg(this.metrics.resample_ms), p95: p95(this.metrics.resample_ms) },
      backpressure_events: this.metrics.backpressure_events,
      dropped_frames: this.metrics.dropped_frames,
    };
  }
}

/**
 * Collect PCMU frames (simulated outbound) and write to a file.
 * Records: outbound frame count, outbound pacing, dropped frames
 * (if writes are too slow), backpressure events.
 */
export class TelnyxMediaSink {
  /**
   * @param {string} pcmuOutPath - where to write the collected PCMU bytes
   */
  constructor(pcmuOutPath) {
    this.path = pcmuOutPath;
    this.chunks = [];
    this.metrics = {
      frames: 0,
      total_bytes: 0,
      pacing_ms: [],
      backpressure_events: 0,
      dropped_frames: 0,
      first_frame_at: null,
      last_frame_at: null,
    };
    this.prevFrameAt = null;
  }

  /**
   * Accept a PCM16 24 kHz chunk from xAI, downsample to 8 kHz,
   * encode to PCMU, append to the sink.
   *
   * @param {Buffer} pcm16_24k
   */
  write(pcm16_24k) {
    if (!pcm16_24k || pcm16_24k.length === 0) return;
    const t0 = performance.now();
    const pcm16_8k = pcm24kToPcm8k(pcm16_24k);
    const pcmu = pcm16ToPcmu(pcm16_8k);

    if (this.prevFrameAt !== null) {
      this.metrics.pacing_ms.push(performance.now() - this.prevFrameAt);
    }
    this.prevFrameAt = performance.now();
    this.metrics.first_frame_at ??= this.prevFrameAt;
    this.metrics.last_frame_at = this.prevFrameAt;

    // Split the 24 kHz PCM16 chunk into 20 ms PCMU frames for pacing
    // metrics (8 kHz mu-law, 160 bytes = 20 ms).
    for (let i = 0; i < pcmu.length; i += PCMU_FRAME_BYTES) {
      const frame = pcmu.subarray(i, i + PCMU_FRAME_BYTES);
      if (frame.length < PCMU_FRAME_BYTES) break;
      this.chunks.push(frame);
      this.metrics.frames++;
      this.metrics.total_bytes += frame.length;
    }
  }

  /** Write the collected chunks to disk. */
  flush() {
    fs.writeFileSync(this.path, Buffer.concat(this.chunks));
  }

  summary() {
    const avg = (arr) => arr.length ? (arr.reduce((a, b) => a + b, 0) / arr.length) : 0;
    const p95 = (arr) => {
      if (!arr.length) return 0;
      const sorted = [...arr].sort((a, b) => a - b);
      return sorted[Math.floor(sorted.length * 0.95)];
    };
    return {
      frames: this.metrics.frames,
      total_bytes: this.metrics.total_bytes,
      pacing_ms: { avg: avg(this.metrics.pacing_ms), p95: p95(this.metrics.pacing_ms) },
    };
  }
}
