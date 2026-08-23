// Turn timers, public match command entry points, and CPU substitution for
// the canonical match runtime. All functions require the registry mutex.

package application

import (
	"fmt"
	"time"

	"buk-yutnori/internal/auth"
	"buk-yutnori/internal/domain"
	"buk-yutnori/internal/domain/cpu"
	"buk-yutnori/internal/domain/match"
	"buk-yutnori/internal/domain/room"
	"buk-yutnori/internal/domain/turn"
	"buk-yutnori/internal/domain/yut"
	"buk-yutnori/internal/protocol"
)

// ---------------------------------------------------------------------------
// Timers

func (registry *RoomRegistry) cancelTimerLocked(rt *matchRuntime) {
	if rt.activeTimer != nil {
		rt.activeTimer.Stop()
		rt.activeTimer = nil
	}
	rt.timerGeneration++
	rt.timerKind = ""
	rt.timerDeadline = time.Time{}
}

// scheduleTimerLocked arms exactly one cancellable server-owned deadline.
// CPU-controlled turns act synchronously and arm nothing.
func (registry *RoomRegistry) scheduleTimerLocked(rt *matchRuntime, kind string, duration time.Duration) {
	registry.cancelTimerLocked(rt)
	if rt.cpuControlled {
		return
	}
	rt.timerKind = kind
	now := registry.matchClock.Now()
	rt.timerDeadline = now.Add(duration)
	generation := rt.timerGeneration
	roomID := rt.roomID
	rt.activeTimer = registry.matchClock.AfterFunc(duration, func() {
		registry.fireTurnTimeout(roomID, generation)
	})
}

func (registry *RoomRegistry) scheduleThrowTimerLocked(rt *matchRuntime) {
	registry.scheduleTimerLocked(rt, matchTimerKindThrow,
		time.Duration(rt.settings.ThrowTimeoutSeconds)*time.Second)
}

func (registry *RoomRegistry) scheduleMoveTimerLocked(rt *matchRuntime) {
	registry.scheduleTimerLocked(rt, matchTimerKindMove,
		time.Duration(rt.settings.MoveTimeoutSeconds)*time.Second)
}

func (rt *matchRuntime) remainingMS(now time.Time) uint64 {
	if rt.timerKind == "" {
		return 0
	}
	remaining := rt.timerDeadline.Sub(now).Milliseconds()
	if remaining < 0 {
		return 0
	}
	return uint64(remaining)
}

// fireTurnTimeout substitutes the acting player with the CPU for the rest of
// the current turn only (docs/03 시간 초과). Stale generations are ignored so
// cancelled or superseded deadlines never fire.
func (registry *RoomRegistry) fireTurnTimeout(roomID domain.RoomID, generation uint64) {
	registry.mutex.Lock()
	defer registry.mutex.Unlock()

	entry, exists := registry.rooms[roomID]
	if !exists {
		return
	}
	rt := entry.runtime
	if rt == nil || generation != rt.timerGeneration || rt.timerKind == "" || rt.cpuControlled {
		return
	}
	player := rt.currentPlayer()
	rt.cpuControlled = true
	registry.cancelTimerLocked(rt)
	if err := registry.emitLocked(rt.roomID, func(sequence uint64) (any, error) {
		return protocol.NewCPUControlStartedEvent(rt.roomID, rt.matchID, sequence, protocol.CPUControlStartedPayload{
			PlayerID: player,
			Reason:   cpuControlReasonTimeout,
		})
	}); err != nil {
		return
	}
	registry.runCpuTurnLocked(entry, rt)
}

// ---------------------------------------------------------------------------
// Public match commands

