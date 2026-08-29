package application

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"buk-yutnori/internal/auth"
	"buk-yutnori/internal/domain"
	"buk-yutnori/internal/domain/room"
	"buk-yutnori/internal/protocol"
)

// The lobby chat scope is not a registry room,
// so RECONNECT against it is a retriable ROOM_NOT_FOUND that never consumes
// a sequence and is not retained as an idempotent result.
func TestRealtimeApplicationRejectsLobbyScopeReconnect(t *testing.T) {
	t.Parallel()
	application := mustRealtimeApplication(t, time.Now)
	defer closeRealtimeApplication(t, application)

	command := reconnectCommand("cmd-reconnect-retired", 0)
	result, err := application.Processor().Process(context.Background(), auth.User{ID: chatTestUserID}, command)
	if err != nil {
		t.Fatalf("reconnect Process() error = %v", err)
	}
	assertReconnectRejection(t, result, "ROOM_NOT_FOUND", true)

	ahead := reconnectCommand("cmd-reconnect-retired-ahead", 99)
	result, err = application.Processor().Process(context.Background(), auth.User{ID: chatTestUserID}, ahead)
	if err != nil {
		t.Fatalf("ahead reconnect Process() error = %v", err)
	}
	assertReconnectRejection(t, result, "ROOM_NOT_FOUND", true)

	retry := reconnectCommand("cmd-reconnect-retired", 0)
	result, err = application.Processor().Process(context.Background(), auth.User{ID: chatTestUserID}, retry)
	if err != nil {
		t.Fatalf("retried reconnect Process() error = %v", err)
	}
	assertReconnectRejection(t, result, "ROOM_NOT_FOUND", true)
	assertBoundary(t, application.sequences, LobbyChatRoomID, 0)
}

func TestRealtimeApplicationLobbyChatSequencesStartAtOne(t *testing.T) {
	t.Parallel()
	application := mustRealtimeApplication(t, time.Now)
	defer closeRealtimeApplication(t, application)

	chatResult, err := application.Processor().Process(
		context.Background(),
		auth.User{ID: chatTestUserID},
		chatCommand("cmd-chat-first", "first message"),
	)
	if err != nil {
		t.Fatalf("chat Process() error = %v", err)
	}
	if chatResult.Payload.EventSequenceStart == nil || *chatResult.Payload.EventSequenceStart != 1 {
		t.Fatalf("chat result = %+v, want sequence 1 without bootstrap event", chatResult)
	}
	assertBoundary(t, application.sequences, LobbyChatRoomID, 1)
}

func TestRealtimeApplicationRoutesUnknownRoomMatchCommandToRegistry(t *testing.T) {
	t.Parallel()
	application := mustRealtimeApplication(t, time.Now)
	defer closeRealtimeApplication(t, application)

	matchID := domain.MatchID("any-match")
	command := protocol.ClientCommand{
		Version: protocol.Version1, Direction: protocol.DirectionClientCommand,
		Type: protocol.CommandThrowYut, CommandID: "cmd-throw-unknown-room",
		RoomID: "other-room", MatchID: &matchID,
		Payload: protocol.EmptyPayload{},
	}
	result, err := application.Processor().Process(context.Background(), auth.User{ID: chatTestUserID}, command)
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	assertReconnectRejection(t, result, "ROOM_NOT_FOUND", true)
}

