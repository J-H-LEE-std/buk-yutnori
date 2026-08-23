// Emission transactions for committed room events (issue #84).
//
// One serialized operation stages every event it produces, persists them
// durably as one atomic batch, and only then consumes their room sequences
// and delivers them to subscribers: validate/compute → persist → commit →
// broadcast (docs/00, spec/turn_state_machine.yaml
// game_state_event_commit_order, ADR-0002, ADR-0014). A durable failure
// fences the room instead of letting memory diverge from the canonical store
// (ADR-0017).

package application

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"buk-yutnori/internal/domain"
	"buk-yutnori/internal/protocol"
	"buk-yutnori/internal/storage"
)

const eventStoreWriteTimeout = 5 * time.Second

// eventStoreContext detaches store operations from caller cancellation:
// accepted commands run to completion at this boundary (ADR-0012), bounded
// by an internal deadline.
func eventStoreContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), eventStoreWriteTimeout)
}

// eventTx stages the events of exactly one serialized registry operation.
type eventTx struct {
	registry *RoomRegistry
	roomID   string
	builders []func(sequence uint64) (any, error)
}

func (registry *RoomRegistry) newEventTx(roomID domain.RoomID) *eventTx {
	return &eventTx{registry: registry, roomID: string(roomID)}
}

// emit stages one event builder. Builders receive their sequence number only
// at flush time; they must capture payload values eagerly so a staged event
// never observes state mutated after it was staged.
func (tx *eventTx) emit(build func(sequence uint64) (any, error)) {
	tx.builders = append(tx.builders, build)
}

// flush persists every staged event durably before consuming their room
// sequences and delivering them. With no store attached it degrades to the
// legacy in-memory behavior used by tests. A durable append failure marks
// the room poisoned: nothing is committed or broadcast, and subsequent
// transitions are refused until restart.
func (tx *eventTx) flush() error {
	if len(tx.builders) == 0 {
		return nil
	}
	registry := tx.registry
	boundary, err := registry.sequences.Boundary(domain.RoomID(tx.roomID))
	if err != nil {
		return err
	}

	messages := make([]any, len(tx.builders))
	rows := make([]storage.EventRow, 0, len(tx.builders))
	for index, build := range tx.builders {
		sequence := boundary + uint64(index) + 1
		message, err := build(sequence)
		if err != nil {
			return err
		}
		messages[index] = message
		if registry.store == nil {
			continue
		}
		encoded, err := json.Marshal(message)
		if err != nil {
			return fmt.Errorf("encode %T for storage: %w", message, err)
		}
		rows = append(rows, storage.EventRow{
			RoomID:      domain.RoomID(tx.roomID),
			Sequence:    sequence,
			EventType:   serverEventType(encoded),
			PayloadJSON: encoded,
		})
	}

	if len(rows) > 0 {
		ctx, cancel := eventStoreContext()
		defer cancel()
		if err := registry.store.AppendRoomEvents(ctx, rows); err != nil {
			if entry, exists := registry.rooms[domain.RoomID(tx.roomID)]; exists {
				entry.poisoned = true
			}
			return fmt.Errorf("%w: append %d events: %v", ErrEventStoreUnavailable, len(rows), err)
		}
	}

	entry := registry.rooms[domain.RoomID(tx.roomID)]
	for range messages {
		if _, err := registry.sequences.CommitNext(domain.RoomID(tx.roomID)); err != nil {
			return err
		}
	}
	if entry == nil {
		// The operation removed the room (prototype cleanup paths); the
		// events were still persisted but nobody is subscribed anymore.
		return nil
	}
	for _, message := range messages {
		switch event := message.(type) {
		case protocol.GameStartingEvent:
			entry.activeGameStarting = message
		case protocol.RoomUpdatedEvent:
			entry.lastRoomUpdated = message
			if event.Payload.Status == protocol.RoomStatusLobby ||
				event.Payload.Status == protocol.RoomStatusInMatch {
				entry.activeGameStarting = nil
			}
		}
		for subscription, user := range registry.eventSubscribers {
			if entry.hasMember(user) {
				registry.deliverLocked(subscription, RoomEvent{Message: message})
			}
		}
	}
	return nil
}

func serverEventType(encoded []byte) string {
	var probe struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(encoded, &probe) != nil {
		return ""
	}
	return probe.Type
}
