// Package auth owns external identity exchange and server session lifecycle.
package auth

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"time"
)

const (
	// SessionLifetime is the canonical browser login duration.
	SessionLifetime = 30 * 24 * time.Hour

	userIDRandomBytes       = 16
	sessionTokenRandomBytes = 32
)

var (
	ErrInvalidConfiguration = errors.New("invalid auth configuration")
	ErrInvalidCredential    = errors.New("invalid Google credential")
	ErrInvalidIdentity      = errors.New("invalid auth identity")
	ErrInvalidSession       = errors.New("invalid session")
	ErrSessionConflict      = errors.New("session or user identifier conflict")
	ErrUnauthenticated      = errors.New("unauthenticated")
)

// UserID is an internal account identifier. It is never a Google subject or a
// browser session identifier.
type UserID string

// Validate reports whether id contains the canonical prefixed 128-bit value.
func (id UserID) Validate() error {
	encoded, ok := strings.CutPrefix(string(id), "usr_")
	if !ok {
		return ErrInvalidIdentity
	}
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(encoded)
	if err != nil || len(decoded) != userIDRandomBytes || base64.RawURLEncoding.EncodeToString(decoded) != encoded {
		return ErrInvalidIdentity
	}
	return nil
}

// GoogleSubject is Google's stable account identifier from the verified sub
// claim.
type GoogleSubject string

// Validate reports whether the verified Google subject is storable.
func (subject GoogleSubject) Validate() error {
	if subject == "" || len(subject) > 255 {
		return ErrInvalidIdentity
	}
	return nil
}

// SessionDigest is the only representation of a session token accepted by a
// Store. The browser's raw token stays above this boundary.
type SessionDigest [32]byte

// GoogleIdentity is the minimal identity extracted from a verified ID token.
type GoogleIdentity struct {
	Subject GoogleSubject
}

// User is the authenticated internal account.
type User struct {
	ID UserID `json:"user_id"`
}

// NewSession contains the hashed and timestamped session data persisted by a
// Store. It intentionally has no raw token field.
type NewSession struct {
	Digest     SessionDigest
	CreatedAt  time.Time
	ExpiresAt  time.Time
	LastUsedAt time.Time
}

// LoginResult contains the raw token only long enough for the HTTP layer to
// place it in an HttpOnly cookie.
type LoginResult struct {
	User      User
	Token     string
	ExpiresAt time.Time
}

// IdentityVerifier validates an external credential before it reaches account
// or session storage.
type IdentityVerifier interface {
	Verify(ctx context.Context, credential string) (GoogleIdentity, error)
}

// Store defines the transaction boundary for resolving a Google account and
// issuing, using, or revoking a server session.
type Store interface {
	IssueSession(ctx context.Context, subject GoogleSubject, proposedUserID UserID, session NewSession) (User, error)
	UseSession(ctx context.Context, digest SessionDigest, usedAt time.Time) (User, error)
	RevokeSession(ctx context.Context, digest SessionDigest, revokedAt time.Time) error
}
