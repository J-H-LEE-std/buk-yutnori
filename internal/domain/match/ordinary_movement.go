package match

import (
	"fmt"

	"buk-yutnori/internal/domain"
	"buk-yutnori/internal/domain/board"
	"buk-yutnori/internal/domain/room"
)

// OrdinaryMovePlans returns the currently legal forward plans without mutation.
func (game *Game) OrdinaryMovePlans(
	teamID domain.TeamID,
	pieceID domain.PieceID,
	result domain.YutResult,
) ([]OrdinaryMovePlan, error) {
	game.mutex.RLock()
	defer game.mutex.RUnlock()

	plans, _, err := game.ordinaryMovePlansLocked(teamID, pieceID, result)
	if err != nil {
		return nil, err
	}
	return cloneOrdinaryMovePlans(plans), nil
}

func (game *Game) ordinaryMovePlansLocked(
	teamID domain.TeamID,
	pieceID domain.PieceID,
	result domain.YutResult,
) ([]OrdinaryMovePlan, []int, error) {
	if game.winnerTeamID != "" {
		return nil, nil, ErrMatchEnded
	}
	selectedIndex, err := game.ownedPieceIndexLocked(teamID, pieceID)
	if err != nil {
		return nil, nil, err
	}
	spaces, err := result.OrdinaryMovementSpaces()
	if err != nil {
		return nil, nil, err
	}

	movingIndices := game.movingPieceIndicesLocked(selectedIndex)
	movedPieceIDs := game.pieceIDsLocked(movingIndices)
	selected := game.pieces[selectedIndex]
	plannerPlans, err := game.planner.ForwardPlans(
		board.Position{State: selected.State, Space: selected.CurrentSpaceID},
		spaces,
		boardShortcutPolicy(game.settings.ShortcutPolicy),
	)
	if err != nil {
		return nil, nil, err
	}
	if err := validateForwardPlans(plannerPlans, game.settings.ShortcutPolicy); err != nil {
		return nil, nil, err
	}

	plans := make([]OrdinaryMovePlan, len(plannerPlans))
	for index, plannerPlan := range plannerPlans {
		plans[index] = OrdinaryMovePlan{
			Route:               plannerPlan.Route,
			DestinationState:    plannerPlan.Destination.State,
			DestinationSpaceID:  plannerPlan.Destination.Space,
			ActualPreviousSpace: plannerPlan.ActualPreviousSpace,
			Traversed:           append([]domain.SpaceID(nil), plannerPlan.Traversed...),
			MovedPieceIDs:       append([]domain.PieceID(nil), movedPieceIDs...),
		}
	}
	return plans, movingIndices, nil
}

func boardShortcutPolicy(policy room.ShortcutPolicy) board.ShortcutPolicy {
	if policy == room.ShortcutForced {
		return board.ForcedShortcuts
	}
	return board.SelectableShortcuts
}

func validateForwardPlans(plans []board.ForwardPlan, policy room.ShortcutPolicy) error {
	switch len(plans) {
	case 0:
		return fmt.Errorf("%w: planner returned no plans", ErrInvalidForwardPlan)
	case 1:
		if policy == room.ShortcutSelectable && plans[0].Route != domain.RouteNormal {
			return fmt.Errorf(
				"%w: selectable policy requires a normal single plan",
				ErrInvalidForwardPlan,
			)
		}
	case 2:
		if policy != room.ShortcutSelectable {
			return fmt.Errorf(
				"%w: forced policy cannot return a route choice",
				ErrInvalidForwardPlan,
			)
		}
		if plans[0].Route != domain.RouteNormal || plans[1].Route != domain.RouteShortcut {
			return fmt.Errorf(
				"%w: selectable plans must be normal followed by shortcut",
				ErrInvalidForwardPlan,
			)
		}
	default:
		return fmt.Errorf(
			"%w: planner returned %d plans, want at most 2",
			ErrInvalidForwardPlan,
			len(plans),
		)
	}
	for _, plan := range plans {
		if err := validateForwardPlan(plan); err != nil {
			return err
		}
	}
	return nil
}

