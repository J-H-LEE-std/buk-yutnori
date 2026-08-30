// Package storage owns the durable canonical event store (ADR-0001,
// ADR-0014). It stores one row per committed room-scoped server event and
// serves ordered replay reads; it never interprets payloads.
package storage

import (
	"context"
	"errors"

	"buk-yutnori/internal/auth"
	"buk-yutnori/internal/domain"
)

// ErrDuplicateEvent identifies an append that would overwrite an existing
// (room_id, sequence) row. Callers must treat it as a programming or
// serialization-boundary violation rather than a retryable condition.
var ErrDuplicateEvent = errors.New("duplicate room event")

// EventRow is one canonical room event. PayloadJSON carries the complete
// wire-form server event exactly as it was broadcast, so replay readers can
// emit stored bytes verbatim.
type EventRow struct {
	RoomID      domain.RoomID
	Sequence    uint64
	EventType   string
	PayloadJSON []byte
	CreatedAtMS int64
}

// EventStore persists canonical room events and reads them back in sequence
// order. Implementations must be safe for concurrent use; the application
// layer serializes writers per room anyway.
type EventStore interface {
	// AppendRoomEvents stores rows atomically: either every row becomes
	// readable or none does. Duplicate (room_id, sequence) rows abort the
	// batch with ErrDuplicateEvent.
	AppendRoomEvents(ctx context.Context, rows []EventRow) error
	// ReadRoomEventsAfter returns every stored event for roomID with
	// sequence greater than afterSequence, ordered by ascending sequence.
	ReadRoomEventsAfter(ctx context.Context, roomID domain.RoomID, afterSequence uint64) ([]EventRow, error)
}

// MatchResult applies one finished match's outcome to its player roster.
// Winners and losers are the authoritative starting roster, not the current
// connection/presence set. CPU-only sides are omitted, so one side may be
// empty; a human user can appear in exactly one side.
type MatchResult struct {
	Winners []auth.UserID
	Losers  []auth.UserID
}

// Validate rejects malformed or overlapping result rosters before a durable
// finalization transaction begins.
func (result MatchResult) Validate() error {
	if len(result.Winners) == 0 && len(result.Losers) == 0 {
		return errors.New("match result requires at least one human player")
	}
	seen := make(map[auth.UserID]struct{}, len(result.Winners)+len(result.Losers))
	for _, side := range [][]auth.UserID{result.Winners, result.Losers} {
		for _, userID := range side {
			if err := userID.Validate(); err != nil {
				return err
			}
			if _, duplicate := seen[userID]; duplicate {
				return errors.New("match result contains duplicate user")
			}
			seen[userID] = struct{}{}
		}
	}
	return nil
}

// MatchResultStore atomically persists terminal match events and their
// corresponding user statistics. The terminal event must never become
// durable without its wins/losses, or vice versa.
type MatchResultStore interface {
	EventStore
	AppendMatchFinalization(ctx context.Context, rows []EventRow, result MatchResult) error
}

// FormatPayloadCopy returns an independent copy of the payload bytes.
func FormatPayloadCopy(payload []byte) []byte {
	return append([]byte(nil), payload...)
}
