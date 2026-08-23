// Authoritative executor for live-match WebSocket commands (issue #82).

package application

import (
	"context"
	"errors"
	"fmt"

	"buk-yutnori/internal/auth"
	"buk-yutnori/internal/protocol"
)

const (
	matchNotActiveCode    = "MATCH_NOT_ACTIVE"
	notMemberCode         = "ROOM_NOT_MEMBER"
	notTurnPlayerCode     = "NOT_YOUR_TURN"
	invalidTurnActionCode = "INVALID_TURN_ACTION"
)

// MatchCommandExecutor applies THROW_YUT, SELECT_RESULT, SELECT_PIECE,
// SELECT_ROUTE, and RECONNECT to the registry-owned canonical match runtime.
type MatchCommandExecutor struct {
	lobbies *RoomRegistry
}

// NewMatchCommandExecutor constructs the match command executor.
func NewMatchCommandExecutor(lobbies *RoomRegistry) (*MatchCommandExecutor, error) {
	if lobbies == nil {
		return nil, fmt.Errorf("%w: room registry is required", ErrInvalidConfiguration)
	}
	return &MatchCommandExecutor{lobbies: lobbies}, nil
}

// Execute implements Executor.
func (executor *MatchCommandExecutor) Execute(ctx context.Context, user auth.User, command protocol.ClientCommand) (protocol.CommandOutcome, error) {
	if executor == nil || executor.lobbies == nil {
		return protocol.CommandOutcome{}, fmt.Errorf("%w: match command executor is required", ErrInvalidConfiguration)
	}
	if err := ctx.Err(); err != nil {
		return protocol.CommandOutcome{}, err
	}
	if err := user.ID.Validate(); err != nil {
		return protocol.CommandOutcome{}, fmt.Errorf("%w: authenticated user_id", ErrInvalidCommand)
	}
	if command.MatchID == nil {
		return protocol.CommandOutcome{}, fmt.Errorf("%w: %s requires match_id", ErrInvalidCommand, command.Type)
	}

	switch command.Type {
	case protocol.CommandThrowYut:
		if _, ok := command.Payload.(protocol.EmptyPayload); !ok {
			return protocol.CommandOutcome{}, fmt.Errorf("%w: invalid THROW_YUT payload", ErrInvalidCommand)
		}
		if err := executor.lobbies.ThrowYut(user.ID, command.RoomID, *command.MatchID); err != nil {
			return executor.rejectMatchError(err), nil
		}
		return acceptedLobbyOutcome(), nil

	case protocol.CommandSelectResult:
		payload, ok := command.Payload.(protocol.SelectResultPayload)
		if !ok {
			return protocol.CommandOutcome{}, fmt.Errorf("%w: invalid SELECT_RESULT payload", ErrInvalidCommand)
		}
		if err := executor.lobbies.SelectResult(user.ID, command.RoomID, *command.MatchID, payload.TokenID); err != nil {
			return executor.rejectMatchError(err), nil
		}
		return acceptedLobbyOutcome(), nil

	case protocol.CommandSelectPiece:
		payload, ok := command.Payload.(protocol.SelectPiecePayload)
		if !ok {
			return protocol.CommandOutcome{}, fmt.Errorf("%w: invalid SELECT_PIECE payload", ErrInvalidCommand)
		}
		if err := executor.lobbies.SelectPiece(user.ID, command.RoomID, *command.MatchID, payload.TokenID, payload.PieceID); err != nil {
			return executor.rejectMatchError(err), nil
		}
		return acceptedLobbyOutcome(), nil

	case protocol.CommandSelectRoute:
		payload, ok := command.Payload.(protocol.SelectRoutePayload)
		if !ok {
			return protocol.CommandOutcome{}, fmt.Errorf("%w: invalid SELECT_ROUTE payload", ErrInvalidCommand)
		}
		if err := executor.lobbies.SelectRoute(user.ID, command.RoomID, *command.MatchID, payload.TokenID, payload.PieceID, payload.Route); err != nil {
			return executor.rejectMatchError(err), nil
		}
		return acceptedLobbyOutcome(), nil

	case protocol.CommandReconnect:
		payload, ok := command.Payload.(protocol.ReconnectPayload)
		if !ok {
			return protocol.CommandOutcome{}, fmt.Errorf("%w: invalid RECONNECT payload", ErrInvalidCommand)
		}
		snapshot, err := executor.lobbies.ReconnectSnapshot(user.ID, command.RoomID, *command.MatchID, payload.LastSequence)
		if err != nil {
			return executor.rejectReconnectError(err), nil
		}
		synchronization, err := protocol.NewReconnectSynchronization(command, snapshot, nil)
		if err != nil {
			return protocol.CommandOutcome{}, fmt.Errorf("build reconnect synchronization: %w", err)
		}
		return protocol.CommandOutcome{
			Status:          protocol.CommandAccepted,
			Synchronization: &synchronization,
		}, nil

	default:
		return protocol.CommandOutcome{}, fmt.Errorf("%w: unsupported match command %s", ErrInvalidCommand, command.Type)
	}
}

