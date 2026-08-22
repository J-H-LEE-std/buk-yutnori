package application

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"buk-yutnori/internal/auth"
	"buk-yutnori/internal/domain"
	"buk-yutnori/internal/domain/room"
	"buk-yutnori/internal/protocol"
)

func TestPrototypeGameSnapshotMatchesSchemaValidatedExample(t *testing.T) {
	encoded, err := json.Marshal(newPrototypeGameSnapshot(prototypeRoomInitializationSequence))
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	want, err := os.ReadFile(filepath.Join("..", "..", "schemas", "examples", "prototype_game_snapshot.json"))
	if err != nil {
		t.Fatalf("read prototype snapshot example: %v", err)
	}
	var gotValue any
	var wantValue any
	if err := json.Unmarshal(encoded, &gotValue); err != nil {
		t.Fatalf("decode generated snapshot: %v", err)
	}
	if err := json.Unmarshal(want, &wantValue); err != nil {
		t.Fatalf("decode example snapshot: %v", err)
	}
	gotCanonical, _ := json.Marshal(gotValue)
	wantCanonical, _ := json.Marshal(wantValue)
	if string(gotCanonical) != string(wantCanonical) {
		t.Fatalf("generated snapshot differs from schema example:\ngot  = %s\nwant = %s", gotCanonical, wantCanonical)
	}
}

func TestPrototypeRealtimeApplicationReturnsLatestAtomicSnapshotWithoutConsumingSequence(t *testing.T) {
	application := mustPrototypeRealtimeApplication(t, time.Now)
	defer closePrototypeRealtimeApplication(t, application)

	chatResult, err := application.Processor().Process(
		context.Background(),
		auth.User{ID: chatTestUserID},
		chatCommand("cmd-chat-before-reconnect", "snapshot boundary"),
	)
	if err != nil {
		t.Fatalf("chat Process() error = %v", err)
	}
	if chatResult.Payload.EventSequenceStart == nil || *chatResult.Payload.EventSequenceStart != 2 {
		t.Fatalf("chat result = %+v, want bootstrap-following sequence 2", chatResult)
	}

	result, err := application.Processor().Process(
		context.Background(),
		auth.User{ID: chatTestUserID},
		reconnectCommand("cmd-reconnect-latest", 1),
	)
	if err != nil {
		t.Fatalf("reconnect Process() error = %v", err)
	}
	if result.Payload.Status != protocol.CommandAccepted || result.Payload.Synchronization == nil ||
		result.Payload.EventSequenceStart != nil || result.Payload.EventSequenceEnd != nil || result.Payload.Error != nil {
		t.Fatalf("reconnect result = %+v", result)
	}
	if len(result.Payload.Synchronization.Events) != 0 {
		t.Fatalf("reconnect events = %s, want latest snapshot with no trailing events", result.Payload.Synchronization.Events)
	}

	var snapshot prototypeGameSnapshot
	if err := json.Unmarshal(result.Payload.Synchronization.Snapshot, &snapshot); err != nil {
		t.Fatalf("decode snapshot: %v", err)
	}
	if snapshot.RoomID != PrototypeRoomID || snapshot.MatchID != PrototypeMatchID || snapshot.Sequence != 2 || snapshot.Status != "starting" {
		t.Fatalf("snapshot scope = %+v", snapshot)
	}
	if len(snapshot.Teams) != 2 || snapshot.Teams[0].TeamID != domain.TeamA || snapshot.Teams[1].TeamID != domain.TeamB ||
		snapshot.Participants == nil || snapshot.ResultQueue == nil || snapshot.Pieces == nil || snapshot.Stacks == nil || snapshot.PositionGroups == nil {
		t.Fatalf("snapshot does not preserve schema-shaped empty collections: %+v", snapshot)
	}
	assertBoundary(t, application.sequences, PrototypeRoomID, 2)
}

