// src/providers/index.js — provider factory.
//
// The dispatcher doesn't care whether the providers are fakes or
// real adapters. The factory in this file is the only thing that
// knows the difference. Switch from fakes to real by setting
// USE_REAL_PROVIDERS=1 (and providing the API keys).
//
// Usage:
//   import { getProviders } from './providers/index.js';
//   const { availability, deposit, booking, managerQueue } = getProviders();

import { makeFakeResDiary } from './fake/resdiary-fake.js';
import { makeFakeDepos } from './fake/depos-fake.js';
import { makeFakeManagerQueue } from './fake/manager-queue-fake.js';
import { makeResDiaryAdapter } from './real/resdiary-adapter.js';
import { makeDeposAdapter } from './real/depos-adapter.js';
import { makeManagerQueueAdapter } from './real/manager-queue-adapter.js';

let cached = null;

/**
 * @returns {{
 *   availability: import('../providers.ts').AvailabilityProvider,
 *   deposit: import('../providers.ts').DepositProvider,
 *   booking: import('../providers.ts').BookingProvider,
 *   managerQueue: import('../providers.ts').ManagerQueueProvider,
 *   kind: 'fake' | 'real',
 * }}
 */
export function getProviders() {
  if (cached) return cached;
  const useReal = process.env.USE_REAL_PROVIDERS === '1';
  if (useReal) {
    const resdiary = makeResDiaryAdapter();
    const depos = makeDeposAdapter();
    const managerQ = makeManagerQueueAdapter();
    cached = {
      availability: resdiary.availability,
      deposit: depos.deposit,
      booking: resdiary.booking,
      managerQueue: managerQ.managerQueue,
      kind: 'real',
    };
  } else {
    const resdiary = makeFakeResDiary();
    const depos = makeFakeDepos();
    const managerQ = makeFakeManagerQueue();
    cached = {
      availability: resdiary.availability,
      deposit: depos.deposit,
      booking: resdiary.booking,
      managerQueue: managerQ.managerQueue,
      kind: 'fake',
      // Test-only accessors, namespaced so they don't collide.
      _internal: {
        resdiary: resdiary._internal,
        depos: depos._internal,
        managerQueue: managerQ._internal,
      },
    };
  }
  return cached;
}

/** Reset the cache (test-only). */
export function resetProviders() {
  cached = null;
}
