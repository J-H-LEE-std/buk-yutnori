// Match event builders and hub emission helpers for the canonical match
// runtime. Every helper consumes exactly one room sequence through the shared
// ADR-0015 hub while the registry mutex is held.

package application

import (
	"errors"
	"fmt"

	"buk-yutnori/internal/domain"
	"buk-yutnori/internal/domain/board"
	"buk-yutnori/internal/domain/match"
	"buk-yutnori/internal/domain/turn"
	"buk-yutnori/internal/protocol"
)

func (registry *RoomRegistry) emitTurnStartedLocked(entry *registeredRoom, rt *matchRuntime) error {
	snapshot := rt.machine.Snapshot()
	return registry.emitLocked(rt.roomID, func(sequence uint64) (any, error) {
		return protocol.NewTurnStartedEvent(rt.roomID, rt.matchID, sequence, protocol.TurnStartedPayload{
			PlayerID:      snapshot.PlayerID,
			Phase:         snapshot.Phase,
			RequiredInput: snapshot.RequiredInput,
			RemainingMS:   rt.remainingMS(registry.matchClock.Now()),
		})
	})
}

func (registry *RoomRegistry) emitYutResultLocked(entry *registeredRoom, rt *matchRuntime, token turn.ResultToken) error {
	return registry.emitLocked(rt.roomID, func(sequence uint64) (any, error) {
		return protocol.NewYutResultEvent(rt.roomID, rt.matchID, sequence, protocol.YutResultPayload{
			PlayerID: token.GeneratedByPlayerID,
			Token: protocol.ResultTokenView{
				TokenID: token.ID,
				Result:  token.Result,
				Origin:  token.Origin,
			},
		})
	})
}

func (registry *RoomRegistry) emitResultQueueUpdatedLocked(entry *registeredRoom, rt *matchRuntime) error {
	tokens := rt.machine.Snapshot().ResultQueue
	views := make([]protocol.ResultTokenView, 0, len(tokens))
	for _, token := range tokens {
		views = append(views, protocol.ResultTokenView{
			TokenID: token.ID,
			Result:  token.Result,
			Origin:  token.Origin,
		})
	}
	return registry.emitLocked(rt.roomID, func(sequence uint64) (any, error) {
		return protocol.NewResultQueueUpdatedEvent(rt.roomID, rt.matchID, sequence, views)
	})
}

func (registry *RoomRegistry) emitMoveRequiredLocked(entry *registeredRoom, rt *matchRuntime, payload protocol.MoveRequiredPayload) error {
	return registry.emitLocked(rt.roomID, func(sequence uint64) (any, error) {
		return protocol.NewMoveRequiredEvent(rt.roomID, rt.matchID, sequence, payload)
	})
}

// emitMoveOutcomeEventsLocked broadcasts the observable effects of one
// committed movement: PIECE_MOVED always, then PIECES_STACKED and
// PIECES_CAPTURED when the resolution produced them.
func (registry *RoomRegistry) emitMoveOutcomeEventsLocked(entry *registeredRoom, rt *matchRuntime, outcome match.MoveOutcome) error {
	if err := registry.emitLocked(rt.roomID, func(sequence uint64) (any, error) {
		return protocol.NewPieceMovedEvent(rt.roomID, rt.matchID, sequence, protocol.PieceMovedPayload{
			PieceIDs:     append([]domain.PieceID(nil), outcome.MovedPieceIDs...),
			FromSpaceID:  optionalSpaceID(outcome.FromSpaceID),
			ToSpaceID:    optionalSpaceID(outcome.ToSpaceID),
			MovementKind: outcome.MovementKind,
		})
	}); err != nil {
		return err
	}
	if len(outcome.StackedPieceIDs) >= 2 && outcome.ToSpaceID != "" {
		stackID := stackIDFor(rt.currentTeam(), outcome.ToSpaceID)
		if err := registry.emitLocked(rt.roomID, func(sequence uint64) (any, error) {
			return protocol.NewPiecesStackedEvent(rt.roomID, rt.matchID, sequence, protocol.PiecesStackedPayload{
				StackID:             stackID,
				PieceIDs:            append([]domain.PieceID(nil), outcome.StackedPieceIDs...),
				SpaceID:             outcome.ToSpaceID,
				ActualPreviousSpace: optionalSpaceID(outcome.ActualPreviousSpace),
			})
		}); err != nil {
			return err
		}
	}
	if len(outcome.CapturedPieceIDs) > 0 && outcome.ToSpaceID != "" {
		if err := registry.emitLocked(rt.roomID, func(sequence uint64) (any, error) {
			return protocol.NewPiecesCapturedEvent(rt.roomID, rt.matchID, sequence, protocol.PiecesCapturedPayload{
				CapturedPieceIDs: append([]domain.PieceID(nil), outcome.CapturedPieceIDs...),
				SpaceID:          outcome.ToSpaceID,
			})
		}); err != nil {
			return err
		}
	}
	return nil
}

func stackIDFor(team domain.TeamID, space domain.SpaceID) string {
	return fmt.Sprintf("stack:%s:%s", team, space)
}

func positionGroupIDFor(team domain.TeamID, state domain.PieceState, space domain.SpaceID) string {
	return fmt.Sprintf("pos:%s:%s:%s", team, state, space)
}

// movablePieceIDs lists the acting team's pieces that have at least one legal
// plan for the currently selected result token.
func (rt *matchRuntime) movablePieceIDs(snapshot turn.Snapshot) ([]domain.PieceID, error) {
	tokenID := snapshot.SelectedTokenID
	result, ok := resultOfToken(snapshot.ResultQueue, tokenID)
	if !ok {
		return nil, fmt.Errorf("%w: unknown selected token", ErrInvalidTurnAction)
	}
	gameSnapshot := rt.game.Snapshot()
	movable := make([]domain.PieceID, 0, rt.settings.PieceCount)
	for _, piece := range gameSnapshot.Pieces {
		if piece.TeamID != rt.currentTeam() || piece.State == domain.PieceFinished {
			continue
		}
		if result == domain.YutBackdo {
			if _, err := rt.game.BackdoMovePlan(rt.currentTeam(), piece.ID); err != nil {
				if isUnavailableMovementError(err) {
					continue
				}
				return nil, err
			}
			movable = append(movable, piece.ID)
			continue
		}
		plans, err := rt.game.OrdinaryMovePlans(rt.currentTeam(), piece.ID, result)
		if err != nil {
			if isUnavailableMovementError(err) {
				continue
			}
			return nil, err
		}
		if len(plans) > 0 {
			movable = append(movable, piece.ID)
		}
	}
	return movable, nil
}

func isUnavailableMovementError(err error) bool {
	return errors.Is(err, board.ErrForwardMovementUnavailable) ||
		errors.Is(err, board.ErrBackdoMovementUnavailable) ||
		errors.Is(err, board.ErrBackdoHistoryUnavailable)
}