func TestPrototypeRealtimeApplicationDoesNotRetainResyncRequired(t *testing.T) {
	application := mustPrototypeRealtimeApplication(t, time.Now)
	defer closePrototypeRealtimeApplication(t, application)
	command := reconnectCommand("cmd-reconnect-retry", 2)

	first, err := application.Processor().Process(context.Background(), auth.User{ID: chatTestUserID}, command)
	if err != nil {
		t.Fatalf("first Process() error = %v", err)
	}
	assertReconnectRejection(t, first, protocol.ErrorCodeResyncRequired, true)

	chat, err := application.Processor().Process(
		context.Background(),
		auth.User{ID: chatTestUserID},
		chatCommand("cmd-advance-boundary", "advance"),
	)
	if err != nil || chat.Payload.EventSequenceStart == nil || *chat.Payload.EventSequenceStart != 2 {
		t.Fatalf("advance result = %+v, error = %v", chat, err)
	}

	second, err := application.Processor().Process(context.Background(), auth.User{ID: chatTestUserID}, command)
	if err != nil {
		t.Fatalf("second Process() error = %v", err)
	}
	if second.Payload.Status != protocol.CommandAccepted || second.Payload.Synchronization == nil {
		t.Fatalf("transient rejection was retained: %+v", second)
	}
}

func TestPrototypeRealtimeApplicationReplaysAcceptedReconnectAtOriginalBoundary(t *testing.T) {
	application := mustPrototypeRealtimeApplication(t, time.Now)
	defer closePrototypeRealtimeApplication(t, application)
	command := reconnectCommand("cmd-reconnect-replay", 0)

	first, err := application.Processor().Process(context.Background(), auth.User{ID: chatTestUserID}, command)
	if err != nil {
		t.Fatalf("first Process() error = %v", err)
	}
	if _, err := application.Processor().Process(
		context.Background(),
		auth.User{ID: chatTestUserID},
		chatCommand("cmd-after-reconnect", "later event"),
	); err != nil {
		t.Fatalf("chat Process() error = %v", err)
	}
	second, err := application.Processor().Process(context.Background(), auth.User{ID: chatTestUserID}, command)
	if err != nil {
		t.Fatalf("second Process() error = %v", err)
	}
	firstJSON, _ := json.Marshal(first)
	secondJSON, _ := json.Marshal(second)
	if string(firstJSON) != string(secondJSON) {
		t.Fatalf("accepted reconnect replay changed:\nfirst = %s\nsecond = %s", firstJSON, secondJSON)
	}
	var snapshot prototypeGameSnapshot
	if err := json.Unmarshal(second.Payload.Synchronization.Snapshot, &snapshot); err != nil {
		t.Fatalf("decode replayed snapshot: %v", err)
	}
	if snapshot.Sequence != 1 {
		t.Fatalf("replayed snapshot sequence = %d, want original boundary 1", snapshot.Sequence)
	}
	assertBoundary(t, application.sequences, PrototypeRoomID, 2)
}

func TestPrototypeRealtimeApplicationRejectsUnknownScopeWithoutClosingActor(t *testing.T) {
	application := mustPrototypeRealtimeApplication(t, time.Now)
	defer closePrototypeRealtimeApplication(t, application)

	wrongRoom := reconnectCommand("cmd-wrong-room", 0)
	wrongRoom.RoomID = "other-room"
	result, err := application.Processor().Process(context.Background(), auth.User{ID: chatTestUserID}, wrongRoom)
	if err != nil {
		t.Fatalf("wrong room Process() error = %v", err)
	}
	assertReconnectRejection(t, result, "ROOM_NOT_FOUND", true)

	wrongMatch := reconnectCommand("cmd-wrong-match", 0)
	matchID := domain.MatchID("other-match")
	wrongMatch.MatchID = &matchID
	result, err = application.Processor().Process(context.Background(), auth.User{ID: chatTestUserID}, wrongMatch)
	if err != nil {
		t.Fatalf("wrong match Process() error = %v", err)
	}
	assertReconnectRejection(t, result, protocol.ErrorCodeResyncRequired, true)

	result, err = application.Processor().Process(
		context.Background(), auth.User{ID: chatTestUserID}, reconnectCommand("cmd-valid-after-scope-errors", 0),
	)
	if err != nil || result.Payload.Status != protocol.CommandAccepted {
		t.Fatalf("valid reconnect after scope errors = %+v, error = %v", result, err)
	}
}

func TestPrototypeRealtimeApplicationPreservesUnsupportedCommandRejection(t *testing.T) {
	application := mustPrototypeRealtimeApplication(t, time.Now)
	defer closePrototypeRealtimeApplication(t, application)
	command := protocol.ClientCommand{
		Version: protocol.Version1, Direction: protocol.DirectionClientCommand,
		Type: protocol.CommandStartGame, CommandID: "cmd-unsupported-room", RoomID: "other-room",
		Payload: protocol.EmptyPayload{},
	}
	result, err := application.Processor().Process(context.Background(), auth.User{ID: chatTestUserID}, command)
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	assertReconnectRejection(t, result, applicationUnavailableCode, true)
}

