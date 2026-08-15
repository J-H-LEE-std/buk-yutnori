package auth

import (
	"context"
	"crypto/sha256"
	"errors"
	"testing"
	"time"
)

func TestMemoryStoreReusesInternalUserAndTracksSessionLifecycle(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := NewMemoryStore()
	created := time.Date(2026, time.August, 15, 12, 0, 0, 0, time.UTC)
	firstDigest := sha256.Sum256([]byte("first-session"))
	secondDigest := sha256.Sum256([]byte("second-session"))

	firstUser, err := store.IssueSession(ctx, "google-sub", testUserID, NewSession{
		Digest: firstDigest, CreatedAt: created, LastUsedAt: created, ExpiresAt: created.Add(SessionLifetime),
	})
	if err != nil {
		t.Fatalf("first IssueSession() error = %v", err)
	}
	secondUser, err := store.IssueSession(ctx, "google-sub", "usr_IiIiIiIiIiIiIiIiIiIiIg", NewSession{
		Digest: secondDigest, CreatedAt: created, LastUsedAt: created, ExpiresAt: created.Add(SessionLifetime),
	})
	if err != nil {
		t.Fatalf("second IssueSession() error = %v", err)
	}
	if firstUser.ID != testUserID || secondUser.ID != firstUser.ID {
		t.Fatalf("users = %+v / %+v", firstUser, secondUser)
	}

	usedAt := created.Add(time.Hour)
	usedUser, err := store.UseSession(ctx, secondDigest, usedAt)
	if err != nil || usedUser != firstUser {
		t.Fatalf("UseSession() = %+v, %v", usedUser, err)
	}
	if err := store.RevokeSession(ctx, secondDigest, usedAt.Add(time.Minute)); err != nil {
		t.Fatalf("RevokeSession() error = %v", err)
	}
	if _, err := store.UseSession(ctx, secondDigest, usedAt.Add(2*time.Minute)); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("UseSession() after revoke error = %v, want ErrUnauthenticated", err)
	}
}

func TestMemoryStoreRejectsExpiredUnknownAndMalformedSessions(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := NewMemoryStore()
	now := time.Date(2026, time.August, 15, 12, 0, 0, 0, time.UTC)
	digest := sha256.Sum256([]byte("session"))

	if _, err := store.IssueSession(ctx, "", testUserID, validNewSession(digest, now)); !errors.Is(err, ErrInvalidIdentity) {
		t.Fatalf("empty subject error = %v", err)
	}
	if _, err := store.IssueSession(ctx, "sub", "", validNewSession(digest, now)); !errors.Is(err, ErrInvalidIdentity) {
		t.Fatalf("empty user ID error = %v", err)
	}
	malformed := validNewSession(digest, now)
	malformed.ExpiresAt = now
	if _, err := store.IssueSession(ctx, "sub", testUserID, malformed); !errors.Is(err, ErrInvalidSession) {
		t.Fatalf("malformed session error = %v", err)
	}

	if _, err := store.UseSession(ctx, digest, now); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("unknown session error = %v", err)
	}
	if err := store.RevokeSession(ctx, digest, now); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("unknown revoke error = %v", err)
	}

	expiredDigest := sha256.Sum256([]byte("expired"))
	if _, err := store.IssueSession(ctx, "sub", testUserID, NewSession{
		Digest: expiredDigest, CreatedAt: now.Add(-SessionLifetime), LastUsedAt: now.Add(-SessionLifetime), ExpiresAt: now,
	}); err != nil {
		t.Fatalf("IssueSession(expired boundary) error = %v", err)
	}
	if _, err := store.UseSession(ctx, expiredDigest, now); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("expired session error = %v", err)
	}
}

func validNewSession(digest SessionDigest, now time.Time) NewSession {
	return NewSession{Digest: digest, CreatedAt: now, LastUsedAt: now, ExpiresAt: now.Add(SessionLifetime)}
}