// liveMatchLocked resolves the caller's live runtime for a started room and
// validates membership plus match scope in one step.
func (registry *RoomRegistry) liveMatchLocked(
	user auth.UserID,
	roomID domain.RoomID,
	matchID domain.MatchID,
) (*registeredRoom, *matchRuntime, error) {
	playerID, err := playerIDFromUser(user)
	if err != nil {
		return nil, nil, err
	}
	entry, exists := registry.rooms[roomID]
	if !exists {
		return nil, nil, ErrRoomNotFound
	}
	if _, member := entry.lobby.Player(playerID); !member {
		return nil, nil, ErrNotMember
	}
	if !entry.started || entry.runtime == nil {
		return nil, nil, ErrMatchNotActive
	}
	if entry.runtime.matchID != matchID {
		return nil, nil, ErrMatchScopeMismatch
	}
	return entry, entry.runtime, nil
}

// ThrowYut consumes THROW_YUT for the acting player and drives every
// automatic resolution step that follows the throw chain.
func (registry *RoomRegistry) ThrowYut(user auth.UserID, roomID domain.RoomID, matchID domain.MatchID) error {
	registry.mutex.Lock()
	defer registry.mutex.Unlock()

	entry, rt, err := registry.liveMatchLocked(user, roomID, matchID)
	if err != nil {
		return err
	}
	if rt.currentPlayer() != domain.PlayerID(user) {
		return ErrNotTurnPlayer
	}
	if rt.machine.Snapshot().RequiredInput != domain.InputThrow {
		return ErrInvalidTurnAction
	}
	if err := registry.performThrowLocked(entry, rt); err != nil {
		return err
	}
	return registry.advanceTurnLocked(entry, rt)
}

// SelectResult consumes SELECT_RESULT while several ordinary tokens compete
// for selection under free movement order.
func (registry *RoomRegistry) SelectResult(user auth.UserID, roomID domain.RoomID, matchID domain.MatchID, tokenID domain.ResultTokenID) error {
	registry.mutex.Lock()
	defer registry.mutex.Unlock()

	entry, rt, err := registry.liveMatchLocked(user, roomID, matchID)
	if err != nil {
		return err
	}
	if rt.currentPlayer() != domain.PlayerID(user) {
		return ErrNotTurnPlayer
	}
	snapshot := rt.machine.Snapshot()
	if snapshot.RequiredInput != domain.InputSelectResult {
		return ErrInvalidTurnAction
	}
	if err := rt.machine.SelectResult(tokenID); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidTurnAction, err)
	}
	if err := registry.emitResultQueueUpdatedLocked(entry, rt); err != nil {
		return err
	}
	return registry.advanceTurnLocked(entry, rt)
}

// SelectPiece consumes SELECT_PIECE and either applies the move directly or
// asks for the shortcut route choice first.
func (registry *RoomRegistry) SelectPiece(user auth.UserID, roomID domain.RoomID, matchID domain.MatchID, tokenID domain.ResultTokenID, pieceID domain.PieceID) error {
	registry.mutex.Lock()
	defer registry.mutex.Unlock()

	entry, rt, err := registry.liveMatchLocked(user, roomID, matchID)
	if err != nil {
		return err
	}
	if rt.currentPlayer() != domain.PlayerID(user) {
		return ErrNotTurnPlayer
	}
	snapshot := rt.machine.Snapshot()
	if snapshot.RequiredInput != domain.InputSelectPiece || snapshot.SelectedTokenID != tokenID {
		return ErrInvalidTurnAction
	}
	return registry.selectPieceInternalLocked(entry, rt, tokenID, pieceID)
}

// SelectRoute consumes SELECT_ROUTE for a piece awaiting its shortcut choice.
func (registry *RoomRegistry) SelectRoute(user auth.UserID, roomID domain.RoomID, matchID domain.MatchID, tokenID domain.ResultTokenID, pieceID domain.PieceID, route domain.Route) error {
	registry.mutex.Lock()
	defer registry.mutex.Unlock()

	entry, rt, err := registry.liveMatchLocked(user, roomID, matchID)
	if err != nil {
		return err
	}
	if rt.currentPlayer() != domain.PlayerID(user) {
		return ErrNotTurnPlayer
	}
	snapshot := rt.machine.Snapshot()
	if snapshot.RequiredInput != domain.InputSelectRoute ||
		snapshot.SelectedTokenID != tokenID || rt.pendingMovePiece != pieceID {
		return ErrInvalidTurnAction
	}
	if err := rt.machine.RouteSelected(tokenID); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidTurnAction, err)
	}
	return registry.applySelectedMoveLocked(entry, rt, tokenID, pieceID, route)
}