func TestPrototypeRealtimeApplicationRoutesLobbyCommandsToRegistry(t *testing.T) {
	lobbies, err := NewRoomRegistry()
	if err != nil {
		t.Fatalf("NewRoomRegistry() error = %v", err)
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
	application := mustPrototypeRealtimeApplicationWithLobbies(t, time.Now, lobbies)
	defer closePrototypeRealtimeApplication(t, application)

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

func TestPrototypeRealtimeApplicationSerializesChatBeforeReconnectSnapshot(t *testing.T) {
	clockEntered := make(chan struct{})
	releaseClock := make(chan struct{})
	clock := func() time.Time {
		close(clockEntered)
		<-releaseClock
		return time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	}
	application := mustPrototypeRealtimeApplication(t, clock)
	defer closePrototypeRealtimeApplication(t, application)
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
	select {
	case result := <-reconnectDone:
		t.Fatalf("reconnect bypassed blocked room command: %+v", result)
	case <-time.After(20 * time.Millisecond):
	}
	close(releaseClock)

	chat := <-chatDone
	if chat.err != nil || chat.result.Payload.EventSequenceStart == nil || *chat.result.Payload.EventSequenceStart != 2 {
		t.Fatalf("serialized chat = %+v, error = %v", chat.result, chat.err)
	}
	reconnect := <-reconnectDone
	if reconnect.err != nil || reconnect.result.Payload.Synchronization == nil {
		t.Fatalf("serialized reconnect = %+v, error = %v", reconnect.result, reconnect.err)
	}
	var snapshot prototypeGameSnapshot
	if err := json.Unmarshal(reconnect.result.Payload.Synchronization.Snapshot, &snapshot); err != nil {
		t.Fatalf("decode snapshot: %v", err)
	}
	if snapshot.Sequence != 2 {
		t.Fatalf("snapshot sequence = %d, want chat boundary 2", snapshot.Sequence)
	}
}

func TestPrototypeRealtimeApplicationCloseReleasesRoomLifetimeState(t *testing.T) {
	application := mustPrototypeRealtimeApplication(t, time.Now)
	if _, err := application.Processor().Process(
		context.Background(), auth.User{ID: chatTestUserID}, reconnectCommand("cmd-retained-before-close", 0),
	); err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	if err := application.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	assertBoundary(t, application.sequences, PrototypeRoomID, 0)
	application.processor.mu.Lock()
	entryCount := len(application.processor.entries)
	application.processor.mu.Unlock()
	if entryCount != 0 {
		t.Fatalf("idempotency entries after close = %d", entryCount)
	}
	_, err := application.Processor().Process(
		context.Background(), auth.User{ID: chatTestUserID}, reconnectCommand("cmd-after-close", 0),
	)
	if !errors.Is(err, ErrRoomActorClosed) {
		t.Fatalf("Process() after close error = %v, want %v", err, ErrRoomActorClosed)
	}
}

func reconnectCommand(commandID string, lastSequence uint64) protocol.ClientCommand {
	matchID := PrototypeMatchID
	return protocol.ClientCommand{
		Version: protocol.Version1, Direction: protocol.DirectionClientCommand,
		Type: protocol.CommandReconnect, CommandID: commandID, RoomID: PrototypeRoomID,
		MatchID: &matchID, Payload: protocol.ReconnectPayload{LastSequence: lastSequence},
	}
}

func mustPrototypeRealtimeApplication(t *testing.T, now func() time.Time) *PrototypeRealtimeApplication {
	t.Helper()
	lobbies, err := NewRoomRegistry()
	if err != nil {
		t.Fatalf("NewRoomRegistry() error = %v", err)
	}
	return mustPrototypeRealtimeApplicationWithLobbies(t, now, lobbies)
}

func mustPrototypeRealtimeApplicationWithLobbies(t *testing.T, now func() time.Time, lobbies *RoomRegistry) *PrototypeRealtimeApplication {
	t.Helper()
	application, err := NewPrototypeRealtimeApplication(now, lobbies)
	if err != nil {
		t.Fatalf("NewPrototypeRealtimeApplication() error = %v", err)
	}
	return application
}

func closePrototypeRealtimeApplication(t *testing.T, application *PrototypeRealtimeApplication) {
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
