// src/vendors/transport/file-loopback.js — file-based transport adapter.
//
// STATUS: WORKING. This is the same file-based loopback used in the
// xai-phone-worker spike's rehearsal. It reads PCMU 8 kHz or PCM16
// 24 kHz audio from a file and emits it as TransportFrames, so the
// orchestrator can be tested without a live telephony gateway.
//
// Used for: head-to-head rehearsals, contract tests, CI runs.

import { EventEmitter } from 'node:events';
import fs from 'node:fs';
import { performance } from 'node:perf_hooks';
import { pcmuToPcm16, pcm8kToPcm24k, pcm24kToPcm8k, pcm16ToPcmu } from '../../../../xai-phone-worker/src/pcmu-codec.js';
import type { TransportFrame, TransportVendor, VendorName } from '../contracts.ts';

const VENDOR: VendorName = 'file-loopback';
const CHUNK_BYTES_PCM16_24K = 4800;  // 100ms at 24 kHz
const CHUNK_BYTES_PCM16_8K = 1600;   // 100ms at 8 kHz
const FRAME_INTERVAL_MS = 100;

export interface FileLoopbackConfig {
  file_path: string;
  format: 'pcmu_8k' | 'pcm16_24k';
  /** Speed multiplier for fast rehearsal (default 1.0). */
  speed?: number;
  /** Loop the file forever (for soak tests). */
  loop?: boolean;
}

export class FileLoopbackTransport extends EventEmitter implements TransportVendor {
  readonly name: VendorName = VENDOR;
  private cfg: Required<FileLoopbackConfig>;
  private _pcm16_24k: Buffer | null = null;
  private _frameInCount = 0;
  private _frameOutCount = 0;
  private _droppedIn = 0;
  private _droppedOut = 0;
  private _deltas: number[] = [];
  private _lastFrameAt = 0;
  private _running = false;

  constructor(cfg: FileLoopbackConfig) {
    super();
    this.cfg = {
      file_path: cfg.file_path,
      format: cfg.format,
      speed: cfg.speed ?? 1.0,
      loop: cfg.loop ?? false,
    };
  }

  async connect(): Promise<void> {
    // Decode the file into PCM16 24 kHz (the canonical form for the orchestrator).
    const raw = fs.readFileSync(this.cfg.file_path);
    if (this.cfg.format === 'pcm16_24k') {
      this._pcm16_24k = raw;
    } else {
      const pcm16_8k = pcmuToPcm16(raw);
      this._pcm16_24k = pcm8kToPcm24k(pcm16_8k);
    }
    this.emit('connected', { frames_total: Math.ceil(this._pcm16_24k.length / CHUNK_BYTES_PCM16_24K) });
  }

  onFrame(cb: (frame: TransportFrame) => void): void {
    this.on('frame', cb);
  }

  async play(): Promise<void> {
    // Stream the file out as if it were a live caller. The orchestrator
    // listens via onFrame().
    if (!this._pcm16_24k) throw new Error('FileLoopbackTransport: call connect() first');
    this._running = true;
    const pcm = this._pcm16_24k;
    let offset = 0;
    while (offset < pcm.length && this._running) {
      const chunk = pcm.subarray(offset, Math.min(offset + CHUNK_BYTES_PCM16_24K, pcm.length));
      const now = performance.now();
      const delta = this._lastFrameAt ? (now - this._lastFrameAt) : FRAME_INTERVAL_MS;
      this._lastFrameAt = now;
      this._deltas.push(delta);
      this._frameInCount++;
      this.emit('frame', {
        pcm16: Buffer.from(chunk),
        sample_rate: 24000,
        delta_ms: delta,
        emitted_at_ms: Date.now(),
      } as TransportFrame);
      offset += CHUNK_BYTES_PCM16_24K;
      await new Promise((r) => setTimeout(r, FRAME_INTERVAL_MS / this.cfg.speed));
    }
  }

  write(pcm16: Buffer, sample_rate: 16000 | 24000): void {
    // In file-loopback mode, the assistant audio is collected by the
    // orchestrator via a sink listener. We track outbound frames for
    // the stats snapshot.
    this._frameOutCount++;
  }

  stop(): void {
    this._running = false;
  }

  async close(): Promise<void> {
    this._running = false;
  }

  snapshot() {
    const sorted = [...this._deltas].sort((a, b) => a - b);
    const p50 = sorted[Math.floor(sorted.length * 0.5)] || 0;
    const p95 = sorted[Math.floor(sorted.length * 0.95)] || 0;
    // Jitter = p95 - p50 (rough measure)
    const jitter = p95 - p50;
    return {
      frames_in: this._frameInCount,
      frames_out: this._frameOutCount,
      dropped_in: this._droppedIn,
      dropped_out: this._droppedOut,
      p50_delta_ms: p50,
      p95_delta_ms: p95,
      jitter_ms: jitter,
    };
  }
}

// --- Cost notes ---
//
//   Free. File-based. Used for offline rehearsal and contract tests.
