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
	Pieces                []Piece
	WinnerTeamID          domain.TeamID
	BukDestinationSpaceID domain.SpaceID
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

// BackdoMovePlan is the currently legal reversal along recorded path history.
type BackdoMovePlan struct {
	DestinationState    domain.PieceState
	DestinationSpaceID  domain.SpaceID
	ActualPreviousSpace domain.SpaceID
	Traversed           []domain.SpaceID
	MovedPieceIDs       []domain.PieceID
}

// BukOutcome describes one automatic Buk resolution.
//
// Move is populated only when the selected group changes spaces. A selected
// group already at DestinationSpaceID consumes Buk without a move.
type BukOutcome struct {
	NoCandidate        bool
	Moved              bool
	DestinationSpaceID domain.SpaceID
	SelectedPieceIDs   []domain.PieceID
	Move               MoveOutcome
}

// TurnOutcome returns the decisions needed by the turn state machine.
func (outcome BukOutcome) TurnOutcome() turn.BukOutcome {
	return turn.BukOutcome{
		NoCandidate:       outcome.NoCandidate,
		CaptureExtraThrow: outcome.Move.CaptureExtraThrow,
		MatchEnded:        outcome.Move.MatchEnded,
	}
}

// MoveOutcome describes one piece movement already committed to the game.
type MoveOutcome struct {
	Route               domain.Route
	MovementKind        domain.MovementKind
	FromSpaceID         domain.SpaceID
	ToSpaceID           domain.SpaceID
	ActualPreviousSpace domain.SpaceID
	MovedPieceIDs       []domain.PieceID
	StackedPieceIDs     []domain.PieceID
	CapturedPieceIDs    []domain.PieceID
	CaptureExtraThrow   bool
	MatchEnded          bool
	WinnerTeamID        domain.TeamID
}

// TurnOutcome returns the decisions needed by the turn state machine.
func (outcome MoveOutcome) TurnOutcome() turn.MoveOutcome {
	return turn.MoveOutcome{
		CaptureExtraThrow: outcome.CaptureExtraThrow,
		MatchEnded:        outcome.MatchEnded,
	}
}
