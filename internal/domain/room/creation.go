// Room creation metadata contract from docs/05 방 생성.

package room

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/rivo/uniseg"
)

// Canonical room creation constraints recorded in spec/room_settings.yaml.
const (
	// MinTitleGraphemes is the smallest allowed room title length.
	MinTitleGraphemes = 1
	// MaxTitleGraphemes is the largest allowed room title length.
	MaxTitleGraphemes = 25
	// RoomPasswordMinLength is the shortest allowed entry password.
	RoomPasswordMinLength = 4
	// RoomPasswordMaxLength is the longest allowed entry password.
	RoomPasswordMaxLength = 16
	// RoomPasswordPattern is the canonical entry password regular expression.
	RoomPasswordPattern = `^[0-9a-zA-Z]{4,16}$`
)

var compiledRoomPasswordPattern = regexp.MustCompile(RoomPasswordPattern)

// ErrInvalidCreation identifies a creator-supplied room field outside the
// canonical contract.
var ErrInvalidCreation = errors.New("invalid room creation")

// CreationError reports every invalid room creation field found in one pass.
type CreationError struct {
	Problems []string
}

// Error implements error.
func (e *CreationError) Error() string {
	return fmt.Sprintf("%v: %s", ErrInvalidCreation, strings.Join(e.Problems, "; "))
}

// Unwrap exposes the sentinel error for errors.Is checks.
func (e *CreationError) Unwrap() error {
	return ErrInvalidCreation
}

// Creation contains the creator-supplied room identification fields.
// An empty Password means the room requires no entry password.
type Creation struct {
	Title    string
	Password string
}

// Validate reports every canonical creation rule the fields violate in one pass.
func (creation Creation) Validate() error {
	var problems []string

	titleGraphemes := uniseg.GraphemeClusterCount(creation.Title)
	if titleGraphemes < MinTitleGraphemes || titleGraphemes > MaxTitleGraphemes {
		problems = append(problems, fmt.Sprintf(
			"title must be %d to %d grapheme clusters, got %d",
			MinTitleGraphemes,
			MaxTitleGraphemes,
			titleGraphemes,
		))
	}

	if creation.Password != "" && !compiledRoomPasswordPattern.MatchString(creation.Password) {
		problems = append(problems, fmt.Sprintf(
			"password must fully match %s when set",
			RoomPasswordPattern,
		))
	}

	if len(problems) > 0 {
		return &CreationError{Problems: problems}
	}
	return nil
}
