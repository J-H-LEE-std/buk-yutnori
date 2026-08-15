package googleid

import (
	"context"
	"errors"
	"testing"

	"buk-yutnori/internal/auth"
	"cloud.google.com/go/auth/credentials/idtoken"
)

func TestVerifierReturnsOnlySubjectFromValidatedGooglePayload(t *testing.T) {
	t.Parallel()

	validator := &stubTokenValidator{payload: &idtoken.Payload{
		Issuer:   "https://accounts.google.com",
		Audience: "web-client-id.apps.googleusercontent.com",
		Subject:  "google-sub-123",
		Claims: map[string]any{
			"email": "private@example.com",
			"name":  "Private Name",
		},
	}}
	verifier, err := newVerifier("web-client-id.apps.googleusercontent.com", validator)
	if err != nil {
		t.Fatalf("newVerifier() error = %v", err)
	}

	identity, err := verifier.Verify(context.Background(), "signed-id-token")
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if identity.Subject != "google-sub-123" {
		t.Fatalf("identity = %+v", identity)
	}
	if validator.credential != "signed-id-token" || validator.audience != "web-client-id.apps.googleusercontent.com" {
		t.Fatalf("validator args = %q / %q", validator.credential, validator.audience)
	}
}

func TestVerifierAcceptsBothCanonicalGoogleIssuers(t *testing.T) {
	t.Parallel()

	for _, issuer := range []string{"accounts.google.com", "https://accounts.google.com"} {
		issuer := issuer
		t.Run(issuer, func(t *testing.T) {
			t.Parallel()
			verifier, err := newVerifier("audience", &stubTokenValidator{payload: &idtoken.Payload{
				Issuer: issuer, Audience: "audience", Subject: "subject",
			}})
			if err != nil {
				t.Fatalf("newVerifier() error = %v", err)
			}
			if _, err := verifier.Verify(context.Background(), "token"); err != nil {
				t.Fatalf("Verify() error = %v", err)
			}
		})
	}
}

func TestVerifierRejectsInvalidValidationAndClaims(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		payload   *idtoken.Payload
		validator error
	}{
		{name: "validator failure", validator: errors.New("bad signature")},
		{name: "nil payload"},
		{name: "wrong issuer", payload: &idtoken.Payload{Issuer: "https://attacker.example", Audience: "audience", Subject: "subject"}},
		{name: "wrong audience", payload: &idtoken.Payload{Issuer: "accounts.google.com", Audience: "other", Subject: "subject"}},
		{name: "empty subject", payload: &idtoken.Payload{Issuer: "accounts.google.com", Audience: "audience"}},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			verifier, err := newVerifier("audience", &stubTokenValidator{payload: test.payload, err: test.validator})
			if err != nil {
				t.Fatalf("newVerifier() error = %v", err)
			}
			if _, err := verifier.Verify(context.Background(), "token"); !errors.Is(err, auth.ErrInvalidCredential) {
				t.Fatalf("Verify() error = %v, want ErrInvalidCredential", err)
			}
		})
	}
}

func TestNewVerifierRejectsMissingAudienceOrValidator(t *testing.T) {
	t.Parallel()

	if _, err := New(""); !errors.Is(err, auth.ErrInvalidConfiguration) {
		t.Fatalf("New(empty) error = %v", err)
	}
	if _, err := newVerifier("audience", nil); !errors.Is(err, auth.ErrInvalidConfiguration) {
		t.Fatalf("newVerifier(nil) error = %v", err)
	}
}

type stubTokenValidator struct {
	payload    *idtoken.Payload
	err        error
	credential string
	audience   string
}

func (v *stubTokenValidator) Validate(_ context.Context, credential string, audience string) (*idtoken.Payload, error) {
	v.credential = credential
	v.audience = audience
	return v.payload, v.err
}
