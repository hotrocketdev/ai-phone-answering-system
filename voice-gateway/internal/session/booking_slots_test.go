package session

import (
	"strings"
	"testing"

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
