// Authoritative start confirmation lifecycle over the room registry.

package application

import (
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
	entry.expiryTimer = time.AfterFunc(room.StartConfirmationWindow, func() {
		registry.awaitDeadlineAndExpire(roomID)
	})
	if err := registry.emitLocked(roomID, func(sequence uint64) (any, error) {
		return protocol.NewGameStartingEvent(roomID, matchID, sequence, confirmation.Snapshot().DeadlineAt)
	}); err != nil {
		return err
	}
	return registry.emitLocked(roomID, func(sequence uint64) (any, error) {
		return protocol.NewRoomUpdatedEvent(roomID, sequence, protocol.RoomStatusStarting)
	})
}

// ConfirmStart records one roster player's confirmation. The last pending
// confirmation closes the window and marks the match as started.
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
	if allConfirmed {
		registry.stopExpiryTimer(entry)
		entry.started = true
		return registry.emitLocked(roomID, func(sequence uint64) (any, error) {
			return protocol.NewRoomUpdatedEvent(roomID, sequence, protocol.RoomStatusInMatch)
		})
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
	if _, err := entry.confirmation.Expire(entry.lobby, registry.clock()); err != nil {
		return err
	}
	registry.stopExpiryTimer(entry)
	entry.confirmation = nil
	return registry.emitLocked(roomID, func(sequence uint64) (any, error) {
		return protocol.NewRoomUpdatedEvent(roomID, sequence, protocol.RoomStatusLobby)
	})
}

// guardLobbyMutation rejects membership mutations while a start window is open
// or after the match has started.
func (registry *RoomRegistry) guardLobbyMutation(entry *registeredRoom) error {
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
