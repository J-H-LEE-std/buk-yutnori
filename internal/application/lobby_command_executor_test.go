package application

import (
	"context"
	"errors"
	"testing"

	"buk-yutnori/internal/auth"
	"buk-yutnori/internal/domain"
	"buk-yutnori/internal/domain/room"
	"buk-yutnori/internal/protocol"
)

const (
	lobbyCreatorID   = auth.UserID("usr_MzMzMzMzMzMzMzMzMzMzMw")
	lobbyOutsiderID  = auth.UserID("usr_RERERERERERERERERERERA")
	lobbySpectatorID = auth.UserID("usr_VVVVVVVVVVVVVVVVVVVVVQ")
)

func lobbyTestCommand(commandType protocol.CommandType, commandID string, roomID domain.RoomID, payload any) protocol.ClientCommand {
	return protocol.ClientCommand{
		Version:   protocol.Version1,
		Direction: protocol.DirectionClientCommand,
		Type:      commandType,
		CommandID: commandID,
		RoomID:    roomID,
		Payload:   payload,
	}
}

func newLobbyExecutorFixture(t *testing.T) (*RoomRegistry, *LobbyCommandExecutor, domain.RoomID) {
	t.Helper()

	registry, err := NewRoomRegistry()
	if err != nil {
		t.Fatalf("NewRoomRegistry() error = %v", err)
	}
	executor, err := NewLobbyCommandExecutor(registry)
	if err != nil {
		t.Fatalf("NewLobbyCommandExecutor() error = %v", err)
	}

	summary, err := registry.Create(CreateRoomInput{
		Creator:  lobbyCreatorID,
		Creation: room.Creation{Title: "로비"},
		Settings: room.DefaultSettings(),
		Team:     domain.TeamA,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	return registry, executor, summary.RoomID
}

func TestLobbyExecutorAcceptsMemberCommands(t *testing.T) {
	_, executor, roomID := newLobbyExecutorFixture(t)
	user := auth.User{ID: lobbyCreatorID}

	tests := []struct {
		name    string
		command protocol.ClientCommand
	}{
		{
			name: "select team",
			command: lobbyTestCommand(
				protocol.CommandSelectTeam, "cmd-team", roomID,
				protocol.SelectTeamPayload{TeamID: domain.TeamB},
			),
		},
		{
			name: "set ready",
			command: lobbyTestCommand(
				protocol.CommandSetReady, "cmd-ready", roomID,
				protocol.SetReadyPayload{Ready: true},
			),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			outcome, err := executor.Execute(context.Background(), user, tt.command)
			if err != nil {
				t.Fatalf("Execute(%s) error = %v", tt.command.Type, err)
			}
			if outcome.Status != protocol.CommandAccepted {
				t.Fatalf("status = %s error = %+v, want accepted", outcome.Status, outcome.Error)
			}
			if outcome.EventSequenceStart != nil || outcome.EventSequenceEnd != nil {
				t.Fatal("lobby commands consume no event sequence")
			}
		})
	}
}

func TestLobbyExecutorRejectsNonPlayersDeterministically(t *testing.T) {
	registry, executor, roomID := newLobbyExecutorFixture(t)

	if _, err := registry.Join(JoinRoomInput{
		User:   lobbySpectatorID,
		RoomID: roomID,
		Role:   RoleSpectator,
	}); err != nil {
		t.Fatalf("Join(spectator) error = %v", err)
	}

	tests := []struct {
		name     string
		userID   auth.UserID
		command  protocol.ClientCommand
		wantCode string
	}{
		{
			name:     "outsider select team",
			userID:   lobbyOutsiderID,
			command:  lobbyTestCommand(protocol.CommandSelectTeam, "cmd-1", roomID, protocol.SelectTeamPayload{TeamID: domain.TeamA}),
			wantCode: roomPlayerRequiredCode,
		},
		{
			name:     "spectator set ready",
			userID:   lobbySpectatorID,
			command:  lobbyTestCommand(protocol.CommandSetReady, "cmd-2", roomID, protocol.SetReadyPayload{Ready: true}),
			wantCode: roomPlayerRequiredCode,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			outcome, err := executor.Execute(context.Background(), auth.User{ID: tt.userID}, tt.command)
			if err != nil {
				t.Fatalf("Execute() error = %v", err)
			}
			if outcome.Status != protocol.CommandRejected || outcome.Error == nil {
				t.Fatalf("outcome = %+v, want rejected with error", outcome)
			}
			if outcome.Error.Code != tt.wantCode {
				t.Fatalf("code = %q, want %q", outcome.Error.Code, tt.wantCode)
			}
			if outcome.Error.Retriable {
				t.Fatalf("code %q must not be retriable", outcome.Error.Code)
			}
		})
	}
}

