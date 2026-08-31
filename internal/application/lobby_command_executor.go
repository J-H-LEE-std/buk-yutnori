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
	startConditionsNotMetCode  = "START_CONDITIONS_NOT_MET"
	startInProgressCode        = "START_CONFIRMATION_IN_PROGRESS"
	matchAlreadyStartedCode    = "MATCH_ALREADY_STARTED"
	noActiveConfirmationCode   = "NO_ACTIVE_START_CONFIRMATION"
	confirmationExpiredCode    = "START_CONFIRMATION_EXPIRED"
	matchScopeMismatchCode     = "MATCH_SCOPE_MISMATCH"
	roomHostRequiredCode       = "ROOM_HOST_REQUIRED"
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

	case protocol.CommandAddCPUPlayer:
		payload, ok := command.Payload.(protocol.AddCPUPlayerPayload)
		if !ok {
			return protocol.CommandOutcome{}, fmt.Errorf("%w: invalid ADD_CPU_PLAYER payload", ErrInvalidCommand)
		}
		if _, err := executor.lobbies.AddCPUPlayer(user.ID, command.RoomID, payload.TeamID); err != nil {
			return executor.rejectLobbyError(err), nil
		}
		return acceptedLobbyOutcome(), nil

	case protocol.CommandRemoveCPUPlayer:
		payload, ok := command.Payload.(protocol.RemoveCPUPlayerPayload)
		if !ok {
			return protocol.CommandOutcome{}, fmt.Errorf("%w: invalid REMOVE_CPU_PLAYER payload", ErrInvalidCommand)
		}
		if err := executor.lobbies.RemoveCPUPlayer(user.ID, command.RoomID, payload.PlayerID); err != nil {
			return executor.rejectLobbyError(err), nil
		}
		return acceptedLobbyOutcome(), nil

	case protocol.CommandStartGame:
		if _, ok := command.Payload.(protocol.EmptyPayload); !ok {
			return protocol.CommandOutcome{}, fmt.Errorf("%w: invalid START_GAME payload", ErrInvalidCommand)
		}
		if err := executor.lobbies.RequestStart(user.ID, command.RoomID); err != nil {
			return executor.rejectStartError(err), nil
		}
		return acceptedLobbyOutcome(), nil

	case protocol.CommandConfirmGameStart:
		if _, ok := command.Payload.(protocol.EmptyPayload); !ok {
			return protocol.CommandOutcome{}, fmt.Errorf("%w: invalid CONFIRM_GAME_START payload", ErrInvalidCommand)
		}
		if command.MatchID == nil {
			return protocol.CommandOutcome{}, fmt.Errorf("%w: CONFIRM_GAME_START requires match_id", ErrInvalidCommand)
		}
		err := executor.lobbies.ConfirmStart(user.ID, command.RoomID, *command.MatchID)
		if err != nil {
			return executor.rejectConfirmError(err), nil
		}
		return acceptedLobbyOutcome(), nil

	default:
		return protocol.CommandOutcome{}, fmt.Errorf("%w: unsupported lobby command %s", ErrInvalidCommand, command.Type)
	}
}

func (executor *LobbyCommandExecutor) rejectLobbyError(err error) protocol.CommandOutcome {
	switch {
	case errors.Is(err, ErrEventStoreUnavailable):
		return rejectedLobbyOutcome("EVENT_STORE_UNAVAILABLE", "일시적 저장 장애입니다. 잠시 후 다시 시도하세요.", true)
	case errors.Is(err, ErrRoomNotFound):
		return rejectedLobbyOutcome("ROOM_NOT_FOUND", "lobby room not found", true)
	case errors.Is(err, room.ErrPlayerNotFound):
		return rejectedLobbyOutcome(roomPlayerRequiredCode, "방 플레이어만 팀과 준비 상태를 변경할 수 있습니다.", false)
	case errors.Is(err, ErrNotRoomHost):
		return rejectedLobbyOutcome(roomHostRequiredCode, "방장만 CPU 플레이어를 변경할 수 있습니다.", false)
	case errors.Is(err, room.ErrReadyPlayerTeamChange):
		return rejectedLobbyOutcome(readyTeamChangeBlockedCode, "준비 완료 상태에서는 팀을 변경할 수 없습니다.", false)
	case errors.Is(err, ErrStartAlreadyRequested):
		return rejectedLobbyOutcome(startInProgressCode, "시작 확인이 진행 중입니다.", false)
	case errors.Is(err, ErrRoomAlreadyStarted):
		return rejectedLobbyOutcome(matchAlreadyStartedCode, "경기가 이미 시작되었습니다.", false)
	default:
		return rejectedLobbyOutcome("INVALID_REQUEST", "요청을 처리할 수 없습니다.", false)
	}
}

func (executor *LobbyCommandExecutor) rejectStartError(err error) protocol.CommandOutcome {
	switch {
	case errors.Is(err, ErrEventStoreUnavailable):
		return rejectedLobbyOutcome("EVENT_STORE_UNAVAILABLE", "일시적 저장 장애입니다. 잠시 후 다시 시도하세요.", true)
	case errors.Is(err, ErrRoomNotFound):
		return rejectedLobbyOutcome("ROOM_NOT_FOUND", "lobby room not found", true)
	case errors.Is(err, ErrNotRoomHost):
		return rejectedLobbyOutcome(roomHostRequiredCode, "방장만 경기 시작을 요청할 수 있습니다.", false)
	case errors.Is(err, ErrStartAlreadyRequested):
		return rejectedLobbyOutcome(startInProgressCode, "시작 확인이 진행 중입니다.", false)
	case errors.Is(err, ErrRoomAlreadyStarted):
		return rejectedLobbyOutcome(matchAlreadyStartedCode, "경기가 이미 시작되었습니다.", false)
	case errors.Is(err, room.ErrStartNotEnoughPlayers),
		errors.Is(err, room.ErrStartTeamsUnbalanced),
		errors.Is(err, room.ErrStartPlayersNotReady):
		return rejectedLobbyOutcome(startConditionsNotMetCode, "아직 경기를 시작할 수 없습니다. 인원과 준비 상태를 확인하세요.", false)
	default:
		return rejectedLobbyOutcome("INVALID_REQUEST", "요청을 처리할 수 없습니다.", false)
	}
}

func (executor *LobbyCommandExecutor) rejectConfirmError(err error) protocol.CommandOutcome {
	switch {
	case errors.Is(err, ErrEventStoreUnavailable):
		return rejectedLobbyOutcome("EVENT_STORE_UNAVAILABLE", "일시적 저장 장애입니다. 잠시 후 다시 시도하세요.", true)
	case errors.Is(err, ErrRoomNotFound):
		return rejectedLobbyOutcome("ROOM_NOT_FOUND", "lobby room not found", true)
	case errors.Is(err, ErrNoActiveStartConfirmation):
		return rejectedLobbyOutcome(noActiveConfirmationCode, "진행 중인 시작 확인이 없습니다.", false)
	case errors.Is(err, ErrMatchScopeMismatch):
		return rejectedLobbyOutcome(matchScopeMismatchCode, "시작 확인 스코프가 일치하지 않습니다.", false)
	case errors.Is(err, room.ErrStartConfirmationExpired):
		return rejectedLobbyOutcome(confirmationExpiredCode, "시작 확인 마감이 지났습니다.", false)
	case errors.Is(err, room.ErrStartConfirmationPlayerNotFound),
		errors.Is(err, room.ErrPlayerNotFound):
		return rejectedLobbyOutcome(roomPlayerRequiredCode, "확인 대상 플레이어가 아닙니다.", false)
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
