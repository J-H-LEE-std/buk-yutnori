// Match event staging helpers for the canonical match runtime. Every helper
// eagerly captures payload values from the live runtime and stages the event
// on the caller's emission transaction; sequence numbers and delivery happen
// at flush time (issue #84).

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

func (registry *RoomRegistry) stageTurnStarted(tx *eventTx, rt *matchRuntime) {
	snapshot := rt.machine.Snapshot()
	remaining := rt.remainingMS(registry.matchClock.Now())
	tx.emit(func(sequence uint64) (any, error) {
		return protocol.NewTurnStartedEvent(rt.roomID, rt.matchID, sequence, protocol.TurnStartedPayload{
			PlayerID:      snapshot.PlayerID,
			Phase:         snapshot.Phase,
			RequiredInput: snapshot.RequiredInput,
			RemainingMS:   remaining,
		})
	})
}

func stageYutResult(tx *eventTx, rt *matchRuntime, token turn.ResultToken) {
	playerID := token.GeneratedByPlayerID
	view := protocol.ResultTokenView{
		TokenID: token.ID,
		Result:  token.Result,
		Origin:  token.Origin,
	}
	tx.emit(func(sequence uint64) (any, error) {
		return protocol.NewYutResultEvent(rt.roomID, rt.matchID, sequence, protocol.YutResultPayload{
			PlayerID: playerID,
			Token:    view,
		})
	})
}

func stageResultQueueUpdated(tx *eventTx, rt *matchRuntime) {
	tokens := rt.machine.Snapshot().ResultQueue
	views := make([]protocol.ResultTokenView, 0, len(tokens))
	for _, token := range tokens {
		views = append(views, protocol.ResultTokenView{
			TokenID: token.ID,
			Result:  token.Result,
			Origin:  token.Origin,
		})
	}
	tx.emit(func(sequence uint64) (any, error) {
		return protocol.NewResultQueueUpdatedEvent(rt.roomID, rt.matchID, sequence, views)
	})
}

// stageMoveSelection deliberately stages the two audit records together.
// eventTx assigns consecutive sequences and persists its builders in one
// transaction before it broadcasts anything.
func stageMoveSelection(tx *eventTx, rt *matchRuntime, tokenID domain.ResultTokenID, pieceID domain.PieceID) {
	tx.emit(func(sequence uint64) (any, error) {
		return protocol.NewResultSelectedEvent(rt.roomID, rt.matchID, sequence, protocol.ResultSelectedPayload{TokenID: tokenID})
	})
	tx.emit(func(sequence uint64) (any, error) {
		return protocol.NewPieceSelectedEvent(rt.roomID, rt.matchID, sequence, protocol.PieceSelectedPayload{TokenID: tokenID, PieceID: pieceID})
	})
}

func stageMoveRequired(tx *eventTx, rt *matchRuntime, payload protocol.MoveRequiredPayload) error {
	tx.emit(func(sequence uint64) (any, error) {
		return protocol.NewMoveRequiredEvent(rt.roomID, rt.matchID, sequence, payload)
	})
	return nil
}

