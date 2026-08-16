package protocol

import (
	"encoding/json"
	"errors"
	"testing"

	"buk-yutnori/internal/domain"
)

func TestNewCommandResultBuildsCanonicalServerResponse(t *testing.T) {
	t.Parallel()

	requestID := "req-1"
	matchID := domain.MatchID("match-1")
	start, end := uint64(42), uint64(44)
	command := ClientCommand{
		Version:   Version1,
		Direction: DirectionClientCommand,
		Type:      CommandThrowYut,
		RequestID: &requestID,
		CommandID: "cmd-1",
		RoomID:    "room-1",
		MatchID:   &matchID,
		Payload:   EmptyPayload{},
	}
	result, err := NewCommandResult(command, CommandOutcome{
		Status:             CommandAccepted,
		EventSequenceStart: &start,
		EventSequenceEnd:   &end,
	})
	if err != nil {
		t.Fatalf("NewCommandResult() error = %v", err)
	}

	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	want := `{"version":1,"direction":"server_response","type":"COMMAND_RESULT","request_id":"req-1","command_id":"cmd-1","sequence":null,"room_id":"room-1","match_id":"match-1","payload":{"status":"accepted","event_sequence_start":42,"event_sequence_end":44,"error":null}}`
	if string(encoded) != want {
		t.Fatalf("encoded result = %s\nwant = %s", encoded, want)
	}

	requestID = "mutated-request"
	matchID = "mutated-match"
	start, end = 100, 101
	encoded, err = json.Marshal(result)
	if err != nil {
		t.Fatalf("json.Marshal() after input mutation error = %v", err)
	}
	if string(encoded) != want {
		t.Fatalf("result retained caller-owned pointers: %s", encoded)
	}
}

func TestNewCommandResultRejectsInvalidOutcomes(t *testing.T) {
	t.Parallel()

	zero, start, end, reversed := uint64(0), uint64(3), uint64(4), uint64(2)
	validCommand := ClientCommand{
		Version: Version1, Direction: DirectionClientCommand, Type: CommandSetReady,
		CommandID: "cmd-1", RoomID: "room-1", Payload: SetReadyPayload{Ready: true},
	}
	tests := []struct {
		name    string
		outcome CommandOutcome
	}{
		{name: "unknown status", outcome: CommandOutcome{Status: "unknown"}},
		{name: "accepted with error", outcome: CommandOutcome{Status: CommandAccepted, Error: &CommandError{Code: "NO", Message: "no", Retriable: false}}},
		{name: "rejected without error", outcome: CommandOutcome{Status: CommandRejected}},
		{name: "missing sequence end", outcome: CommandOutcome{Status: CommandAccepted, EventSequenceStart: &start}},
		{name: "missing sequence start", outcome: CommandOutcome{Status: CommandAccepted, EventSequenceEnd: &end}},
		{name: "zero sequence range", outcome: CommandOutcome{Status: CommandAccepted, EventSequenceStart: &zero, EventSequenceEnd: &zero}},
		{name: "reversed sequence", outcome: CommandOutcome{Status: CommandAccepted, EventSequenceStart: &start, EventSequenceEnd: &reversed}},
		{name: "empty error code", outcome: CommandOutcome{Status: CommandRejected, Error: &CommandError{Message: "rejected"}}},
		{name: "empty error message", outcome: CommandOutcome{Status: CommandRejected, Error: &CommandError{Code: "REJECTED"}}},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := NewCommandResult(validCommand, test.outcome); !errors.Is(err, ErrInvalidCommandOutcome) {
				t.Fatalf("NewCommandResult() error = %v, want ErrInvalidCommandOutcome", err)
			}
		})
	}
}
