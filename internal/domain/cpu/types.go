package cpu

import (
	"buk-yutnori/internal/domain"
	"buk-yutnori/internal/domain/match"
)

// Action identifies the next authoritative operation proposed by the CPU.
type Action string

const (
	ActionMovePiece     Action = "move_piece"
	ActionResolveBuk    Action = "resolve_buk"
	ActionDiscardResult Action = "discard_result"
)

// Decision is an uncommitted CPU proposal that the authoritative match and
// turn engines must validate again before applying.
type Decision struct {
	Action  Action
	TokenID domain.ResultTokenID
	Result  domain.YutResult
	PieceID domain.PieceID
	Route   domain.Route
}

// MatchReader is the read-only match boundary required by Policy.
type MatchReader interface {
	Snapshot() match.Snapshot
	OrdinaryMovePlans(
		teamID domain.TeamID,
		pieceID domain.PieceID,
		result domain.YutResult,
	) ([]match.OrdinaryMovePlan, error)
	BackdoMovePlan(teamID domain.TeamID, pieceID domain.PieceID) (match.BackdoMovePlan, error)
}

// BoundedSource supplies a uniform integer in the half-open interval [0, limit).
// math/rand/v2.Rand satisfies this interface.
type BoundedSource interface {
	Uint64N(limit uint64) uint64
}
