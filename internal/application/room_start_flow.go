// Authoritative start confirmation lifecycle over the room registry.

package application

import (
	"errors"
	"sort"
	"time"

	"buk-yutnori/internal/auth"
	"buk-yutnori/internal/domain"
	"buk-yutnori/internal/domain/room"
	"buk-yutnori/internal/protocol"
)

// RequestStart opens the canonical 10-second start confirmation window for
// the room owner. Eligibility follows the pure Lobby rules; the deadline is a
// monotonic server clock instant (ADR-0003).
func (registry *RoomRegistry) RequestStart(user auth.UserID, roomID domain.RoomID) error {
	if err := user.Validate(); err != nil {
		return err
	}

	registry.mutex.Lock()
	defer registry.mutex.Unlock()

	entry, exists := registry.rooms[roomID]
	if !exists {
		return ErrRoomNotFound
	}
	if entry.poisoned {
		return ErrEventStoreUnavailable
	}
	if entry.host != user {
		return ErrNotRoomHost
	}
	if entry.started {
		return ErrRoomAlreadyStarted
	}
	if entry.confirmation != nil {
		return ErrStartAlreadyRequested
	}

	rawMatchID, err := registry.randomID()
	if err != nil {
		return err
	}
	matchID := domain.MatchID(rawMatchID)
	startedAt := registry.clock()
	confirmation, err := room.NewStartConfirmation(entry.lobby, matchID, startedAt)
	if err != nil {
		return err
	}

	entry.confirmation = confirmation
	entry.roomStatus = protocol.RoomStatusStarting
	entry.expiryTimer = time.AfterFunc(room.StartConfirmationWindow, func() {
		registry.awaitDeadlineAndExpire(roomID)
	})
	tx := registry.newEventTx(roomID)
	deadlineAt := confirmation.Snapshot().DeadlineAt
	tx.emit(func(sequence uint64) (any, error) {
		return protocol.NewGameStartingEvent(roomID, matchID, sequence, deadlineAt)
	})
	tx.emit(func(sequence uint64) (any, error) {
		return protocol.NewRoomUpdatedEvent(roomID, sequence, entry.roomStatus)
	})
	return tx.flush()
}

// ConfirmStart records one roster player's confirmation. The last pending
// confirmation closes the window, assembles the canonical match runtime from
// the confirmed roster, and emits the in-match transition broadcasts.
func (registry *RoomRegistry) ConfirmStart(user auth.UserID, roomID domain.RoomID, matchID domain.MatchID) error {
	playerID, err := playerIDFromUser(user)
	if err != nil {
		return err
	}
	if err := matchID.Validate(); err != nil {
		return err
	}

	registry.mutex.Lock()
	defer registry.mutex.Unlock()

	entry, exists := registry.rooms[roomID]
	if !exists {
		return ErrRoomNotFound
	}
	if entry.confirmation == nil || entry.started {
		return ErrNoActiveStartConfirmation
	}
	if entry.confirmation.Snapshot().MatchID != matchID {
		return ErrMatchScopeMismatch
	}
	allConfirmed, err := entry.confirmation.Confirm(playerID, registry.clock())
	if err != nil {
		return err
	}
	if !allConfirmed {
		return nil
	}

	tx := registry.newEventTx(roomID)

	// The runtime is assembled before the started flip so a construction
	// failure cannot leave a started room without its canonical runtime.
	runtime, err := registry.newMatchRuntime(entry, roomID, matchID)
	if err != nil {
		// The domain confirmation already flipped to its irreversible
		// Confirmed state, and every cleanup guard requires Pending, so an
		// unhandled failure here would wedge the room forever: no restart,
		// no mutations, and the expiry timer becomes a permanent no-op.
		// Compensate with the canonical failed-start transition instead;
		// the compensation broadcast rides the same transaction.
		registry.compensateFailedStartLocked(entry, roomID, tx)
		return errors.Join(err, tx.flush())
	}
	registry.stopExpiryTimer(entry)
	entry.started = true
	entry.roomStatus = protocol.RoomStatusInMatch
	tx.emit(func(sequence uint64) (any, error) {
		return protocol.NewRoomUpdatedEvent(roomID, sequence, entry.roomStatus)
	})
	if err := registry.startMatchBroadcastsLocked(tx, entry, runtime); err != nil {
		return err
	}
	return tx.flush()
}