func TestLobbyExecutorBlocksReadyTeamChange(t *testing.T) {
	_, executor, roomID := newLobbyExecutorFixture(t)
	user := auth.User{ID: lobbyCreatorID}

	setReady := lobbyTestCommand(protocol.CommandSetReady, "cmd-ready-on", roomID, protocol.SetReadyPayload{Ready: true})
	if _, err := executor.Execute(context.Background(), user, setReady); err != nil {
		t.Fatalf("Execute(SET_READY true) error = %v", err)
	}

	selectTeam := lobbyTestCommand(protocol.CommandSelectTeam, "cmd-team-blocked", roomID, protocol.SelectTeamPayload{TeamID: domain.TeamB})
	outcome, err := executor.Execute(context.Background(), user, selectTeam)
	if err != nil {
		t.Fatalf("Execute(SELECT_TEAM) error = %v", err)
	}
	if outcome.Error == nil || outcome.Error.Code != readyTeamChangeBlockedCode || outcome.Error.Retriable {
		t.Fatalf("outcome error = %+v, want %q not retriable", outcome.Error, readyTeamChangeBlockedCode)
	}
}

func TestLobbyExecutorUnknownRoomIsRetriableNotFound(t *testing.T) {
	_, executor, _ := newLobbyExecutorFixture(t)

	outcome, err := executor.Execute(
		context.Background(), auth.User{ID: lobbyCreatorID},
		lobbyTestCommand(protocol.CommandSetReady, "cmd-unknown",
			domain.RoomID("00000000000000000000000000000000"), protocol.SetReadyPayload{Ready: true}),
	)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if outcome.Error == nil || outcome.Error.Code != "ROOM_NOT_FOUND" || !outcome.Error.Retriable {
		t.Fatalf("outcome error = %+v, want ROOM_NOT_FOUND retriable", outcome.Error)
	}
}

func TestLobbyExecutorRejectsInvalidUsageWithoutOutcome(t *testing.T) {
	_, executor, roomID := newLobbyExecutorFixture(t)
	user := auth.User{ID: lobbyCreatorID}

	tests := []struct {
		name    string
		command func() protocol.ClientCommand
	}{
		{
			name: "unsupported command type",
			command: func() protocol.ClientCommand {
				return lobbyTestCommand(protocol.CommandThrowYut, "cmd-yut", roomID, protocol.EmptyPayload{})
			},
		},
		{
			name: "wrong payload type for select team",
			command: func() protocol.ClientCommand {
				return lobbyTestCommand(protocol.CommandSelectTeam, "cmd-bad-payload", roomID, protocol.SetReadyPayload{Ready: true})
			},
		},
		{
			name: "wrong payload type for set ready",
			command: func() protocol.ClientCommand {
				return lobbyTestCommand(protocol.CommandSetReady, "cmd-bad-payload-2", roomID, protocol.SelectTeamPayload{TeamID: domain.TeamA})
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := executor.Execute(context.Background(), user, tt.command())
			if !errors.Is(err, ErrInvalidCommand) {
				t.Fatalf("Execute() error = %v, want ErrInvalidCommand", err)
			}
		})
	}
}

func TestLobbyExecutorConstructorAndContextGuards(t *testing.T) {
	registry, err := NewRoomRegistry()
	if err != nil {
		t.Fatalf("NewRoomRegistry() error = %v", err)
	}

	if _, err := NewLobbyCommandExecutor(nil); !errors.Is(err, ErrInvalidConfiguration) {
		t.Fatalf("NewLobbyCommandExecutor(nil) error = %v", err)
	}
	_, executor, roomID := newLobbyExecutorFixture(t)

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	command := lobbyTestCommand(protocol.CommandSetReady, "cmd-canceled", roomID, protocol.SetReadyPayload{Ready: true})
	if _, err := executor.Execute(canceled, auth.User{ID: lobbyCreatorID}, command); !errors.Is(err, context.Canceled) {
		t.Fatalf("Execute(canceled) error = %v, want context.Canceled", err)
	}
	_ = registry
}
