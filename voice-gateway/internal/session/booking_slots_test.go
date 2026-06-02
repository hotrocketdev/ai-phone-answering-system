package session

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/voxlane/voice-gateway/internal/config"
	cartesiarend "github.com/voxlane/voice-gateway/internal/renderer/cartesia"
	"github.com/voxlane/voice-gateway/internal/session/sm"
)

func TestParseBookingSlotsTomorrowAt7PM(t *testing.T) {
	update := parseBookingSlots("Tomorrow at 7pm.", sm.BookingData{})
	if update.Date != "tomorrow" {
		t.Fatalf("date = %q, want tomorrow", update.Date)
	}
	if update.Time != "19:00" {
		t.Fatalf("time = %q, want 19:00", update.Time)
	}
}

func TestParseBookingSlotsTomorrowAt7PMForFourPeople(t *testing.T) {
	update := parseBookingSlots("Tomorrow at 7pm for four people.", sm.BookingData{})
	if update.Date != "tomorrow" || update.Time != "19:00" || update.PartySize != 4 {
		t.Fatalf("update = %+v, want tomorrow/19:00/4", update)
	}
}

func TestParseBookingSlotsWordTimePM(t *testing.T) {
	update := parseBookingSlots("at seven p.m.", sm.BookingData{})
	if update.Time != "19:00" {
		t.Fatalf("time = %q, want 19:00", update.Time)
	}
}

func TestParseBookingSlotsOClockDigit(t *testing.T) {
	update := parseBookingSlots("Tomorrow at 7 o'clock.", sm.BookingData{})
	if update.Date != "tomorrow" || update.Time != "19:00" {
		t.Fatalf("update = %+v, want tomorrow/19:00", update)
	}
}

func TestParseBookingSlotsOClockWord(t *testing.T) {
	update := parseBookingSlots("Tomorrow at four o'clock.", sm.BookingData{})
	if update.Date != "tomorrow" || update.Time != "16:00" {
		t.Fatalf("update = %+v, want tomorrow/16:00", update)
	}
}

func TestParseBookingSlotsOClockWithMorningStaysMorning(t *testing.T) {
	update := parseBookingSlots("Tomorrow at ten o'clock in the morning.", sm.BookingData{})
	if update.Time != "10:00" {
		t.Fatalf("time = %q, want 10:00 (morning should not flip to PM)", update.Time)
	}
}

func TestParseBookingSlotsOClockNoon(t *testing.T) {
	update := parseBookingSlots("at twelve o'clock.", sm.BookingData{})
	if update.Time != "12:00" {
		t.Fatalf("time = %q, want 12:00", update.Time)
	}
}

func TestParseBookingSlotsForFour(t *testing.T) {
	update := parseBookingSlots("For four.", sm.BookingData{})
	if update.PartySize != 4 {
		t.Fatalf("party size = %d, want 4", update.PartySize)
	}
}

func TestParseBookingSlotsFourPeople(t *testing.T) {
	update := parseBookingSlots("Four people.", sm.BookingData{})
	if update.PartySize != 4 {
		t.Fatalf("party size = %d, want 4", update.PartySize)
	}
}

func TestParseBookingSlotsName(t *testing.T) {
	update := parseBookingSlots("My name is Jorge.", sm.BookingData{})
	if update.Name != "Jorge" {
		t.Fatalf("name = %q, want Jorge", update.Name)
	}
}

func TestParseBookingSlotsNameAlongsideDateTimeParty(t *testing.T) {
	update := parseBookingSlots("I'd like to book a table for tomorrow at 7pm for four people, my name is George.", sm.BookingData{})
	if update.Date != "tomorrow" || update.Time != "19:00" || update.PartySize != 4 || update.Name != "George" {
		t.Fatalf("update = %+v, want tomorrow/19:00/4/George", update)
	}
}

func TestParseBookingSlotsNameAlongsideEverythingInOneTurn(t *testing.T) {
	update := parseBookingSlots("Hi, I'd like to book a table for tomorrow at 7 o'clock for four people. My name is George, and my number is 07917 715 734.", sm.BookingData{})
	if update.Date != "tomorrow" || update.Time != "19:00" || update.PartySize != 4 || update.Name != "George" || update.Phone != "07917715734" {
		t.Fatalf("update = %+v, want tomorrow/19:00/4/George/phone", update)
	}
}