// stageMoveOutcomeEvents stages the observable effects of one committed
// movement: PIECE_MOVED always, then PIECES_STACKED and PIECES_CAPTURED when
// the resolution produced them.
func stageMoveOutcomeEvents(tx *eventTx, rt *matchRuntime, outcome match.MoveOutcome) {
	pieceIDs := append([]domain.PieceID(nil), outcome.MovedPieceIDs...)
	fromSpace := optionalSpaceID(outcome.FromSpaceID)
	toSpace := optionalSpaceID(outcome.ToSpaceID)
	movementKind := outcome.MovementKind
	tx.emit(func(sequence uint64) (any, error) {
		return protocol.NewPieceMovedEvent(rt.roomID, rt.matchID, sequence, protocol.PieceMovedPayload{
			PieceIDs:     pieceIDs,
			FromSpaceID:  fromSpace,
			ToSpaceID:    toSpace,
			MovementKind: movementKind,
		})
	})

	if len(outcome.StackedPieceIDs) >= 2 && outcome.ToSpaceID != "" {
		stackID := stackIDFor(rt.currentTeam(), outcome.ToSpaceID)
		stacked := append([]domain.PieceID(nil), outcome.StackedPieceIDs...)
		stackSpace := outcome.ToSpaceID
		previousSpace := optionalSpaceID(outcome.ActualPreviousSpace)
		tx.emit(func(sequence uint64) (any, error) {
			return protocol.NewPiecesStackedEvent(rt.roomID, rt.matchID, sequence, protocol.PiecesStackedPayload{
				StackID:             stackID,
				PieceIDs:            stacked,
				SpaceID:             stackSpace,
				ActualPreviousSpace: previousSpace,
			})
		})
	}
	if len(outcome.CapturedPieceIDs) > 0 && outcome.ToSpaceID != "" {
		captured := append([]domain.PieceID(nil), outcome.CapturedPieceIDs...)
		captureSpace := outcome.ToSpaceID
		tx.emit(func(sequence uint64) (any, error) {
			return protocol.NewPiecesCapturedEvent(rt.roomID, rt.matchID, sequence, protocol.PiecesCapturedPayload{
				CapturedPieceIDs: captured,
				SpaceID:          captureSpace,
			})
		})
	}
}

func stackIDFor(team domain.TeamID, space domain.SpaceID) string {
	return fmt.Sprintf("stack:%s:%s", team, space)
}

func positionGroupIDFor(team domain.TeamID, state domain.PieceState, space domain.SpaceID) string {
	return fmt.Sprintf("pos:%s:%s:%s", team, state, space)
}

// moveCandidates calculates every legal exposed result/piece pair from the
// authoritative game state. Finished pieces and unavailable plans never
// reach clients. unusable contains exposed ordinary tokens with no candidate.
func (rt *matchRuntime) moveCandidates(snapshot turn.Snapshot) ([]protocol.MoveCandidate, []domain.ResultTokenID, error) {
	available := availableTokensFor(rt.settings.MovementOrder, snapshot.ResultQueue)
	gameSnapshot := rt.game.Snapshot()
	candidates := make([]protocol.MoveCandidate, 0, len(available)*rt.settings.PieceCount)
	unusable := make([]domain.ResultTokenID, 0, len(available))
	for _, token := range available {
		if token.Result == domain.YutBuk {
			break
		}
		before := len(candidates)
		for _, piece := range gameSnapshot.Pieces {
			if piece.TeamID != rt.currentTeam() || piece.State == domain.PieceFinished {
				continue
			}
			if token.Result == domain.YutBackdo {
				if _, err := rt.game.BackdoMovePlan(rt.currentTeam(), piece.ID); err != nil {
					if isUnavailableMovementError(err) {
						continue
					}
					return nil, nil, err
				}
				candidates = append(candidates, protocol.MoveCandidate{TokenID: token.ID, PieceID: piece.ID, Routes: []domain.Route{}})
				continue
			}
			plans, err := rt.game.OrdinaryMovePlans(rt.currentTeam(), piece.ID, token.Result)
			if err != nil {
				if isUnavailableMovementError(err) {
					continue
				}
				return nil, nil, err
			}
			routes := make([]domain.Route, 0, len(plans))
			for _, plan := range plans {
				routes = append(routes, plan.Route)
			}
			if len(routes) > 0 {
				candidates = append(candidates, protocol.MoveCandidate{TokenID: token.ID, PieceID: piece.ID, Routes: routes})
			}
		}
		if len(candidates) == before {
			unusable = append(unusable, token.ID)
		}
	}
	return candidates, unusable, nil
}

func isUnavailableMovementError(err error) bool {
	return errors.Is(err, board.ErrForwardMovementUnavailable) ||
		errors.Is(err, board.ErrBackdoMovementUnavailable) ||
		errors.Is(err, board.ErrBackdoHistoryUnavailable)
}
