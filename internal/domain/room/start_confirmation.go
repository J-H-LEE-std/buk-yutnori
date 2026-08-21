package room

import (
	"errors"
	"fmt"
	"sort"
	"time"

	"buk-yutnori/internal/domain"
)

// StartConfirmationWindow is the canonical server-owned response window.
const StartConfirmationWindow = 10 * time.Second

// StartConfirmationStatus identifies the terminal progress of one start attempt.
type StartConfirmationStatus string

const (
	StartConfirmationPending   StartConfirmationStatus = "pending"
	StartConfirmationConfirmed StartConfirmationStatus = "confirmed"
	StartConfirmationFailed    StartConfirmationStatus = "failed"
)

var (
	// ErrInvalidStartConfirmation reports a missing or unusable attempt value.
	ErrInvalidStartConfirmation = errors.New("invalid start confirmation")

	// ErrStartConfirmationPlayerNotFound rejects a response outside the captured roster.
	ErrStartConfirmationPlayerNotFound = errors.New("player is not part of start confirmation")

	// ErrStartConfirmationExpired rejects responses received at or after the deadline.
	ErrStartConfirmationExpired = errors.New("start confirmation deadline has expired")

	// ErrStartConfirmationNotExpired rejects a timeout transition before its deadline.
	ErrStartConfirmationNotExpired = errors.New("start confirmation deadline has not expired")

	// ErrStartConfirmationClosed rejects changes after success or timeout failure.
	ErrStartConfirmationClosed = errors.New("start confirmation is closed")
)

// StartConfirmationSnapshot is a value copy suitable for application decisions.
type StartConfirmationSnapshot struct {
	MatchID          domain.MatchID
	DeadlineAt       time.Time
	Status           StartConfirmationStatus
	PendingPlayerIDs []domain.PlayerID
}

// StartConfirmation contains one immutable roster's confirmation progress.
// Its future room actor owns serialization; this type does not synchronize callers.
type StartConfirmation struct {
	matchID    domain.MatchID
	deadlineAt time.Time
	status     StartConfirmationStatus
	confirmed  map[domain.PlayerID]bool
}

// NewStartConfirmation captures an eligible lobby roster and fixes its deadline.
func NewStartConfirmation(lobby *Lobby, matchID domain.MatchID, startedAt time.Time) (*StartConfirmation, error) {
	if lobby == nil {
		return nil, ErrInvalidStartConfirmation
	}
	if err := matchID.Validate(); err != nil {
		return nil, fmt.Errorf("match_id: %w", err)
	}
	if err := lobby.ValidateStart(); err != nil {
		return nil, err
	}

	confirmed := make(map[domain.PlayerID]bool, len(lobby.players))
	for id := range lobby.players {
		confirmed[id] = false
	}
	return &StartConfirmation{
		matchID:    matchID,
		deadlineAt: startedAt.Add(StartConfirmationWindow),
		status:     StartConfirmationPending,
		confirmed:  confirmed,
	}, nil
}

// Snapshot returns the attempt state without exposing its mutable roster map.
func (confirmation *StartConfirmation) Snapshot() StartConfirmationSnapshot {
	if confirmation == nil {
		return StartConfirmationSnapshot{}
	}
	return StartConfirmationSnapshot{
		MatchID:          confirmation.matchID,
		DeadlineAt:       confirmation.deadlineAt,
		Status:           confirmation.status,
		PendingPlayerIDs: confirmation.pendingPlayerIDs(),
	}
}

// Confirm records one captured player's response when received before the
// deadline. The returned boolean is true only when this attempt is confirmed.
func (confirmation *StartConfirmation) Confirm(id domain.PlayerID, receivedAt time.Time) (bool, error) {
	if confirmation == nil || confirmation.confirmed == nil {
		return false, ErrInvalidStartConfirmation
	}
	if err := id.Validate(); err != nil {
		return false, err
	}
	if confirmation.status == StartConfirmationFailed {
		return false, ErrStartConfirmationExpired
	}
	if confirmation.status != StartConfirmationPending {
		return false, ErrStartConfirmationClosed
	}
	if !receivedAt.Before(confirmation.deadlineAt) {
		return false, ErrStartConfirmationExpired
	}
	confirmed, exists := confirmation.confirmed[id]
	if !exists {
		return false, fmt.Errorf("%w: %s", ErrStartConfirmationPlayerNotFound, id)
	}
	if confirmed {
		return false, nil
	}

	confirmation.confirmed[id] = true
	if len(confirmation.pendingPlayerIDs()) != 0 {
		return false, nil
	}
	confirmation.status = StartConfirmationConfirmed
	return true, nil
}

// Expire applies the canonical failed-start lobby transition at or after the
// deadline. The attempt becomes failed only after the lobby transition succeeds.
func (confirmation *StartConfirmation) Expire(lobby *Lobby, now time.Time) ([]domain.PlayerID, error) {
	if confirmation == nil || confirmation.confirmed == nil || lobby == nil {
		return nil, ErrInvalidStartConfirmation
	}
	if confirmation.status != StartConfirmationPending {
		return nil, ErrStartConfirmationClosed
	}
	if now.Before(confirmation.deadlineAt) {
		return nil, ErrStartConfirmationNotExpired
	}

	nonresponders := confirmation.pendingPlayerIDs()
	if err := lobby.FailStartConfirmation(nonresponders); err != nil {
		return nil, err
	}
	confirmation.status = StartConfirmationFailed
	return append([]domain.PlayerID(nil), nonresponders...), nil
}

func (confirmation *StartConfirmation) pendingPlayerIDs() []domain.PlayerID {
	pending := make([]domain.PlayerID, 0, len(confirmation.confirmed))
	for id, confirmed := range confirmation.confirmed {
		if !confirmed {
			pending = append(pending, id)
		}
	}
	sort.Slice(pending, func(left, right int) bool {
		return pending[left] < pending[right]
	})
	return pending
}