func TestParseBookingSlotsPhone(t *testing.T) {
	update := parseBookingSlots("My number is 07917 715734.", sm.BookingData{})
	if update.Phone != "07917715734" {
		t.Fatalf("phone = %q, want 07917715734", update.Phone)
	}
}

func TestMergeBookingSlotsDoesNotOverwriteExistingDateTime(t *testing.T) {
	current := sm.BookingData{Date: "tomorrow", Time: "19:00"}
	update := bookingSlotUpdate{Date: "friday", Time: "20:00", PartySize: 4}
	merged := mergeBookingSlots(current, update, false)
	if merged.Date != "tomorrow" || merged.Time != "19:00" {
		t.Fatalf("date/time overwritten: %+v", merged)
	}
	if merged.PartySize != 4 {
		t.Fatalf("party size = %d, want 4", merged.PartySize)
	}
}

func TestMergeBookingSlotsOverridesImpliedPartySizeSentinel(t *testing.T) {
	current := sm.BookingData{Date: "tomorrow", Time: "19:00", PartySize: -1, Name: "provided"}
	update := parseBookingSlots("Four.", sm.BookingData{Date: "tomorrow", Time: "19:00"})
	merged := mergeBookingSlots(current, update, false)
	if merged.PartySize != 4 {
		t.Fatalf("party size = %d, want 4 (real user value must override implied -1 sentinel)", merged.PartySize)
	}
}

func TestMergeBookingSlotsOverridesImpliedNameSentinel(t *testing.T) {
	current := sm.BookingData{Date: "tomorrow", Time: "19:00", PartySize: 4, Name: "provided"}
	update := parseBookingSlots("My name is Jorge.", sm.BookingData{Date: "tomorrow", Time: "19:00", PartySize: 4})
	merged := mergeBookingSlots(current, update, false)
	if merged.Name != "Jorge" {
		t.Fatalf("name = %q, want Jorge (real user value must override implied provided sentinel)", merged.Name)
	}
}

func TestFirstMissingBookingField(t *testing.T) {
	b := sm.BookingData{}
	if got := firstMissingBookingField(b); got != "date" {
		t.Fatalf("first missing = %q, want date", got)
	}
	b.Date = "tomorrow"
	b.Time = "19:00"
	if got := firstMissingBookingField(b); got != "guest_count" {
		t.Fatalf("first missing = %q, want guest_count", got)
	}
	b.PartySize = 4
	if got := firstMissingBookingField(b); got != "name" {
		t.Fatalf("first missing = %q, want name", got)
	}
	b.Name = "Jorge"
	if got := firstMissingBookingField(b); got != "phone" {
		t.Fatalf("first missing = %q, want phone", got)
	}
	b.Phone = "07917715734"
	if got := firstMissingBookingField(b); got != "" {
		t.Fatalf("first missing = %q, want empty", got)
	}
}

func TestExpectedBookingFieldFromAssistant(t *testing.T) {
	if got := expectedBookingFieldFromAssistant("Thanks, and how many people is that for?"); got != "guest_count" {
		t.Fatalf("field = %q, want guest_count", got)
	}
}

func TestNaturalBookingQuestionBookingIntentDate(t *testing.T) {
	got := naturalBookingQuestion("date", bookingSlotUpdate{}, sm.BookingData{})
	want := "Of course, I can help with that. What date would you like to book for?"
	if got != want {
		t.Fatalf("question = %q, want %q", got, want)
	}
}

func TestNaturalBookingQuestionDateOnlyAsksTime(t *testing.T) {
	got := naturalBookingQuestion("time", bookingSlotUpdate{Date: "tomorrow"}, sm.BookingData{Date: "tomorrow"})
	want := "Lovely. What time would you like?"
	if got != want {
		t.Fatalf("question = %q, want %q", got, want)
	}
}

func TestNaturalBookingQuestionDateTimeAsksGuests(t *testing.T) {
	got := naturalBookingQuestion("guest_count", bookingSlotUpdate{Date: "tomorrow", Time: "19:00"}, sm.BookingData{Date: "tomorrow", Time: "19:00"})
	want := "Perfect. How many guests will that be for?"
	if got != want {
		t.Fatalf("question = %q, want %q", got, want)
	}
}

func TestNaturalBookingQuestionDateTimeGuestsAsksName(t *testing.T) {
	got := naturalBookingQuestion("name", bookingSlotUpdate{Date: "tomorrow", Time: "19:00", PartySize: 4}, sm.BookingData{Date: "tomorrow", Time: "19:00", PartySize: 4})
	want := "Great. Can I take your name please?"
	if got != want {
		t.Fatalf("question = %q, want %q", got, want)
	}
}

