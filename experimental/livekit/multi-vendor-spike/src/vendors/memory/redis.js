// src/vendors/memory/redis.js — Redis caller-context memory adapter.
//
// STATUS: PARTIAL. The redis adapter uses ioredis, which we'll install
// when the user gives the go-ahead. The interface is the same as the
// in-memory stub (memory/redis-mem.js) so the orchestrator can run
// against either.
//
// Why it matters: the receptionist says "Welcome back, George" instead
// of "What's your name?" when it has the caller's history. This
// doesn't reduce first-audio latency, but it dramatically improves
// perceived human-likeness on repeat calls.
//
// We already have Redis on the production VPS (used by the gateway),
// so adding a small client library is free.

import { EventEmitter } from 'node:events';

const VENDOR = 'redis';
const KEY_PREFIX = 'voxlane:caller:';
const DEFAULT_TTL_S = 60 * 60 * 24 * 90;  // 90 days

/**
 * @typedef {Object} RedisConfig
 * @property {string} url
 * @property {string} [password]
 * @property {string} [tenant]
 * @property {number} [default_ttl_s]
 */

export class RedisMemory extends EventEmitter {
  /** @type {string} */
  name = VENDOR;
  /** @type {Required<RedisConfig>} */
  cfg;
  /** @type {any} */
  _client = null;  // Redis from 'ioredis'

  /** @param {RedisConfig} cfg */
  constructor(cfg) {
    super();
    this.cfg = {
      url: cfg.url,
      password: cfg.password || '',
      tenant: cfg.tenant || 'default',
      default_ttl_s: cfg.default_ttl_s ?? DEFAULT_TTL_S,
    };
  }

  async connect() {
    // TODO: implement when user gives go-ahead and ioredis is installed.
    // Sketch:
    //   const { default: Redis } = await import('ioredis');
    //   this._client = new Redis({
    //     host: this._url.host,
    //     port: this._url.port,
    //     password: this.cfg.password || undefined,
    //     tls: this._url.protocol === 'rediss:' ? {} : undefined,
    //   });
    throw new Error('RedisMemory.connect: not yet implemented. Install ioredis and fill in.');
  }

  /** @param {string} phone */
  async get(phone) {
    if (!this._client) throw new Error('RedisMemory: not connected');
    const raw = await this._client.get(this._key(phone));
    return raw ? JSON.parse(raw) : null;
  }

  /** @param {string} phone @param {import('../contracts.ts').CallerContext} ctx @param {number} [ttl_seconds] */
  async set(phone, ctx, ttl_seconds) {
    if (!this._client) throw new Error('RedisMemory: not connected');
    const key = this._key(phone);
    const value = JSON.stringify(ctx);
    if (ttl_seconds) {
      await this._client.set(key, value, 'EX', ttl_seconds);
    } else {
      await this._client.set(key, value, 'EX', this.cfg.default_ttl_s);
    }
  }

  /** @param {string} phone @param {{ ts_ms: number; summary: string }} entry */
  async append(phone, entry) {
    if (!this._client) throw new Error('RedisMemory: not connected');
    const key = this._key(phone) + ':history';
    await this._client.rpush(key, JSON.stringify(entry));
    await this._client.expire(key, this.cfg.default_ttl_s);
  }

  async close() {
    if (this._client) {
      try { await this._client.quit(); } catch (_) { /* ignore */ }
      this._client = null;
    }
  }

  /** @param {string} phone */
  _key(phone) {
    return `${KEY_PREFIX}${this.cfg.tenant}:${phone}`;
  }
}

// --- Cost notes ---
//
//   Self-hosted (production VPS):  $0/month  (Redis is already running)
//   Upstash:                       free tier covers us
//   Redis Cloud:                   $5-15/month
//
//   The win is UX, not latency. "Welcome back, George" vs "What's your
//   name?" is the difference between a tool and a receptionist.
