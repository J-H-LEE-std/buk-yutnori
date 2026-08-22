package room

import (
	"errors"
	"strings"
	"testing"
)

func TestValidCreation(t *testing.T) {
	tests := []struct {
		name     string
		creation Creation
	}{
		{
			name:     "one grapheme composed hangul title",
			creation: Creation{Title: "가"},
		},
		{
			name:     "one grapheme decomposed hangul title",
			creation: Creation{Title: "\u1100\u1161"},
		},
		{
			name:     "twenty five grapheme title",
			creation: Creation{Title: strings.Repeat("가", MaxTitleGraphemes)},
		},
		{
			name:     "composed hangul counts one per visible syllable",
			creation: Creation{Title: "한글제목"},
		},
		{
			name:     "decomposed jamo counts one per visible syllable",
			creation: Creation{Title: "\u1112\u1161\u11ab\u1100\u1173\u11af"},
		},
		{
			name:     "latin digit title at limit",
			creation: Creation{Title: strings.Repeat("a", MaxTitleGraphemes)},
		},
		{
			name:     "no password",
			creation: Creation{Title: "방 제목"},
		},
		{
			name:     "four character alphanumeric password",
			creation: Creation{Title: "방 제목", Password: "a1B2"},
		},
		{
			name:     "sixteen character alphanumeric password",
			creation: Creation{Title: "방 제목", Password: "abcdefghij012345"},
		},
		{
			name:     "mixed case digit password",
			creation: Creation{Title: "방 제목", Password: "0123456789AbCdEf"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.creation.Validate(); err != nil {
				t.Fatalf("Validate(%+v) error = %v, want nil", tt.creation, err)
			}
		})
	}
}

func TestRejectsInvalidTitle(t *testing.T) {
	tests := []struct {
		name  string
		title string
	}{
		{
			name:  "empty title",
			title: "",
		},
		{
			name:  "twenty six grapheme title",
			title: strings.Repeat("가", MaxTitleGraphemes+1),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			creation := Creation{Title: tt.title}

			err := creation.Validate()
			if err == nil {
				t.Fatalf("Validate(title=%q) error = nil, want invalid creation error", tt.title)
			}
			if !errors.Is(err, ErrInvalidCreation) {
				t.Fatalf("Validate(title=%q) error = %v, want ErrInvalidCreation", tt.title, err)
			}
			assertMentionsField(t, err, "title")
		})
	}
}

func TestRejectsInvalidPassword(t *testing.T) {
	tests := []struct {
		name     string
		password string
	}{
		{
			name:     "three characters",
			password: "ab1",
		},
		{
			name:     "seventeen characters",
			password: strings.Repeat("a", RoomPasswordMaxLength+1),
		},
		{
			name:     "hangul characters",
			password: "비밀1234",
		},
		{
			name:     "symbol characters",
			password: "pass!123",
		},
		{
			name:     "space characters",
			password: "pass 123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			creation := Creation{Title: "방 제목", Password: tt.password}

			err := creation.Validate()
			if err == nil {
				t.Fatalf("Validate(password=%q) error = nil, want invalid creation error", tt.password)
			}
			if !errors.Is(err, ErrInvalidCreation) {
				t.Fatalf(
					"Validate(password=%q) error = %v, want ErrInvalidCreation",
					tt.password,
					err,
				)
			}
			assertMentionsField(t, err, "password")
		})
	}
}

func TestCreationErrorListsAllProblems(t *testing.T) {
	creation := Creation{}

	err := creation.Validate()
	if err == nil {
		t.Fatal("Validate(empty) error = nil, want invalid creation error")
	}

	var creationErr *CreationError
	if !errors.As(err, &creationErr) {
		t.Fatalf("Validate(empty) error = %T, want *CreationError", err)
	}
	if len(creationErr.Problems) != 1 {
		t.Fatalf("Problems = %v, want exactly the title problem", creationErr.Problems)
	}
	assertMentionsField(t, err, "title")
}

func TestRoomPasswordPatternMatchesSpec(t *testing.T) {
	if RoomPasswordPattern != `^[0-9a-zA-Z]{4,16}$` {
		t.Fatalf("RoomPasswordPattern = %q, want canonical docs/05 pattern", RoomPasswordPattern)
	}
}

func assertMentionsField(t *testing.T, err error, field string) {
	t.Helper()

	var creationErr *CreationError
	if !errors.As(err, &creationErr) {
		t.Fatalf("error = %v, want *CreationError", err)
	}
	for _, problem := range creationErr.Problems {
		if strings.Contains(problem, field) {
			return
		}
	}
	t.Fatalf("Problems = %v, want a problem mentioning %q", creationErr.Problems, field)
}
