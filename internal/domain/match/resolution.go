package match

import (
	"buk-yutnori/internal/domain"
	"buk-yutnori/internal/domain/room"
)

func (game *Game) applyOrdinaryMovePlanLocked(
	teamID domain.TeamID,
	pieceID domain.PieceID,
	result domain.YutResult,
	plan OrdinaryMovePlan,
	movingIndices []int,
) MoveOutcome {
	selectedIndex := game.pieceIndex[pieceID]
	fromSpaceID := game.pieces[selectedIndex].CurrentSpaceID
	movedPieceIDs := game.pieceIDsLocked(movingIndices)
	destinationAllies := game.destinationAlliesLocked(teamID, plan, movingIndices)
	capturedIndices := game.capturedPieceIndicesLocked(teamID, plan)
	capturedPieceIDs := game.pieceIDsLocked(capturedIndices)

	for _, index := range capturedIndices {
		game.pieces[index].State = domain.PieceWaiting
		game.pieces[index].CurrentSpaceID = ""
		game.pieces[index].ActualPreviousSpace = ""
	}
	finalGroup := append(append([]int(nil), movingIndices...), destinationAllies...)
	for _, index := range finalGroup {
		game.pieces[index].State = plan.DestinationState
		game.pieces[index].CurrentSpaceID = plan.DestinationSpaceID
		game.pieces[index].ActualPreviousSpace = plan.ActualPreviousSpace
		if plan.DestinationState == domain.PieceFinished {
			game.pieces[index].CurrentSpaceID = ""
			game.pieces[index].ActualPreviousSpace = ""
		}
	}

	matchEnded := game.allTeamPiecesFinishedLocked(teamID)
	if matchEnded {
		game.winnerTeamID = teamID
	}
	movementKind := domain.MovementForward
	if plan.DestinationState == domain.PieceFinished {
		movementKind = domain.MovementFinish
	}

	var stackedPieceIDs []domain.PieceID
	if game.settings.StackingEnabled && plan.DestinationState != domain.PieceFinished &&
		len(finalGroup) > 1 {
		stackedPieceIDs = game.teamPieceIDsAtDestinationLocked(teamID, plan)
	}
	captureExtraThrow := len(capturedPieceIDs) > 0 &&
		game.captureGrantsExtraThrowLocked(result)
	if matchEnded {
		captureExtraThrow = false
	}

	return MoveOutcome{
		Route:             plan.Route,
		MovementKind:      movementKind,
		FromSpaceID:       fromSpaceID,
		ToSpaceID:         plan.DestinationSpaceID,
		MovedPieceIDs:     movedPieceIDs,
		StackedPieceIDs:   stackedPieceIDs,
		CapturedPieceIDs:  capturedPieceIDs,
		CaptureExtraThrow: captureExtraThrow,
		MatchEnded:        matchEnded,
		WinnerTeamID:      game.winnerTeamID,
	}
}

func (game *Game) destinationAlliesLocked(
	teamID domain.TeamID,
	plan OrdinaryMovePlan,
	movingIndices []int,
) []int {
	if !game.settings.StackingEnabled || plan.DestinationState == domain.PieceFinished {
		return nil
	}
	moving := make(map[int]bool, len(movingIndices))
	for _, index := range movingIndices {
		moving[index] = true
	}
	var indices []int
	for index, piece := range game.pieces {
		if !moving[index] && piece.TeamID == teamID && piece.State == plan.DestinationState &&
			piece.CurrentSpaceID == plan.DestinationSpaceID {
			indices = append(indices, index)
		}
	}
	return indices
}

func (game *Game) capturedPieceIndicesLocked(
	teamID domain.TeamID,
	plan OrdinaryMovePlan,
) []int {
	if plan.DestinationState == domain.PieceFinished {
		return nil
	}
	var indices []int
	for index, piece := range game.pieces {
		if piece.TeamID != teamID && piece.State == plan.DestinationState &&
			piece.CurrentSpaceID == plan.DestinationSpaceID {
			indices = append(indices, index)
		}
	}
	return indices
}

func (game *Game) teamPieceIDsAtDestinationLocked(
	teamID domain.TeamID,
	plan OrdinaryMovePlan,
) []domain.PieceID {
	var ids []domain.PieceID
	for _, piece := range game.pieces {
		if piece.TeamID == teamID && piece.State == plan.DestinationState &&
			piece.CurrentSpaceID == plan.DestinationSpaceID {
			ids = append(ids, piece.ID)
		}
	}
	return ids
}

func (game *Game) allTeamPiecesFinishedLocked(teamID domain.TeamID) bool {
	for _, piece := range game.pieces {
		if piece.TeamID == teamID && piece.State != domain.PieceFinished {
			return false
		}
	}
	return true
}

func (game *Game) captureGrantsExtraThrowLocked(result domain.YutResult) bool {
	switch game.settings.CaptureExtraThrow {
	case room.CaptureExtraThrowAlways:
		return true
	case room.CaptureExtraThrowDoToGeolPlusSpecial:
		return result == domain.YutDo || result == domain.YutGae || result == domain.YutGeol
	case room.CaptureExtraThrowNone:
		return false
	default:
		return false
	}
}