func TestNaturalBookingQuestionGuestCountAsksName(t *testing.T) {
	got := naturalBookingQuestion("name", bookingSlotUpdate{PartySize: 4}, sm.BookingData{Date: "tomorrow", Time: "19:00", PartySize: 4})
	want := "Great. Can I take your name please?"
	if got != want {
		t.Fatalf("question = %q, want %q", got, want)
	}
}

func TestNaturalBookingQuestionNameAsksPhone(t *testing.T) {
	got := naturalBookingQuestion("phone", bookingSlotUpdate{Name: "George"}, sm.BookingData{Date: "tomorrow", Time: "19:00", PartySize: 4, Name: "George"})
	want := "Thanks, George. And what's the best contact number?"
	if got != want {
		t.Fatalf("question = %q, want %q", got, want)
	}
}

func TestClarificationBookingQuestionForName(t *testing.T) {
	if got := clarificationBookingQuestion("name", 1); got != "Sorry, could you say your name again please?" {
		t.Fatalf("first clarification = %q", got)
	}
	if got := clarificationBookingQuestion("name", 2); got != "Sorry, could you spell your name for me please?" {
		t.Fatalf("second clarification = %q", got)
	}
}

func TestBookingResponsesAvoidForbiddenPhrases(t *testing.T) {
	responses := []string{
		nextBookingQuestion("date"),
		nextBookingQuestion("time"),
		nextBookingQuestion("guest_count"),
		nextBookingQuestion("name"),
		nextBookingQuestion("phone"),
		clarificationBookingQuestion("name", 1),
		clarificationBookingQuestion("name", 2),
	}
	forbidden := []string{
		"i'm here to help",
		"would you like to chat",
		"how may i assist you today",
		"for when",
		"what's the date",
	}
	for _, response := range responses {
		lower := strings.ToLower(response)
		for _, phrase := range forbidden {
			if strings.Contains(lower, phrase) {
				t.Fatalf("response %q contains forbidden phrase %q", response, phrase)
			}
		}
	}
}

func TestStateStillControlsMissingSlotOrder(t *testing.T) {
	b := mergeBookingSlots(sm.BookingData{}, parseBookingSlots("Tomorrow at seven p.m. for four people.", sm.BookingData{}), false)
	if got := firstMissingBookingField(b); got != "name" {
		t.Fatalf("missing = %q, want name", got)
	}
	if got := naturalBookingQuestion(firstMissingBookingField(b), bookingSlotUpdate{Date: "tomorrow", Time: "19:00", PartySize: 4}, b); got != "Great. Can I take your name please?" {
		t.Fatalf("question = %q", got)
	}
}

func TestSetBookingAskedLockedDoesNotFlipBookingLive(t *testing.T) {
	s := &Session{}
	s.setBookingAskedLocked("date")
	if s.bookingAsked != "date" {
		t.Errorf("bookingAsked = %q, want date", s.bookingAsked)
	}
	if s.bookingLive {
		t.Error("bookingLive should remain false after setBookingAskedLocked; the caller transcript, not the assistant's question, is what makes a booking live")
	}
}

func TestHandleCallerTranscriptPartialInfoDoesNotForceDeterministicQuestion(t *testing.T) {
	s := newBookingTestSession()
	s.bookingLive = true
	s.setBookingAskedLocked("date")
	s.handleCallerTranscript(context.Background(), "Tomorrow at seven p.m.")
	if !s.bookingLive {
		t.Error("bookingLive should remain true after caller provides date+time")
	}
	if s.booking.Date != "tomorrow" || s.booking.Time != "19:00" {
		t.Errorf("booking = %+v, want date=tomorrow time=19:00", s.booking)
	}
	if text, ok := tryDequeue(s, 50*time.Millisecond); ok {
		t.Errorf("no deterministic question should be enqueued (OpenAI should handle the natural reply); got %q", text)
	}
}

func TestHandleCallerTranscriptUnparseableInputStillClarifies(t *testing.T) {
	s := newBookingTestSession()
	s.bookingLive = true
	s.booking = sm.BookingData{Date: "tomorrow", Time: "19:00", PartySize: 4}
	s.setBookingAskedLocked("name")
	s.handleCallerTranscript(context.Background(), "Ja, ik doad")
	text, ok := tryDequeue(s, 50*time.Millisecond)
	if !ok {
		t.Fatal("clarification should be enqueued when input is unparseable for the requested slot")
	}
	if !strings.Contains(strings.ToLower(text), "say your name again") {
		t.Errorf("clarification = %q, want it to ask for the name", text)
	}
}

