package board

import (
	"errors"
	"fmt"

	"buk-yutnori/internal/domain"
)

var (
	// ErrInvalidForwardSpaces identifies a non-positive forward distance.
	ErrInvalidForwardSpaces = errors.New("invalid forward spaces")

	// ErrForwardMovementUnavailable identifies a state that cannot move forward.
	ErrForwardMovementUnavailable = errors.New("forward movement is unavailable")

	// ErrNoForwardSpace identifies an incomplete graph with no legal next space.
	ErrNoForwardSpace = errors.New("no legal forward space")
)

// ForwardPlanner is the minimal immutable board dependency required by a
// future piece and match movement engine.
type ForwardPlanner interface {
	ForwardPlans(position Position, spaces int, policy ShortcutPolicy) ([]ForwardPlan, error)
}

// ForwardPlan is one complete legal forward route for a movement request.
//
// Selectable route choices produce a normal plan followed by a shortcut plan.
// A single plan means no player route decision is required. Traversed contains
// entered logical board spaces and excludes the conceptual finish node.
type ForwardPlan struct {
	Route               domain.Route
	Destination         Position
	ActualPreviousSpace SpaceID
	Traversed           []SpaceID
}

var _ ForwardPlanner = (*Graph)(nil)

// ForwardPlans returns every legal forward plan from position for spaces.
//
// A route policy applies only when the movement starts on a route-choice
// space. Route choices encountered after movement begins use their canonical
// normal edge, because a shortcut is available only to a piece that stopped
// exactly on the choice space before this movement.
func (g *Graph) ForwardPlans(
	position Position,
	spaces int,
	policy ShortcutPolicy,
) ([]ForwardPlan, error) {
	if spaces <= 0 {
		return nil, fmt.Errorf("%w: got %d", ErrInvalidForwardSpaces, spaces)
	}
	if err := validateShortcutPolicy(policy); err != nil {
		return nil, err
	}

	origin, atHomeCheckpoint, err := g.forwardOrigin(position)
	if err != nil {
		return nil, err
	}
	if atHomeCheckpoint {
		return []ForwardPlan{{
			Route:       domain.RouteNormal,
			Destination: Position{State: PieceFinished},
		}}, nil
	}

	choice, startsAtChoice := g.routeChoices[origin]
	switch {
	case startsAtChoice && policy == SelectableShortcuts:
		normal, err := g.buildForwardPlan(origin, spaces, domain.RouteNormal, choice.normal)
		if err != nil {
			return nil, err
		}
		shortcut, err := g.buildForwardPlan(origin, spaces, domain.RouteShortcut, choice.shortcut)
		if err != nil {
			return nil, err
		}
		return []ForwardPlan{normal, shortcut}, nil
	case startsAtChoice && policy == ForcedShortcuts:
		destination, ok := g.forcedEdges[origin]
		if !ok {
			return nil, fmt.Errorf("%w: forced route from %q", ErrNoForwardSpace, origin)
		}
		plan, err := g.buildForwardPlan(origin, spaces, domain.RouteShortcut, destination)
		if err != nil {
			return nil, err
		}
		return []ForwardPlan{plan}, nil
	default:
		plan, err := g.buildForwardPlan(origin, spaces, domain.RouteNormal, "")
		if err != nil {
			return nil, err
		}
		return []ForwardPlan{plan}, nil
	}
}

func (g *Graph) forwardOrigin(position Position) (SpaceID, bool, error) {
	switch position.State {
	case PieceWaiting:
		if position.Space != "" {
			return "", false, fmt.Errorf(
				"%w: waiting piece must not have space %q",
				ErrInvalidPosition,
				position.Space,
			)
		}
		return g.startSpace, false, nil
	case PieceOnBoard:
		if _, ok := g.nodeByID[position.Space]; !ok {
			return "", false, fmt.Errorf("%w: %q", ErrUnknownSpace, position.Space)
		}
		if position.Space == g.homeCheckpointSpace {
			return "", false, fmt.Errorf(
				"%w: %q must use home checkpoint state",
				ErrInvalidPosition,
				position.Space,
			)
		}
		return position.Space, false, nil
	case PieceHomeCheckpoint:
		if position.Space != g.homeCheckpointSpace {
			return "", false, fmt.Errorf(
				"%w: home checkpoint must be %q, got %q",
				ErrInvalidPosition,
				g.homeCheckpointSpace,
				position.Space,
			)
		}
		return position.Space, true, nil
	case PieceFinished:
		if position.Space != "" {
			return "", false, fmt.Errorf(
				"%w: finished piece must not have space %q",
				ErrInvalidPosition,
				position.Space,
			)
		}
		return "", false, fmt.Errorf(
			"%w: piece state %q",
			ErrForwardMovementUnavailable,
			position.State,
		)
	default:
		return "", false, fmt.Errorf(
			"%w: unknown piece state %q",
			ErrInvalidPosition,
			position.State,
		)
	}
}

func (g *Graph) buildForwardPlan(
	origin SpaceID,
	spaces int,
	route domain.Route,
	firstDestination SpaceID,
) (ForwardPlan, error) {
	current := origin
	var actualPrevious SpaceID
	var traversed []SpaceID
	homeCheckpointReached := false

	for step := 0; step < spaces; step++ {
		if homeCheckpointReached {
			return ForwardPlan{
				Route:       route,
				Destination: Position{State: PieceFinished},
				Traversed:   traversed,
			}, nil
		}

		var next SpaceID
		if step == 0 && firstDestination != "" {
			next = firstDestination
		} else {
			var err error
			next, err = g.normalForwardSpace(current)
			if err != nil {
				return ForwardPlan{}, err
			}
		}
		actualPrevious = current
		current = next
		traversed = append(traversed, next)
		homeCheckpointReached = current == g.homeCheckpointSpace
	}

	destination := Position{State: PieceOnBoard, Space: current}
	if homeCheckpointReached {
		destination.State = PieceHomeCheckpoint
	}
	return ForwardPlan{
		Route:               route,
		Destination:         destination,
		ActualPreviousSpace: actualPrevious,
		Traversed:           traversed,
	}, nil
}

func (g *Graph) normalForwardSpace(origin SpaceID) (SpaceID, error) {
	if choice, ok := g.routeChoices[origin]; ok {
		return choice.normal, nil
	}
	destinations := g.adjacency[origin]
	if len(destinations) != 1 {
		return "", fmt.Errorf(
			"%w: normal route from %q has %d destinations",
			ErrNoForwardSpace,
			origin,
			len(destinations),
		)
	}
	return destinations[0], nil
}