func validateForwardPlan(plan board.ForwardPlan) error {
	if err := plan.Route.Validate(); err != nil {
		return fmt.Errorf("%w: route: %w", ErrInvalidForwardPlan, err)
	}
	for index, spaceID := range plan.Traversed {
		if err := spaceID.Validate(); err != nil {
			return fmt.Errorf("%w: traversed[%d]: %w", ErrInvalidForwardPlan, index, err)
		}
	}
	switch plan.Destination.State {
	case domain.PieceOnBoard, domain.PieceHomeCheckpoint:
		if err := plan.Destination.Space.Validate(); err != nil {
			return fmt.Errorf("%w: destination space: %w", ErrInvalidForwardPlan, err)
		}
		if err := plan.ActualPreviousSpace.Validate(); err != nil {
			return fmt.Errorf("%w: actual previous space: %w", ErrInvalidForwardPlan, err)
		}
		if len(plan.Traversed) == 0 ||
			plan.Traversed[len(plan.Traversed)-1] != plan.Destination.Space {
			return fmt.Errorf(
				"%w: traversal must end at destination %q",
				ErrInvalidForwardPlan,
				plan.Destination.Space,
			)
		}
		if len(plan.Traversed) > 1 &&
			plan.Traversed[len(plan.Traversed)-2] != plan.ActualPreviousSpace {
			return fmt.Errorf(
				"%w: traversal predecessor must be actual previous space %q",
				ErrInvalidForwardPlan,
				plan.ActualPreviousSpace,
			)
		}
	case domain.PieceFinished:
		if plan.Destination.Space != "" || plan.ActualPreviousSpace != "" {
			return fmt.Errorf("%w: finished destination retains path state", ErrInvalidForwardPlan)
		}
	default:
		return fmt.Errorf(
			"%w: destination state %q",
			ErrInvalidForwardPlan,
			plan.Destination.State,
		)
	}
	return nil
}

func cloneOrdinaryMovePlans(plans []OrdinaryMovePlan) []OrdinaryMovePlan {
	cloned := make([]OrdinaryMovePlan, len(plans))
	for index, plan := range plans {
		cloned[index] = plan
		cloned[index].Traversed = append([]domain.SpaceID(nil), plan.Traversed...)
		cloned[index].MovedPieceIDs = append([]domain.PieceID(nil), plan.MovedPieceIDs...)
	}
	return cloned
}

// ApplyOrdinaryMove validates and atomically commits one ordinary forward move.
//
// Plans are recalculated while holding the game lock so a previously displayed
// plan cannot be applied after authoritative state changes.
func (game *Game) ApplyOrdinaryMove(
	teamID domain.TeamID,
	pieceID domain.PieceID,
	result domain.YutResult,
	selectedRoute domain.Route,
) (MoveOutcome, error) {
	game.mutex.Lock()
	defer game.mutex.Unlock()

	plans, movingIndices, err := game.ordinaryMovePlansLocked(teamID, pieceID, result)
	if err != nil {
		return MoveOutcome{}, err
	}
	plan, err := selectOrdinaryMovePlan(plans, selectedRoute)
	if err != nil {
		return MoveOutcome{}, err
	}
	return game.applyOrdinaryMovePlanLocked(teamID, pieceID, result, plan, movingIndices), nil
}

func selectOrdinaryMovePlan(
	plans []OrdinaryMovePlan,
	selectedRoute domain.Route,
) (OrdinaryMovePlan, error) {
	if len(plans) == 1 {
		if selectedRoute != "" {
			return OrdinaryMovePlan{}, ErrRouteSelectionNotAllowed
		}
		return plans[0], nil
	}
	if selectedRoute == "" {
		return OrdinaryMovePlan{}, ErrRouteSelectionRequired
	}
	if err := selectedRoute.Validate(); err != nil {
		return OrdinaryMovePlan{}, fmt.Errorf("%w: %w", ErrInvalidRouteSelection, err)
	}
	for _, plan := range plans {
		if plan.Route == selectedRoute {
			return plan, nil
		}
	}
	return OrdinaryMovePlan{}, fmt.Errorf("%w: %q", ErrInvalidRouteSelection, selectedRoute)
}
