package match

import "errors"

var (
	// ErrInvalidGameConfig identifies an invalid immutable game setup.
	ErrInvalidGameConfig = errors.New("invalid game configuration")

	// ErrUnknownPiece identifies a piece ID that does not belong to this game.
	ErrUnknownPiece = errors.New("unknown piece")

	// ErrPieceNotOwned identifies a piece selected by the opposing team.
	ErrPieceNotOwned = errors.New("piece is not owned by team")

	// ErrRouteSelectionRequired identifies a move with an unresolved route choice.
	ErrRouteSelectionRequired = errors.New("route selection is required")

	// ErrRouteSelectionNotAllowed identifies a route supplied for a deterministic move.
	ErrRouteSelectionNotAllowed = errors.New("route selection is not allowed")

	// ErrInvalidRouteSelection identifies a route that is not one of the current plans.
	ErrInvalidRouteSelection = errors.New("invalid route selection")

	// ErrMatchEnded identifies an attempted action after victory was decided.
	ErrMatchEnded = errors.New("match has ended")

	// ErrInvalidForwardPlan identifies a board planner result that cannot be applied.
	ErrInvalidForwardPlan = errors.New("invalid forward plan")
)
