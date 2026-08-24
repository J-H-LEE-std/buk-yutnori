package application

import (
	"context"
	"errors"
	"testing"

	"buk-yutnori/internal/auth"
	"buk-yutnori/internal/domain"
	"buk-yutnori/internal/domain/yut"
	"buk-yutnori/internal/storage"
)

// flakyStore fails the next N appends and then behaves like a healthy store.
type flakyStore struct {
	failuresRemaining int
	rows              []storage.EventRow
}

func (store *flakyStore) AppendRoomEvents(ctx context.Context, rows []storage.EventRow) error {
	if store.failuresRemaining > 0 {
		store.failuresRemaining--
		return errors.New("injected transient store failure")
	}
	store.rows = append(store.rows, rows...)
	return nil
}

func (store *flakyStore) ReadRoomEventsAfter(ctx context.Context, roomID domain.RoomID, afterSequence uint64) ([]storage.EventRow, error) {
	read := make([]storage.EventRow, 0)
	for _, row := range store.rows {
		if row.RoomID == roomID && row.Sequence > afterSequence {
			read = append(read, row)
		}
	}
	return read, nil
}

func newStoragePauseFixture(t *testing.T, initialFailures int) *matchFixture {
	t.Helper()
	fixture := newMatchFixture(t, nil)
	t.Cleanup(fixture.recorder.close)
	fixture.registry.setEventStoreForTest(&flakyStore{failuresRemaining: initialFailures})
	scripted := fixture.runtime()
	scripted.throwResult = func(yut.Mode) (domain.YutResult, error) { return domain.YutGae, nil }
	return fixture
}

// An initial durable failure must degrade into the operational pause without
// committing or broadcasting anything, while RECONNECT stays available.
func TestStorageFailureAutoPausesWithoutCommitOrBroadcast(t *testing.T) {
	t.Parallel()

	fixture := newStoragePauseFixture(t, 1)
	rt := fixture.runtime()
	current := rt.currentPlayer()

	boundaryBefore := boundaryOf(t, fixture.registry, fixture.roomID)
	recordedBefore := len(fixture.recorder.snapshotEvents())

	err := fixture.registry.ThrowYut(auth.UserID(current), fixture.roomID, fixture.matchID)
	if !errors.Is(err, ErrEventStoreUnavailable) {
		t.Fatalf("ThrowYut under storage failure = %v, want ErrEventStoreUnavailable", err)
	}

	entry := fixture.registry.rooms[fixture.roomID]
	pausedRuntime := entry.runtime
	if !pausedRuntime.storagePaused || len(pausedRuntime.pendingRows) == 0 {
		t.Fatalf("storage pause not entered: paused=%v pending=%d", pausedRuntime.storagePaused, len(pausedRuntime.pendingRows))
	}
	if got := boundaryOf(t, fixture.registry, fixture.roomID); got != boundaryBefore {
		t.Fatalf("boundary advanced during failure: %d -> %d", boundaryBefore, got)
	}
	events := fixture.recorder.snapshotEvents()
	if len(events) != recordedBefore {
		t.Fatalf("%d events were broadcast despite the storage pause", len(events)-recordedBefore)
	}
	if pausedRuntime.preservedTimerKind == "" || pausedRuntime.preservedRemaining <= 0 {
		t.Fatalf("turn window not preserved: kind=%q remaining=%v",
			pausedRuntime.preservedTimerKind, pausedRuntime.preservedRemaining)
	}

	// Live commands are rejected; RECONNECT still serves the pre-blip state.
	if err := fixture.registry.ThrowYut(auth.UserID(current), fixture.roomID, fixture.matchID); !errors.Is(err, ErrEventStoreUnavailable) {
		t.Fatalf("command during storage pause = %v, want ErrEventStoreUnavailable", err)
	}
	if _, bundleErr := fixture.registry.ReconnectBundle(
		auth.UserID(current), fixture.roomID, fixture.matchID, 0,
	); bundleErr != nil {
		t.Fatalf("RECONNECT during storage pause = %v, want success", bundleErr)
	}
}

