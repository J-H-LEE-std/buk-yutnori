package storage

import (
	"context"
	"crypto/sha256"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"buk-yutnori/internal/auth"
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

func TestLatestRoomEventSequenceDoesNotLoadEventPayloads(t *testing.T) {
	t.Parallel()

	store := openTestStore(t)
	defer store.Close()
	roomID := domain.RoomID("lobby")
	if err := store.AppendRoomEvents(context.Background(), []EventRow{
		{RoomID: roomID, Sequence: 4, EventType: "CHAT_MESSAGE", PayloadJSON: []byte(`{"text":"first"}`)},
		{RoomID: roomID, Sequence: 9, EventType: "CHAT_MESSAGE", PayloadJSON: []byte(`{"text":"last"}`)},
	}); err != nil {
		t.Fatalf("AppendRoomEvents() error = %v", err)
	}

	boundary, err := store.LatestRoomEventSequence(context.Background(), roomID)
	if err != nil || boundary != 9 {
		t.Fatalf("LatestRoomEventSequence() = %d, %v, want 9, nil", boundary, err)
	}
	empty, err := store.LatestRoomEventSequence(context.Background(), domain.RoomID("empty-scope"))
	if err != nil || empty != 0 {
		t.Fatalf("LatestRoomEventSequence(empty) = %d, %v, want 0, nil", empty, err)
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

func TestSQLiteAuthStorePersistsStableUserAndSessionAcrossRestart(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "auth.db")
	store, err := OpenSQLite(path)
	if err != nil {
		t.Fatalf("OpenSQLite() error = %v", err)
	}
	ctx := context.Background()
	created := time.Date(2026, time.August, 29, 12, 0, 0, 0, time.UTC)
	firstDigest := sha256.Sum256([]byte("first-browser-session"))
	secondDigest := sha256.Sum256([]byte("second-browser-session"))
	firstUserID := auth.UserID("usr_EREREREREREREREREREREQ")

	firstUser, err := store.IssueSession(ctx, "google-subject", firstUserID, auth.NewSession{
		Digest: firstDigest, CreatedAt: created, LastUsedAt: created, ExpiresAt: created.Add(auth.SessionLifetime),
	})
	if err != nil {
		t.Fatalf("first IssueSession() error = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	store, err = OpenSQLite(path)
	if err != nil {
		t.Fatalf("reopen SQLite store error = %v", err)
	}
	defer store.Close()
	usedUser, err := store.UseSession(ctx, firstDigest, created.Add(time.Hour))
	if err != nil || usedUser != firstUser {
		t.Fatalf("UseSession() after restart = %+v, %v; want %+v, nil", usedUser, err, firstUser)
	}
	var expiresAtMS, lastUsedAtMS int64
	if err := store.db.QueryRow(`SELECT expires_at_ms, last_used_at_ms FROM sessions WHERE digest = ?`, firstDigest[:]).Scan(&expiresAtMS, &lastUsedAtMS); err != nil {
		t.Fatalf("read persisted session timestamps error = %v", err)
	}
	if expiresAtMS != created.Add(auth.SessionLifetime).UnixMilli() || lastUsedAtMS != created.Add(time.Hour).UnixMilli() {
		t.Fatalf("persisted expiry/last use = %d/%d, want %d/%d", expiresAtMS, lastUsedAtMS, created.Add(auth.SessionLifetime).UnixMilli(), created.Add(time.Hour).UnixMilli())
	}
	secondUser, err := store.IssueSession(ctx, "google-subject", auth.UserID("usr_IiIiIiIiIiIiIiIiIiIiIg"), auth.NewSession{
		Digest: secondDigest, CreatedAt: created, LastUsedAt: created, ExpiresAt: created.Add(auth.SessionLifetime),
	})
	if err != nil {
		t.Fatalf("second IssueSession() error = %v", err)
	}
	if secondUser != firstUser {
		t.Fatalf("same subject users = %+v / %+v, want stable internal user", firstUser, secondUser)
	}

	var digestBytes int
	if err := store.db.QueryRow(`SELECT length(digest) FROM sessions WHERE user_id = ? LIMIT 1`, string(firstUser.ID)).Scan(&digestBytes); err != nil {
		t.Fatalf("read stored digest length error = %v", err)
	}
	if digestBytes != sha256.Size {
		t.Fatalf("stored digest length = %d, want %d", digestBytes, sha256.Size)
	}
}

func TestSQLiteAuthStoreRejectsInvalidExpiredRevokedAndConflictingSessions(t *testing.T) {
	t.Parallel()

	store := openTestStore(t)
	defer store.Close()
	ctx := context.Background()
	now := time.Date(2026, time.August, 29, 12, 0, 0, 0, time.UTC)
	digest := sha256.Sum256([]byte("valid-session"))
	userID := auth.UserID("usr_EREREREREREREREREREREQ")
	session := auth.NewSession{Digest: digest, CreatedAt: now, LastUsedAt: now, ExpiresAt: now.Add(auth.SessionLifetime)}
	if _, err := store.IssueSession(ctx, "google-subject", userID, session); err != nil {
		t.Fatalf("IssueSession() error = %v", err)
	}

	if _, err := store.UseSession(ctx, digest, now.Add(-time.Hour)); !errors.Is(err, auth.ErrInvalidSession) {
		t.Fatalf("UseSession(before created) error = %v, want ErrInvalidSession", err)
	}
	if err := store.RevokeSession(ctx, digest, now.Add(time.Hour)); err != nil {
		t.Fatalf("first RevokeSession() error = %v", err)
	}
	if err := store.RevokeSession(ctx, digest, now.Add(2*time.Hour)); err != nil {
		t.Fatalf("idempotent RevokeSession() error = %v", err)
	}
	if _, err := store.UseSession(ctx, digest, now.Add(2*time.Hour)); !errors.Is(err, auth.ErrUnauthenticated) {
		t.Fatalf("UseSession(revoked) error = %v, want ErrUnauthenticated", err)
	}
	if err := store.RevokeSession(ctx, sha256.Sum256([]byte("missing")), now); !errors.Is(err, auth.ErrUnauthenticated) {
		t.Fatalf("RevokeSession(missing) error = %v, want ErrUnauthenticated", err)
	}

	expiredDigest := sha256.Sum256([]byte("expired-session"))
	if _, err := store.IssueSession(ctx, "other-subject", auth.UserID("usr_IiIiIiIiIiIiIiIiIiIiIg"), auth.NewSession{
		Digest: expiredDigest, CreatedAt: now.Add(-auth.SessionLifetime), LastUsedAt: now.Add(-auth.SessionLifetime), ExpiresAt: now,
	}); err != nil {
		t.Fatalf("IssueSession(expired) error = %v", err)
	}
	if _, err := store.UseSession(ctx, expiredDigest, now); !errors.Is(err, auth.ErrUnauthenticated) {
		t.Fatalf("UseSession(expired) error = %v, want ErrUnauthenticated", err)
	}

	if _, err := store.IssueSession(ctx, "conflicting-subject", auth.UserID("usr_MzMzMzMzMzMzMzMzMzMzMw"), session); !errors.Is(err, auth.ErrSessionConflict) {
		t.Fatalf("IssueSession(duplicate digest) error = %v, want ErrSessionConflict", err)
	}
	var conflictingUsers int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM users WHERE google_subject = 'conflicting-subject'`).Scan(&conflictingUsers); err != nil {
		t.Fatalf("count rolled-back user error = %v", err)
	}
	if conflictingUsers != 0 {
		t.Fatalf("duplicate session leaked %d user rows", conflictingUsers)
	}
	if _, err := store.IssueSession(ctx, "third-subject", userID, auth.NewSession{
		Digest: sha256.Sum256([]byte("user-id-collision")), CreatedAt: now, LastUsedAt: now, ExpiresAt: now.Add(auth.SessionLifetime),
	}); !errors.Is(err, auth.ErrSessionConflict) {
		t.Fatalf("IssueSession(user ID collision) error = %v, want ErrSessionConflict", err)
	}
}

func TestSQLiteAuthSchemaCoexistsWithCanonicalEvents(t *testing.T) {
	t.Parallel()

	store := openTestStore(t)
	defer store.Close()
	ctx := context.Background()
	if err := store.AppendRoomEvents(ctx, []EventRow{{
		RoomID: domain.RoomID("room-auth-event"), Sequence: 1, EventType: "ROOM_UPDATED", PayloadJSON: []byte(`{}`),
	}}); err != nil {
		t.Fatalf("AppendRoomEvents() error = %v", err)
	}
	if _, err := store.IssueSession(ctx, "google-subject", auth.UserID("usr_EREREREREREREREREREREQ"), auth.NewSession{
		Digest: sha256.Sum256([]byte("coexistence")), CreatedAt: time.Unix(0, 0).UTC(), LastUsedAt: time.Unix(0, 0).UTC(), ExpiresAt: time.Unix(1, 0).UTC(),
	}); err != nil {
		t.Fatalf("IssueSession() error = %v", err)
	}
	var eventRows, userRows int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM room_events`).Scan(&eventRows); err != nil {
		t.Fatalf("count event rows error = %v", err)
	}
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&userRows); err != nil {
		t.Fatalf("count user rows error = %v", err)
	}
	if eventRows != 1 || userRows != 1 {
		t.Fatalf("stored rows events=%d users=%d, want 1/1", eventRows, userRows)
	}

	var foreignKeys int
	if err := store.db.QueryRow(`PRAGMA foreign_keys`).Scan(&foreignKeys); err != nil {
		t.Fatalf("read foreign_keys pragma error = %v", err)
	}
	if foreignKeys != 1 {
		t.Fatalf("foreign_keys = %d, want enabled", foreignKeys)
	}
}

func TestOpenSQLiteUpgradesExistingEventDatabaseWithoutLosingEvents(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "v1-events.db")
	store, err := OpenSQLite(path)
	if err != nil {
		t.Fatalf("OpenSQLite() error = %v", err)
	}
	roomID := domain.RoomID("room-schema-upgrade")
	if err := store.AppendRoomEvents(context.Background(), []EventRow{{
		RoomID: roomID, Sequence: 1, EventType: "ROOM_UPDATED", PayloadJSON: []byte(`{"revision":1}`),
	}}); err != nil {
		t.Fatalf("seed legacy event error = %v", err)
	}
	for _, statement := range []string{
		`DROP TABLE sessions`,
		`DROP TABLE users`,
		`PRAGMA user_version = 1`,
	} {
		if _, err := store.db.Exec(statement); err != nil {
			t.Fatalf("prepare legacy schema with %q error = %v", statement, err)
		}
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	store, err = OpenSQLite(path)
	if err != nil {
		t.Fatalf("upgrade OpenSQLite() error = %v", err)
	}
	defer store.Close()
	read, err := store.ReadRoomEventsAfter(context.Background(), roomID, 0)
	if err != nil || len(read) != 1 || read[0].Sequence != 1 {
		t.Fatalf("legacy events after upgrade = %+v, %v; want sequence 1", read, err)
	}
	var version int
	if err := store.db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		t.Fatalf("read upgraded schema version error = %v", err)
	}
	if version != sqliteSchemaVersion {
		t.Fatalf("upgraded schema version = %d, want %d", version, sqliteSchemaVersion)
	}
	if _, err := store.IssueSession(context.Background(), "google-subject", auth.UserID("usr_EREREREREREREREREREREQ"), auth.NewSession{
		Digest: sha256.Sum256([]byte("upgraded-session")), CreatedAt: time.Unix(0, 0).UTC(), LastUsedAt: time.Unix(0, 0).UTC(), ExpiresAt: time.Unix(1, 0).UTC(),
	}); err != nil {
		t.Fatalf("IssueSession() on upgraded schema error = %v", err)
	}
}
