package protocol

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"buk-yutnori/internal/auth"
	"buk-yutnori/internal/domain"
)

func TestNewChatMessageEventBuildsCanonicalServerEvent(t *testing.T) {
	t.Parallel()

	event, err := NewChatMessageEvent(
		domain.RoomID("lobby"),
		7,
		auth.UserID("usr_EREREREREREREREREREREQ"),
		"안녕하세요 👋",
		time.Date(2026, 8, 16, 12, 34, 56, 123000000, time.FixedZone("KST", 9*60*60)),
	)
	if err != nil {
		t.Fatalf("NewChatMessageEvent() error = %v", err)
	}
	encoded, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	want := `{"version":1,"direction":"server_event","type":"CHAT_MESSAGE","sequence":7,"room_id":"lobby","payload":{"message_id":"chat-7","sender_user_id":"usr_EREREREREREREREREREREQ","text":"안녕하세요 👋","sent_at":"2026-08-16T03:34:56.123Z"}}`
	if string(encoded) != want {
		t.Fatalf("encoded event = %s\nwant = %s", encoded, want)
	}
}

func TestNewChatMessageEventRejectsInvalidValues(t *testing.T) {
	t.Parallel()

	validRoom := domain.RoomID("lobby")
	validUser := auth.UserID("usr_EREREREREREREREREREREQ")
	validTime := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name     string
		roomID   domain.RoomID
		sequence uint64
		userID   auth.UserID
		text     string
		sentAt   time.Time
	}{
		{name: "empty room", sequence: 1, userID: validUser, text: "hello", sentAt: validTime},
		{name: "zero sequence", roomID: validRoom, userID: validUser, text: "hello", sentAt: validTime},
		{name: "invalid user", roomID: validRoom, sequence: 1, text: "hello", sentAt: validTime},
		{name: "empty text", roomID: validRoom, sequence: 1, userID: validUser, sentAt: validTime},
		{name: "too long", roomID: validRoom, sequence: 1, userID: validUser, text: strings.Repeat("가", 201), sentAt: validTime},
		{name: "zero time", roomID: validRoom, sequence: 1, userID: validUser, text: "hello"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := NewChatMessageEvent(test.roomID, test.sequence, test.userID, test.text, test.sentAt); !errors.Is(err, ErrInvalidServerEvent) {
				t.Fatalf("NewChatMessageEvent() error = %v, want %v", err, ErrInvalidServerEvent)
			}
		})
	}
}
