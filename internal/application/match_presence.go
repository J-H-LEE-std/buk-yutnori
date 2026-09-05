package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"buk-yutnori/internal/auth"
	"buk-yutnori/internal/domain"
	"buk-yutnori/internal/protocol"
	"buk-yutnori/internal/storage"
)

const (
	allPlayersDisconnectedGrace = 30 * time.Second
	gameEndedReasonDisconnected = "all_players_disconnected"
)

// ConnectionOpened registers one authenticated WebSocket. Only the first
// socket for a user creates a presence transition; additional tabs only raise
// the reference count.
func (registry *RoomRegistry) ConnectionOpened(user auth.UserID) error {
	if _, err := playerIDFromUser(user); err != nil {
		return err
	}
	registry.mutex.Lock()
	defer registry.mutex.Unlock()
	registry.presenceEnabled = true
	previous := registry.connections[user]
	registry.connections[user] = previous + 1
	if previous != 0 {
		return nil
	}
	return registry.applyPresenceTransitionLocked(user, true)
}

// ConnectionClosed releases one authenticated WebSocket. Unknown or duplicate
// closes are harmless, and only the last socket creates a presence transition.
func (registry *RoomRegistry) ConnectionClosed(user auth.UserID) error {
	if _, err := playerIDFromUser(user); err != nil {
		return err
	}
	registry.mutex.Lock()
	defer registry.mutex.Unlock()
	count := registry.connections[user]
	if count == 0 {
		return nil
	}
	if count > 1 {
		registry.connections[user] = count - 1
		return nil
	}
	delete(registry.connections, user)
	return registry.applyPresenceTransitionLocked(user, false)
}

func (registry *RoomRegistry) userConnectedLocked(user auth.UserID) bool {
	if !registry.presenceEnabled {
		return true
	}
	return registry.connections[user] > 0
}

func (registry *RoomRegistry) playerConnectedLocked(player domain.PlayerID) bool {
	return registry.userConnectedLocked(auth.UserID(player))
}

func (registry *RoomRegistry) applyPresenceTransitionLocked(user auth.UserID, connected bool) error {
	playerID := domain.PlayerID(user)
	var transitionErrors []error
	for _, roomID := range append([]domain.RoomID(nil), registry.ordering...) {
		entry, exists := registry.rooms[roomID]
		if !exists || entry.poisoned {
			continue
		}
		player, isPlayer := entry.lobby.Player(playerID)
		if !isPlayer || player.CPU {
			continue
		}
		if !connected && entry.confirmation != nil && !entry.started {
			if err := entry.confirmation.MarkDisconnected(playerID); err != nil {
				transitionErrors = append(transitionErrors, err)
			}
			continue
		}
		if !entry.started || entry.runtime == nil {
			continue
		}
		rt := entry.runtime
		tx := registry.newEventTx(roomID)
		if connected {
			tx.emit(func(sequence uint64) (any, error) {
				return protocol.NewPlayerReconnectedEvent(roomID, rt.matchID, sequence, playerID, true)
			})
			if rt.allPlayersDisconnected {
				registry.resumeAllDisconnectedLocked(entry, rt, tx)
			}
		} else {
			tx.emit(func(sequence uint64) (any, error) {
				return protocol.NewPlayerDisconnectedEvent(roomID, rt.matchID, sequence, playerID)
			})
			if entry.host == user && rt.paused {
				if rt.pauseExpiryTimer != nil {
					rt.pauseExpiryTimer.Stop()
					rt.pauseExpiryTimer = nil
				}
				if err := registry.resumeMatchLocked(tx, rt, protocol.ResumeReasonHostDisconnected); err != nil {
					transitionErrors = append(transitionErrors, err)
					continue
				}
			}
			if registry.allHumanPlayersDisconnectedLocked(entry) {
				registry.suspendAllDisconnectedLocked(rt)
			} else if rt.currentPlayer() == playerID && !rt.cpuControlled && !rt.paused && !rt.storagePaused {
				registry.startDisconnectedCPUControlLocked(entry, rt, tx)
			}
		}
		if rt.storagePaused {
			if err := registry.appendDeferredPresenceEventsLocked(tx, rt); err != nil {
				transitionErrors = append(transitionErrors, err)
			}
			if connected && rt.activeTimer == nil {
				registry.restartStorageRetryLocked(rt)
			}
			continue
		}
		if err := tx.flush(); err != nil {
			transitionErrors = append(transitionErrors, err)
		}
	}
	return errors.Join(transitionErrors...)
}

