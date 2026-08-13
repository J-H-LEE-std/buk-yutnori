package board

import (
	"errors"
	"fmt"
)

var (
	// ErrBackdoMovementUnavailable identifies a piece state that cannot move backward.
	ErrBackdoMovementUnavailable = errors.New("backdo movement is unavailable")

	// ErrBackdoHistoryUnavailable identifies a movable piece without a usable previous space.
	ErrBackdoHistoryUnavailable = errors.New("backdo path history is unavailable")
)

// MovementPlanner is the immutable board dependency required by the match engine.
type MovementPlanner interface {
	ForwardPlanner
	BackdoPlanner
}

// BackdoPlanner computes the canonical one-space reversal from recorded path state.
type BackdoPlanner interface {
	BackdoPlan(position Position, actualPrevious SpaceID) (BackdoPlan, error)
}

// BackdoPlan is the deterministic reversal of one piece or position group.
//
// ActualPreviousSpace is the path state to store after applying the plan.
// Traversed contains only the recorded destination. It may be non-adjacent in
// the static graph when the preceding movement was Buk.
type BackdoPlan struct {
	Destination         Position
	ActualPreviousSpace SpaceID
	Traversed           []SpaceID
}

var (
	_ BackdoPlanner   = (*Graph)(nil)
	_ MovementPlanner = (*Graph)(nil)
)

// BackdoPlan returns the one-space reversal recorded in actualPrevious.
func (g *Graph) BackdoPlan(position Position, actualPrevious SpaceID) (BackdoPlan, error) {
	switch position.State {
	case PieceWaiting, PieceFinished:
		return BackdoPlan{}, fmt.Errorf(
			"%w: piece state %q",
			ErrBackdoMovementUnavailable,
			position.State,
		)
	case PieceOnBoard:
		if _, ok := g.nodeByID[position.Space]; !ok {
			return BackdoPlan{}, fmt.Errorf("%w: %q", ErrUnknownSpace, position.Space)
		}
		if position.Space == g.homeCheckpointSpace {
			return BackdoPlan{}, fmt.Errorf(
				"%w: %q must use home checkpoint state",
				ErrInvalidPosition,
				position.Space,
			)
		}
	case PieceHomeCheckpoint:
		if position.Space != g.homeCheckpointSpace {
			return BackdoPlan{}, fmt.Errorf(
				"%w: home checkpoint must be %q, got %q",
				ErrInvalidPosition,
				g.homeCheckpointSpace,
				position.Space,
			)
		}
	default:
		return BackdoPlan{}, fmt.Errorf(
			"%w: unknown piece state %q",
			ErrInvalidPosition,
			position.State,
		)
	}

	if actualPrevious == "" {
		return BackdoPlan{}, fmt.Errorf(
			"%w: current %q",
			ErrBackdoHistoryUnavailable,
			position.Space,
		)
	}
	if _, ok := g.nodeByID[actualPrevious]; !ok {
		return BackdoPlan{}, fmt.Errorf("%w: %q", ErrUnknownSpace, actualPrevious)
	}

	destinationState := PieceOnBoard
	if actualPrevious == g.homeCheckpointSpace {
		destinationState = PieceHomeCheckpoint
	}
	return BackdoPlan{
		Destination: Position{
			State: destinationState,
			Space: actualPrevious,
		},
		ActualPreviousSpace: position.Space,
		Traversed:           []SpaceID{actualPrevious},
	}, nil
}
