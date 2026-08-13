package board

import (
	"errors"
	"testing"
)

func TestGraphImplementsBackdoPlanner(t *testing.T) {
	var _ BackdoPlanner = loadCanonicalGraph(t)
}

func TestBackdoPlanUsesActualPreviousSpace(t *testing.T) {
	graph := loadCanonicalGraph(t)
	tests := []struct {
		name             string
		position         Position
		actualPrevious   SpaceID
		wantDestination  Position
		wantNextPrevious SpaceID
	}{
		{
			name:             "first space returns to home checkpoint",
			position:         Position{State: PieceOnBoard, Space: "do"},
			actualPrevious:   "chammeogi",
			wantDestination:  Position{State: PieceHomeCheckpoint, Space: "chammeogi"},
			wantNextPrevious: "do",
		},
		{
			name:             "home checkpoint returns to entry space",
			position:         Position{State: PieceHomeCheckpoint, Space: "chammeogi"},
			actualPrevious:   "nal_yut",
			wantDestination:  Position{State: PieceOnBoard, Space: "nal_yut"},
			wantNextPrevious: "chammeogi",
		},
		{
			name:             "center is entered only from recorded history",
			position:         Position{State: PieceOnBoard, Space: "sok_yut"},
			actualPrevious:   "bang",
			wantDestination:  Position{State: PieceOnBoard, Space: "bang"},
			wantNextPrevious: "sok_yut",
		},
		{
			name:             "Buk history may be a non-adjacent board space",
			position:         Position{State: PieceOnBoard, Space: "jji_do"},
			actualPrevious:   "mo",
			wantDestination:  Position{State: PieceOnBoard, Space: "mo"},
			wantNextPrevious: "jji_do",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			plan, err := graph.BackdoPlan(test.position, test.actualPrevious)
			if err != nil {
				t.Fatalf("BackdoPlan() error = %v", err)
			}
			if plan.Destination != test.wantDestination ||
				plan.ActualPreviousSpace != test.wantNextPrevious {
				t.Fatalf(
					"BackdoPlan() = %#v, want destination %#v and previous %q",
					plan,
					test.wantDestination,
					test.wantNextPrevious,
				)
			}
			if len(plan.Traversed) != 1 || plan.Traversed[0] != test.wantDestination.Space {
				t.Fatalf("Traversed = %v, want [%s]", plan.Traversed, test.wantDestination.Space)
			}
		})
	}
}

func TestBackdoPlanRejectsUnavailableAndInvalidState(t *testing.T) {
	graph := loadCanonicalGraph(t)
	tests := []struct {
		name           string
		position       Position
		actualPrevious SpaceID
		wantError      error
	}{
		{
			name:      "waiting piece",
			position:  Position{State: PieceWaiting},
			wantError: ErrBackdoMovementUnavailable,
		},
		{
			name:      "finished piece",
			position:  Position{State: PieceFinished},
			wantError: ErrBackdoMovementUnavailable,
		},
		{
			name:           "missing path history",
			position:       Position{State: PieceOnBoard, Space: "do"},
			actualPrevious: "",
			wantError:      ErrBackdoHistoryUnavailable,
		},
		{
			name:           "unknown current space",
			position:       Position{State: PieceOnBoard, Space: "missing"},
			actualPrevious: "do",
			wantError:      ErrUnknownSpace,
		},
		{
			name:           "unknown previous space",
			position:       Position{State: PieceOnBoard, Space: "do"},
			actualPrevious: "missing",
			wantError:      ErrUnknownSpace,
		},
		{
			name:           "home space in on-board state",
			position:       Position{State: PieceOnBoard, Space: "chammeogi"},
			actualPrevious: "nal_yut",
			wantError:      ErrInvalidPosition,
		},
		{
			name:           "home-checkpoint state on wrong space",
			position:       Position{State: PieceHomeCheckpoint, Space: "do"},
			actualPrevious: "nal_yut",
			wantError:      ErrInvalidPosition,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := graph.BackdoPlan(test.position, test.actualPrevious)
			if !errors.Is(err, test.wantError) {
				t.Fatalf("BackdoPlan() error = %v, want %v", err, test.wantError)
			}
		})
	}
}
