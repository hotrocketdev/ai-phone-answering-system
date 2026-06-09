// src/providers.ts — DISPATCHER PROVIDER CONTRACTS
//
// Clean, frozen TypeScript-style interfaces for the four providers
// the booking flow depends on. The dispatcher.js (and any future
// implementations) imports these via JSDoc typedefs (see
// src/dispatcher.js).
//
// Contract principles:
//   - Provider implementations are swappable. The dispatcher does
//     not know whether it's talking to the in-process FakeResDiary
//     or the real ResDiary adapter. They both implement
//     AvailabilityProvider.
//   - Errors are typed, not thrown-as-strings. Each method returns
//     a Result<T, ProviderError> so the caller can branch on
//     machine-readable codes (TOOL_FAILED, RETRYABLE, etc.)
//   - Idempotency keys are part of the contract. ResDiary and
//     Depos both accept a client-supplied idempotency key so a
//     retry from the worker doesn't double-book or double-charge.
//   - No method in any provider mutates state outside its own
//     scope. The dispatcher is the only thing that orchestrates
//     the flow (avail -> hold -> book) and is responsible for
//     compensation if any step fails.

/* eslint-disable @typescript-eslint/no-explicit-any */

// --- Common types -----------------------------------------------------------

/** ISO 8601 date (YYYY-MM-DD) or natural language. */
export type IsoDate = string;
/** 24-hour time HH:MM. */
export type Time24 = string;
/** Integer count of guests. */
export type PartySize = number;
/** E.164 or UK local format. */
export type Phone = string;
/** Free-text customer name. */
export type CustomerName = string;
/** Stable identifier for the caller. Caller-supplied. */
export type IdempotencyKey = string;

/** Provider error categories the dispatcher can branch on. */
export type ProviderErrorCode =
  | 'NOT_FOUND'              // resource (slot, booking, message) not found
  | 'INVALID_INPUT'          // bad date, time, party size, etc.
  | 'UNAVAILABLE'            // slot taken / outside hours
  | 'ALTERNATIVE_OFFERED'    // slot taken, but an alternative was returned
  | 'PROVIDER_TIMEOUT'       // upstream did not respond in time
  | 'PROVIDER_ERROR'         // upstream returned 5xx
  | 'DECLINED'               // card declined (Depos-specific)
  | 'INTERNAL_ERROR';        // unexpected local error

export interface ProviderError {
  code: ProviderErrorCode;
  message: string;
  /** Optional machine-readable details (e.g. upstream error id). */
  detail?: Record<string, unknown>;
  /** If true, the dispatcher may safely retry with the same idempotency key. */
  retryable: boolean;
}

/** Result<T, ProviderError>. The dispatcher NEVER throws; it returns this. */
export type Result<T> =
  | { ok: true; value: T }
  | { ok: false; error: ProviderError };

// --- AvailabilityProvider ---------------------------------------------------

export interface AvailabilityRequest {
  date: IsoDate;
  time: Time24;
  party_size: PartySize;
  /** Optional restaurant/venue id; defaults to RESDIARY_VENUE_ID. */
  venue_id?: string;
}

export interface AvailabilityResponse {
  available: boolean;
  /** When `available` is false, this is the next available slot. */
  next_slot: {
    date: IsoDate;
    time: Time24;
    party_size: PartySize;
  } | null;
  /** Human-readable reason ("A table is available.", "Outside hours.", etc.) */
  message: string;
}

export interface AvailabilityProvider {
  check(req: AvailabilityRequest, idempotencyKey: IdempotencyKey): Promise<Result<AvailabilityResponse>>;
}

// --- DepositProvider --------------------------------------------------------

export interface DepositHoldRequest {
  /** ResDiary booking id (set by BookingProvider.create) or a temporary id. */
  booking_id: string;
  /** Amount in the smallest currency unit (pence for GBP). */
  amount_cents: number;
  currency: 'GBP' | 'EUR' | 'USD' | string;
  /** Customer's email (Depos sends the hold link to this address). */
  customer_email: string;
  /** Optional: customer's name for the hold description. */
  customer_name?: string;
  /** Caller's phone (some PSPs include it in the hold metadata). */
  customer_phone?: Phone;
  /** Idempotency key MUST be set by the dispatcher so retries don't double-charge. */
  idempotency_key: IdempotencyKey;
  /** Expiry in seconds (default 24h). */
  expires_in_seconds?: number;
}

export interface DepositHoldResponse {
  hold_id: string;
  /** URL the customer can be sent to confirm the deposit (if PSP requires). */
  confirmation_url?: string;
  /** Time the hold expires. */
  expires_at: string;
  status: 'held' | 'pending_confirmation' | 'failed';
}

export interface DepositCompensationRequest {
  /** The hold_id returned by DeposProvider.hold. */
  hold_id: string;
  /** Reason for compensation (refund, release). */
  reason: 'booking_failed' | 'booking_cancelled' | 'manual_refund' | string;
}

export interface DepositProvider {
  hold(req: DepositHoldRequest): Promise<Result<DepositHoldResponse>>;
  /** Release or refund a held deposit. Idempotent. */
  compensate(req: DepositCompensationRequest, idempotencyKey: IdempotencyKey): Promise<Result<{ released: boolean }>>;
}

// --- BookingProvider --------------------------------------------------------

export interface BookingCreateRequest {
  date: IsoDate;
  time: Time24;
  party_size: PartySize;
  customer: {
    name: CustomerName;
    phone: Phone;
    email?: string;
  };
  notes?: string;
  /** Resolved hold_id from DepositProvider.hold (set after a successful hold). */
  deposit_hold_id?: string;
  /** Idempotency key. MUST be set by the dispatcher. */
  idempotency_key: IdempotencyKey;
}

export interface BookingResponse {
  id: string;
  confirmation_code: string;
  status: 'created' | 'pending' | 'failed';
  /** Echoed back for cross-checks. */
  date: IsoDate;
  time: Time24;
  party_size: PartySize;
}

export interface BookingProvider {
  create(req: BookingCreateRequest): Promise<Result<BookingResponse>>;
  /** Optional: get a booking by confirmation_code. For confirmations. */
  get(confirmationCode: string): Promise<Result<BookingResponse>>;
}

// --- ManagerQueueProvider ---------------------------------------------------

export interface ManagerEscalationRequest {
  topic: string;
  message: string;
  caller_name?: CustomerName;
  caller_phone?: Phone;
  /** Optional: original booking attempt context for the manager. */
  booking_context?: {
    date?: IsoDate;
    time?: Time24;
    party_size?: PartySize;
  };
}

export interface ManagerMessageResponse {
  id: string;
  status: 'accepted' | 'queued' | 'failed';
  callback_required: boolean;
  received_at: string;
}

export interface ManagerQueueProvider {
  send(req: ManagerEscalationRequest, idempotencyKey: IdempotencyKey): Promise<Result<ManagerMessageResponse>>;
}

// --- Aggregated dispatcher (for completeness) -------------------------------

/** The full provider surface the dispatcher depends on. */
export interface BookingProviderSet {
  availability: AvailabilityProvider;
  deposit: DepositProvider;
  booking: BookingProvider;
  managerQueue: ManagerQueueProvider;
}

/** A factory that returns a fresh provider set (real or fake). */
export type ProviderFactory = () => BookingProviderSet;
