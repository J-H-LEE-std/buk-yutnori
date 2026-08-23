package application

import (
	"context"
	"errors"
	"testing"
	"time"

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

	registry, err := NewRoomRegistry(time.Now)
	if err != nil {
		t.Fatalf("NewRoomRegistry(time.Now) error = %v", err)
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
	t.Parallel()

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
	t.Parallel()

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
	t.Parallel()

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
	t.Parallel()

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
	t.Parallel()

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
	t.Parallel()

	registry, err := NewRoomRegistry(time.Now)
	if err != nil {
		t.Fatalf("NewRoomRegistry(time.Now) error = %v", err)
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

func TestLobbyExecutorMapsStartFlowRejections(t *testing.T) {
	t.Parallel()

	clock := &manualClock{current: time.Date(2026, 8, 23, 11, 0, 0, 0, time.UTC)}
	registry := newTestRegistryWithClock(t, clock.Now)
	executor, err := NewLobbyCommandExecutor(registry)
	if err != nil {
		t.Fatalf("NewLobbyCommandExecutor() error = %v", err)
	}

	summary, err := registry.Create(CreateRoomInput{
		Creator:  lobbyCreatorID,
		Creation: room.Creation{Title: "매핑 방"},
		Settings: room.DefaultSettings(),
		Team:     domain.TeamA,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if _, err := registry.Join(JoinRoomInput{
		User:   lobbyOutsiderID,
		RoomID: summary.RoomID,
		Role:   RolePlayer,
		Team:   domain.TeamB,
	}); err != nil {
		t.Fatalf("Join(second player) error = %v", err)
	}

	startCommand := func(id string) protocol.ClientCommand {
		return lobbyTestCommand(protocol.CommandStartGame, id, summary.RoomID, protocol.EmptyPayload{})
	}
	assertOutcome := func(t *testing.T, outcome protocol.CommandOutcome, err error, wantCode string, wantRetriable bool) {
		t.Helper()
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		if outcome.Status != protocol.CommandRejected || outcome.Error == nil {
			t.Fatalf("outcome = %+v, want rejected", outcome)
		}
		if outcome.Error.Code != wantCode || outcome.Error.Retriable != wantRetriable {
			t.Fatalf("error = %+v, want code %q retriable %v", outcome.Error, wantCode, wantRetriable)
		}
	}

	outcome, err := executor.Execute(context.Background(), auth.User{ID: lobbyOutsiderID}, startCommand("s1"))
	assertOutcome(t, outcome, err, roomHostRequiredCode, false)

	outcome, err = executor.Execute(context.Background(), auth.User{ID: lobbySpectatorID}, startCommand("s2"))
	assertOutcome(t, outcome, err, roomHostRequiredCode, false)

	if err := registry.SetReady(lobbyOutsiderID, summary.RoomID, true); err != nil {
		t.Fatalf("SetReady(eligibility precondition) error = %v", err)
	}
	if err := registry.SetReady(lobbyCreatorID, summary.RoomID, true); err != nil {
		t.Fatalf("SetReady(host ready precondition) error = %v", err)
	}
	if err := registry.RequestStart(lobbyCreatorID, summary.RoomID); err != nil {
		t.Fatalf("RequestStart() error = %v", err)
	}
	_, _, activeMatchID := readStartState(registry, summary.RoomID)

	outcome, err = executor.Execute(context.Background(), auth.User{ID: lobbyCreatorID}, startCommand("s3"))
	assertOutcome(t, outcome, err, startInProgressCode, false)

	mutation := lobbyTestCommand(protocol.CommandSetReady, "s4", summary.RoomID, protocol.SetReadyPayload{Ready: false})
	outcome, err = executor.Execute(context.Background(), auth.User{ID: lobbyCreatorID}, mutation)
	assertOutcome(t, outcome, err, startInProgressCode, false)

	wrongScope := lobbyTestCommand(protocol.CommandConfirmGameStart, "s5", summary.RoomID, protocol.EmptyPayload{})
	wrongScope.MatchID = &activeMatchID
	wrongScope.Payload = protocol.SelectTeamPayload{TeamID: domain.TeamA}
	if _, err := executor.Execute(context.Background(), auth.User{ID: lobbyCreatorID}, wrongScope); !errors.Is(err, ErrInvalidCommand) {
		t.Fatalf("CONFIRM with wrong payload type error = %v, want ErrInvalidCommand", err)
	}
	wrongScope.Payload = protocol.EmptyPayload{}
	scopeMismatch := activeMatchID + "zz"
	wrongScope.MatchID = &scopeMismatch
	outcome, err = executor.Execute(context.Background(), auth.User{ID: lobbyOutsiderID}, wrongScope)
	assertOutcome(t, outcome, err, matchScopeMismatchCode, false)

	noWindowRoom := lobbyTestCommand(protocol.CommandConfirmGameStart, "s6",
		domain.RoomID("00000000000000000000000000000000"), protocol.EmptyPayload{})
	unknown := domain.MatchID("unknown")
	noWindowRoom.MatchID = &unknown
	outcome, err = executor.Execute(context.Background(), auth.User{ID: lobbyCreatorID}, noWindowRoom)
	assertOutcome(t, outcome, err, "ROOM_NOT_FOUND", true)

	clock.Advance(room.StartConfirmationWindow + time.Second)
	if err := registry.ExpireStartConfirmation(summary.RoomID); err != nil {
		t.Fatalf("deterministic expiry cleanup error = %v", err)
	}

	lateConfirm := lobbyTestCommand(protocol.CommandConfirmGameStart, "s8", summary.RoomID, protocol.EmptyPayload{})
	lateConfirm.MatchID = &activeMatchID
	outcome, err = executor.Execute(context.Background(), auth.User{ID: lobbyOutsiderID}, lateConfirm)
	assertOutcome(t, outcome, err, noActiveConfirmationCode, false)

	unreadyRoom, err := registry.Create(CreateRoomInput{
		Creator:  lobbyCreatorID,
		Creation: room.Creation{Title: "미준비 방"},
		Settings: room.DefaultSettings(),
		Team:     domain.TeamA,
	})
	if err != nil {
		t.Fatalf("Create(unready room) error = %v", err)
	}
	if _, err := registry.Join(JoinRoomInput{
		User:   lobbySpectatorID,
		RoomID: unreadyRoom.RoomID,
		Role:   RolePlayer,
		Team:   domain.TeamB,
	}); err != nil {
		t.Fatalf("Join(unready room player) error = %v", err)
	}
	blocked := lobbyTestCommand(protocol.CommandStartGame, "s9", unreadyRoom.RoomID, protocol.EmptyPayload{})
	outcome, err = executor.Execute(context.Background(), auth.User{ID: lobbyCreatorID}, blocked)
	assertOutcome(t, outcome, err, startConditionsNotMetCode, false)

	if err := registry.SetReady(lobbyCreatorID, unreadyRoom.RoomID, true); err != nil {
		t.Fatalf("SetReady(host) error = %v", err)
	}
	if _, err := registry.Join(JoinRoomInput{
		User:   auth.UserID(startRosterIDs[3]),
		RoomID: unreadyRoom.RoomID,
		Role:   RolePlayer,
		Team:   domain.TeamA,
	}); err != nil {
		t.Fatalf("Join(second player) error = %v", err)
	}
	if err := registry.SetReady(auth.UserID(startRosterIDs[3]), unreadyRoom.RoomID, true); err != nil {
		t.Fatalf("SetReady(second) error = %v", err)
	}
	if _, err := registry.Join(JoinRoomInput{
		User:   auth.UserID(startRosterIDs[4]),
		RoomID: unreadyRoom.RoomID,
		Role:   RolePlayer,
		Team:   domain.TeamB,
	}); err != nil {
		t.Fatalf("Join(third player) error = %v", err)
	}
	if err := registry.SetReady(auth.UserID(startRosterIDs[4]), unreadyRoom.RoomID, true); err != nil {
		t.Fatalf("SetReady(third) error = %v", err)
	}
	if err := registry.SetReady(lobbySpectatorID, unreadyRoom.RoomID, true); err != nil {
		t.Fatalf("SetReady(remaining player) error = %v", err)
	}
	if err := registry.RequestStart(lobbyCreatorID, unreadyRoom.RoomID); err != nil {
		t.Fatalf("RequestStart(eligible) error = %v", err)
	}
	_, _, windowMatch := readStartState(registry, unreadyRoom.RoomID)
	nonRoster := lobbyTestCommand(protocol.CommandConfirmGameStart, "s10", unreadyRoom.RoomID, protocol.EmptyPayload{})
	nonRoster.MatchID = &windowMatch
	outcome, err = executor.Execute(context.Background(), auth.User{ID: lobbyOutsiderID}, nonRoster)
	assertOutcome(t, outcome, err, roomPlayerRequiredCode, false)

	clock.Advance(room.StartConfirmationWindow + time.Second)
	expired := lobbyTestCommand(protocol.CommandConfirmGameStart, "s11", unreadyRoom.RoomID, protocol.EmptyPayload{})
	expired.MatchID = &windowMatch
	outcome, err = executor.Execute(context.Background(), auth.User{ID: lobbySpectatorID}, expired)
	assertOutcome(t, outcome, err, confirmationExpiredCode, false)

	if err := registry.ExpireStartConfirmation(unreadyRoom.RoomID); err != nil {
		t.Fatalf("deterministic expiry cleanup error = %v", err)
	}

	fullRoom, err := registry.Create(CreateRoomInput{
		Creator:  lobbyCreatorID,
		Creation: room.Creation{Title: "전원 확인 방"},
		Settings: room.DefaultSettings(),
		Team:     domain.TeamA,
	})
	if err != nil {
		t.Fatalf("Create(full room) error = %v", err)
	}
	if _, err := registry.Join(JoinRoomInput{
		User:   lobbyOutsiderID,
		RoomID: fullRoom.RoomID,
		Role:   RolePlayer,
		Team:   domain.TeamB,
	}); err != nil {
		t.Fatalf("Join(full room player) error = %v", err)
	}
	for _, user := range []auth.UserID{lobbyCreatorID, lobbyOutsiderID} {
		if err := registry.SetReady(user, fullRoom.RoomID, true); err != nil {
			t.Fatalf("SetReady(%s) error = %v", user, err)
		}
	}
	if err := registry.RequestStart(lobbyCreatorID, fullRoom.RoomID); err != nil {
		t.Fatalf("RequestStart(full room) error = %v", err)
	}
	_, _, fullMatch := readStartState(registry, fullRoom.RoomID)
	for _, user := range []auth.UserID{lobbyCreatorID, lobbyOutsiderID} {
		if err := registry.ConfirmStart(user, fullRoom.RoomID, fullMatch); err != nil {
			t.Fatalf("ConfirmStart(%s) error = %v", user, err)
		}
	}
	afterStarted := lobbyTestCommand(protocol.CommandSetReady, "s12", fullRoom.RoomID, protocol.SetReadyPayload{Ready: true})
	outcome, err = executor.Execute(context.Background(), auth.User{ID: lobbyOutsiderID}, afterStarted)
	assertOutcome(t, outcome, err, matchAlreadyStartedCode, false)
}
