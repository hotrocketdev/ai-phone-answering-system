package redis

import (
	"encoding/json"
	"testing"
	"time"
)

func TestSessionState_MarshalRoundtrip(t *testing.T) {
	state := &SessionState{
		CallSid:     "CA_TEST_001",
		TenantID:    "tenant_01",
		PhoneFrom:   "+441234567890",
		PhoneTo:     "+440000000000",
		MetaState:   "ACTIVE",
		ConvState:   "GREETING",
		InputAudioSecs:  12.5,
		OutputAudioSecs: 8.3,
		TextTokensIn:    450,
		TextTokensOut:   320,
		CreatedAt:    time.Now(),
		LastActivity: time.Now(),
		ConversationHistory: []Turn{
			{Role: "assistant", Content: "Hello, Bella Roma!", Timestamp: time.Now()},
			{Role: "user", Content: "I'd like to book a table.", Timestamp: time.Now()},
		},
	}

	data, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var decoded SessionState
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if decoded.CallSid != "CA_TEST_001" {
		t.Errorf("expected CA_TEST_001, got %s", decoded.CallSid)
	}
	if decoded.MetaState != "ACTIVE" {
		t.Errorf("expected ACTIVE, got %s", decoded.MetaState)
	}
	if len(decoded.ConversationHistory) != 2 {
		t.Errorf("expected 2 turns, got %d", len(decoded.ConversationHistory))
	}
	if decoded.InputAudioSecs != 12.5 {
		t.Errorf("expected 12.5, got %f", decoded.InputAudioSecs)
	}
}

func TestToolCallRecord_MarshalRoundtrip(t *testing.T) {
	record := ToolCallRecord{
		Tool:      "check_availability",
		Args:      json.RawMessage(`{"date":"2026-05-22","time":"19:00","partySize":4}`),
		Result:    json.RawMessage(`{"success":true,"data":{"available":true}}`),
		Timestamp: time.Now(),
	}

	data, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var decoded ToolCallRecord
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if decoded.Tool != "check_availability" {
		t.Errorf("expected check_availability, got %s", decoded.Tool)
	}

	var args map[string]interface{}
	json.Unmarshal(decoded.Args, &args)
	if args["date"] != "2026-05-22" {
		t.Errorf("expected date 2026-05-22, got %v", args["date"])
	}
}

func TestSessionState_EmptyHistory(t *testing.T) {
	state := &SessionState{
		CallSid:             "CA_TEST_002",
		ConversationHistory: []Turn{},
	}

	data, _ := json.Marshal(state)
	var decoded SessionState
	json.Unmarshal(data, &decoded)

	if decoded.ConversationHistory == nil {
		t.Error("expected empty slice, got nil")
	}
}