// A recovered store commits the whole deferred batch - missed events, the
// GAME_PAUSED marker, and GAME_RESUMED(storage_recovered) - in one atomic
// append with contiguous sequences, then restores the preserved window.
func TestRecoveryDeliversDeferredBatchAndRestoresWindow(t *testing.T) {
	t.Parallel()

	fixture := newStoragePauseFixture(t, 1)
	rt := fixture.runtime()
	current := rt.currentPlayer()
	boundaryBefore := boundaryOf(t, fixture.registry, fixture.roomID)

	if err := fixture.registry.ThrowYut(auth.UserID(current), fixture.roomID, fixture.matchID); !errors.Is(err, ErrEventStoreUnavailable) {
		t.Fatalf("expected storage-failure rejection, got %v", err)
	}

	healthy := &fakeEventStore{}
	fixture.registry.setEventStoreForTest(healthy)
	fixture.clock.Advance(storageRetryDelays[0])

	all := fixture.recorder.snapshotEvents()
	delivered := all[len(all)-5:]
	wantOrder := []string{"YUT_RESULT", "RESULT_QUEUE_UPDATED", "MOVE_REQUIRED", "GAME_PAUSED", "GAME_RESUMED"}
	if len(delivered) != len(wantOrder) {
		t.Fatalf("recovery delivered %d events (%v), want %v", len(delivered), all[len(all)-8:], wantOrder)
	}
	for index, kind := range wantOrder {
		if delivered[index].Type != kind {
			t.Fatalf("delivery[%d] = %s, want %s (full tail: %v)",
				index, delivered[index].Type, kind, delivered)
		}
	}

	for index, envelope := range delivered {
		want := boundaryBefore + uint64(index) + 1
		if envelope.Sequence != want {
			t.Fatalf("delivery[%d] sequence = %d, want %d (contiguity)", index, envelope.Sequence, want)
		}
	}
	lastResumed := delivered[len(delivered)-1]
	if lastResumed.Payload.Reason != "storage_recovered" {
		t.Fatalf("resume reason = %q, want storage_recovered", lastResumed.Payload.Reason)
	}

	rt = fixture.runtime()
	if rt.storagePaused || rt.pendingRows != nil {
		t.Fatalf("runtime did not clear the operational pause")
	}
	if rt.timerKind != matchTimerKindMove || rt.preservedTimerKind != "" {
		t.Fatalf("window restoration broken: kind=%q preserved=%q", rt.timerKind, rt.preservedTimerKind)
	}

	// The restored movement window expires into CPU substitution exactly once.
	fixture.clock.Advance(fixture.moveTimeout())
	cpuEvents := fixture.recorder.ofTypes("CPU_CONTROL_STARTED")
	if len(cpuEvents) != 1 || cpuEvents[0].Payload.PlayerID != current {
		t.Fatalf("CPU substitution after recovery = %+v", cpuEvents)
	}
	if len(healthy.rows) < 6 {
		t.Fatalf("healthy store only holds %d rows after recovery", len(healthy.rows))
	}
}

// Exhausting the retry schedule invalidates the match, notifies clients even
// though persistence fails, and returns the room to a joinable post_match.
func TestRetryExhaustionInvalidatesMatchAndNotifies(t *testing.T) {
	t.Parallel()

	fixture := newStoragePauseFixture(t, 99)
	host := fixture.users[0]
	rt := fixture.runtime()
	current := rt.currentPlayer()
	matchID := rt.matchID

	if err := fixture.registry.ThrowYut(auth.UserID(current), fixture.roomID, matchID); !errors.Is(err, ErrEventStoreUnavailable) {
		t.Fatalf("initial failure = %v, want ErrEventStoreUnavailable", err)
	}

	total := storageRetryDelays[0] + storageRetryDelays[1] + storageRetryDelays[2]
	fixture.clock.Advance(total)

	invalidEnds := fixture.recorder.ofTypes("GAME_ENDED")
	if len(invalidEnds) != 1 || invalidEnds[0].Payload.Status != "invalid" ||
		invalidEnds[0].Payload.WinnerTeamID != nil ||
		invalidEnds[0].Payload.Reason != gameEndedReasonStorageRetryExhausted {
		t.Fatalf("GAME_ENDED = %+v, want invalid termination", invalidEnds)
	}
	roomUpdates := fixture.recorder.ofTypes("ROOM_UPDATED")
	if len(roomUpdates) == 0 || roomUpdates[len(roomUpdates)-1].Payload.Status != "post_match" {
		t.Fatalf("post-match ROOM_UPDATED missing: %+v", roomUpdates)
	}

	entry := fixture.registry.rooms[fixture.roomID]
	if entry.started || entry.runtime != nil || entry.poisoned {
		t.Fatalf("terminal teardown incomplete: started=%v runtime=%v poisoned=%v",
			entry.started, entry.runtime, entry.poisoned)
	}
	membership, membershipErr := fixture.registry.Membership(host, fixture.roomID)
	if membershipErr != nil || membership.Ready {
		t.Fatalf("membership after invalidation = %+v error = %v, want cleared ready", membership, membershipErr)
	}
	listed := false
	for _, summary := range fixture.registry.List() {
		if summary.RoomID == fixture.roomID {
			listed = true
		}
	}
	if !listed {
		t.Fatal("invalidated room must stay listed for rematches")
	}

	command := reconnectCommandFor(fixture.roomID, matchID, "cmd-reconnect-invalidated", 0)
	result, procErr := fixture.processor.Process(context.Background(), auth.User{ID: host}, command)
	if procErr != nil {
		t.Fatalf("RECONNECT process error = %v", procErr)
	}
	if result.Payload.Status != "rejected" || result.Payload.Error == nil ||
		result.Payload.Error.Code != "RESYNC_REQUIRED" {
		t.Fatalf("RECONNECT after invalidation = %+v, want RESYNC_REQUIRED", result)
	}
}
