# Dispatcher Provider Contracts

**Status:** Frozen. Source: `experimental/livekit/xai-phone-worker/src/providers.ts`. The Node runtime uses JSDoc typedefs that mirror these (see `src/dispatcher.js`).

## Design principles

1. **Swappable providers.** The dispatcher doesn't know whether it's talking to a fake or a real adapter. Both implement the same interface. The factory in `src/providers/index.js` is the only thing that knows the difference.
2. **No thrown errors.** Every provider method returns a `Result<T, ProviderError>`. The dispatcher branches on `ok` and `error.code` — never on string messages.
3. **Idempotency keys are first-class.** Every state-mutating method accepts an `idempotencyKey`. A retry from the worker doesn't double-book or double-charge.
4. **No cross-provider calls.** The dispatcher is the only thing that orchestrates the flow (avail -> hold -> book). Individual providers don't call each other.
5. **Compensation lives in the dispatcher.** If a booking fails after a hold, the dispatcher calls `deposit.compensate`. The provider implementations don't have a "you're being compensated" hook.

## Provider interfaces

### `AvailabilityProvider` (ResDiary or equivalent)

```typescript
interface AvailabilityRequest {
  date: IsoDate;
  time: Time24;
  party_size: PartySize;
  venue_id?: string;
}

interface AvailabilityResponse {
  available: boolean;
  next_slot: { date, time, party_size } | null;
  message: string;
}

interface AvailabilityProvider {
  check(req: AvailabilityRequest, idempotencyKey: IdempotencyKey): Promise<Result<AvailabilityResponse>>;
}
```

Errors: `NOT_FOUND`, `INVALID_INPUT`, `UNAVAILABLE`, `ALTERNATIVE_OFFERED`, `PROVIDER_TIMEOUT`, `PROVIDER_ERROR`.

### `DepositProvider` (Depos or equivalent)

```typescript
interface DepositHoldRequest {
  booking_id: string;
  amount_cents: number;
  currency: 'GBP' | 'EUR' | 'USD' | string;
  customer_email: string;
  customer_name?: string;
  customer_phone?: Phone;
  idempotency_key: IdempotencyKey;
  expires_in_seconds?: number;
}

interface DepositHoldResponse {
  hold_id: string;
  confirmation_url?: string;
  expires_at: string;
  status: 'held' | 'pending_confirmation' | 'failed';
}

interface DepositCompensationRequest {
  hold_id: string;
  reason: 'booking_failed' | 'booking_cancelled' | 'manual_refund' | string;
}

interface DepositProvider {
  hold(req: DepositHoldRequest): Promise<Result<DepositHoldResponse>>;
  compensate(req: DepositCompensationRequest, idempotencyKey: IdempotencyKey): Promise<Result<{ released: boolean }>>;
}
```

Errors: `NOT_FOUND`, `INVALID_INPUT`, `DECLINED`, `PROVIDER_TIMEOUT`, `PROVIDER_ERROR`.

### `BookingProvider` (ResDiary or equivalent)

```typescript
interface BookingCreateRequest {
  date: IsoDate;
  time: Time24;
  party_size: PartySize;
  customer: { name: CustomerName; phone: Phone; email?: string };
  notes?: string;
  deposit_hold_id?: string;
  idempotency_key: IdempotencyKey;
}

interface BookingResponse {
  id: string;
  confirmation_code: string;
  status: 'created' | 'pending' | 'failed';
  date: IsoDate;
  time: Time24;
  party_size: PartySize;
}

interface BookingProvider {
  create(req: BookingCreateRequest): Promise<Result<BookingResponse>>;
  get(confirmationCode: string): Promise<Result<BookingResponse>>;
}
```

Errors: `NOT_FOUND`, `INVALID_INPUT`, `UNAVAILABLE`, `PROVIDER_TIMEOUT`, `PROVIDER_ERROR`.

### `ManagerQueueProvider` (Porto Douro callback queue or equivalent)

```typescript
interface ManagerEscalationRequest {
  topic: string;
  message: string;
  caller_name?: CustomerName;
  caller_phone?: Phone;
  booking_context?: { date?, time?, party_size? };
}

interface ManagerMessageResponse {
  id: string;
  status: 'accepted' | 'queued' | 'failed';
  callback_required: boolean;
  received_at: string;
}

interface ManagerQueueProvider {
  send(req: ManagerEscalationRequest, idempotencyKey: IdempotencyKey): Promise<Result<ManagerMessageResponse>>;
}
```

Errors: `INVALID_INPUT`, `PROVIDER_TIMEOUT`, `PROVIDER_ERROR`.

## Result type

```typescript
type Result<T> =
  | { ok: true; value: T }
  | { ok: false; error: ProviderError };

interface ProviderError {
  code: ProviderErrorCode;
  message: string;
  detail?: Record<string, unknown>;
  retryable: boolean; // true if a retry with the same idempotency key is safe
}
```

## Common types

```typescript
type IsoDate = string;        // ISO 8601 YYYY-MM-DD or natural language
type Time24 = string;         // 24-hour HH:MM
type PartySize = number;      // integer 1-20
type Phone = string;          // E.164 or UK local
type CustomerName = string;
type IdempotencyKey = string; // client-supplied, MUST be unique per logical operation
```

## Idempotency-key contract

The dispatcher is responsible for generating one idempotency key per logical operation (e.g., one per booking attempt) and passing derived keys to sub-operations:

```javascript
const key = generateIdempotencyKey(); // 16+ random bytes hex

// 1. Hold deposit with derived key
deposit.hold({ ..., idempotency_key: key + ':hold' });

// 2. Create booking with derived key
booking.create({ ..., idempotency_key: key + ':book' });

// 3. (If 2 fails) Compensate with derived key
deposit.compensate({ ... }, key + ':compensate');

// 4. (If 1 fails) Don't compensate (no hold was made)
```

A retry of the same logical operation MUST reuse the same base `key`. The derived `':hold'` / `':book'` / `':compensate'` suffixes let the provider de-duplicate per-sub-operation while keeping the logical operation atomic.

## Switching from fakes to real providers

```bash
# Set env (not committed):
USE_REAL_PROVIDERS=1
RESDIARY_API_KEY=...
RESDIARY_BASE_URL=https://api.resdiary.com/v1
RESDIARY_VENUE_ID=...
DEPOS_API_KEY=...
DEPOS_BASE_URL=https://api.deposits.com/v1
DEPOSIT_PENCE_PER_COVER=2000
MANAGER_QUEUE_URL=https://...
MANAGER_QUEUE_KEY=...

# Run the same contract tests — they exercise both fakes and reals:
npm test
```

The dispatcher doesn't change. The factory in `src/providers/index.js` is the only file that switches.
