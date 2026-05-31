package sm

import (
	"testing"
)

func TestNew_StartsInGreeting(t *testing.T) {
	m := New("Bella Roma")
	if m.Current() != StateGreeting {
		t.Errorf("expected GREETING, got %s", m.Current())
	}
}

func TestTransition_ValidPath(t *testing.T) {
	m := New("Test")

	steps := []State{
		StateCollectBooking,
		StateCheckAvail,
		StateConfirm,
		StateClosing,
	}

	for _, step := range steps {
		if err := m.Transition(step); err != nil {
			t.Fatalf("unexpected error transitioning to %s: %v", step, err)
		}
	}
}

func TestTransition_Invalid(t *testing.T) {
	m := New("Test")

	// Cannot go from GREETING directly to CONFIRM
	err := m.Transition(StateConfirm)
	if err == nil {
		t.Fatal("expected error for invalid transition")
	}
}

func TestTransition_ClosingIsTerminal(t *testing.T) {
	m := New("Test")
	m.Transition(StateClosing)

	err := m.Transition(StateGreeting)
	if err == nil {
		t.Fatal("expected error — CLOSING is terminal")
	}
	if m.Current() != StateClosing {
		t.Errorf("state should remain CLOSING, got %s", m.Current())
	}
}

func TestTransition_GreetingToFAQ(t *testing.T) {
	m := New("Test")
	m.Transition(StateFAQ)

	if m.Current() != StateFAQ {
		t.Errorf("expected FAQ, got %s", m.Current())
	}
}

func TestReturnFromFAQ(t *testing.T) {
	m := New("Test")

	// Go GREETING → FAQ → return → GREETING
	m.Transition(StateFAQ)
	if err := m.ReturnFromFAQ(); err != nil {
		t.Fatalf("ReturnFromFAQ failed: %v", err)
	}
	if m.Current() != StateGreeting {
		t.Errorf("expected GREETING after FAQ return, got %s", m.Current())
	}
}

func TestReturnFromFAQ_NotInFAQ(t *testing.T) {
	m := New("Test")
	err := m.ReturnFromFAQ()
	if err == nil {
		t.Fatal("expected error when not in FAQ state")
	}
}

func TestAvailableTools_Greeting(t *testing.T) {
	m := New("Test")
	tools := m.AvailableTools()
	if len(tools) != 0 {
		t.Errorf("expected 0 tools in GREETING, got %d", len(tools))
	}
}

func TestAvailableTools_CheckAvail(t *testing.T) {
	m := New("Test")
	m.Transition(StateCollectBooking)
	m.Transition(StateCheckAvail)

	tools := m.AvailableTools()
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool in CHECK_AVAILABILITY, got %d", len(tools))
	}
	if tools[0].Name != "check_availability" {
		t.Errorf("expected check_availability, got %s", tools[0].Name)
	}
}

func TestIsToolAllowed(t *testing.T) {
	m := New("Test")
	m.Transition(StateCollectBooking)
	m.Transition(StateCheckAvail)

	if !m.IsToolAllowed("check_availability") {
		t.Error("check_availability should be allowed")
	}
	if m.IsToolAllowed("create_booking") {
		t.Error("create_booking should NOT be allowed in CHECK_AVAILABILITY")
	}
}

func TestAvailableTools_Confirm(t *testing.T) {
	m := New("Test")
	m.Transition(StateCollectBooking)
	m.Transition(StateCheckAvail)
	m.Transition(StateConfirm)

	tools := m.AvailableTools()
	if len(tools) != 1 || tools[0].Name != "create_booking" {
		t.Errorf("expected [create_booking] in CONFIRM, got %v", toolNames(tools))
	}
}

func TestBookingData_MissingFields(t *testing.T) {
	b := BookingData{}
	missing := b.MissingFields()
	if len(missing) != 5 {
		t.Errorf("expected 5 missing fields, got %d: %v", len(missing), missing)
	}
}

func TestBookingData_SetAndComplete(t *testing.T) {
	m := New("Test")
	m.SetBookingData("partySize", 4)
	m.SetBookingData("date", "2026-05-22")
	m.SetBookingData("time", "19:00")
	m.SetBookingData("name", "James")
	m.SetBookingData("phone", "+441234567890")

	if !m.Booking().IsComplete() {
		t.Error("booking should be complete")
	}
}

func TestValidateResponse_Success(t *testing.T) {
	m := New("Test")
	m.Transition(StateCollectBooking)
	m.Transition(StateCheckAvail)
	m.Transition(StateConfirm)

	// Valid: create_booking succeeded
	err := m.ValidateResponse(&ToolResult{Name: "create_booking", Success: true})
	if err != nil {
		t.Errorf("expected no guardrail violation: %v", err)
	}
}

