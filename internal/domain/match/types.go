package match

import (
	"buk-yutnori/internal/domain"
	"buk-yutnori/internal/domain/turn"
)

// TeamSetup declares the stable piece IDs owned by one canonical team.
type TeamSetup struct {
	TeamID   domain.TeamID
	PieceIDs []domain.PieceID
}

// Piece is the authoritative dynamic state of one piece.
type Piece struct {
	ID                  domain.PieceID
	TeamID              domain.TeamID
	State               domain.PieceState
	CurrentSpaceID      domain.SpaceID
	ActualPreviousSpace domain.SpaceID
}

// Snapshot is an atomic copy of all match state exposed by Game.
type Snapshot struct {
	Pieces       []Piece
	WinnerTeamID domain.TeamID
}

// OrdinaryMovePlan is one currently legal ordinary forward movement.
type OrdinaryMovePlan struct {
	Route               domain.Route
	DestinationState    domain.PieceState
	DestinationSpaceID  domain.SpaceID
	ActualPreviousSpace domain.SpaceID
	Traversed           []domain.SpaceID
	MovedPieceIDs       []domain.PieceID
}

// MoveOutcome describes an ordinary movement already committed to the game.
type MoveOutcome struct {
	Route             domain.Route
	MovementKind      domain.MovementKind
	FromSpaceID       domain.SpaceID
	ToSpaceID         domain.SpaceID
	MovedPieceIDs     []domain.PieceID
	StackedPieceIDs   []domain.PieceID
	CapturedPieceIDs  []domain.PieceID
	CaptureExtraThrow bool
	MatchEnded        bool
	WinnerTeamID      domain.TeamID
}

// TurnOutcome returns the decisions needed by the turn state machine.
func (outcome MoveOutcome) TurnOutcome() turn.MoveOutcome {
	return turn.MoveOutcome{
		CaptureExtraThrow: outcome.CaptureExtraThrow,
		MatchEnded:        outcome.MatchEnded,
	}
}
