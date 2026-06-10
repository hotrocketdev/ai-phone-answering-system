// src/vendors/memory/redis-mem.js — in-memory stub for Redis.
//
// STATUS: WORKING. Same interface as redis.js but uses a Map in
// process memory. Used for contract tests, CI, and local rehearsal
// without a Redis instance.

import { EventEmitter } from 'node:events';

const VENDOR = 'redis-mem';

/**
 * @typedef {Object} Entry
 * @property {import('../contracts.ts').CallerContext} ctx
 * @property {number} expires_at_ms
 */

export class RedisMemMemory extends EventEmitter {
  /** @type {string} */
  name = VENDOR;
  /** @type {Map<string, Entry>} */
  _store = new Map();
  /** @type {Map<string, Array<{ ts_ms: number; summary: string }>>} */
  _histories = new Map();
  /** @type {number} */
  _defaultTtlS;

  /** @param {{ default_ttl_s?: number }} [opts] */
  constructor(opts = {}) {
    super();
    this._defaultTtlS = opts.default_ttl_s ?? 60 * 60 * 24 * 90;
  }

  /** @param {string} phone */
  async get(phone) {
    this._gc();
    const entry = this._store.get(phone);
    if (!entry) return null;
    return entry.ctx;
  }

  /** @param {string} phone @param {import('../contracts.ts').CallerContext} ctx @param {number} [ttl_seconds] */
  async set(phone, ctx, ttl_seconds) {
    const ttl = ttl_seconds ?? this._defaultTtlS;
    this._store.set(phone, {
      ctx,
      expires_at_ms: Date.now() + ttl * 1000,
    });
  }

  /** @param {string} phone @param {{ ts_ms: number; summary: string }} entry */
  async append(phone, entry) {
    let arr = this._histories.get(phone);
    if (!arr) {
      arr = [];
      this._histories.set(phone, arr);
    }
    arr.push(entry);
  }

  async close() {
    this._store.clear();
    this._histories.clear();
  }

  _gc() {
    const now = Date.now();
    for (const [k, v] of this._store) {
      if (v.expires_at_ms < now) this._store.delete(k);
    }
  }
}
