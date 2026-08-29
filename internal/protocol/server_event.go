package protocol

import (
	"errors"
	"fmt"
	"time"
	"unicode/utf8"

	"buk-yutnori/internal/auth"
	"buk-yutnori/internal/domain"
)

const (
	DirectionServerEvent Direction = "server_event"
	EventChatMessage               = "CHAT_MESSAGE"
)

var ErrInvalidServerEvent = errors.New("invalid server event")

// ChatMessagePayload is the v1 public payload for a chat event. The current
// production scope is the authenticated `lobby` channel; the event keeps only
// the stable internal sender ID until profile nicknames exist.
type ChatMessagePayload struct {
	MessageID    string      `json:"message_id"`
	SenderUserID auth.UserID `json:"sender_user_id"`
	Text         string      `json:"text"`
	SentAt       string      `json:"sent_at"`
}

// ChatMessageEvent is the typed v1 room_id-scoped CHAT_MESSAGE server event.
type ChatMessageEvent struct {
	Version   int                `json:"version"`
	Direction Direction          `json:"direction"`
	Type      string             `json:"type"`
	Sequence  uint64             `json:"sequence"`
	RoomID    domain.RoomID      `json:"room_id"`
	Payload   ChatMessagePayload `json:"payload"`
}

// NewChatMessageEvent constructs a validated immutable chat event.
func NewChatMessageEvent(roomID domain.RoomID, sequence uint64, senderUserID auth.UserID, text string, sentAt time.Time) (ChatMessageEvent, error) {
	if err := roomID.Validate(); err != nil {
		return ChatMessageEvent{}, fmt.Errorf("%w: room_id: %v", ErrInvalidServerEvent, err)
	}
	if sequence == 0 {
		return ChatMessageEvent{}, fmt.Errorf("%w: sequence must start at one", ErrInvalidServerEvent)
	}
	if err := senderUserID.Validate(); err != nil {
		return ChatMessageEvent{}, fmt.Errorf("%w: sender_user_id: %v", ErrInvalidServerEvent, err)
	}
	if text == "" || utf8.RuneCountInString(text) > MaxChatCodePoints {
		return ChatMessageEvent{}, fmt.Errorf("%w: text must contain 1-%d code points", ErrInvalidServerEvent, MaxChatCodePoints)
	}
	if sentAt.IsZero() {
		return ChatMessageEvent{}, fmt.Errorf("%w: sent_at is required", ErrInvalidServerEvent)
	}
	return ChatMessageEvent{
		Version:   Version1,
		Direction: DirectionServerEvent,
		Type:      EventChatMessage,
		Sequence:  sequence,
		RoomID:    roomID,
		Payload: ChatMessagePayload{
			MessageID:    fmt.Sprintf("chat-%d", sequence),
			SenderUserID: senderUserID,
			Text:         text,
			SentAt:       sentAt.UTC().Format(time.RFC3339Nano),
		},
	}, nil
}
