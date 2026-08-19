package protocol

import (
	"encoding/json"
	"errors"
	"testing"

	"buk-yutnori/internal/domain"
)

func TestNewReconnectSynchronizationRejectsInvalidBoundaries(t *testing.T) {
	t.Parallel()

	matchID := domain.MatchID("match-1")
	command := ClientCommand{
		Version: Version1, Direction: DirectionClientCommand, Type: CommandReconnect,
		CommandID: "cmd-1", RoomID: "room-1", MatchID: &matchID,
		Payload: ReconnectPayload{LastSequence: 40},
	}
	validSnapshot := json.RawMessage(`{"room_id":"room-1","match_id":"match-1","sequence":41}`)
	validEvent := json.RawMessage(`{"version":1,"direction":"server_event","type":"PLAYER_RECONNECTED","sequence":42,"room_id":"room-1","match_id":"match-1","payload":{}}`)

	tests := []struct {
		name     string
		command  ClientCommand
		snapshot json.RawMessage
		events   []json.RawMessage
	}{
		{name: "invalid command envelope", command: commandWithVersion(command, 0), snapshot: validSnapshot},
		{name: "non reconnect command", command: commandWithType(command, CommandSetReady), snapshot: validSnapshot},
		{name: "missing match", command: commandWithoutMatch(command), snapshot: validSnapshot},
		{name: "snapshot behind client", command: commandWithLastSequence(command, 42), snapshot: validSnapshot},
		{name: "snapshot wrong room", command: command, snapshot: json.RawMessage(`{"room_id":"room-2","match_id":"match-1","sequence":41}`)},
		{name: "snapshot wrong match", command: command, snapshot: json.RawMessage(`{"room_id":"room-1","match_id":"match-2","sequence":41}`)},
		{name: "snapshot zero sequence", command: command, snapshot: json.RawMessage(`{"room_id":"room-1","match_id":"match-1","sequence":0}`)},
		{name: "snapshot duplicate field", command: command, snapshot: json.RawMessage(`{"room_id":"room-1","room_id":"room-1","match_id":"match-1","sequence":41}`)},
		{name: "event gap", command: command, snapshot: validSnapshot, events: []json.RawMessage{json.RawMessage(`{"version":1,"direction":"server_event","type":"PLAYER_RECONNECTED","sequence":43,"room_id":"room-1","match_id":"match-1","payload":{}}`)}},
		{name: "event duplicate sequence", command: command, snapshot: validSnapshot, events: []json.RawMessage{validEvent, validEvent}},
		{name: "event wrong room", command: command, snapshot: validSnapshot, events: []json.RawMessage{json.RawMessage(`{"version":1,"direction":"server_event","type":"PLAYER_RECONNECTED","sequence":42,"room_id":"room-2","match_id":"match-1","payload":{}}`)}},
		{name: "event wrong match", command: command, snapshot: validSnapshot, events: []json.RawMessage{json.RawMessage(`{"version":1,"direction":"server_event","type":"PLAYER_RECONNECTED","sequence":42,"room_id":"room-1","match_id":"match-2","payload":{}}`)}},
		{name: "event invalid direction", command: command, snapshot: validSnapshot, events: []json.RawMessage{json.RawMessage(`{"version":1,"direction":"server_response","type":"PLAYER_RECONNECTED","sequence":42,"room_id":"room-1","match_id":"match-1","payload":{}}`)}},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := NewReconnectSynchronization(test.command, test.snapshot, test.events); !errors.Is(err, ErrInvalidReconnectSynchronization) {
				t.Fatalf("NewReconnectSynchronization() error = %v, want ErrInvalidReconnectSynchronization", err)
			}
		})
	}
}

func TestNewReconnectSynchronizationNormalizesEmptyEvents(t *testing.T) {
	t.Parallel()

	matchID := domain.MatchID("match-1")
	command := ClientCommand{
		Version: Version1, Direction: DirectionClientCommand, Type: CommandReconnect,
		CommandID: "cmd-1", RoomID: "room-1", MatchID: &matchID,
		Payload: ReconnectPayload{LastSequence: 41},
	}
	synchronization, err := NewReconnectSynchronization(
		command,
		json.RawMessage(`{"room_id":"room-1","match_id":"match-1","sequence":41}`),
		nil,
	)
	if err != nil {
		t.Fatalf("NewReconnectSynchronization() error = %v", err)
	}
	encoded, err := json.Marshal(synchronization)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	if string(encoded) != `{"snapshot":{"room_id":"room-1","match_id":"match-1","sequence":41},"events":[]}` {
		t.Fatalf("encoded synchronization = %s", encoded)
	}
}

func commandWithType(command ClientCommand, commandType CommandType) ClientCommand {
	command.Type = commandType
	return command
}

func commandWithVersion(command ClientCommand, version int) ClientCommand {
	command.Version = version
	return command
}

func commandWithoutMatch(command ClientCommand) ClientCommand {
	command.MatchID = nil
	return command
}

func commandWithLastSequence(command ClientCommand, lastSequence uint64) ClientCommand {
	command.Payload = ReconnectPayload{LastSequence: lastSequence}
	return command
}
