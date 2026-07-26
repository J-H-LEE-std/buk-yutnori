package domain

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestCanonicalEnumValues(t *testing.T) {
	tests := []struct {
		name   string
		values []validatableString
		want   []string
	}{
		{
			name:   "TeamID",
			values: []validatableString{TeamA, TeamB},
			want:   []string{"A", "B"},
		},
		{
			name: "PieceState",
			values: []validatableString{
				PieceWaiting,
				PieceOnBoard,
				PieceHomeCheckpoint,
				PieceFinished,
			},
			want: []string{"waiting", "on_board", "home_checkpoint", "finished"},
		},
		{
			name: "YutResult",
			values: []validatableString{
				YutDo,
				YutGae,
				YutGeol,
				YutYut,
				YutMo,
				YutBackdo,
				YutBuk,
			},
			want: []string{"do", "gae", "geol", "yut", "mo", "backdo", "buk"},
		},
		{
			name: "TurnPhase",
			values: []validatableString{
				TurnStart,
				TurnWaitThrow,
				TurnThrowingChain,
				TurnResolveQueue,
				TurnWaitResultSelection,
				TurnWaitPieceSelection,
				TurnWaitRouteSelection,
				TurnApplyMove,
				TurnResolveStackCaptureFinish,
				TurnResolveBuk,
				TurnCPUControl,
				TurnPaused,
				TurnEnd,
				TurnMatchEnd,
			},
			want: []string{
				"turn_start",
				"wait_throw",
				"throwing_chain",
				"resolve_queue",
				"wait_result_selection",
				"wait_piece_selection",
				"wait_route_selection",
				"apply_move",
				"resolve_stack_capture_finish",
				"resolve_buk",
				"cpu_control",
				"paused",
				"turn_end",
				"match_end",
			},
		},
		{
			name: "RequiredInput",
			values: []validatableString{
				InputNone,
				InputThrow,
				InputSelectResult,
				InputSelectPiece,
				InputSelectRoute,
			},
			want: []string{"none", "throw", "select_result", "select_piece", "select_route"},
		},
		{
			name:   "Route",
			values: []validatableString{RouteNormal, RouteShortcut},
			want:   []string{"normal", "shortcut"},
		},
		{
			name: "MovementKind",
			values: []validatableString{
				MovementForward,
				MovementBackdo,
				MovementBuk,
				MovementFinish,
			},
			want: []string{"forward", "backdo", "buk", "finish"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if len(test.values) != len(test.want) {
				t.Fatalf("test setup: %d values, want %d", len(test.values), len(test.want))
			}
			for index, value := range test.values {
				if err := value.Validate(); err != nil {
					t.Errorf("Validate(%q) error = %v", value.String(), err)
				}
				got, err := json.Marshal(value)
				if err != nil {
					t.Errorf("Marshal(%q) error = %v", value.String(), err)
					continue
				}
				wantJSON, err := json.Marshal(test.want[index])
				if err != nil {
					t.Fatalf("Marshal(want) error = %v", err)
				}
				if string(got) != string(wantJSON) {
					t.Errorf("Marshal(%q) = %s, want %s", value.String(), got, wantJSON)
				}
			}
		})
	}
}

func TestEnumsRejectUnknownValues(t *testing.T) {
	tests := []struct {
		name  string
		value validatableString
	}{
		{name: "TeamID", value: TeamID("C")},
		{name: "PieceState", value: PieceState("captured")},
		{name: "YutResult", value: YutResult("back_do")},
		{name: "TurnPhase", value: TurnPhase("moving")},
		{name: "RequiredInput", value: RequiredInput("select_team")},
		{name: "Route", value: Route("center")},
		{name: "MovementKind", value: MovementKind("reverse")},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.value.Validate(); !errors.Is(err, ErrInvalidEnumValue) {
				t.Fatalf("Validate() error = %v, want ErrInvalidEnumValue", err)
			}
			if _, err := json.Marshal(test.value); !errors.Is(err, ErrInvalidEnumValue) {
				t.Fatalf("Marshal() error = %v, want ErrInvalidEnumValue", err)
			}
		})
	}
}

func TestEnumsRejectInvalidJSONWithoutChangingReceiver(t *testing.T) {
	t.Run("TeamID", func(t *testing.T) {
		assertInvalidEnumJSONPreserves(t, TeamA)
	})
	t.Run("PieceState", func(t *testing.T) {
		assertInvalidEnumJSONPreserves(t, PieceWaiting)
	})
	t.Run("YutResult", func(t *testing.T) {
		assertInvalidEnumJSONPreserves(t, YutDo)
	})
	t.Run("TurnPhase", func(t *testing.T) {
		assertInvalidEnumJSONPreserves(t, TurnStart)
	})
	t.Run("RequiredInput", func(t *testing.T) {
		assertInvalidEnumJSONPreserves(t, InputNone)
	})
	t.Run("Route", func(t *testing.T) {
		assertInvalidEnumJSONPreserves(t, RouteNormal)
	})
	t.Run("MovementKind", func(t *testing.T) {
		assertInvalidEnumJSONPreserves(t, MovementForward)
	})
}

type validatableString interface {
	Validate() error
	String() string
}

func assertInvalidEnumJSONPreserves[T comparable](t *testing.T, initial T) {
	t.Helper()

	for _, input := range []string{`"unknown"`, `7`, `null`} {
		value := initial
		err := json.Unmarshal([]byte(input), &value)
		if input == `"unknown"` && !errors.Is(err, ErrInvalidEnumValue) {
			t.Errorf("Unmarshal(%s) error = %v, want ErrInvalidEnumValue", input, err)
		} else if err == nil {
			t.Errorf("Unmarshal(%s) error = nil", input)
		}
		if value != initial {
			t.Errorf("receiver after Unmarshal(%s) = %v, want %v", input, value, initial)
		}
	}
}
