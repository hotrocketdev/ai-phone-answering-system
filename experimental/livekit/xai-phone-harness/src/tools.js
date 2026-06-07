// tools.js — function-call stub dispatcher.
//
// Mirrors dispatchToolCall in xai_livekit.go (LiveKit spike). The
// production worker will replace this with real booking/availability
// dispatch, manager escalation, etc. For the harness, it just returns
// synthetic responses so the round-trip is observable in the log.

export function dispatchToolCall(name, args) {
  switch (name) {
    case 'availability.check':
      return {
        available: true,
        next_slot: '19:00',
        message: 'A table is available.',
      };
    case 'booking.create':
      return {
        status: 'created',
        confirmation_id: 'TEST-1234',
      };
    case 'manager.escalate':
      return {
        status: 'message_taken',
        callback_required: true,
      };
    default:
      return {
        error: 'unknown_tool',
        detail: `dispatcher has no handler for ${JSON.stringify(name)}`,
      };
  }
}
