// Room event hub delivering registry transitions to member subscribers.

package application

import (
	"buk-yutnori/internal/auth"
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

	buffer := registry.eventBufferSize
	if buffer <= 0 {
		buffer = roomEventBuffer
	}
	subscription := &RoomEventSubscription{
		registry: registry,
		events:   make(chan RoomEvent, buffer),
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
