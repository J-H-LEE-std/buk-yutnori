// SQLite implementation of the canonical event and authentication stores
// (ADR-0001, ADR-0005, ADR-0014): one process-local database, WAL journaling,
// and a single writer connection.

package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"buk-yutnori/internal/auth"
	"buk-yutnori/internal/domain"
	"buk-yutnori/internal/profile"
	_ "modernc.org/sqlite"
)

const sqliteSchemaVersion = 3

// SQLiteEventStore is the canonical room-event and authentication store backed
// by one local SQLite database file. Open one instance per process; the
// application layer already serializes event commits per room.
type SQLiteEventStore struct {
	db *sql.DB
}

var _ auth.Store = (*SQLiteEventStore)(nil)
var _ profile.Store = (*SQLiteEventStore)(nil)

// OpenSQLite opens or creates the database file and applies the canonical
// schema. The returned store enables WAL journaling and full synchronous
// durability because rows must be durable before their in-memory state and
// broadcast are allowed to commit.
func OpenSQLite(path string) (*SQLiteEventStore, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("sqlite path is required")
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite %q: %w", path, err)
	}
	// One connection keeps SQLite in single-writer mode and removes any
	// cross-connection locking concerns; the application serializes commits.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	for _, pragma := range []string{
		"PRAGMA journal_mode = WAL",
		"PRAGMA synchronous = FULL",
		"PRAGMA busy_timeout = 5000",
		"PRAGMA foreign_keys = ON",
	} {
		if _, err := db.Exec(pragma); err != nil {
			db.Close()
			return nil, fmt.Errorf("apply %s: %w", pragma, err)
		}
	}
	const schema = `
CREATE TABLE IF NOT EXISTS room_events (
	room_id       TEXT    NOT NULL,
	sequence      INTEGER NOT NULL,
	event_type    TEXT    NOT NULL,
	payload       TEXT    NOT NULL,
	created_at_ms INTEGER NOT NULL,
	PRIMARY KEY (room_id, sequence)
) WITHOUT ROWID;

CREATE TABLE IF NOT EXISTS users (
	user_id        TEXT NOT NULL PRIMARY KEY,
	google_subject TEXT NOT NULL UNIQUE
) WITHOUT ROWID;

CREATE TABLE IF NOT EXISTS sessions (
	digest          BLOB    NOT NULL PRIMARY KEY,
	user_id         TEXT    NOT NULL,
	created_at_ms   INTEGER NOT NULL,
	expires_at_ms   INTEGER NOT NULL,
	last_used_at_ms INTEGER NOT NULL,
	revoked_at_ms   INTEGER,
	FOREIGN KEY (user_id) REFERENCES users(user_id)
) WITHOUT ROWID;

CREATE TABLE IF NOT EXISTS profiles (
	user_id    TEXT    NOT NULL PRIMARY KEY,
	nickname   TEXT    NOT NULL UNIQUE,
	is_public  INTEGER NOT NULL DEFAULT 0 CHECK (is_public IN (0, 1)),
	wins       INTEGER NOT NULL DEFAULT 0 CHECK (wins >= 0),
	losses     INTEGER NOT NULL DEFAULT 0 CHECK (losses >= 0),
	FOREIGN KEY (user_id) REFERENCES users(user_id)
) WITHOUT ROWID;
`
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("create canonical schema: %w", err)
	}
	if _, err := db.Exec(fmt.Sprintf(
		"PRAGMA user_version = %d", sqliteSchemaVersion,
	)); err != nil {
		db.Close()
		return nil, fmt.Errorf("record schema version: %w", err)
	}
	return &SQLiteEventStore{db: db}, nil
}

