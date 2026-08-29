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
	want := `{"version":1,"direction":"server_event","type":"CHAT_MESSAGE","sequence":7,"room_id":"lobby","payload":{"message_id":"chat-7","sender_user_id":"usr_EREREREREREREREREREREQ","sender_nickname":"usr_EREREREREREREREREREREQ","text":"안녕하세요 👋","sent_at":"2026-08-16T03:34:56.123Z"}}`
	if string(encoded) != want {
		t.Fatalf("encoded event = %s\nwant = %s", encoded, want)
	}
}

func TestNewChatMessageEventWithNicknamePreservesStableIdentity(t *testing.T) {
	t.Parallel()

	event, err := NewChatMessageEventWithNickname(
		domain.RoomID("lobby"), 1, auth.UserID("usr_EREREREREREREREREREREQ"), "가나다", "반가워요", time.Unix(0, 0).UTC(),
	)
	if err != nil {
		t.Fatalf("NewChatMessageEventWithNickname() error = %v", err)
	}
	if event.Payload.SenderUserID != "usr_EREREREREREREREREREREQ" || event.Payload.SenderNickname != "가나다" {
		t.Fatalf("payload identity/display = %+v", event.Payload)
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
		nickname string
		text     string
		sentAt   time.Time
	}{
		{name: "empty room", sequence: 1, userID: validUser, nickname: "사용자", text: "hello", sentAt: validTime},
		{name: "zero sequence", roomID: validRoom, userID: validUser, nickname: "사용자", text: "hello", sentAt: validTime},
		{name: "invalid user", roomID: validRoom, sequence: 1, nickname: "사용자", text: "hello", sentAt: validTime},
		{name: "empty nickname", roomID: validRoom, sequence: 1, userID: validUser, text: "hello", sentAt: validTime},
		{name: "empty text", roomID: validRoom, sequence: 1, userID: validUser, nickname: "사용자", sentAt: validTime},
		{name: "too long", roomID: validRoom, sequence: 1, userID: validUser, nickname: "사용자", text: strings.Repeat("가", 201), sentAt: validTime},
		{name: "zero time", roomID: validRoom, sequence: 1, userID: validUser, nickname: "사용자", text: "hello"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := NewChatMessageEventWithNickname(test.roomID, test.sequence, test.userID, test.nickname, test.text, test.sentAt); !errors.Is(err, ErrInvalidServerEvent) {
				t.Fatalf("NewChatMessageEvent() error = %v, want %v", err, ErrInvalidServerEvent)
			}
		})
	}
}
