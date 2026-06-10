// src/vendors/memory/redis-mem.js — in-memory stub for Redis.
//
// STATUS: WORKING. Same interface as redis.js but uses a Map in
// process memory. Used for contract tests, CI, and local rehearsal
// without a Redis instance.

import { EventEmitter } from 'node:events';
import type { CallerContext, MemoryVendor, VendorName } from '../contracts.ts';

const VENDOR: VendorName = 'redis-mem';

interface Entry {
  ctx: CallerContext;
  expires_at_ms: number;
}

export class RedisMemMemory extends EventEmitter implements MemoryVendor {
  readonly name: VendorName = VENDOR;
  private _store: Map<string, Entry> = new Map();
  private _histories: Map<string, Array<{ ts_ms: number; summary: string }>> = new Map();
  private _defaultTtlS: number;

  constructor(opts: { default_ttl_s?: number } = {}) {
    super();
    this._defaultTtlS = opts.default_ttl_s ?? 60 * 60 * 24 * 90;
  }

  async get(phone: string): Promise<CallerContext | null> {
    this._gc();
    const entry = this._store.get(phone);
    if (!entry) return null;
    return entry.ctx;
  }

  async set(phone: string, ctx: CallerContext, ttl_seconds?: number): Promise<void> {
    const ttl = ttl_seconds ?? this._defaultTtlS;
    this._store.set(phone, {
      ctx,
      expires_at_ms: Date.now() + ttl * 1000,
    });
  }

  async append(phone: string, entry: { ts_ms: number; summary: string }): Promise<void> {
    let arr = this._histories.get(phone);
    if (!arr) {
      arr = [];
      this._histories.set(phone, arr);
    }
    arr.push(entry);
  }

  async close(): Promise<void> {
    this._store.clear();
    this._histories.clear();
  }

  private _gc(): void {
    const now = Date.now();
    for (const [k, v] of this._store) {
      if (v.expires_at_ms < now) this._store.delete(k);
    }
  }
}
