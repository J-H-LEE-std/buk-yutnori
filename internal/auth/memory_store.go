package auth

import (
	"context"
	"sync"
	"time"
)

// MemoryStore is a concurrency-safe prototype and test adapter. Production
// persistence is supplied through the same Store boundary.
type MemoryStore struct {
	mu               sync.Mutex
	usersBySubject   map[GoogleSubject]User
	subjectsByUserID map[UserID]GoogleSubject
	sessions         map[SessionDigest]*memorySession
}

type memorySession struct {
	userID     UserID
	createdAt  time.Time
	expiresAt  time.Time
	lastUsedAt time.Time
	revokedAt  *time.Time
}

// NewMemoryStore constructs an empty in-memory auth store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		usersBySubject:   make(map[GoogleSubject]User),
		subjectsByUserID: make(map[UserID]GoogleSubject),
		sessions:         make(map[SessionDigest]*memorySession),
	}
}

// IssueSession atomically resolves or creates an internal user and records the
// hashed session.
func (s *MemoryStore) IssueSession(ctx context.Context, subject GoogleSubject, proposedUserID UserID, session NewSession) (User, error) {
	if err := ctx.Err(); err != nil {
		return User{}, err
	}
	if err := subject.Validate(); err != nil {
		return User{}, err
	}
	if err := proposedUserID.Validate(); err != nil {
		return User{}, ErrInvalidIdentity
	}
	if err := validateNewSession(session); err != nil {
		return User{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.sessions[session.Digest]; exists {
		return User{}, ErrSessionConflict
	}
	user, exists := s.usersBySubject[subject]
	if !exists {
		if previousSubject, collision := s.subjectsByUserID[proposedUserID]; collision && previousSubject != subject {
			return User{}, ErrSessionConflict
		}
		user = User{ID: proposedUserID}
		s.usersBySubject[subject] = user
		s.subjectsByUserID[user.ID] = subject
	}
	s.sessions[session.Digest] = &memorySession{
		userID:     user.ID,
		createdAt:  session.CreatedAt,
		expiresAt:  session.ExpiresAt,
		lastUsedAt: session.LastUsedAt,
	}
	return user, nil
}

// UseSession authenticates an active session and advances its last-used time.
func (s *MemoryStore) UseSession(ctx context.Context, digest SessionDigest, usedAt time.Time) (User, error) {
	if err := ctx.Err(); err != nil {
		return User{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	session, exists := s.sessions[digest]
	if !exists || session.revokedAt != nil || !usedAt.Before(session.expiresAt) {
		return User{}, ErrUnauthenticated
	}
	if usedAt.Before(session.createdAt) {
		return User{}, ErrInvalidSession
	}
	if usedAt.After(session.lastUsedAt) {
		session.lastUsedAt = usedAt
	}
	return User{ID: session.userID}, nil
}

// RevokeSession marks a session unusable. Repeated revocation is idempotent.
func (s *MemoryStore) RevokeSession(ctx context.Context, digest SessionDigest, revokedAt time.Time) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	session, exists := s.sessions[digest]
	if !exists {
		return ErrUnauthenticated
	}
	if session.revokedAt != nil {
		return nil
	}
	when := revokedAt
	session.revokedAt = &when
	return nil
}

func validateNewSession(session NewSession) error {
	if session.Digest == (SessionDigest{}) || session.CreatedAt.IsZero() || session.ExpiresAt.IsZero() || session.LastUsedAt.IsZero() {
		return ErrInvalidSession
	}
	if !session.ExpiresAt.After(session.CreatedAt) || session.LastUsedAt.Before(session.CreatedAt) || !session.LastUsedAt.Before(session.ExpiresAt) {
		return ErrInvalidSession
	}
	return nil
}