func TestValidateResponse_FailsWithoutSuccess(t *testing.T) {
	m := New("Test")
	m.Transition(StateCollectBooking)
	m.Transition(StateCheckAvail)
	m.Transition(StateConfirm)

	// Invalid: create_booking failed
	err := m.ValidateResponse(&ToolResult{Name: "create_booking", Success: false})
	if err == nil {
		t.Fatal("expected guardrail violation for failed create_booking")
	}
}

func TestValidateResponse_FailsWithoutToolCall(t *testing.T) {
	m := New("Test")
	m.Transition(StateCollectBooking)
	m.Transition(StateCheckAvail)
	m.Transition(StateConfirm)

	// Invalid: no tool called at all
	err := m.ValidateResponse(nil)
	if err == nil {
		t.Fatal("expected guardrail violation for missing tool call")
	}
}

func TestBuildSystemPrompt_ContainsRestaurant(t *testing.T) {
	m := New("Bella Roma")
	prompt := m.BuildSystemPrompt()
	if !containsStr(prompt, "Bella Roma") {
		t.Error("prompt should contain restaurant name")
	}
	if containsStr(prompt, "Portuguese restaurant in Birmingham") {
		t.Error("prompt should not hardcode tenant-specific restaurant facts")
	}
}

func TestBuildSystemPrompt_UsesLayeredArchitecture(t *testing.T) {
	m := NewWithTenant(TenantConfig{
		BusinessName: "Test Restaurant",
		AgentName:    "Sam",
		Industry:     "restaurant",
	})
	prompt := m.BuildSystemPrompt()
	for _, want := range []string{
		"LAYER 1: VOXLANE CORE RECEPTIONIST",
		"LAYER 2: RESTAURANT BEHAVIOUR PACK",
		"LAYER 3: TENANT CONFIGURATION",
		"Agent name: Sam.",
		"Business name: Test Restaurant.",
	} {
		if !containsStr(prompt, want) {
			t.Fatalf("prompt missing layered architecture text %q", want)
		}
	}
}

func TestBuildSystemPrompt_ContainsGuardrails(t *testing.T) {
	m := New("Test")
	prompt := m.BuildSystemPrompt()
	if !containsStr(prompt, "Never confirm a booking") {
		t.Error("prompt should contain anti-hallucination guardrail")
	}
}

func TestBuildSystemPrompt_BookingIntentRule(t *testing.T) {
	m := New("Test")
	prompt := m.BuildSystemPrompt()
	if !containsStr(prompt, "treat that as a booking") {
		t.Error("prompt should treat table/reservation requests as booking intent")
	}
	if !containsStr(prompt, "Do not ask whether they want a table or a drink") {
		t.Error("prompt should forbid table-or-drink clarification after booking intent")
	}
	if !containsStr(prompt, "Do not say \"I'm all ears\"") {
		t.Error("prompt should forbid generic chatty assistant language")
	}
	if !containsStr(prompt, "Do not sound excited") {
		t.Error("prompt should keep Alex calm")
	}
	if !containsStr(prompt, "gives multiple details in one answer") {
		t.Error("prompt should preserve combined date/time answers")
	}
	if !containsStr(prompt, "do not ask for the date or time again") {
		t.Error("prompt should not repeat questions after combined date/time answers")
	}
	if !containsStr(prompt, "Collect exactly: date") {
		t.Error("prompt should source the restaurant booking workflow from the restaurant pack layer")
	}
}

func TestBuildSystemPrompt_GeneralReceptionistAndRestaurantEnquiries(t *testing.T) {
	m := New("Test")
	prompt := m.BuildSystemPrompt()
	for _, want := range []string{
		"manager, owner, named staff member, or transfer",
		"take a message and callback details",
		"Opening days and times",
		"address, phone, opening hours, parking, live music, manager names, and staff names must come from tenant config or tools",
		"offer to take a message or arrange a callback",
	} {
		if !containsStr(prompt, want) {
			t.Fatalf("prompt missing receptionist behaviour %q", want)
		}
	}
}

func TestBuildSystemPrompt_StateSpecific(t *testing.T) {
	m := New("Test")
	m.Transition(StateCollectBooking)
	m.SetBookingData("partySize", 4)
	m.SetBookingData("date", "2026-05-22")

	prompt := m.BuildSystemPrompt()
	if !containsStr(prompt, "BOOKING") {
		t.Error("prompt should contain current state context")
	}
	if !containsStr(prompt, "time") || !containsStr(prompt, "name") {
		t.Error("prompt should contain missing booking fields")
	}
}

func TestPrevious(t *testing.T) {
	m := New("Test")
	m.Transition(StateCollectBooking)
	if m.Previous() != StateGreeting {
		t.Errorf("expected GREETING as previous, got %s", m.Previous())
	}
}

// ─── Helpers ─────────────────────────────────────────────────────────────

func toolNames(tools []Tool) []string {
	names := make([]string, len(tools))
	for i, t := range tools {
		names[i] = t.Name
	}
	return names
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
