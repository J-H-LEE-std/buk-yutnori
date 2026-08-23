// Room event hub delivering registry transitions to member subscribers.

package application

import (
	"buk-yutnori/internal/auth"
	"buk-yutnori/internal/domain"
	"buk-yutnori/internal/protocol"
)

const roomEventBuffer = 16

// RoomEvent is one delivered server event of any supported type.
type RoomEvent struct {
	Message any
}

// RoomEventSubscription streams hub events for one authenticated connection.
type RoomEventSubscription struct {
	registry *RoomRegistry
	events   chan RoomEvent
	done     chan struct{}
}

// Events returns the delivery channel. It closes when the subscriber is
// dropped for backpressure.
func (subscription *RoomEventSubscription) Events() <-chan RoomEvent {
	return subscription.events
}

// Done signals drop; reading Events after Done fails closed.
func (subscription *RoomEventSubscription) Done() <-chan struct{} {
	return subscription.done
}

// Close unsubscribes the connection.
func (subscription *RoomEventSubscription) Close() {
	registry := subscription.registry
	registry.mutex.Lock()
	defer registry.mutex.Unlock()
	if _, exists := registry.eventSubscribers[subscription]; exists {
		delete(registry.eventSubscribers, subscription)
		close(subscription.done)
		close(subscription.events)
	}
}

// SubscribeEvents registers one connection for events of every room the user
// is currently a member of, plus future rooms they join while subscribed.
// Registration and the latest-state snapshot are atomic within the same
// critical section as state changes (ADR-0015 invariant).
func (registry *RoomRegistry) SubscribeEvents(user auth.UserID) (*RoomEventSubscription, error) {
	if err := user.Validate(); err != nil {
		return nil, err
	}

	registry.mutex.Lock()
	defer registry.mutex.Unlock()

	subscription := &RoomEventSubscription{
		registry: registry,
		events:   make(chan RoomEvent, roomEventBuffer),
		done:     make(chan struct{}),
	}
	registry.eventSubscribers[subscription] = user

	for _, entry := range registry.rooms {
		if !entry.hasMember(user) {
			continue
		}
		if entry.lastRoomUpdated != nil && !registry.deliverLocked(subscription, RoomEvent{Message: entry.lastRoomUpdated}) {
			break
		}
		if entry.activeGameStarting != nil {
			registry.deliverLocked(subscription, RoomEvent{Message: entry.activeGameStarting})
		}
	}
	return subscription, nil
}

func (entry *registeredRoom) hasMember(user auth.UserID) bool {
	if _, spectator := entry.spectators[user]; spectator {
		return true
	}
	player, err := playerIDFromUser(user)
	if err != nil {
		return false
	}
	_, player_ := entry.lobby.Player(player)
	return player_
}

// emitLocked consumes one room sequence, publishes the built message to every
// member subscriber, and refreshes caches — all inside the caller's critical
// section so ordering matches sequence order exactly (ADR-0015).
func (registry *RoomRegistry) emitLocked(roomID domain.RoomID, build func(sequence uint64) (any, error)) error {
	sequence, err := registry.sequences.CommitNext(roomID)
	if err != nil {
		return err
	}
	message, err := build(sequence)
	if err != nil {
		return err
	}

	entry := registry.rooms[roomID]
	switch event := message.(type) {
	case protocol.GameStartingEvent:
		entry.activeGameStarting = message
	case protocol.RoomUpdatedEvent:
		entry.lastRoomUpdated = message
		if event.Payload.Status == protocol.RoomStatusLobby || event.Payload.Status == protocol.RoomStatusInMatch {
			entry.activeGameStarting = nil
		}
	}

	for subscription, user := range registry.eventSubscribers {
		if entry.hasMember(user) {
			registry.deliverLocked(subscription, RoomEvent{Message: message})
		}
	}
	return nil
}

// deliverLocked performs the non-blocking send and drops slow subscribers.
// The caller holds the registry mutex.
func (registry *RoomRegistry) deliverLocked(subscription *RoomEventSubscription, event RoomEvent) bool {
	select {
	case subscription.events <- event:
		return true
	default:
		delete(registry.eventSubscribers, subscription)
		close(subscription.done)
		close(subscription.events)
		return false
	}
}
