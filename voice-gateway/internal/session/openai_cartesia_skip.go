package session

import (
	"encoding/json"
	"log"
)

// shouldSkipOpenAICartesiaEnqueue is the pre-emptive gate that runs in the
// response.done handler, BEFORE the OpenAI assistant transcript is enqueued
// to Cartesia. When the deterministic booking layer is going to ask for the
// same booking slot, this gate returns true so the handler returns early
// and the OpenAI natural question is NOT played to the caller. The
// deterministic booking layer (forceBookingQuestion in handleCallerTranscript)
// produces exactly one approved question instead.
//
// The check is intentionally narrow:
//
//   - bookingLive must be true (caller is in an active booking flow).
//   - The OpenAI transcript must map to a known booking field
//     (date / time / guest_count / name / phone).
//   - The current first-missing slot must be non-empty.
//   - The OpenAI field must equal the first-missing slot.
//   - The OpenAI field must NOT equal the already-asked slot. If asked ==
//     missing, handleCallerTranscript's missing==asked branch returns
//     without enqueueing — the booking fix is not going to fire — so we
//     must let OpenAI's question through (silence would be worse).
//
// If any condition fails, the function returns false and the caller proceeds
// with the normal enqueue.
func (s *Session) shouldSkipOpenAICartesiaEnqueue(raw json.RawMessage) (bool, string) {
	s.mu.Lock()
	bookingLive := s.bookingLive
	booking := s.booking
	asked := s.bookingAsked
	s.mu.Unlock()

	if !bookingLive {
		return false, ""
	}

	text := extractTranscript(raw)
	field := expectedBookingFieldFromAssistant(text)
	if field == "" {
		return false, ""
	}

	missing := firstMissingBookingField(booking)
	if missing == "" {
		return false, ""
	}
	if field != missing {
		return false, ""
	}
	if field == asked {
		// Booking fix has already asked for this slot in a previous turn
		// and the missing==asked branch in handleCallerTranscript will
		// skip. Letting OpenAI's enqueue through is the only way the
		// caller hears any question.
		return false, ""
	}

	return true, field
}

// logOpenAICartesiaSkip writes the single-line skip log the user asked for.
// Kept in its own function so the response.done handler stays readable and
// the log format is unit-testable as a constant if we ever need to.
func (s *Session) logOpenAICartesiaSkip(slot string) {
	log.Printf("[%s] openai_cartesia_enqueue_skipped=true reason=booking_deterministic_followup missing_slot=%s",
		s.ID, slot)
}
