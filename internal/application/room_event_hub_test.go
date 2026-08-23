package application

import (
	"testing"
	"time"

	"buk-yutnori/internal/auth"
	"buk-yutnori/internal/protocol"
)

func drainEvent(t *testing.T, subscription *RoomEventSubscription) protocol.RoomUpdatedEvent {
	t.Helper()

	select {
	case event := <-subscription.Events():
		return event.Message.(protocol.RoomUpdatedEvent)
	case <-time.After(time.Second):
		t.Fatal("expected cached room event was not delivered")
		return protocol.RoomUpdatedEvent{}
	}
}

// Invariant 1 (ADR-0015): subscription registration and the latest-state
// snapshot are atomic — a fresh subscriber immediately receives the room's
// cached ROOM_UPDATED without any interim poll.
func TestSubscribeDeliversLatestStateSnapshot(t *testing.T) {
	t.Parallel()

	fixture := newStartFixture(t, 2)
	subscription, err := fixture.registry.SubscribeEvents(lobbyCreatorID)
	if err != nil {
		t.Fatalf("SubscribeEvents() error = %v", err)
	}
	defer subscription.Close()

	event := drainEvent(t, subscription)
	if event.Payload.Status != protocol.RoomStatusLobby || event.Payload.Revision != event.Sequence || event.Sequence == 0 {
		t.Fatalf("cached event = %+v, want lobby whose revision equals its sequence", event)
	}
}

// Invariant 2 (ADR-0015): while a start window is open, subscribing delivers
// the active GAME_STARTING so late/dropped clients recover match_id.
func TestGameStartingRedeliveredDuringWindow(t *testing.T) {
	t.Parallel()

	fixture := newStartFixture(t, 2)
	if err := fixture.registry.RequestStart(lobbyCreatorID, fixture.roomID); err != nil {
		t.Fatalf("RequestStart() error = %v", err)
	}

	subscription, err := fixture.registry.SubscribeEvents(auth.UserID(startRosterIDs[1]))
	if err != nil {
		t.Fatalf("SubscribeEvents() error = %v", err)
	}
	defer func() {
		resolveWindow(t, fixture)
		subscription.Close()
	}()

	updated := drainEvent(t, subscription)
	if updated.Payload.Status != protocol.RoomStatusStarting {
		t.Fatalf("first snapshot = %+v, want status starting", updated)
	}

	select {
	case event := <-subscription.Events():
		starting := event.Message.(protocol.GameStartingEvent)
		if starting.MatchID == "" || starting.Payload.ConfirmationDeadlineAt == "" {
			t.Fatalf("GAME_STARTING = %+v, want match id and deadline", starting)
		}
	case <-time.After(time.Second):
		t.Fatal("active GAME_STARTING was not redelivered on subscribe")
	}
}

func TestEmissionsKeepSequenceOrderingAndFilterNonMembers(t *testing.T) {
	t.Parallel()

	fixture := newStartFixture(t, 2)
	outsiderSubscription, err := fixture.registry.SubscribeEvents(auth.UserID(startLobbyOutsiderID))
	if err != nil {
		t.Fatalf("SubscribeEvents(outsider) error = %v", err)
	}
	defer outsiderSubscription.Close()

	memberSubscription, err := fixture.registry.SubscribeEvents(lobbyCreatorID)
	if err != nil {
		t.Fatalf("SubscribeEvents(member) error = %v", err)
	}
	defer memberSubscription.Close()
	cached := drainEvent(t, memberSubscription) // latest snapshot at subscribe time

	if err := fixture.registry.SetReady(lobbyCreatorID, fixture.roomID, false); err != nil {
		t.Fatalf("SetReady() error = %v", err)
	}
	event := drainEvent(t, memberSubscription)
	if event.Payload.Revision != cached.Payload.Revision+1 {
		t.Fatalf("emitted revision = %d, want exactly cached+1 (%d)", event.Payload.Revision, cached.Payload.Revision)
	}

	select {
	case stale := <-outsiderSubscription.Events():
		t.Fatalf("non-member received %+v", stale.Message)
	case <-time.After(100 * time.Millisecond):
	}
}
