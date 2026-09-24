package protocol

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
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

func TestNewReconnectSynchronizationEnforcesMaximumReplayEvents(t *testing.T) {
	t.Parallel()

	matchID := domain.MatchID("match-1")
	command := ClientCommand{
		Version: Version1, Direction: DirectionClientCommand, Type: CommandReconnect,
		CommandID: "cmd-1", RoomID: "room-1", MatchID: &matchID,
		Payload: ReconnectPayload{LastSequence: 40},
	}
	snapshot := json.RawMessage(`{"room_id":"room-1","match_id":"match-1","sequence":41}`)
	events := make([]json.RawMessage, MaxReconnectEvents)
	for index := range events {
		events[index] = json.RawMessage(`{"version":1,"direction":"server_event","type":"PLAYER_RECONNECTED","sequence":` +
			fmt.Sprint(42+index) + `,"room_id":"room-1","match_id":"match-1","payload":{}}`)
	}
	if _, err := NewReconnectSynchronization(command, snapshot, events); err != nil {
		t.Fatalf("NewReconnectSynchronization(%d events) error = %v", len(events), err)
	}
	if _, err := NewReconnectSynchronization(command, snapshot, append(events, events[len(events)-1])); !errors.Is(err, ErrInvalidReconnectSynchronization) {
		t.Fatalf("NewReconnectSynchronization(%d events) error = %v, want ErrInvalidReconnectSynchronization", MaxReconnectEvents+1, err)
	}
}

func TestReconnectEventLimitMatchesSchemaAndBrowser(t *testing.T) {
	root := filepath.Join("..", "..")
	schemaPath := filepath.Join(root, "schemas", "ws_server_response.schema.json")
	schemaData, err := os.ReadFile(schemaPath)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", schemaPath, err)
	}
	var schema map[string]any
	if err := json.Unmarshal(schemaData, &schema); err != nil {
		t.Fatalf("Unmarshal(%q) error = %v", schemaPath, err)
	}
	allOf := schema["allOf"].([]any)
	properties := allOf[2].(map[string]any)["properties"].(map[string]any)
	payloadProperties := properties["payload"].(map[string]any)["properties"].(map[string]any)
	synchronization := payloadProperties["synchronization"].(map[string]any)["oneOf"].([]any)
	eventArray := synchronization[1].(map[string]any)["properties"].(map[string]any)["events"].(map[string]any)
	schemaLimit := int(eventArray["maxItems"].(float64))
	if schemaLimit != MaxReconnectEvents {
		t.Fatalf("schema replay event limit = %d, Go limit = %d", schemaLimit, MaxReconnectEvents)
	}

	shellPath := filepath.Join(root, "client", "web", "shell.html")
	shellData, err := os.ReadFile(shellPath)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", shellPath, err)
	}
	matches := regexp.MustCompile(`(?m)^\s*replayEvents:\s*([0-9]+),$`).FindStringSubmatch(string(shellData))
	if len(matches) != 2 {
		t.Fatal("browser replay event limit was not found")
	}
	browserLimit, err := strconv.Atoi(matches[1])
	if err != nil {
		t.Fatalf("parse browser replay event limit: %v", err)
	}
	if browserLimit != MaxReconnectEvents {
		t.Fatalf("browser replay event limit = %d, Go limit = %d", browserLimit, MaxReconnectEvents)
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