func (registry *RoomRegistry) restartStorageRetryLocked(rt *matchRuntime) {
	rt.retryAttempt = 0
	roomID := rt.roomID
	rt.activeTimer = registry.matchClock.AfterFunc(storageRetryDelays[0], func() {
		registry.fireStorageRetry(roomID, 0)
	})
}

func (registry *RoomRegistry) allHumanPlayersDisconnectedLocked(entry *registeredRoom) bool {
	for id, player := range entry.lobby.Players() {
		if !player.CPU && registry.playerConnectedLocked(id) {
			return false
		}
	}
	return true
}

func (registry *RoomRegistry) startDisconnectedCPUControlLocked(entry *registeredRoom, rt *matchRuntime, tx *eventTx) {
	player := rt.currentPlayer()
	rt.cpuControlled = true
	rt.cpuControlReason = cpuControlReasonDisconnected
	registry.cancelTimerLocked(rt)
	tx.emit(func(sequence uint64) (any, error) {
		return protocol.NewCPUControlStartedEvent(rt.roomID, rt.matchID, sequence, protocol.CPUControlStartedPayload{
			PlayerID: player, Reason: cpuControlReasonDisconnected,
		})
	})
	registry.runCpuTurnLocked(entry, rt, tx)
}

func (registry *RoomRegistry) suspendAllDisconnectedLocked(rt *matchRuntime) {
	if rt.allPlayersDisconnected {
		return
	}
	rt.allPlayersDisconnected = true
	// A storage pause already preserved the gameplay window and repurposed the
	// active timer for persistence retries. Presence suspension must not cancel
	// that independent retry loop.
	if rt.storagePaused {
		rt.presenceTimerKind = ""
		rt.presenceRemaining = 0
	} else {
		rt.presenceTimerKind = rt.timerKind
		if rt.timerKind != "" {
			rt.presenceRemaining = time.Duration(rt.remainingMS(registry.matchClock.Now())) * time.Millisecond
		}
		registry.cancelTimerLocked(rt)
	}
	rt.presenceGeneration++
	generation := rt.presenceGeneration
	roomID := rt.roomID
	rt.presenceTimer = registry.matchClock.AfterFunc(allPlayersDisconnectedGrace, func() {
		registry.fireAllDisconnectedExpiry(roomID, generation)
	})
}

func (registry *RoomRegistry) resumeAllDisconnectedLocked(entry *registeredRoom, rt *matchRuntime, tx *eventTx) {
	if !rt.allPlayersDisconnected {
		return
	}
	rt.allPlayersDisconnected = false
	rt.presenceGeneration++
	if rt.presenceTimer != nil {
		rt.presenceTimer.Stop()
		rt.presenceTimer = nil
	}
	if rt.paused || rt.storagePaused {
		return
	}
	if rt.cpuPlayers[rt.currentPlayer()] || !registry.playerConnectedLocked(rt.currentPlayer()) {
		rt.cpuControlled = true
		reason := cpuControlReasonLobbyPlayer
		if !rt.cpuPlayers[rt.currentPlayer()] {
			reason = cpuControlReasonDisconnected
		}
		rt.cpuControlReason = reason
		player := rt.currentPlayer()
		tx.emit(func(sequence uint64) (any, error) {
			return protocol.NewCPUControlStartedEvent(rt.roomID, rt.matchID, sequence, protocol.CPUControlStartedPayload{PlayerID: player, Reason: reason})
		})
		registry.runCpuTurnLocked(entry, rt, tx)
	} else {
		rt.cpuControlled = false
		rt.cpuControlReason = ""
		kind := rt.presenceTimerKind
		remaining := rt.presenceRemaining
		if kind == "" {
			kind = rt.preservedTimerKind
			remaining = rt.preservedRemaining
		}
		switch kind {
		case matchTimerKindThrow:
			registry.armTimerLocked(rt, matchTimerKindThrow, remaining)
		case matchTimerKindMove:
			registry.armTimerLocked(rt, matchTimerKindMove, remaining)
		}
	}
	rt.presenceTimerKind = ""
	rt.presenceRemaining = 0
	rt.preservedTimerKind = ""
	rt.preservedRemaining = 0
}