// IssueSession atomically resolves a verified Google subject to its stable
// internal user ID and stores only the SHA-256 session digest. It implements
// auth.Store without ever accepting a browser session token.
func (store *SQLiteEventStore) IssueSession(ctx context.Context, subject auth.GoogleSubject, proposedUserID auth.UserID, session auth.NewSession) (auth.User, error) {
	if store == nil || store.db == nil {
		return auth.User{}, errors.New("sqlite event store is closed")
	}
	if err := ctx.Err(); err != nil {
		return auth.User{}, err
	}
	if err := subject.Validate(); err != nil {
		return auth.User{}, err
	}
	if err := proposedUserID.Validate(); err != nil {
		return auth.User{}, auth.ErrInvalidIdentity
	}
	if err := validateAuthNewSession(session); err != nil {
		return auth.User{}, err
	}

	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return auth.User{}, fmt.Errorf("begin auth session issue: %w", err)
	}
	defer tx.Rollback()

	userID, err := storedUserID(ctx, tx, subject)
	switch {
	case err == nil:
	case errors.Is(err, sql.ErrNoRows):
		userID = proposedUserID
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO users (user_id, google_subject) VALUES (?, ?)`,
			string(userID), string(subject),
		); err != nil {
			if isUniqueViolation(err) {
				return auth.User{}, auth.ErrSessionConflict
			}
			return auth.User{}, fmt.Errorf("insert auth user: %w", err)
		}
	default:
		return auth.User{}, fmt.Errorf("resolve auth user: %w", err)
	}
	if err := userID.Validate(); err != nil {
		return auth.User{}, auth.ErrInvalidIdentity
	}

	if _, err := tx.ExecContext(ctx, `INSERT INTO sessions
		(digest, user_id, created_at_ms, expires_at_ms, last_used_at_ms, revoked_at_ms)
		VALUES (?, ?, ?, ?, ?, NULL)`,
		session.Digest[:], string(userID), session.CreatedAt.UTC().UnixMilli(),
		session.ExpiresAt.UTC().UnixMilli(), session.LastUsedAt.UTC().UnixMilli(),
	); err != nil {
		if isUniqueViolation(err) {
			return auth.User{}, auth.ErrSessionConflict
		}
		return auth.User{}, fmt.Errorf("insert auth session: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return auth.User{}, fmt.Errorf("commit auth session issue: %w", err)
	}
	return auth.User{ID: userID}, nil
}

// UseSession resolves an active, unexpired digest and monotonically advances
// last_used_at without extending the absolute expiration deadline.
func (store *SQLiteEventStore) UseSession(ctx context.Context, digest auth.SessionDigest, usedAt time.Time) (auth.User, error) {
	if store == nil || store.db == nil {
		return auth.User{}, errors.New("sqlite event store is closed")
	}
	if err := ctx.Err(); err != nil {
		return auth.User{}, err
	}

	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return auth.User{}, fmt.Errorf("begin auth session use: %w", err)
	}
	defer tx.Rollback()

	var (
		userIDValue  string
		createdAtMS  int64
		expiresAtMS  int64
		lastUsedAtMS int64
		revokedAtMS  sql.NullInt64
	)
	err = tx.QueryRowContext(ctx, `SELECT user_id, created_at_ms, expires_at_ms, last_used_at_ms, revoked_at_ms
		FROM sessions WHERE digest = ?`, digest[:]).Scan(
		&userIDValue, &createdAtMS, &expiresAtMS, &lastUsedAtMS, &revokedAtMS,
	)
	if errors.Is(err, sql.ErrNoRows) || revokedAtMS.Valid {
		return auth.User{}, auth.ErrUnauthenticated
	}
	if err != nil {
		return auth.User{}, fmt.Errorf("read auth session: %w", err)
	}

	usedAtMS := usedAt.UTC().UnixMilli()
	if usedAtMS >= expiresAtMS {
		return auth.User{}, auth.ErrUnauthenticated
	}
	if usedAtMS < createdAtMS {
		return auth.User{}, auth.ErrInvalidSession
	}
	if usedAtMS > lastUsedAtMS {
		if _, err := tx.ExecContext(ctx, `UPDATE sessions SET last_used_at_ms = ? WHERE digest = ?`, usedAtMS, digest[:]); err != nil {
			return auth.User{}, fmt.Errorf("update auth session last use: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return auth.User{}, fmt.Errorf("commit auth session use: %w", err)
	}
	user := auth.User{ID: auth.UserID(userIDValue)}
	if err := user.ID.Validate(); err != nil {
		return auth.User{}, auth.ErrUnauthenticated
	}
	return user, nil
}

// RevokeSession makes an existing digest unusable. Repeated revocation is
// idempotent, matching the authenticated HTTP logout contract.
func (store *SQLiteEventStore) RevokeSession(ctx context.Context, digest auth.SessionDigest, revokedAt time.Time) error {
	if store == nil || store.db == nil {
		return errors.New("sqlite event store is closed")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	result, err := store.db.ExecContext(ctx,
		`UPDATE sessions SET revoked_at_ms = COALESCE(revoked_at_ms, ?) WHERE digest = ?`,
		revokedAt.UTC().UnixMilli(), digest[:],
	)
	if err != nil {
		return fmt.Errorf("revoke auth session: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read auth session revoke result: %w", err)
	}
	if affected == 0 {
		return auth.ErrUnauthenticated
	}
	return nil
}

func storedUserID(ctx context.Context, tx *sql.Tx, subject auth.GoogleSubject) (auth.UserID, error) {
	var userID string
	if err := tx.QueryRowContext(ctx, `SELECT user_id FROM users WHERE google_subject = ?`, string(subject)).Scan(&userID); err != nil {
		return "", err
	}
	return auth.UserID(userID), nil
}

func validateAuthNewSession(session auth.NewSession) error {
	if session.Digest == (auth.SessionDigest{}) || session.CreatedAt.IsZero() || session.ExpiresAt.IsZero() || session.LastUsedAt.IsZero() {
		return auth.ErrInvalidSession
	}
	if !session.ExpiresAt.After(session.CreatedAt) || session.LastUsedAt.Before(session.CreatedAt) || !session.LastUsedAt.Before(session.ExpiresAt) {
		return auth.ErrInvalidSession
	}
	return nil
}

// Save creates or updates the caller-owned durable profile. Match result
// accounting deliberately does not share this path: it is authoritative match
// runtime work with separate transaction and audit requirements.
func (store *SQLiteEventStore) Save(ctx context.Context, value profile.Profile) error {
	if store == nil || store.db == nil {
		return errors.New("sqlite event store is closed")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := value.Validate(); err != nil {
		return err
	}
	public := 0
	if value.Public {
		public = 1
	}
	_, err := store.db.ExecContext(ctx, `INSERT INTO profiles (user_id, nickname, is_public)
		VALUES (?, ?, ?)
		ON CONFLICT(user_id) DO UPDATE SET nickname = excluded.nickname, is_public = excluded.is_public`,
		string(value.UserID), string(value.Nickname), public,
	)
	if err == nil {
		return nil
	}
	if isUniqueViolation(err) {
		return profile.ErrNicknameTaken
	}
	if isForeignKeyViolation(err) {
		return profile.ErrNotFound
	}
	return fmt.Errorf("save profile: %w", err)
}

// Lookup returns one durable profile, including private statistics for the
// HTTP boundary to decide which fields a specific response can expose.
func (store *SQLiteEventStore) Lookup(ctx context.Context, userID auth.UserID) (profile.Profile, error) {
	if store == nil || store.db == nil {
		return profile.Profile{}, errors.New("sqlite event store is closed")
	}
	if err := ctx.Err(); err != nil {
		return profile.Profile{}, err
	}
	if err := userID.Validate(); err != nil {
		return profile.Profile{}, err
	}
	var (
		value    profile.Profile
		public   int
		wins     int64
		losses   int64
		nickname string
	)
	err := store.db.QueryRowContext(ctx,
		`SELECT nickname, is_public, wins, losses FROM profiles WHERE user_id = ?`, string(userID),
	).Scan(&nickname, &public, &wins, &losses)
	if errors.Is(err, sql.ErrNoRows) {
		return profile.Profile{}, profile.ErrNotFound
	}
	if err != nil {
		return profile.Profile{}, fmt.Errorf("lookup profile: %w", err)
	}
	validatedNickname, err := profile.ParseNickname(nickname)
	if err != nil || (public != 0 && public != 1) || wins < 0 || losses < 0 {
		return profile.Profile{}, errors.New("stored profile violates canonical constraints")
	}
	value = profile.Profile{
		UserID: userID, Nickname: validatedNickname, Public: public == 1,
		Wins: uint64(wins), Losses: uint64(losses),
	}
	return value, nil
}

// AppendRoomEvents stores every row in one transaction: readers observe all
// rows or none, and a duplicate (room_id, sequence) aborts the whole batch.
func (store *SQLiteEventStore) AppendRoomEvents(ctx context.Context, rows []EventRow) error {
	if store == nil || store.db == nil {
		return errors.New("sqlite event store is closed")
	}
	if len(rows) == 0 {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin event append: %w", err)
	}
	defer tx.Rollback()

	statement, err := tx.PrepareContext(ctx,
		`INSERT INTO room_events (room_id, sequence, event_type, payload, created_at_ms)
		 VALUES (?, ?, ?, ?, ?)`,
	)
	if err != nil {
		return fmt.Errorf("prepare event insert: %w", err)
	}
	defer statement.Close()

	now := time.Now().UnixMilli()
	for index, row := range rows {
		createdAt := row.CreatedAtMS
		if createdAt == 0 {
			createdAt = now
		}
		if row.RoomID.Validate() != nil || row.Sequence == 0 || len(row.PayloadJSON) == 0 {
			return fmt.Errorf("invalid event row[%d]", index)
		}
		if _, err := statement.ExecContext(
			ctx,
			string(row.RoomID),
			int64(row.Sequence),
			row.EventType,
			string(row.PayloadJSON),
			createdAt,
		); err != nil {
			if isUniqueViolation(err) {
				return fmt.Errorf("%w: %s/%d", ErrDuplicateEvent, row.RoomID, row.Sequence)
			}
			return fmt.Errorf("insert event %s/%d: %w", row.RoomID, row.Sequence, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit event append: %w", err)
	}
	return nil
}

func isUniqueViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}

func isForeignKeyViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "FOREIGN KEY constraint failed")
}

// ReadRoomEventsAfter returns the room's stored events with sequence greater
// than afterSequence in ascending order. The composite primary key serves the
// range scan; no secondary index exists by ADR-0014.
func (store *SQLiteEventStore) ReadRoomEventsAfter(ctx context.Context, roomID domain.RoomID, afterSequence uint64) ([]EventRow, error) {
	if store == nil || store.db == nil {
		return nil, errors.New("sqlite event store is closed")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	rows, err := store.db.QueryContext(ctx,
		`SELECT room_id, sequence, event_type, payload, created_at_ms
		 FROM room_events
		 WHERE room_id = ? AND sequence > ?
		 ORDER BY sequence ASC`,
		string(roomID),
		int64(afterSequence),
	)
	if err != nil {
		return nil, fmt.Errorf("query events after %d: %w", afterSequence, err)
	}
	defer rows.Close()

	events := make([]EventRow, 0, 16)
	for rows.Next() {
		var (
			row         EventRow
			roomIDValue string
			sequence    int64
			payload     string
			createdAt   int64
		)
		if err := rows.Scan(&roomIDValue, &sequence, &row.EventType, &payload, &createdAt); err != nil {
			return nil, fmt.Errorf("scan event row: %w", err)
		}
		row.RoomID = domain.RoomID(roomIDValue)
		row.Sequence = uint64(sequence)
		row.PayloadJSON = []byte(payload)
		row.CreatedAtMS = createdAt
		events = append(events, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate event rows: %w", err)
	}
	return events, nil
}

// LatestRoomEventSequence returns a scope's durable sequence boundary without
// loading its event payloads. It is used by permanent live-only scopes at
// process start to avoid duplicate primary keys after restart.
func (store *SQLiteEventStore) LatestRoomEventSequence(ctx context.Context, roomID domain.RoomID) (uint64, error) {
	if store == nil || store.db == nil {
		return 0, errors.New("sqlite event store is closed")
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if err := roomID.Validate(); err != nil {
		return 0, fmt.Errorf("invalid room_id: %w", err)
	}
	var sequence sql.NullInt64
	if err := store.db.QueryRowContext(ctx,
		`SELECT MAX(sequence) FROM room_events WHERE room_id = ?`, string(roomID),
	).Scan(&sequence); err != nil {
		return 0, fmt.Errorf("read latest event sequence: %w", err)
	}
	if !sequence.Valid {
		return 0, nil
	}
	if sequence.Int64 < 0 {
		return 0, errors.New("stored event sequence is negative")
	}
	return uint64(sequence.Int64), nil
}

// Close releases the underlying database handle.
func (store *SQLiteEventStore) Close() error {
	if store == nil || store.db == nil {
		return nil
	}
	err := store.db.Close()
	store.db = nil
	return err
}