func (executor *MatchCommandExecutor) rejectMatchError(err error) protocol.CommandOutcome {
	switch {
	case errors.Is(err, ErrRoomNotFound):
		return rejectedLobbyOutcome("ROOM_NOT_FOUND", "lobby room not found", true)
	case errors.Is(err, ErrNotMember):
		return rejectedLobbyOutcome(notMemberCode, "방 멤버만 이 명령을 보낼 수 있습니다.", false)
	case errors.Is(err, ErrNotTurnPlayer):
		return rejectedLobbyOutcome(notTurnPlayerCode, "지금은 상대 차례입니다.", false)
	case errors.Is(err, ErrInvalidTurnAction):
		return rejectedLobbyOutcome(invalidTurnActionCode, "현재 단계에서 수행할 수 없는 행동입니다.", false)
	case errors.Is(err, ErrMatchScopeMismatch):
		return rejectedLobbyOutcome(matchScopeMismatchCode, "경기 스코프가 일치하지 않습니다.", false)
	case errors.Is(err, ErrMatchNotActive):
		return rejectedLobbyOutcome(matchNotActiveCode, "진행 중인 경기가 없습니다.", true)
	case errors.Is(err, auth.ErrUnauthenticated):
		return rejectedLobbyOutcome("INVALID_REQUEST", "요청을 처리할 수 없습니다.", false)
	default:
		return rejectedLobbyOutcome("INVALID_REQUEST", "요청을 처리할 수 없습니다.", false)
	}
}

// rejectReconnectError follows ADR-0009: any scope or staleness condition
// that prevents building a contiguous bundle for the exact room_id+match_id
// is a transient RESYNC_REQUIRED so the client resynchronizes from zero.
func (executor *MatchCommandExecutor) rejectReconnectError(err error) protocol.CommandOutcome {
	switch {
	case errors.Is(err, ErrRoomNotFound):
		return rejectedLobbyOutcome("ROOM_NOT_FOUND", "lobby room not found", true)
	case errors.Is(err, ErrNotMember):
		return rejectedLobbyOutcome(notMemberCode, "방 멤버만 이 명령을 보낼 수 있습니다.", false)
	case errors.Is(err, ErrMatchScopeMismatch),
		errors.Is(err, ErrMatchNotActive),
		errors.Is(err, ErrClientSequenceAhead):
		return rejectedLobbyOutcome(protocol.ErrorCodeResyncRequired, "재동기화가 필요합니다.", true)
	case errors.Is(err, auth.ErrUnauthenticated):
		return rejectedLobbyOutcome("INVALID_REQUEST", "요청을 처리할 수 없습니다.", false)
	default:
		return rejectedLobbyOutcome("INVALID_REQUEST", "요청을 처리할 수 없습니다.", false)
	}
}
