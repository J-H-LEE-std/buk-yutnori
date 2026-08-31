package domain

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestStringIDsValidate(t *testing.T) {
	tests := []struct {
		name      string
		valid     func() error
		invalid   func() error
		spaceOnly func() error
	}{
		{
			name:      "PlayerID",
			valid:     func() error { return PlayerID("player-1").Validate() },
			invalid:   func() error { return PlayerID("").Validate() },
			spaceOnly: func() error { return PlayerID(" ").Validate() },
		},
		{
			name:      "PieceID",
			valid:     func() error { return PieceID("A-1").Validate() },
			invalid:   func() error { return PieceID("").Validate() },
			spaceOnly: func() error { return PieceID(" ").Validate() },
		},
		{
			name:      "SpaceID",
			valid:     func() error { return SpaceID("chammeogi").Validate() },
			invalid:   func() error { return SpaceID("").Validate() },
			spaceOnly: func() error { return SpaceID(" ").Validate() },
		},
		{
			name:      "RoomID",
			valid:     func() error { return RoomID("room-1").Validate() },
			invalid:   func() error { return RoomID("").Validate() },
			spaceOnly: func() error { return RoomID(" ").Validate() },
		},
		{
			name:      "MatchID",
			valid:     func() error { return MatchID("match-1").Validate() },
			invalid:   func() error { return MatchID("").Validate() },
			spaceOnly: func() error { return MatchID(" ").Validate() },
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.valid(); err != nil {
				t.Fatalf("valid value rejected: %v", err)
			}
			if err := test.spaceOnly(); err != nil {
				t.Fatalf("schema-valid non-empty value rejected: %v", err)
			}
			if err := test.invalid(); !errors.Is(err, ErrInvalidID) {
				t.Fatalf("empty value error = %v, want ErrInvalidID", err)
			}
		})
	}
}

func TestStringIDsUseStableJSONStringRepresentation(t *testing.T) {
	tests := []struct {
		name  string
		value any
		want  string
	}{
		{name: "PlayerID", value: PlayerID("player-1"), want: `"player-1"`},
		{name: "PieceID", value: PieceID("A-1"), want: `"A-1"`},
		{name: "SpaceID", value: SpaceID("chammeogi"), want: `"chammeogi"`},
		{name: "RoomID", value: RoomID("room-1"), want: `"room-1"`},
		{name: "MatchID", value: MatchID("match-1"), want: `"match-1"`},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := json.Marshal(test.value)
			if err != nil {
				t.Fatalf("Marshal() error = %v", err)
			}
			if string(got) != test.want {
				t.Fatalf("Marshal() = %s, want %s", got, test.want)
			}
		})
	}
}

func TestStringIDsUnmarshalJSONString(t *testing.T) {
	t.Run("PlayerID", func(t *testing.T) {
		assertJSONRoundTrip(t, PlayerID("player-1"))
	})
	t.Run("PieceID", func(t *testing.T) {
		assertJSONRoundTrip(t, PieceID("A-1"))
	})
	t.Run("SpaceID", func(t *testing.T) {
		assertJSONRoundTrip(t, SpaceID("chammeogi"))
	})
	t.Run("RoomID", func(t *testing.T) {
		assertJSONRoundTrip(t, RoomID("room-1"))
	})
	t.Run("MatchID", func(t *testing.T) {
		assertJSONRoundTrip(t, MatchID("match-1"))
	})
	t.Run("TeamID", func(t *testing.T) {
		assertJSONRoundTrip(t, TeamA)
	})
	t.Run("PieceState", func(t *testing.T) {
		assertJSONRoundTrip(t, PieceOnBoard)
	})
	t.Run("YutResult", func(t *testing.T) {
		assertJSONRoundTrip(t, YutBackdo)
	})
	t.Run("TurnPhase", func(t *testing.T) {
		assertJSONRoundTrip(t, TurnWaitMoveSelection)
	})
	t.Run("RequiredInput", func(t *testing.T) {
		assertJSONRoundTrip(t, InputSelectMove)
	})
	t.Run("Route", func(t *testing.T) {
		assertJSONRoundTrip(t, RouteShortcut)
	})
	t.Run("MovementKind", func(t *testing.T) {
		assertJSONRoundTrip(t, MovementFinish)
	})
}

func TestStringIDsRejectInvalidJSON(t *testing.T) {
	t.Run("PlayerID", func(t *testing.T) {
		assertInvalidIDJSONPreserves(t, PlayerID("player-1"))
	})
	t.Run("PieceID", func(t *testing.T) {
		assertInvalidIDJSONPreserves(t, PieceID("A-1"))
	})
	t.Run("SpaceID", func(t *testing.T) {
		assertInvalidIDJSONPreserves(t, SpaceID("chammeogi"))
	})
	t.Run("RoomID", func(t *testing.T) {
		assertInvalidIDJSONPreserves(t, RoomID("room-1"))
	})
	t.Run("MatchID", func(t *testing.T) {
		assertInvalidIDJSONPreserves(t, MatchID("match-1"))
	})
}

func TestInvalidStringIDsCannotBeMarshaled(t *testing.T) {
	values := []any{
		PlayerID(""),
		PieceID(""),
		SpaceID(""),
		RoomID(""),
		MatchID(""),
	}

	for _, value := range values {
		if _, err := json.Marshal(value); !errors.Is(err, ErrInvalidID) {
			t.Errorf("Marshal(%T) error = %v, want ErrInvalidID", value, err)
		}
	}
}

func assertJSONRoundTrip[T comparable](t *testing.T, want T) {
	t.Helper()

	data, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	var got T
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if got != want {
		t.Fatalf("round trip = %v, want %v", got, want)
	}
}

func assertInvalidIDJSONPreserves[T comparable](t *testing.T, initial T) {
	t.Helper()

	for _, input := range []string{`""`, `42`, `null`} {
		value := initial
		err := json.Unmarshal([]byte(input), &value)
		if input == `""` && !errors.Is(err, ErrInvalidID) {
			t.Errorf("Unmarshal(%s) error = %v, want ErrInvalidID", input, err)
		} else if err == nil {
			t.Errorf("Unmarshal(%s) error = nil", input)
		}
		if value != initial {
			t.Errorf("receiver after Unmarshal(%s) = %v, want %v", input, value, initial)
		}
	}
}