func TestHandleCallerTranscriptAllSlotsCapturedFiresCompletion(t *testing.T) {
	s := newBookingTestSession()
	s.bookingLive = true
	s.booking = sm.BookingData{Date: "tomorrow", Time: "19:00", PartySize: 4, Name: "George", Phone: "07917715734"}
	s.setBookingAskedLocked("phone")
	s.handleCallerTranscript(context.Background(), "My number is 07917 715734.")
	text, ok := tryDequeue(s, 50*time.Millisecond)
	if !ok {
		t.Fatal("completion message should be enqueued when all slots are captured")
	}
	if !strings.Contains(strings.ToLower(text), "one moment, i'll check") {
		t.Errorf("completion = %q, want 'One moment, I'll check that.'", text)
	}
}

func TestHandleCallerTranscriptNoDuplicateNameQuestionWhenNameIsMissingAndUserGivesName(t *testing.T) {
	s := newBookingTestSession()
	s.bookingLive = true
	s.booking = sm.BookingData{Date: "tomorrow", Time: "19:00", PartySize: 4}
	s.bookingClarifyField = ""
	s.bookingClarifyCount = 0
	s.handleCallerTranscript(context.Background(), "My name is George")
	if text, ok := tryDequeue(s, 50*time.Millisecond); ok {
		t.Errorf("no deterministic name question should be enqueued (OpenAI should handle the natural reply); got %q", text)
	}
	if s.booking.Name != "George" {
		t.Errorf("name = %q, want George", s.booking.Name)
	}
}

func TestHandleCallerTranscriptNoDuplicatePhoneQuestionWhenPhoneIsMissingAndUserGivesPhone(t *testing.T) {
	s := newBookingTestSession()
	s.bookingLive = true
	s.booking = sm.BookingData{Date: "tomorrow", Time: "19:00", PartySize: 4, Name: "George"}
	s.bookingClarifyField = ""
	s.bookingClarifyCount = 0
	s.handleCallerTranscript(context.Background(), "My number is 07917 715734")
	text, ok := tryDequeue(s, 50*time.Millisecond)
	if ok && strings.Contains(strings.ToLower(text), "phone number") {
		t.Errorf("no duplicate phone question should be enqueued; got %q", text)
	}
	if !strings.Contains(s.booking.Phone, "791771573") {
		t.Errorf("phone = %q, want to contain 791771573", s.booking.Phone)
	}
}

func newBookingTestSession() *Session {
	return &Session{
		ID:                  "test-booking",
		Config:              &config.Config{},
		stopCh:              make(chan struct{}),
		cartesiaRenderQueue: make(chan string, 32),
		cartesiaRender:      &cartesiarend.Renderer{},
	}
}

func tryDequeue(s *Session, timeout time.Duration) (string, bool) {
	select {
	case text := <-s.cartesiaRenderQueue:
		return text, true
	case <-time.After(timeout):
		return "", false
	}
}

func TestHandleCallerTranscriptFirstTurnDoesNotEnqueueDuplicateQuestion(t *testing.T) {
	s := newBookingTestSession()
	s.setBookingAskedLocked("date")

	s.handleCallerTranscript(context.Background(), "I'd like to book a table")

	if s.bookingLive != true {
		t.Error("bookingLive should be true after caller expresses booking intent")
	}
	if text, ok := tryDequeue(s, 50*time.Millisecond); ok {
		t.Errorf("no question should be enqueued (assistant already asked %q); got %q", s.bookingAsked, text)
	}
}

func TestHandleCallerTranscriptRepeatedTurnStillClarifiesWhenLive(t *testing.T) {
	s := newBookingTestSession()
	s.bookingLive = true
	s.setBookingAskedLocked("date")

	s.handleCallerTranscript(context.Background(), "uh")

	text, ok := tryDequeue(s, 50*time.Millisecond)
	if !ok {
		t.Fatal("clarification should be enqueued when bookingLive and missing==asked and no slot provided")
	}
	if !strings.Contains(strings.ToLower(text), "repeat the date") {
		t.Errorf("clarification = %q, want it to ask for the date", text)
	}
}
