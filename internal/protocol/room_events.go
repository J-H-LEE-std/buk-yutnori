package protocol

import (
	"fmt"
	"time"

	"buk-yutnori/internal/domain"
)

const (
	EventRoomUpdated    = "ROOM_UPDATED"
	EventGameStarting   = "GAME_STARTING"
	RoomStatusLobby     = "lobby"
	RoomStatusStarting  = "starting"
	RoomStatusInMatch   = "in_match"
	RoomStatusPostMatch = "post_match"
)

// RoomUpdatedPayload is the v1 public payload of a room lifecycle signal.
type RoomUpdatedPayload struct {
	Revision uint64 `json:"revision"`
	Status   string `json:"status"`
}

// RoomUpdatedEvent is the typed v1 room-scoped ROOM_UPDATED server event.
type RoomUpdatedEvent struct {
	Version   int                `json:"version"`
	Direction Direction          `json:"direction"`
	Type      string             `json:"type"`
	Sequence  uint64             `json:"sequence"`
	RoomID    domain.RoomID      `json:"room_id"`
	Payload   RoomUpdatedPayload `json:"payload"`
}

// NewRoomUpdatedEvent constructs a validated immutable ROOM_UPDATED event.
// The revision is the room sequence this event consumed.
func NewRoomUpdatedEvent(roomID domain.RoomID, sequence uint64, status string) (RoomUpdatedEvent, error) {
	if err := roomID.Validate(); err != nil {
		return RoomUpdatedEvent{}, fmt.Errorf("%w: room_id: %v", ErrInvalidServerEvent, err)
	}
	if sequence == 0 {
		return RoomUpdatedEvent{}, fmt.Errorf("%w: sequence must start at one", ErrInvalidServerEvent)
	}
	switch status {
	case RoomStatusLobby, RoomStatusStarting, RoomStatusInMatch, RoomStatusPostMatch, "closed":
	default:
		return RoomUpdatedEvent{}, fmt.Errorf("%w: unknown room status %q", ErrInvalidServerEvent, status)
	}
	return RoomUpdatedEvent{
		Version: Version1, Direction: DirectionServerEvent, Type: EventRoomUpdated,
		Sequence: sequence, RoomID: roomID,
		Payload: RoomUpdatedPayload{Revision: sequence, Status: status},
	}, nil
}

// GameStartingPayload is the v1 public payload announcing one start window.
type GameStartingPayload struct {
	ConfirmationDeadlineAt string `json:"confirmation_deadline_at"`
}

// GameStartingEvent is the typed v1 room-scoped GAME_STARTING server event.
type GameStartingEvent struct {
	Version   int                 `json:"version"`
	Direction Direction           `json:"direction"`
	Type      string              `json:"type"`
	Sequence  uint64              `json:"sequence"`
	RoomID    domain.RoomID       `json:"room_id"`
	MatchID   domain.MatchID      `json:"match_id"`
	Payload   GameStartingPayload `json:"payload"`
}

// NewGameStartingEvent constructs a validated immutable GAME_STARTING event.
// The deadline string is display-only; authoritative judgement stays on the
// server monotonic clock (ADR-0003).
func NewGameStartingEvent(roomID domain.RoomID, matchID domain.MatchID, sequence uint64, deadline time.Time) (GameStartingEvent, error) {
	if err := roomID.Validate(); err != nil {
		return GameStartingEvent{}, fmt.Errorf("%w: room_id: %v", ErrInvalidServerEvent, err)
	}
	if err := matchID.Validate(); err != nil {
		return GameStartingEvent{}, fmt.Errorf("%w: match_id: %v", ErrInvalidServerEvent, err)
	}
	if sequence == 0 {
		return GameStartingEvent{}, fmt.Errorf("%w: sequence must start at one", ErrInvalidServerEvent)
	}
	if deadline.IsZero() {
		return GameStartingEvent{}, fmt.Errorf("%w: deadline is required", ErrInvalidServerEvent)
	}
	return GameStartingEvent{
		Version: Version1, Direction: DirectionServerEvent, Type: EventGameStarting,
		Sequence: sequence, RoomID: roomID, MatchID: matchID,
		Payload: GameStartingPayload{ConfirmationDeadlineAt: deadline.UTC().Format(time.RFC3339)},
	}, nil
}