// compensateFailedStartLocked unwinds an open start window after the roster
// confirmed but the match runtime could not be assembled. It mirrors the
// observable effects of ExpireStartConfirmation minus responder removal (the
// full roster responded), stages the lobby transition on the caller's
// transaction, and returns the room to a retryable lobby state.
func (registry *RoomRegistry) compensateFailedStartLocked(entry *registeredRoom, roomID domain.RoomID, tx *eventTx) {
	registry.stopExpiryTimer(entry)
	entry.confirmation = nil
	entry.roomStatus = protocol.RoomStatusLobby
	resetReadyStatesLocked(entry)
	tx.emit(func(sequence uint64) (any, error) {
		return protocol.NewRoomUpdatedEvent(roomID, sequence, entry.roomStatus)
	})
}

// resetReadyStatesLocked clears every player's ready flag so the next start
// requires explicit re-confirmation of readiness.
func resetReadyStatesLocked(entry *registeredRoom) error {
	players := entry.lobby.Players()
	ids := make([]domain.PlayerID, 0, len(players))
	for id := range players {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(left, right int) bool { return ids[left] < ids[right] })
	for _, id := range ids {
		if err := entry.lobby.SetReady(id, players[id].CPU); err != nil {
			return err
		}
	}
	return nil
}

// awaitDeadlineAndExpire runs on the expiry timer goroutine and re-arms itself
// until the monotonic deadline passes, then submits the canonical failed-start
// transition through the same mutex that serializes commands.
func (registry *RoomRegistry) awaitDeadlineAndExpire(roomID domain.RoomID) {
	registry.mutex.Lock()
	entry, exists := registry.rooms[roomID]
	if !exists || entry.confirmation == nil || entry.confirmation.Snapshot().Status != room.StartConfirmationPending {
		registry.mutex.Unlock()
		return
	}
	deadline := entry.confirmation.Snapshot().DeadlineAt
	remaining := time.Until(deadline)
	registry.mutex.Unlock()

	if remaining > 0 {
		time.AfterFunc(remaining, func() { registry.awaitDeadlineAndExpire(roomID) })
		return
	}
	_ = registry.ExpireStartConfirmation(roomID)
}

// ExpireStartConfirmation applies the canonical failed-start transition at or
// after the deadline: nonresponders leave the lobby and every remaining
// player's ready state resets in one atomic step. It clears the closed attempt
// so the balanced remaining roster may request a restart.
func (registry *RoomRegistry) ExpireStartConfirmation(roomID domain.RoomID) error {
	registry.mutex.Lock()
	defer registry.mutex.Unlock()

	entry, exists := registry.rooms[roomID]
	if !exists || entry.confirmation == nil || entry.confirmation.Snapshot().Status != room.StartConfirmationPending {
		return nil
	}
	if entry.poisoned {
		return ErrEventStoreUnavailable
	}
	if _, err := entry.confirmation.Expire(entry.lobby, registry.clock()); err != nil {
		return err
	}
	registry.stopExpiryTimer(entry)
	entry.confirmation = nil
	entry.roomStatus = protocol.RoomStatusLobby
	tx := registry.newEventTx(roomID)
	tx.emit(func(sequence uint64) (any, error) {
		return protocol.NewRoomUpdatedEvent(roomID, sequence, entry.roomStatus)
	})
	return tx.flush()
}

// guardLobbyMutation rejects membership mutations while a start window is open
// or after the match has started.
func (registry *RoomRegistry) guardLobbyMutation(entry *registeredRoom) error {
	if entry.poisoned {
		return ErrEventStoreUnavailable
	}
	if entry.started {
		return ErrRoomAlreadyStarted
	}
	if entry.confirmation != nil {
		return ErrStartAlreadyRequested
	}
	return nil
}

func (registry *RoomRegistry) stopExpiryTimer(entry *registeredRoom) {
	if entry.expiryTimer != nil {
		entry.expiryTimer.Stop()
		entry.expiryTimer = nil
	}
}