// ---------------------------------------------------------------------------
// Turn flow internals

func (registry *RoomRegistry) performThrowLocked(entry *registeredRoom, rt *matchRuntime) error {
	origin, err := rt.machine.BeginThrow()
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidTurnAction, err)
	}
	result, err := rt.throwResult(yut.Mode{
		BackdoEnabled:  rt.settings.BackdoEnabled,
		BukModeEnabled: rt.settings.BukModeEnabled,
	})
	if err != nil {
		return err
	}
	token := turn.ResultToken{
		ID:                  rt.nextTokenID(),
		Result:              result,
		Origin:              origin,
		GeneratedByPlayerID: rt.currentPlayer(),
	}
	if err := rt.machine.RecordThrow(token); err != nil {
		return err
	}
	if err := registry.emitYutResultLocked(entry, rt, token); err != nil {
		return err
	}
	if err := registry.emitResultQueueUpdatedLocked(entry, rt); err != nil {
		return err
	}
	// Yut/Mo extra throws reset the throw window immediately (docs/03).
	if rt.machine.Snapshot().Phase == domain.TurnWaitThrow {
		registry.scheduleThrowTimerLocked(rt)
		return registry.emitTurnStartedLocked(entry, rt)
	}
	return nil
}

type resolutionStep int

const (
	stepContinue resolutionStep = iota
	stepAwaitInput
	stepStopped
)

// advanceTurnLocked performs automatic steps until an external decision is
// required, the turn ends, or the match ends.
func (registry *RoomRegistry) advanceTurnLocked(entry *registeredRoom, rt *matchRuntime) error {
	for {
		step, err := registry.stepResolutionLocked(entry, rt)
		if err != nil || step != stepContinue {
			return err
		}
	}
}

func (registry *RoomRegistry) stepResolutionLocked(entry *registeredRoom, rt *matchRuntime) (resolutionStep, error) {
	snapshot := rt.machine.Snapshot()
	switch snapshot.Phase {
	case domain.TurnResolveQueue:
		if err := rt.machine.ResolveQueue(); err != nil {
			return stepStopped, err
		}
		return registry.afterQueueResolvedLocked(entry, rt)
	case domain.TurnResolveBuk:
		return registry.resolveBukHeadLocked(entry, rt, snapshot.SelectedTokenID)
	case domain.TurnWaitPieceSelection:
		awaiting, err := registry.enterPieceSelectionLocked(entry, rt)
		if err != nil || awaiting {
			return stepAwaitInput, err
		}
		return stepContinue, nil
	case domain.TurnWaitResultSelection:
		if rt.cpuControlled {
			return stepAwaitInput, nil
		}
		available := availableTokensFor(rt.settings.MovementOrder, snapshot.ResultQueue)
		return stepAwaitInput, registry.emitMoveRequiredLocked(entry, rt, protocol.MoveRequiredPayload{
			RequiredInput: domain.InputSelectResult,
			TokenIDs:      availableTokenIDs(available),
		})
	case domain.TurnEnd:
		return stepStopped, registry.endTurnLocked(entry, rt)
	case domain.TurnMatchEnd:
		return stepStopped, registry.finishMatchLocked(entry, rt)
	default:
		return stepStopped, nil
	}
}

