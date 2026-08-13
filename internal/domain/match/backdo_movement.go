package match

import (
	"fmt"

	"buk-yutnori/internal/domain"
	"buk-yutnori/internal/domain/board"
)

// BackdoMovePlan returns the selected piece's legal reversal without mutation.
func (game *Game) BackdoMovePlan(
	teamID domain.TeamID,
	pieceID domain.PieceID,
) (BackdoMovePlan, error) {
	game.mutex.RLock()
	defer game.mutex.RUnlock()

	plan, _, err := game.backdoMovePlanLocked(teamID, pieceID)
	if err != nil {
		return BackdoMovePlan{}, err
	}
	return cloneBackdoMovePlan(plan), nil
}

func (game *Game) backdoMovePlanLocked(
	teamID domain.TeamID,
	pieceID domain.PieceID,
) (BackdoMovePlan, []int, error) {
	if game.winnerTeamID != "" {
		return BackdoMovePlan{}, nil, ErrMatchEnded
	}
	selectedIndex, err := game.ownedPieceIndexLocked(teamID, pieceID)
	if err != nil {
		return BackdoMovePlan{}, nil, err
	}
	selected := game.pieces[selectedIndex]
	plannerPlan, err := game.planner.BackdoPlan(
		board.Position{State: selected.State, Space: selected.CurrentSpaceID},
		selected.ActualPreviousSpace,
	)
	if err != nil {
		return BackdoMovePlan{}, nil, err
	}
	if err := validateBackdoPlan(plannerPlan, selected.CurrentSpaceID); err != nil {
		return BackdoMovePlan{}, nil, err
	}

	movingIndices := game.movingPieceIndicesLocked(selectedIndex)
	return BackdoMovePlan{
		DestinationState:    plannerPlan.Destination.State,
		DestinationSpaceID:  plannerPlan.Destination.Space,
		ActualPreviousSpace: plannerPlan.ActualPreviousSpace,
		Traversed:           append([]domain.SpaceID(nil), plannerPlan.Traversed...),
		MovedPieceIDs:       game.pieceIDsLocked(movingIndices),
	}, movingIndices, nil
}

func validateBackdoPlan(plan board.BackdoPlan, sourceSpaceID domain.SpaceID) error {
	if plan.Destination.State != domain.PieceOnBoard &&
		plan.Destination.State != domain.PieceHomeCheckpoint {
		return fmt.Errorf(
			"%w: Backdo destination state %q",
			ErrInvalidBackdoPlan,
			plan.Destination.State,
		)
	}
	if err := plan.Destination.Space.Validate(); err != nil {
		return fmt.Errorf("%w: destination space: %w", ErrInvalidBackdoPlan, err)
	}
	if plan.ActualPreviousSpace != sourceSpaceID {
		return fmt.Errorf(
			"%w: previous %q does not match source %q",
			ErrInvalidBackdoPlan,
			plan.ActualPreviousSpace,
			sourceSpaceID,
		)
	}
	if len(plan.Traversed) != 1 || plan.Traversed[0] != plan.Destination.Space {
		return fmt.Errorf(
			"%w: traversal must contain only destination %q",
			ErrInvalidBackdoPlan,
			plan.Destination.Space,
		)
	}
	return nil
}

func cloneBackdoMovePlan(plan BackdoMovePlan) BackdoMovePlan {
	plan.Traversed = append([]domain.SpaceID(nil), plan.Traversed...)
	plan.MovedPieceIDs = append([]domain.PieceID(nil), plan.MovedPieceIDs...)
	return plan
}

// ApplyBackdoMove validates and atomically commits one canonical reversal.
func (game *Game) ApplyBackdoMove(
	teamID domain.TeamID,
	pieceID domain.PieceID,
) (MoveOutcome, error) {
	game.mutex.Lock()
	defer game.mutex.Unlock()

	plan, movingIndices, err := game.backdoMovePlanLocked(teamID, pieceID)
	if err != nil {
		return MoveOutcome{}, err
	}
	return game.applyMoveResolutionLocked(
		teamID,
		pieceID,
		domain.YutBackdo,
		moveResolutionPlan{
			MovementKind:        domain.MovementBackdo,
			DestinationState:    plan.DestinationState,
			DestinationSpaceID:  plan.DestinationSpaceID,
			ActualPreviousSpace: plan.ActualPreviousSpace,
			MovingIndices:       movingIndices,
		},
	), nil
}
