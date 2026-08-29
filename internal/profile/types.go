// Package profile owns nickname validation and profile persistence contracts.
package profile

import (
	"context"
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"

	"buk-yutnori/internal/auth"

	"github.com/rivo/uniseg"
)

const (
	// MinNicknameGraphemes and MaxNicknameGraphemes are the canonical v1
	// nickname boundary. They count user-perceived Unicode grapheme clusters.
	MinNicknameGraphemes = 2
	MaxNicknameGraphemes = 20
)

var (
	ErrInvalidNickname = errors.New("invalid nickname")
	ErrNotFound        = errors.New("profile not found")
	ErrNicknameTaken   = errors.New("nickname already in use")
)

// Nickname is a validated v1 display name.
type Nickname string

// ParseNickname validates the canonical nickname policy. Exact stored UTF-8
// bytes are used for uniqueness; Unicode case folding is deliberately not a
// hidden policy because it has not been specified for v1.
func ParseNickname(value string) (Nickname, error) {
	if !utf8.ValidString(value) || value != strings.TrimSpace(value) {
		return "", ErrInvalidNickname
	}
	for _, runeValue := range value {
		if unicode.IsControl(runeValue) {
			return "", ErrInvalidNickname
		}
	}
	count := uniseg.GraphemeClusterCount(value)
	if count < MinNicknameGraphemes || count > MaxNicknameGraphemes {
		return "", ErrInvalidNickname
	}
	return Nickname(value), nil
}

// Profile contains the durable profile fields. Wins and losses are initialized
// to zero here; authoritative match-finalization updates are a later scope.
type Profile struct {
	UserID   auth.UserID
	Nickname Nickname
	Public   bool
	Wins     uint64
	Losses   uint64
}

// Validate guards every storage implementation against malformed profile
// records, including callers that do not originate at the HTTP boundary.
func (value Profile) Validate() error {
	if err := value.UserID.Validate(); err != nil {
		return err
	}
	parsed, err := ParseNickname(string(value.Nickname))
	if err != nil || parsed != value.Nickname {
		return ErrInvalidNickname
	}
	return nil
}

// Store isolates profile persistence from HTTP and future database choices.
type Store interface {
	Save(ctx context.Context, value Profile) error
	Lookup(ctx context.Context, userID auth.UserID) (Profile, error)
}
