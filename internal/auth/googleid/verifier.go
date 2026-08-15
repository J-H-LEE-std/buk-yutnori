// Package googleid adapts Google's ID token verifier to the auth identity
// boundary.
package googleid

import (
	"context"
	"fmt"

	"buk-yutnori/internal/auth"
	"cloud.google.com/go/auth/credentials/idtoken"
)

const (
	issuerAccountsGoogle    = "accounts.google.com"
	issuerAccountsGoogleURL = "https://accounts.google.com"
)

type tokenValidator interface {
	Validate(ctx context.Context, credential string, audience string) (*idtoken.Payload, error)
}

// Verifier validates a Google-signed ID token for one configured web client
// audience and returns only its stable subject.
type Verifier struct {
	audience  string
	validator tokenValidator
}

// New constructs a Google verifier using Google's cached JWK validator.
func New(audience string) (*Verifier, error) {
	validator, err := idtoken.NewValidator(nil)
	if err != nil {
		return nil, fmt.Errorf("create Google ID token validator: %w", err)
	}
	return newVerifier(audience, validator)
}

func newVerifier(audience string, validator tokenValidator) (*Verifier, error) {
	if audience == "" || validator == nil {
		return nil, auth.ErrInvalidConfiguration
	}
	return &Verifier{audience: audience, validator: validator}, nil
}

// Verify checks signature, expiry and audience through Google's validator,
// then independently restricts issuer, audience and subject claims.
func (v *Verifier) Verify(ctx context.Context, credential string) (auth.GoogleIdentity, error) {
	if credential == "" {
		return auth.GoogleIdentity{}, auth.ErrInvalidCredential
	}
	payload, err := v.validator.Validate(ctx, credential, v.audience)
	if err != nil {
		return auth.GoogleIdentity{}, fmt.Errorf("%w: Google token validation failed", auth.ErrInvalidCredential)
	}
	if payload == nil || payload.Audience != v.audience || payload.Subject == "" {
		return auth.GoogleIdentity{}, auth.ErrInvalidCredential
	}
	if payload.Issuer != issuerAccountsGoogle && payload.Issuer != issuerAccountsGoogleURL {
		return auth.GoogleIdentity{}, auth.ErrInvalidCredential
	}
	return auth.GoogleIdentity{Subject: auth.GoogleSubject(payload.Subject)}, nil
}
