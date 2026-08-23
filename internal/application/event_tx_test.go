package application

import (
	"context"
	"errors"
	"sync"
	"testing"

	"buk-yutnori/internal/auth"
	"buk-yutnori/internal/domain"
	"buk-yutnori/internal/storage"
)

// fakeEventStore records appended rows and can be pointed at a failure for
// injection tests.
type fakeEventStore struct {
	mu   sync.Mutex
	rows []storage.EventRow
	fail error
}

func (store *fakeEventStore) AppendRoomEvents(ctx context.Context, rows []storage.EventRow) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.fail != nil {
		return store.fail
	}
	for _, row := range rows {
		row.PayloadJSON = storage.FormatPayloadCopy(row.PayloadJSON)
		store.rows = append(store.rows, row)
	}
	return nil
}

func (store *fakeEventStore) ReadRoomEventsAfter(ctx context.Context, roomID domain.RoomID, afterSequence uint64) ([]storage.EventRow, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.fail != nil {
		return nil, store.fail
	}
	read := make([]storage.EventRow, 0)
	for _, row := range store.rows {
		if row.RoomID == roomID && row.Sequence > afterSequence {
			read = append(read, row)
		}
	}
	return read, nil
}

func (store *fakeEventStore) count() int {
	store.mu.Lock()
	defer store.mu.Unlock()
	return len(store.rows)
}

func (store *fakeEventStore) setFailure(err error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.fail = err
}

// Every broadcast event must land in the canonical store with the same
// sequence and byte-identical payload, with no gaps (issue #84).
func TestMatchEventsPersistBeforeBroadcastWithContiguousSequences(t *testing.T) {
	t.Parallel()

	fixture := newMatchFixture(t, nil)
	defer fixture.recorder.close()
	rt := fixture.runtime()
	firstPlayer := rt.currentPlayer()
	fixture.scriptThrowsFor(map[domain.PlayerID][]domain.YutResult{
		firstPlayer: {domain.YutDo},
	})

	fixture.throwUntilResolved(t, firstPlayer)
	fixture.driveUntilPlayerOrEnd(t, firstPlayer)

	rows := fixture.store.rows
	events := fixture.recorder.snapshotEvents()
	if len(events) == 0 || len(rows) < len(events) {
		t.Fatalf("rows=%d recorded=%d, want every recorded event stored", len(rows), len(events))
	}

	offset := len(rows) - len(events)
	for index, envelope := range events {
		row := rows[offset+index]
		if row.Sequence != envelope.Sequence {
			t.Fatalf("row[%d] sequence=%d recorded=%d", index, row.Sequence, envelope.Sequence)
		}
		if string(row.PayloadJSON) != string(envelope.Raw) {
			t.Fatalf("stored payload %s differs from broadcast %s", row.PayloadJSON, envelope.Raw)
		}
	}
	for index := 1; index < len(rows); index++ {
		if rows[index].Sequence != rows[index-1].Sequence+1 {
			t.Fatalf("stored sequences are not contiguous at %d: %d then %d",
				index, rows[index-1].Sequence, rows[index].Sequence)
		}
	}
}

// A durable append failure must fence the room: nothing committed, nothing
// broadcast, every subsequent transition refused until restart.
func TestStorageFailureFencesRoomWithoutCommitOrBroadcast(t *testing.T) {
	t.Parallel()

	fixture := newMatchFixture(t, nil)
	defer fixture.recorder.close()
	rt := fixture.runtime()
	current := rt.currentPlayer()

	boundaryBefore := boundaryOf(t, fixture.registry, fixture.roomID)
	recordedBefore := len(fixture.recorder.snapshotEvents())

	injected := errors.New("injected disk failure")
	fixture.registry.setEventStoreForTest(&failingStore{err: injected})

	err := fixture.registry.ThrowYut(auth.UserID(current), fixture.roomID, fixture.matchID)
	if !errors.Is(err, ErrEventStoreUnavailable) {
		t.Fatalf("ThrowYut under storage failure = %v, want ErrEventStoreUnavailable", err)
	}

	entry := fixture.registry.rooms[fixture.roomID]
	if !entry.poisoned {
		t.Fatal("room was not fenced after the durable failure")
	}
	if got := boundaryOf(t, fixture.registry, fixture.roomID); got != boundaryBefore {
		t.Fatalf("sequence boundary advanced during failure: %d -> %d", boundaryBefore, got)
	}
	if got := len(fixture.recorder.snapshotEvents()); got != recordedBefore {
		t.Fatalf("%d events were broadcast despite the failure", got-recordedBefore)
	}

	if err := fixture.registry.ThrowYut(auth.UserID(current), fixture.roomID, fixture.matchID); !errors.Is(err, ErrEventStoreUnavailable) {
		t.Fatalf("post-fence ThrowYut = %v, want ErrEventStoreUnavailable", err)
	}
	if err := fixture.registry.SetReady(fixture.users[0], fixture.roomID, true); !errors.Is(err, ErrEventStoreUnavailable) {
		t.Fatalf("post-fence SetReady = %v, want ErrEventStoreUnavailable", err)
	}
	for _, summary := range fixture.registry.List() {
		if summary.RoomID == fixture.roomID {
			t.Fatal("fenced room must not advertise itself in the open-room list")
		}
	}
	if _, bundleErr := fixture.registry.ReconnectBundle(
		auth.UserID(current), fixture.roomID, fixture.matchID, 0,
	); !errors.Is(bundleErr, ErrEventStoreUnavailable) {
		t.Fatalf("post-fence RECONNECT = %v, want ErrEventStoreUnavailable", bundleErr)
	}
}

type failingStore struct{ err error }

func (store *failingStore) AppendRoomEvents(ctx context.Context, rows []storage.EventRow) error {
	return store.err
}

func (store *failingStore) ReadRoomEventsAfter(ctx context.Context, roomID domain.RoomID, afterSequence uint64) ([]storage.EventRow, error) {
	return nil, store.err
}

// The replay read path verifies stored contiguity before serving bundles.
func TestReplayPayloadsRejectsGaps(t *testing.T) {
	t.Parallel()

	gapped := []storage.EventRow{
		{RoomID: "r", Sequence: 4, EventType: "A", PayloadJSON: []byte(`{"sequence":4}`)},
		{RoomID: "r", Sequence: 6, EventType: "B", PayloadJSON: []byte(`{"sequence":6}`)},
	}
	if _, err := replayPayloads(gapped); err == nil {
		t.Fatal("gap in stored rows unexpectedly accepted")
	}

	contiguous := []storage.EventRow{
		{RoomID: "r", Sequence: 5, EventType: "A", PayloadJSON: []byte(`{"sequence":5}`)},
		{RoomID: "r", Sequence: 6, EventType: "B", PayloadJSON: []byte(`{"sequence":6}`)},
	}
	events, err := replayPayloads(contiguous)
	if err != nil || len(events) != 2 ||
		string(events[0]) != `{"sequence":5}` || string(events[1]) != `{"sequence":6}` {
		t.Fatalf("replayPayloads(contiguous) = %v, %v", events, err)
	}
}