// afterQueueResolvedLocked swaps the armed deadline for the move-decision
// window once per throw chain (docs/03: 결과·말·지름길 선택을 하나의 이동
// 처리 시간에 포함) and reports whether an automatic step must continue.
func (registry *RoomRegistry) afterQueueResolvedLocked(entry *registeredRoom, rt *matchRuntime) (resolutionStep, error) {
	snapshot := rt.machine.Snapshot()
	switch snapshot.Phase {
	case domain.TurnWaitPieceSelection, domain.TurnWaitResultSelection:
		if rt.timerKind != matchTimerKindMove && !rt.cpuControlled {
			registry.scheduleMoveTimerLocked(rt)
		}
	}
	return stepContinue, nil
}

// resolveBukHeadLocked applies the automatic canonical Buk resolution: the
// server computes candidates and applies weighted selection without any user
// piece choice (docs/03 북 처리).
func (registry *RoomRegistry) resolveBukHeadLocked(entry *registeredRoom, rt *matchRuntime, tokenID domain.ResultTokenID) (resolutionStep, error) {
	outcome, err := rt.game.ResolveBuk(rt.currentTeam())
	if err != nil {
		return stepStopped, err
	}
	var sourceSpaceID *domain.SpaceID
	if outcome.Moved && outcome.Move.FromSpaceID != "" {
		value := outcome.Move.FromSpaceID
		sourceSpaceID = &value
	}
	movedPieceIDs := outcome.SelectedPieceIDs
	if movedPieceIDs == nil {
		movedPieceIDs = []domain.PieceID{}
	}
	if err := registry.emitLocked(rt.roomID, func(sequence uint64) (any, error) {
		return protocol.NewBukResolvedEvent(rt.roomID, rt.matchID, sequence, protocol.BukResolvedPayload{
			TokenID:            tokenID,
			DestinationSpaceID: outcome.DestinationSpaceID,
			MovedPieceIDs:      movedPieceIDs,
			SourceSpaceID:      sourceSpaceID,
			NoCandidate:        outcome.NoCandidate,
		})
	}); err != nil {
		return stepStopped, err
	}
	if outcome.Moved {
		if err := registry.emitMoveOutcomeEventsLocked(entry, rt, outcome.Move); err != nil {
			return stepStopped, err
		}
	}
	if err := rt.machine.CompleteBuk(tokenID, outcome.TurnOutcome()); err != nil {
		return stepStopped, err
	}
	if err := registry.emitResultQueueUpdatedLocked(entry, rt); err != nil {
		return stepStopped, err
	}
	switch rt.machine.Snapshot().Phase {
	case domain.TurnMatchEnd:
		return stepStopped, registry.finishMatchLocked(entry, rt)
	case domain.TurnWaitThrow:
		// Capture extra throw granted during Buk resolution.
		registry.scheduleThrowTimerLocked(rt)
		return stepStopped, registry.emitTurnStartedLocked(entry, rt)
	default:
		return stepContinue, nil
	}
}

// enterPieceSelectionLocked resolves a freshly entered wait_piece_selection
// phase. Tokens without any legal piece are discarded automatically because
// the v1 protocol has no client-facing discard command; the outcome equals
// the forced player choice documented in docs/03 (discard_only_that_token).
func (registry *RoomRegistry) enterPieceSelectionLocked(entry *registeredRoom, rt *matchRuntime) (bool, error) {
	snapshot := rt.machine.Snapshot()
	tokenID := snapshot.SelectedTokenID
	movable, err := rt.movablePieceIDs(snapshot)
	if err != nil {
		return false, err
	}
	if len(movable) > 0 {
		if rt.cpuControlled {
			return true, nil
		}
		return true, registry.emitMoveRequiredLocked(entry, rt, protocol.MoveRequiredPayload{
			RequiredInput: domain.InputSelectPiece,
			TokenIDs:      []domain.ResultTokenID{tokenID},
			PieceIDs:      movable,
		})
	}
	if err := rt.machine.DiscardUnusableResult(tokenID); err != nil {
		return false, err
	}
	return false, registry.emitResultQueueUpdatedLocked(entry, rt)
}

