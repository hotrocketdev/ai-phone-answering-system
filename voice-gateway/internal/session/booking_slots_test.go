package session

import (
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
