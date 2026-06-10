// src/vendors/transport/livekit.js — LiveKit Cloud / self-hosted transport.
//
// STATUS: SKELETON. LiveKit is a WebRTC-based real-time transport with
// sub-100ms p50 latency, jitter buffer, and built-in audio codecs
// (Opus at 48 kHz). It's the most-used transport in the livekit
// production landscape (Daily, Vapi, etc. all build on WebRTC).
//
// Reference: https://docs.livekit.io/home/
//
// Why it's in the spike: even if we keep xAI as the LLM+TTS, the
// worker <-> caller transport is a separate decision. The current
// xai-phone-worker path goes:
//
//   Telnyx gateway (PCMU 8 kHz) -> production gateway (TBD) -> worker
//
// A LiveKit-based path would be:
//
//   LiveKit Cloud (Opus 48 kHz) -> LiveKit Agent SDK -> worker
//
// The win is on the transport side: sub-100ms vs ~200ms+ for the
// gateway-mediated path. Smaller win than the STT latency fix, but
// stacks.
//
// Browser path was NO-GO (commit 0e5d259: LiveKit Go SDK outbound
// Opus transport + browser SDP doesn't support L16). Phone path with
// LiveKit is unproven as of 2026-06-09.
//
// When the user gives the go-ahead:
//   1. npm install @livekit/agents
//   2. Set LIVEKIT_URL, LIVEKIT_API_KEY, LIVEKIT_API_SECRET in .env
//   3. Test against a Telnyx Programmable Voice call that bridges
//      to a LiveKit room
//   4. If it works, swap FileLoopbackTransport for LiveKitTransport
//      in the production worker

import { EventEmitter } from 'node:events';
import type { TransportFrame, TransportVendor, VendorName } from '../contracts.ts';

const VENDOR: VendorName = 'livekit';

interface LiveKitConfig {
  url: string;             // 'wss://your-project.livekit.cloud'
  api_key: string;
  api_secret: string;
  room_name: string;
  participant_identity: string;  // the worker's identity
  /** Subscribe to the caller's audio track only. */
  track_subscribe?: 'caller' | 'all';
}

export class LiveKitTransport extends EventEmitter implements TransportVendor {
  readonly name: VendorName = VENDOR;
  private cfg: LiveKitConfig;
  // private _room: any = null;  // Room from 'livekit-client' (browser) or 'livekit-server-sdk' (Node)
  private _frameInCount = 0;
  private _frameOutCount = 0;

  constructor(cfg: LiveKitConfig) {
    super();
    this.cfg = cfg;
  }

  async connect(): Promise<void> {
    // TODO: implement when @livekit/server-sdk or @livekit/agents is installed.
    // Sketch:
    //   const { RoomServiceClient, Room } = await import('livekit-server-sdk');
    //   const room = new Room();
    //   await room.connect(this.cfg.url, await this._generateToken());
    //   room.on('trackSubscribed', (track) => {
    //     if (track.kind === 'audio') {
    //       track.on('data', (pcm) => {
    //         this._frameInCount++;
    //         this.emit('frame', { pcm16: Buffer.from(pcm), sample_rate: 48000, delta_ms: 0, emitted_at_ms: Date.now() });
    //       });
    //     }
    //   });
    //   this._room = room;
    throw new Error('LiveKitTransport.connect: not yet implemented. Install livekit-server-sdk and fill in.');
  }

  onFrame(cb: (frame: TransportFrame) => void): void {
    this.on('frame', cb);
  }

  write(pcm16: Buffer, sample_rate: 16000 | 24000): void {
    // TODO: publish PCM16 to a LiveKit track.
    // LiveKit wants Opus-encoded audio; you can either:
    //   (a) pre-encode to Opus (e.g. via @livekit/opus or ffmpeg) and publish
    //   (b) use a local audio source that takes PCM and encodes internally
    // The Opus path is the recommended one; it's what every other
    // LiveKit agent does.
    this._frameOutCount++;
  }

  async close(): Promise<void> {
    // TODO: room.disconnect()
  }

  snapshot() {
    return {
      frames_in: this._frameInCount,
      frames_out: this._frameOutCount,
      dropped_in: 0,
      dropped_out: 0,
      p50_delta_ms: 0,
      p95_delta_ms: 0,
      jitter_ms: 0,
    };
  }
}

// --- Cost notes ---
//
//   LiveKit Cloud free tier:    1,000 participant-minutes/mo
//   LiveKit Cloud pay-as-you-go: $0.004/participant-min
//   Self-hosted:                free (your own infra)
//
//   At 50 calls/day x 3 min = 150 min/day = 4,500 min/mo
//     Pay-as-you-go: $18/mo
//     Self-hosted:   $0 + VPS CPU
//
//   Compared to file-based rehearsal ($0), LiveKit adds $18-30/mo.
//   The latency win (~100-150ms) is on top of the STT fix.