func (registry *RoomRegistry) selectPieceInternalLocked(
	entry *registeredRoom,
	rt *matchRuntime,
	tokenID domain.ResultTokenID,
	pieceID domain.PieceID,
) error {
	snapshot := rt.machine.Snapshot()
	result, ok := resultOfToken(snapshot.ResultQueue, tokenID)
	if !ok {
		return fmt.Errorf("%w: unknown selected token", ErrInvalidTurnAction)
	}
	if result == domain.YutBackdo {
		if _, err := rt.game.BackdoMovePlan(rt.currentTeam(), pieceID); err != nil {
			return fmt.Errorf("%w: %v", ErrInvalidTurnAction, err)
		}
		if err := rt.machine.PieceSelected(tokenID, false); err != nil {
			return fmt.Errorf("%w: %v", ErrInvalidTurnAction, err)
		}
		return registry.applySelectedMoveLocked(entry, rt, tokenID, pieceID, "")
	}
	plans, err := rt.game.OrdinaryMovePlans(rt.currentTeam(), pieceID, result)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidTurnAction, err)
	}
	routeRequired := len(plans) == 2
	if err := rt.machine.PieceSelected(tokenID, routeRequired); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidTurnAction, err)
	}
	if routeRequired {
		rt.pendingMovePiece = pieceID
		if rt.cpuControlled {
			// CPU-controlled turns do not broadcast per-step input requests;
			// CPU_CONTROL_STARTED already announced the substitution.
			return nil
		}
		return registry.emitMoveRequiredLocked(entry, rt, protocol.MoveRequiredPayload{
			RequiredInput: domain.InputSelectRoute,
			TokenIDs:      []domain.ResultTokenID{tokenID},
			PieceIDs:      []domain.PieceID{pieceID},
		})
	}
	return registry.applySelectedMoveLocked(entry, rt, tokenID, pieceID, "")
}

func (registry *RoomRegistry) applySelectedMoveLocked(
	entry *registeredRoom,
	rt *matchRuntime,
	tokenID domain.ResultTokenID,
	pieceID domain.PieceID,
	route domain.Route,
) error {
	result, ok := resultOfToken(rt.machine.Snapshot().ResultQueue, tokenID)
	if !ok {
		return fmt.Errorf("%w: unknown selected token", ErrInvalidTurnAction)
	}
	var outcome match.MoveOutcome
	var err error
	if result == domain.YutBackdo {
		outcome, err = rt.game.ApplyBackdoMove(rt.currentTeam(), pieceID)
	} else {
		outcome, err = rt.game.ApplyOrdinaryMove(rt.currentTeam(), pieceID, result, route)
	}
	if err != nil {
		return err
	}
	return registry.completeMoveLocked(entry, rt, tokenID, outcome)
}

func (registry *RoomRegistry) completeMoveLocked(
	entry *registeredRoom,
	rt *matchRuntime,
	tokenID domain.ResultTokenID,
	outcome match.MoveOutcome,
) error {
	if err := rt.machine.MoveApplied(tokenID); err != nil {
		return err
	}
	if err := registry.emitMoveOutcomeEventsLocked(entry, rt, outcome); err != nil {
		return err
	}
	if err := rt.machine.CompleteMove(tokenID, outcome.TurnOutcome()); err != nil {
		return err
	}
	if err := registry.emitResultQueueUpdatedLocked(entry, rt); err != nil {
		return err
	}
	switch rt.machine.Snapshot().Phase {
	case domain.TurnMatchEnd:
		return registry.finishMatchLocked(entry, rt)
	case domain.TurnWaitThrow:
		// Capture extra throw resets the throw window (docs/03).
		registry.scheduleThrowTimerLocked(rt)
		return registry.emitTurnStartedLocked(entry, rt)
	default:
		return registry.advanceTurnLocked(entry, rt)
	}
}

// ---------------------------------------------------------------------------
// CPU substitution

