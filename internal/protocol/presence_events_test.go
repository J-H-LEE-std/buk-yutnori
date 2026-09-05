package protocol

import (
	"encoding/json"
	"errors"
	"testing"

	"buk-yutnori/internal/domain"
)

func TestPresenceEventsBuildCanonicalMatchSignals(t *testing.T) {
	disconnected, err := NewPlayerDisconnectedEvent("room-1", "match-1", 7, "player-1")
	if err != nil {
		t.Fatalf("NewPlayerDisconnectedEvent() error = %v", err)
	}
	encoded, _ := json.Marshal(disconnected)
	if got, want := string(encoded), `{"version":1,"direction":"server_event","type":"PLAYER_DISCONNECTED","sequence":7,"room_id":"room-1","match_id":"match-1","payload":{"player_id":"player-1"}}`; got != want {
		t.Fatalf("disconnected JSON = %s, want %s", got, want)
	}

	reconnected, err := NewPlayerReconnectedEvent("room-1", "match-1", 8, "player-1", true)
	if err != nil {
		t.Fatalf("NewPlayerReconnectedEvent() error = %v", err)
	}
	encoded, _ = json.Marshal(reconnected)
	if got, want := string(encoded), `{"version":1,"direction":"server_event","type":"PLAYER_RECONNECTED","sequence":8,"room_id":"room-1","match_id":"match-1","payload":{"player_id":"player-1","control_restored":true}}`; got != want {
		t.Fatalf("reconnected JSON = %s, want %s", got, want)
	}
}

func TestPresenceEventsRejectInvalidScopeAndPlayer(t *testing.T) {
	tests := []struct {
		name string
		call func() error
	}{
		{name: "empty room", call: func() error { _, err := NewPlayerDisconnectedEvent("", "match-1", 1, "player-1"); return err }},
		{name: "empty match", call: func() error { _, err := NewPlayerReconnectedEvent("room-1", "", 1, "player-1", true); return err }},
		{name: "zero sequence", call: func() error { _, err := NewPlayerDisconnectedEvent("room-1", "match-1", 0, "player-1"); return err }},
		{name: "invalid player", call: func() error {
			_, err := NewPlayerReconnectedEvent("room-1", "match-1", 1, domain.PlayerID(""), false)
			return err
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.call(); !errors.Is(err, ErrInvalidServerEvent) {
				t.Fatalf("error = %v, want %v", err, ErrInvalidServerEvent)
			}
		})
	}
}
