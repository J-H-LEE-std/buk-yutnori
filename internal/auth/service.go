package auth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"sync"
	"time"
)

// Service exchanges verified external identities for server-owned sessions.
type Service struct {
	verifier IdentityVerifier
	store    Store
	random   io.Reader
	randomMu sync.Mutex
	clock    func() time.Time
}

// NewService validates and constructs an authentication service.
func NewService(verifier IdentityVerifier, store Store, random io.Reader, clock func() time.Time) (*Service, error) {
	if verifier == nil || store == nil || random == nil || clock == nil {
		return nil, ErrInvalidConfiguration
	}
	return &Service{verifier: verifier, store: store, random: random, clock: clock}, nil
}

// Login validates a Google credential and creates a new 30-day server session.
func (s *Service) Login(ctx context.Context, credential string) (LoginResult, error) {
	if credential == "" {
		return LoginResult{}, ErrInvalidCredential
	}
	identity, err := s.verifier.Verify(ctx, credential)
	if err != nil {
		return LoginResult{}, fmt.Errorf("verify Google identity: %w", err)
	}
	if err := identity.Subject.Validate(); err != nil {
		return LoginResult{}, ErrInvalidCredential
	}

	userIDBytes, tokenBytes, err := s.generateIdentifiers()
	if err != nil {
		return LoginResult{}, err
	}
	proposedUserID := UserID("usr_" + base64.RawURLEncoding.EncodeToString(userIDBytes))
	rawToken := base64.RawURLEncoding.EncodeToString(tokenBytes)
	digest := sha256.Sum256([]byte(rawToken))
	now := s.clock().UTC()
	expiresAt := now.Add(SessionLifetime)

	user, err := s.store.IssueSession(ctx, identity.Subject, proposedUserID, NewSession{
		Digest:     digest,
		CreatedAt:  now,
		ExpiresAt:  expiresAt,
		LastUsedAt: now,
	})
	if err != nil {
		return LoginResult{}, fmt.Errorf("issue server session: %w", err)
	}
	if err := user.ID.Validate(); err != nil {
		return LoginResult{}, ErrInvalidIdentity
	}
	return LoginResult{User: user, Token: rawToken, ExpiresAt: expiresAt}, nil
}

func (s *Service) generateIdentifiers() ([]byte, []byte, error) {
	s.randomMu.Lock()
	defer s.randomMu.Unlock()

	userIDBytes := make([]byte, userIDRandomBytes)
	if _, err := io.ReadFull(s.random, userIDBytes); err != nil {
		return nil, nil, fmt.Errorf("generate internal user ID: %w", err)
	}
	tokenBytes := make([]byte, sessionTokenRandomBytes)
	if _, err := io.ReadFull(s.random, tokenBytes); err != nil {
		return nil, nil, fmt.Errorf("generate session token: %w", err)
	}
	return userIDBytes, tokenBytes, nil
}

// Authenticate resolves a raw cookie token to an internal user.
func (s *Service) Authenticate(ctx context.Context, rawToken string) (User, error) {
	if rawToken == "" {
		return User{}, ErrUnauthenticated
	}
	digest := sha256.Sum256([]byte(rawToken))
	user, err := s.store.UseSession(ctx, digest, s.clock().UTC())
	if err != nil {
		return User{}, fmt.Errorf("use server session: %w", err)
	}
	if err := user.ID.Validate(); err != nil {
		return User{}, ErrUnauthenticated
	}
	return user, nil
}

// Logout revokes the server session represented by a raw cookie token.
func (s *Service) Logout(ctx context.Context, rawToken string) error {
	if rawToken == "" {
		return ErrUnauthenticated
	}
	digest := sha256.Sum256([]byte(rawToken))
	if err := s.store.RevokeSession(ctx, digest, s.clock().UTC()); err != nil {
		return fmt.Errorf("revoke server session: %w", err)
	}
	return nil
}
