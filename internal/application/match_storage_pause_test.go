package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
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
	seen              map[string]struct{}
}

func (store *flakyStore) AppendRoomEvents(ctx context.Context, rows []storage.EventRow) error {
	if store.failuresRemaining > 0 {
		store.failuresRemaining--
		return errors.New("injected transient store failure")
	}
	for _, row := range rows {
		key := string(row.RoomID) + "/" + strconv.FormatUint(row.Sequence, 10)
		if _, duplicate := store.seen[key]; duplicate {
			return fmt.Errorf("%w: %s", storage.ErrDuplicateEvent, key)
		}
	}
	for _, row := range rows {
		key := string(row.RoomID) + "/" + strconv.FormatUint(row.Sequence, 10)
		store.seen[key] = struct{}{}
		store.rows = append(store.rows, row)
	}
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
	fixture.registry.setEventStoreForTest(&flakyStore{
		failuresRemaining: initialFailures,
		seen:              make(map[string]struct{}),
	})
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

	// A pure storage-failure pause has no auto-resume deadline to expose.
	entrySnapshot, snapErr := fixture.registry.assembleGameSnapshotLocked(entry, boundaryBefore)
	if snapErr != nil {
		t.Fatalf("assemble snapshot error = %v", snapErr)
	}
	if !entrySnapshot.Pause.Paused || entrySnapshot.Pause.EndsAt != nil {
		t.Fatalf("pure storage pause leaked ends_at: %+v", entrySnapshot.Pause)
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
	assertNoDuplicateSequences(t, healthy.rows)
	// Prefix must be exactly the deferred batch (3) + pause marker + resume
	// marker; later events (restored-window expiry) may follow.
	deferred := []string{"YUT_RESULT", "RESULT_QUEUE_UPDATED", "MOVE_REQUIRED", "GAME_PAUSED", "GAME_RESUMED"}
	if len(healthy.rows) < len(deferred) {
		t.Fatalf("healthy store holds %d rows, want at least the deferred batch", len(healthy.rows))
	}
	for index, kind := range deferred {
		if got := healthy.rows[index].EventType; got != kind {
			t.Fatalf("recovered row[%d] = %s, want %s", index, got, kind)
		}
	}
	for _, row := range healthy.rows {
		if row.EventType != "GAME_PAUSED" {
			continue
		}
		var envelope struct {
			Payload struct {
				PreservedTimerMS uint64 `json:"preserved_timer_remaining_ms"`
			} `json:"payload"`
		}
		if json.Unmarshal(row.PayloadJSON, &envelope) != nil || envelope.Payload.PreservedTimerMS == 0 {
			t.Fatalf("GAME_PAUSED marker lost the preserved window: %s", row.PayloadJSON)
		}
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

// A host pause whose own flush fails must survive the operational pause:
// the expiry timer is never cancelled, recovery leaves the host pause
// intact, and its expiry still auto-resumes with the preserved window.
func TestHostPauseSurvivesStorageFailureAndSettlesAfterRecovery(t *testing.T) {
	t.Parallel()

	fixture := newStoragePauseFixture(t, 99)
	host := fixture.users[0]
	scripted := fixture.runtime()
	scripted.throwResult = func(yut.Mode) (domain.YutResult, error) { return domain.YutGae, nil }
	firstPlayer := scripted.currentPlayer()
	fixture.driveUntilPlayerOrEnd(t, firstPlayer)

	current := fixture.runtime().currentPlayer()
	if err := fixture.registry.ThrowYut(auth.UserID(current), fixture.roomID, fixture.matchID); err != nil {
		t.Fatalf("THROW_YUT(%s) error = %v", current, err)
	}
	if got := fixture.runtime().timerKind; got != matchTimerKindMove {
		t.Fatalf("armed window = %q, want move", got)
	}

	// The pause command itself hits the failing store: its GAME_PAUSED is
	// deferred, but the host pause state must survive intact.
	err := fixture.registry.PauseGame(auth.UserID(host), fixture.roomID, fixture.matchID, 5)
	if !errors.Is(err, ErrEventStoreUnavailable) {
		t.Fatalf("PauseGame under storage failure = %v, want ErrEventStoreUnavailable", err)
	}
	rt := fixture.runtime()
	if !rt.paused || !rt.storagePaused {
		t.Fatalf("combined pause state = user=%v storage=%v", rt.paused, rt.storagePaused)
	}
	if rt.pauseExpiryTimer == nil {
		t.Fatal("host pause expiry timer was cancelled by the storage pause")
	}
	if rt.preservedTimerKind != matchTimerKindMove || rt.preservedRemaining <= 0 {
		t.Fatalf("preserved window overwritten: kind=%q remaining=%v",
			rt.preservedTimerKind, rt.preservedRemaining)
	}

	// Recovering the store lets the deferred marker through, then settles
	// the surviving host pause without touching the preserved window.
	fixture.registry.setEventStoreForTest(&fakeEventStore{})
	fixture.clock.Advance(storageRetryDelays[0])
	rt = fixture.runtime()
	if rt.storagePaused || rt.paused {
		t.Fatalf("post-recovery pause state = storage=%v user=%v", rt.storagePaused, rt.paused)
	}
	resumed := fixture.recorder.ofTypes("GAME_RESUMED")
	if len(resumed) < 1 {
		t.Fatal("no GAME_RESUMED after recovery")
	}

	// The preserved movement window comes back with the expired host pause.
	if rt.timerKind != matchTimerKindMove || rt.preservedTimerKind != "" {
		t.Fatalf("restored window = kind %q preserved %q", rt.timerKind, rt.preservedTimerKind)
	}
	if rt.activeTimer == nil {
		t.Fatal("restored window has no live timer behind it")
	}

	// The game continues normally for the acting player.
	movable, movableErr := rt.movablePieceIDs(rt.machine.Snapshot())
	if movableErr != nil || len(movable) == 0 {
		t.Fatalf("no movable pieces after settled resume: %v / %v", movable, movableErr)
	}
	if err := fixture.registry.SelectPiece(
		auth.UserID(current), fixture.roomID, fixture.matchID,
		rt.machine.Snapshot().SelectedTokenID, movable[0],
	); err != nil {
		t.Fatalf("SELECT_PIECE after settled resume error = %v", err)
	}
}
