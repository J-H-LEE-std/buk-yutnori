package storage

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"buk-yutnori/internal/domain"
)

func openTestStore(t *testing.T) *SQLiteEventStore {
	t.Helper()
	path := filepath.Join(t.TempDir(), "events.db")
	store, err := OpenSQLite(path)
	if err != nil {
		t.Fatalf("OpenSQLite(%q) error = %v", path, err)
	}
	return store
}

func TestOpenSQLiteAppliesCanonicalSchemaAndWAL(t *testing.T) {
	t.Parallel()

	store := openTestStore(t)
	defer store.Close()

	var journalMode string
	if err := store.db.QueryRow("PRAGMA journal_mode").Scan(&journalMode); err != nil {
		t.Fatalf("read journal_mode: %v", err)
	}
	if journalMode != "wal" {
		t.Fatalf("journal_mode = %q, want wal", journalMode)
	}

	var userVersion int
	if err := store.db.QueryRow("PRAGMA user_version").Scan(&userVersion); err != nil {
		t.Fatalf("read user_version: %v", err)
	}
	if userVersion != sqliteSchemaVersion {
		t.Fatalf("user_version = %d, want %d", userVersion, sqliteSchemaVersion)
	}
}

func TestAppendRoomEventsRoundTripsThroughReplayRead(t *testing.T) {
	t.Parallel()

	store := openTestStore(t)
	defer store.Close()
	ctx := context.Background()
	roomID := domain.RoomID("room-replay-1")

	rows := []EventRow{
		{RoomID: roomID, Sequence: 1, EventType: "ROOM_UPDATED", PayloadJSON: []byte(`{"type":"ROOM_UPDATED","sequence":1}`)},
		{RoomID: roomID, Sequence: 2, EventType: "GAME_STARTED", PayloadJSON: []byte(`{"type":"GAME_STARTED","sequence":2}`)},
		{RoomID: roomID, Sequence: 3, EventType: "YUT_RESULT", PayloadJSON: []byte(`{"type":"YUT_RESULT","sequence":3}`)},
	}
	if err := store.AppendRoomEvents(ctx, rows); err != nil {
		t.Fatalf("AppendRoomEvents() error = %v", err)
	}

	read, err := store.ReadRoomEventsAfter(ctx, roomID, 0)
	if err != nil {
		t.Fatalf("ReadRoomEventsAfter(0) error = %v", err)
	}
	if len(read) != 3 {
		t.Fatalf("read %d rows, want 3", len(read))
	}
	for index, row := range read {
		if row.Sequence != uint64(index+1) {
			t.Fatalf("rows[%d].Sequence = %d, want %d", index, row.Sequence, index+1)
		}
		if string(row.PayloadJSON) != string(rows[index].PayloadJSON) {
			t.Fatalf("payload[%d] = %s, want %s (bytes must round-trip verbatim)", index, row.PayloadJSON, rows[index].PayloadJSON)
		}
		if row.CreatedAtMS == 0 {
			t.Fatalf("rows[%d] missing created_at_ms", index)
		}
	}

	tail, err := store.ReadRoomEventsAfter(ctx, roomID, 1)
	if err != nil {
		t.Fatalf("ReadRoomEventsAfter(1) error = %v", err)
	}
	if len(tail) != 2 || tail[0].Sequence != 2 || tail[1].Sequence != 3 {
		t.Fatalf("tail = %+v, want sequences 2 and 3", tail)
	}

	other, err := store.ReadRoomEventsAfter(ctx, domain.RoomID("room-other"), 0)
	if err != nil || len(other) != 0 {
		t.Fatalf("unrelated room read = %+v error = %v, want empty", other, err)
	}
}

func TestAppendRoomEventsIsAtomicAndRejectsDuplicates(t *testing.T) {
	t.Parallel()

	store := openTestStore(t)
	defer store.Close()
	ctx := context.Background()
	roomID := domain.RoomID("room-dup-1")

	first := []EventRow{
		{RoomID: roomID, Sequence: 1, EventType: "ROOM_UPDATED", PayloadJSON: []byte(`{}`)},
	}
	if err := store.AppendRoomEvents(ctx, first); err != nil {
		t.Fatalf("seed append error = %v", err)
	}

	conflicting := []EventRow{
		{RoomID: roomID, Sequence: 2, EventType: "TURN_STARTED", PayloadJSON: []byte(`{"n":2}`)},
		{RoomID: roomID, Sequence: 1, EventType: "DUPLICATE", PayloadJSON: []byte(`{}`)},
	}
	err := store.AppendRoomEvents(ctx, conflicting)
	if !errors.Is(err, ErrDuplicateEvent) {
		t.Fatalf("conflicting append error = %v, want ErrDuplicateEvent", err)
	}

	// The whole batch must have been rolled back: sequence 2 is unreadable.
	read, readErr := store.ReadRoomEventsAfter(ctx, roomID, 1)
	if readErr != nil {
		t.Fatalf("post-abort read error = %v", readErr)
	}
	if len(read) != 0 {
		t.Fatalf("aborted batch leaked %d rows", len(read))
	}

	retry := []EventRow{
		{RoomID: roomID, Sequence: 2, EventType: "TURN_STARTED", PayloadJSON: []byte(`{"n":2}`)},
	}
	if err := store.AppendRoomEvents(ctx, retry); err != nil {
		t.Fatalf("retry append error = %v", err)
	}
}

func TestClosedStoreRejectsOperations(t *testing.T) {
	t.Parallel()

	store := openTestStore(t)
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	ctx := context.Background()
	if err := store.AppendRoomEvents(ctx, nil); err == nil {
		t.Fatal("append on closed store unexpectedly succeeded")
	}
	if _, err := store.ReadRoomEventsAfter(ctx, domain.RoomID("r"), 0); err == nil {
		t.Fatal("read on closed store unexpectedly succeeded")
	}
	if err := store.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
}
