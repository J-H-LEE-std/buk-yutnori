package auth

import (
	"errors"
	"strings"
	"testing"
)

func TestUserIDValidatesCanonicalInternalFormat(t *testing.T) {
	t.Parallel()

	if err := testUserID.Validate(); err != nil {
		t.Fatalf("valid UserID error = %v", err)
	}
	for _, invalid := range []UserID{"", "google-sub", "usr_short", "usr_ERERERERERERERERERERE="} {
		invalid := invalid
		t.Run(string(invalid), func(t *testing.T) {
			t.Parallel()
			if err := invalid.Validate(); !errors.Is(err, ErrInvalidIdentity) {
				t.Fatalf("UserID(%q).Validate() error = %v", invalid, err)
			}
		})
	}
}

func TestGoogleSubjectValidatesStorageBoundary(t *testing.T) {
	t.Parallel()

	if err := GoogleSubject("1234567890").Validate(); err != nil {
		t.Fatalf("valid subject error = %v", err)
	}
	for _, invalid := range []GoogleSubject{"", GoogleSubject(strings.Repeat("a", 256))} {
		if err := invalid.Validate(); !errors.Is(err, ErrInvalidIdentity) {
			t.Fatalf("GoogleSubject length %d error = %v", len(invalid), err)
		}
	}
}
