package profile

import (
	"errors"
	"strings"
	"testing"
)

func TestParseNicknameAcceptsUnicodeGraphemeRange(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{
		"가나",
		"e\u0301x",
		strings.Repeat("가", 20),
	} {
		got, err := ParseNickname(raw)
		if err != nil {
			t.Fatalf("ParseNickname(%q) error = %v", raw, err)
		}
		if got != Nickname(raw) {
			t.Fatalf("ParseNickname(%q) = %q", raw, got)
		}
	}
}

func TestParseNicknameRejectsInvalidValues(t *testing.T) {
	t.Parallel()

	for name, raw := range map[string]string{
		"one grapheme":        "가",
		"too many graphemes":  strings.Repeat("가", 21),
		"leading whitespace":  " 가나",
		"trailing whitespace": "가나 ",
		"control character":   "가\n나",
		"invalid utf8":        string([]byte{0xff, 'a'}),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := ParseNickname(raw); !errors.Is(err, ErrInvalidNickname) {
				t.Fatalf("ParseNickname(%q) error = %v, want ErrInvalidNickname", raw, err)
			}
		})
	}
}