func TestRealtimeApplicationRoutesRoomLobbyCommandsToRegistry(t *testing.T) {
	t.Parallel()
	lobbies, err := NewRoomRegistry(time.Now)
	if err != nil {
		t.Fatalf("NewRoomRegistry(time.Now) error = %v", err)
	}
	summary, err := lobbies.Create(CreateRoomInput{
		Creator:  chatTestUserID,
		Creation: room.Creation{Title: "로비 방"},
		Settings: room.DefaultSettings(),
		Team:     domain.TeamA,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	application := mustRealtimeApplicationWithLobbies(t, time.Now, lobbies)
	defer closeRealtimeApplication(t, application)

	selectTeam := protocol.ClientCommand{
		Version: protocol.Version1, Direction: protocol.DirectionClientCommand,
		Type: protocol.CommandSelectTeam, CommandID: "cmd-team-1",
		RoomID: summary.RoomID, Payload: protocol.SelectTeamPayload{TeamID: domain.TeamB},
	}
	result, err := application.Processor().Process(context.Background(), auth.User{ID: chatTestUserID}, selectTeam)
	if err != nil || result.Payload.Status != protocol.CommandAccepted {
		t.Fatalf("SELECT_TEAM result = %+v error = %v, want accepted", result, err)
	}

	replay, err := application.Processor().Process(context.Background(), auth.User{ID: chatTestUserID}, selectTeam)
	if err != nil || replay.Payload.Status != protocol.CommandAccepted || replay.CommandID != selectTeam.CommandID {
		t.Fatalf("SELECT_TEAM replay = %+v error = %v, want deterministic accepted replay", replay, err)
	}

	setReady := protocol.ClientCommand{
		Version: protocol.Version1, Direction: protocol.DirectionClientCommand,
		Type: protocol.CommandSetReady, CommandID: "cmd-ready-1",
		RoomID: summary.RoomID, Payload: protocol.SetReadyPayload{Ready: true},
	}
	if _, err := application.Processor().Process(context.Background(), auth.User{ID: chatTestUserID}, setReady); err != nil {
		t.Fatalf("SET_READY Process() error = %v", err)
	}
	blockedTeam := selectTeam
	blockedTeam.CommandID = "cmd-team-2"
	blockedTeam.Payload = protocol.SelectTeamPayload{TeamID: domain.TeamA}
	result, err = application.Processor().Process(context.Background(), auth.User{ID: chatTestUserID}, blockedTeam)
	if err != nil {
		t.Fatalf("blocked SELECT_TEAM Process() error = %v", err)
	}
	assertReconnectRejection(t, result, readyTeamChangeBlockedCode, false)

	membership, err := lobbies.Membership(chatTestUserID, summary.RoomID)
	if err != nil {
		t.Fatalf("Membership() error = %v", err)
	}
	if !membership.Ready || membership.Team != domain.TeamB {
		t.Fatalf("membership = %+v, want team B and ready", membership)
	}

	unknownRoom := setReady
	unknownRoom.RoomID = domain.RoomID("00000000000000000000000000000000")
	unknownRoom.CommandID = "cmd-ready-unknown"
	result, err = application.Processor().Process(context.Background(), auth.User{ID: chatTestUserID}, unknownRoom)
	if err != nil {
		t.Fatalf("unknown room Process() error = %v", err)
	}
	assertReconnectRejection(t, result, "ROOM_NOT_FOUND", true)
}

func TestRealtimeApplicationSerializesLobbyChatThroughActor(t *testing.T) {
	clockEntered := make(chan struct{})
	releaseClock := make(chan struct{})
	clock := func() time.Time {
		close(clockEntered)
		<-releaseClock
		return time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	}
	application := mustRealtimeApplication(t, clock)
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseClock) }) }
	defer release()
	// Registered after release so a failing assertion releases the blocked
	// chat execution before the actor Close waits on it.
	defer closeRealtimeApplication(t, application)
	type processResult struct {
		result protocol.CommandResult
		err    error
	}
	chatDone := make(chan processResult, 1)
	go func() {
		result, err := application.Processor().Process(
			context.Background(), auth.User{ID: chatTestUserID}, chatCommand("cmd-serialized-chat", "first"),
		)
		chatDone <- processResult{result: result, err: err}
	}()
	select {
	case <-clockEntered:
	case <-time.After(time.Second):
		t.Fatal("chat command did not enter actor-owned execution")
	}

	reconnectDone := make(chan processResult, 1)
	go func() {
		result, err := application.Processor().Process(
			context.Background(), auth.User{ID: chatTestUserID}, reconnectCommand("cmd-serialized-reconnect", 0),
		)
		reconnectDone <- processResult{result: result, err: err}
	}()
	reconnect := <-reconnectDone
	if reconnect.err != nil {
		t.Fatalf("serialized reconnect error = %v", reconnect.err)
	}
	// The retired prototype scope is not a registry room, so the match
	// executor reports the unknown room without consuming any sequence.
	assertReconnectRejection(t, reconnect.result, "ROOM_NOT_FOUND", true)
	release()

	chat := <-chatDone
	if chat.err != nil || chat.result.Payload.EventSequenceStart == nil || *chat.result.Payload.EventSequenceStart != 1 {
		t.Fatalf("serialized chat = %+v, error = %v", chat.result, chat.err)
	}
}

func TestRealtimeApplicationCloseReleasesLobbyChatLifetimeState(t *testing.T) {
	t.Parallel()
	application := mustRealtimeApplication(t, time.Now)
	if _, err := application.Processor().Process(
		context.Background(), auth.User{ID: chatTestUserID}, chatCommand("cmd-retained-before-close", "kept"),
	); err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	if err := application.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	assertBoundary(t, application.sequences, LobbyChatRoomID, 0)
	application.processor.mu.Lock()
	entryCount := len(application.processor.entries)
	application.processor.mu.Unlock()
	if entryCount != 0 {
		t.Fatalf("idempotency entries after close = %d", entryCount)
	}
	_, err := application.Processor().Process(
		context.Background(), auth.User{ID: chatTestUserID}, chatCommand("cmd-after-close", "again"),
	)
	if !errors.Is(err, ErrRoomActorClosed) {
		t.Fatalf("Process() after close error = %v, want %v", err, ErrRoomActorClosed)
	}
}

func reconnectCommand(commandID string, lastSequence uint64) protocol.ClientCommand {
	matchID := domain.MatchID("prototype-match")
	return protocol.ClientCommand{
		Version: protocol.Version1, Direction: protocol.DirectionClientCommand,
		Type: protocol.CommandReconnect, CommandID: commandID, RoomID: LobbyChatRoomID,
		MatchID: &matchID, Payload: protocol.ReconnectPayload{LastSequence: lastSequence},
	}
}

func mustRealtimeApplication(t *testing.T, now func() time.Time) *RealtimeApplication {
	t.Helper()
	lobbies, err := NewRoomRegistry(time.Now)
	if err != nil {
		t.Fatalf("NewRoomRegistry(time.Now) error = %v", err)
	}
	return mustRealtimeApplicationWithLobbies(t, now, lobbies)
}

func mustRealtimeApplicationWithLobbies(t *testing.T, now func() time.Time, lobbies *RoomRegistry) *RealtimeApplication {
	t.Helper()
	application, err := NewRealtimeApplication(now, lobbies, nil)
	if err != nil {
		t.Fatalf("NewRealtimeApplication() error = %v", err)
	}
	return application
}

func closeRealtimeApplication(t *testing.T, application *RealtimeApplication) {
	t.Helper()
	if err := application.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func assertReconnectRejection(t *testing.T, result protocol.CommandResult, code string, retriable bool) {
	t.Helper()
	if result.Payload.Status != protocol.CommandRejected || result.Payload.Error == nil ||
		result.Payload.Error.Code != code || result.Payload.Error.Retriable != retriable ||
		result.Payload.EventSequenceStart != nil || result.Payload.EventSequenceEnd != nil || result.Payload.Synchronization != nil {
		t.Fatalf("reconnect result = %+v, want %s retriable=%v", result, code, retriable)
	}
}