// runCpuTurnLocked completes the substituted turn synchronously: the CPU
// throws, selects results, moves pieces, and resolves Buk until the turn
// ends or the match finishes (docs/03: 해당 턴 전체를 CPU가 이어서 완료한다).
func (registry *RoomRegistry) runCpuTurnLocked(entry *registeredRoom, rt *matchRuntime) {
	for {
		if entry.runtime != rt || rt.machine == nil {
			return
		}
		switch rt.machine.Snapshot().Phase {
		case domain.TurnWaitThrow:
			if err := registry.performThrowLocked(entry, rt); err != nil {
				return
			}
		case domain.TurnResolveQueue:
			if err := registry.advanceTurnLocked(entry, rt); err != nil {
				return
			}
		case domain.TurnWaitResultSelection, domain.TurnWaitPieceSelection, domain.TurnWaitRouteSelection:
			decision, err := rt.cpu.Decide(rt.game, rt.machine.Snapshot(), rt.currentTeam())
			if err != nil {
				return
			}
			if err := registry.applyCPUDecisionLocked(entry, rt, decision); err != nil {
				return
			}
		default:
			return
		}
		if entry.runtime != rt || rt.machine == nil {
			return
		}
		if !rt.cpuControlled {
			// The turn ended; the next player acts under human control.
			return
		}
	}
}

func (registry *RoomRegistry) applyCPUDecisionLocked(entry *registeredRoom, rt *matchRuntime, decision cpu.Decision) error {
	switch rt.machine.Snapshot().Phase {
	case domain.TurnWaitResultSelection:
		if err := rt.machine.SelectResult(decision.TokenID); err != nil {
			return err
		}
		return registry.emitResultQueueUpdatedLocked(entry, rt)
	case domain.TurnWaitPieceSelection:
		if decision.Action == cpu.ActionDiscardResult {
			if err := rt.machine.DiscardUnusableResult(decision.TokenID); err != nil {
				return err
			}
			return registry.emitResultQueueUpdatedLocked(entry, rt)
		}
		return registry.selectPieceInternalLocked(entry, rt, decision.TokenID, decision.PieceID)
	case domain.TurnWaitRouteSelection:
		pieceID := rt.pendingMovePiece
		if decision.Route.Validate() != nil {
			return fmt.Errorf("%w: CPU route %q", ErrInvalidTurnAction, decision.Route)
		}
		if err := rt.machine.RouteSelected(decision.TokenID); err != nil {
			return err
		}
		return registry.applySelectedMoveLocked(entry, rt, decision.TokenID, pieceID, decision.Route)
	default:
		return fmt.Errorf("%w: CPU decision in phase %q", ErrInvalidTurnAction, rt.machine.Snapshot().Phase)
	}
}

func availableTokenIDs(tokens []turn.ResultToken) []domain.ResultTokenID {
	ids := make([]domain.ResultTokenID, 0, len(tokens))
	for _, token := range tokens {
		ids = append(ids, token.ID)
	}
	return ids
}

// availableTokensFor mirrors ResultQueue.Available for snapshots: FIFO
// exposes the head; free order exposes every ordinary token before the Buk
// barrier (docs/03 결과 큐).
func availableTokensFor(order room.MovementOrder, tokens []turn.ResultToken) []turn.ResultToken {
	if len(tokens) == 0 {
		return nil
	}
	limit := len(tokens)
	switch order {
	case room.MovementFIFO:
		limit = 1
	default:
		for index, token := range tokens {
			if token.Result == domain.YutBuk {
				limit = index
				break
			}
		}
	}
	if limit == 0 {
		limit = 1
	}
	return tokens[:limit]
}

func resultOfToken(tokens []turn.ResultToken, tokenID domain.ResultTokenID) (domain.YutResult, bool) {
	for _, token := range tokens {
		if token.ID == tokenID {
			return token.Result, true
		}
	}
	return "", false
}

func optionalSpaceID(space domain.SpaceID) *domain.SpaceID {
	if space == "" {
		return nil
	}
	value := space
	return &value
}
