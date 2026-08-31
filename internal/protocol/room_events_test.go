package protocol

import (
	"encoding/json"
	"errors"
	"testing"

	"buk-yutnori/internal/domain"
)

func TestNewPlayerKickedEventBuildsCanonicalSignal(t *testing.T) {
	t.Parallel()
	event, err := NewPlayerKickedEvent("room-1", 8, "player-2")
	if err != nil {
		t.Fatalf("NewPlayerKickedEvent() error = %v", err)
	}
	encoded, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	want := `{"version":1,"direction":"server_event","type":"PLAYER_KICKED","sequence":8,"room_id":"room-1","payload":{"player_id":"player-2"}}`
	if string(encoded) != want {
		t.Fatalf("encoded event = %s, want %s", encoded, want)
	}
}

func TestNewPlayerKickedEventRejectsInvalidScope(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name     string
		roomID   domain.RoomID
		sequence uint64
		playerID domain.PlayerID
	}{
		{name: "room", sequence: 1, playerID: "player-2"},
		{name: "sequence", roomID: "room-1", playerID: "player-2"},
		{name: "player", roomID: "room-1", sequence: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := NewPlayerKickedEvent(test.roomID, test.sequence, test.playerID); !errors.Is(err, ErrInvalidServerEvent) {
				t.Fatalf("NewPlayerKickedEvent() error = %v, want %v", err, ErrInvalidServerEvent)
			}
		})
	}
}
