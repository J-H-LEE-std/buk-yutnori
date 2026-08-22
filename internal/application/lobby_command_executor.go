// Authoritative executor for pre-match lobby WebSocket commands.

package application

import (
	"context"
	"errors"
	"fmt"

	"buk-yutnori/internal/auth"
	"buk-yutnori/internal/domain/room"
	"buk-yutnori/internal/protocol"
)

const (
	roomPlayerRequiredCode     = "ROOM_PLAYER_REQUIRED"
	readyTeamChangeBlockedCode = "READY_TEAM_CHANGE_BLOCKED"
)

// LobbyCommandExecutor applies SELECT_TEAM and SET_READY to the room
// registry's authoritative lobby state. It never touches match execution.
type LobbyCommandExecutor struct {
	lobbies *RoomRegistry
}

// NewLobbyCommandExecutor constructs the lobby command executor.
func NewLobbyCommandExecutor(lobbies *RoomRegistry) (*LobbyCommandExecutor, error) {
	if lobbies == nil {
		return nil, fmt.Errorf("%w: room registry is required", ErrInvalidConfiguration)
	}
	return &LobbyCommandExecutor{lobbies: lobbies}, nil
}

// Execute implements Executor for the two lobby command types.
func (executor *LobbyCommandExecutor) Execute(ctx context.Context, user auth.User, command protocol.ClientCommand) (protocol.CommandOutcome, error) {
	if executor == nil || executor.lobbies == nil {
		return protocol.CommandOutcome{}, fmt.Errorf("%w: lobby command executor is required", ErrInvalidConfiguration)
	}
	if err := ctx.Err(); err != nil {
		return protocol.CommandOutcome{}, err
	}
	if err := user.ID.Validate(); err != nil {
		return protocol.CommandOutcome{}, fmt.Errorf("%w: authenticated user_id", ErrInvalidCommand)
	}

	switch command.Type {
	case protocol.CommandSelectTeam:
		payload, ok := command.Payload.(protocol.SelectTeamPayload)
		if !ok {
			return protocol.CommandOutcome{}, fmt.Errorf("%w: invalid SELECT_TEAM payload", ErrInvalidCommand)
		}
		if err := executor.lobbies.ChangeTeam(user.ID, command.RoomID, payload.TeamID); err != nil {
			return executor.rejectLobbyError(err), nil
		}
		return acceptedLobbyOutcome(), nil

	case protocol.CommandSetReady:
		payload, ok := command.Payload.(protocol.SetReadyPayload)
		if !ok {
			return protocol.CommandOutcome{}, fmt.Errorf("%w: invalid SET_READY payload", ErrInvalidCommand)
		}
		if err := executor.lobbies.SetReady(user.ID, command.RoomID, payload.Ready); err != nil {
			return executor.rejectLobbyError(err), nil
		}
		return acceptedLobbyOutcome(), nil

	default:
		return protocol.CommandOutcome{}, fmt.Errorf("%w: unsupported lobby command %s", ErrInvalidCommand, command.Type)
	}
}

func (executor *LobbyCommandExecutor) rejectLobbyError(err error) protocol.CommandOutcome {
	switch {
	case errors.Is(err, ErrRoomNotFound):
		return rejectedLobbyOutcome("ROOM_NOT_FOUND", "lobby room not found", true)
	case errors.Is(err, room.ErrPlayerNotFound):
		return rejectedLobbyOutcome(roomPlayerRequiredCode, "방 플레이어만 팀과 준비 상태를 변경할 수 있습니다.", false)
	case errors.Is(err, room.ErrReadyPlayerTeamChange):
		return rejectedLobbyOutcome(readyTeamChangeBlockedCode, "준비 완료 상태에서는 팀을 변경할 수 없습니다.", false)
	default:
		return rejectedLobbyOutcome("INVALID_REQUEST", "요청을 처리할 수 없습니다.", false)
	}
}

func acceptedLobbyOutcome() protocol.CommandOutcome {
	return protocol.CommandOutcome{Status: protocol.CommandAccepted}
}

func rejectedLobbyOutcome(code, message string, retriable bool) protocol.CommandOutcome {
	return protocol.CommandOutcome{
		Status: protocol.CommandRejected,
		Error:  &protocol.CommandError{Code: code, Message: message, Retriable: retriable},
	}
}
