// SQLite implementation of the canonical event store (ADR-0001, ADR-0014):
// one process-local database, WAL journaling, a single writer connection,
// and exactly one table keyed by (room_id, sequence) with no secondary
// indexes.

package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"buk-yutnori/internal/domain"
	_ "modernc.org/sqlite"
)

const sqliteSchemaVersion = 1

// SQLiteEventStore is the canonical room event store backed by one local
// SQLite database file. Open one instance per process; the application layer
// already serializes event commits per room.
type SQLiteEventStore struct {
	db *sql.DB
}

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

// Close releases the underlying database handle.
func (store *SQLiteEventStore) Close() error {
	if store == nil || store.db == nil {
		return nil
	}
	err := store.db.Close()
	store.db = nil
	return err
}