func (registry *RoomRegistry) fireAllDisconnectedExpiry(roomID domain.RoomID, generation uint64) {
	registry.mutex.Lock()
	defer registry.mutex.Unlock()
	entry, exists := registry.rooms[roomID]
	if !exists || entry.runtime == nil || entry.poisoned {
		return
	}
	rt := entry.runtime
	if !rt.allPlayersDisconnected || rt.presenceGeneration != generation || !registry.allHumanPlayersDisconnectedLocked(entry) {
		return
	}
	registry.terminateDisconnectedMatchLocked(entry, rt)
}

func (registry *RoomRegistry) terminateDisconnectedMatchLocked(entry *registeredRoom, rt *matchRuntime) {
	registry.cancelTimerLocked(rt)
	if rt.pauseExpiryTimer != nil {
		rt.pauseExpiryTimer.Stop()
	}
	if rt.presenceTimer != nil {
		rt.presenceTimer.Stop()
	}
	boundary, err := registry.sequences.Boundary(rt.roomID)
	if err != nil {
		slog.Error("presence invalidation boundary failed", "room_id", rt.roomID, "error", err)
		return
	}
	messages := append([]any(nil), rt.pendingMessages...)
	rows := append([]storage.EventRow(nil), rt.pendingRows...)
	next := boundary + uint64(len(rows)) + 1
	ended, endErr := protocol.NewInvalidGameEndedEvent(rt.roomID, rt.matchID, next, gameEndedReasonDisconnected)
	closed, closeErr := protocol.NewRoomUpdatedEvent(rt.roomID, next+1, "closed")
	if endErr != nil || closeErr != nil {
		slog.Error("presence invalidation event build failed", "room_id", rt.roomID, "error", errors.Join(endErr, closeErr))
		return
	}
	messages = append(messages, ended, closed)
	for _, message := range []any{ended, closed} {
		encoded, marshalErr := json.Marshal(message)
		if marshalErr != nil {
			slog.Error("presence invalidation encode failed", "room_id", rt.roomID, "error", marshalErr)
			return
		}
		rows = append(rows, storage.EventRow{RoomID: rt.roomID, Sequence: next, EventType: serverEventType(message), PayloadJSON: encoded})
		next++
	}
	persisted := registry.store == nil
	if registry.store != nil {
		ctx, cancel := context.WithTimeout(context.Background(), eventStoreWriteTimeout)
		persistErr := registry.store.AppendRoomEvents(ctx, rows)
		cancel()
		persisted = persistErr == nil
		if persistErr != nil {
			slog.Error("presence invalidation persist failed; closing in memory", "room_id", rt.roomID, "error", persistErr)
		}
	}
	if persisted {
		for range messages {
			if _, commitErr := registry.sequences.CommitNext(rt.roomID); commitErr != nil {
				slog.Error("presence invalidation sequence commit failed", "room_id", rt.roomID, "error", commitErr)
				break
			}
		}
	}
	registry.publishCommittedLocked(entry, messages)
	delete(registry.rooms, rt.roomID)
	registry.removeRoomOrderingLocked(rt.roomID)
	registry.sequences.ForgetClosedRoom(rt.roomID)
}

func (registry *RoomRegistry) appendDeferredPresenceEventsLocked(tx *eventTx, rt *matchRuntime) error {
	boundary, err := registry.sequences.Boundary(rt.roomID)
	if err != nil {
		return err
	}
	for _, build := range tx.builders {
		sequence := boundary + uint64(len(rt.pendingRows)) + 1
		message, err := build(sequence)
		if err != nil {
			return err
		}
		encoded, err := json.Marshal(message)
		if err != nil {
			return fmt.Errorf("encode deferred presence event: %w", err)
		}
		rt.pendingMessages = append(rt.pendingMessages, message)
		rt.pendingRows = append(rt.pendingRows, storage.EventRow{
			RoomID: rt.roomID, Sequence: sequence, EventType: serverEventType(message), PayloadJSON: encoded,
		})
	}
	return nil
}
