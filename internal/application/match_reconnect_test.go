package application

import (
	"context"
	"encoding/json"
	"testing"

	"buk-yutnori/internal/auth"
	"buk-yutnori/internal/domain"
	"buk-yutnori/internal/domain/yut"
	"buk-yutnori/internal/protocol"
)

func reconnectCommandFor(roomID domain.RoomID, matchID domain.MatchID, commandID string, lastSequence uint64) protocol.ClientCommand {
	scope := matchID
	return protocol.ClientCommand{
		Version: protocol.Version1, Direction: protocol.DirectionClientCommand,
		Type: protocol.CommandReconnect, CommandID: commandID, RoomID: roomID,
		MatchID: &scope, Payload: protocol.ReconnectPayload{LastSequence: lastSequence},
	}
}

func decodeSnapshotScope(t *testing.T, raw json.RawMessage) (gameSnapshotJSON, uint64) {
	t.Helper()
	var snapshot gameSnapshotJSON
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		t.Fatalf("decode snapshot: %v", err)
	}
	var generic struct {
		Sequence uint64 `json:"sequence"`
	}
	if err := json.Unmarshal(raw, &generic); err != nil {
		t.Fatalf("decode snapshot sequence: %v", err)
	}
	if len(snapshot.Teams) != 2 || snapshot.RoomID == "" || snapshot.MatchID == "" || generic.Sequence == 0 {
		t.Fatalf("snapshot scope incomplete: %+v", snapshot)
	}
	return snapshot, generic.Sequence
}

// RECONNECT for a started registry room returns the real assembled roster
// snapshot at the current boundary without consuming a sequence.
func TestReconnectReturnsRealAssembledSnapshotForStartedRoom(t *testing.T) {
	t.Parallel()

	fixture := newMatchFixture(t, nil)
	defer fixture.recorder.close()

	boundary := boundaryOf(t, fixture.registry, fixture.roomID)
	command := reconnectCommandFor(fixture.roomID, fixture.matchID, "cmd-reconnect-live", 0)
	result, err := fixture.processor.Process(context.Background(), auth.User{ID: fixture.users[1]}, command)
	if err != nil {
		t.Fatalf("RECONNECT Process() error = %v", err)
	}
	if result.Payload.Status != protocol.CommandAccepted || result.Payload.Synchronization == nil ||
		result.Payload.EventSequenceStart != nil || result.Payload.Error != nil {
		t.Fatalf("reconnect result = %+v", result)
	}
	if len(result.Payload.Synchronization.Events) != 0 {
		t.Fatalf("replay events = %s; live-only hub returns snapshot-only bundles (ADR-0014 pending)", result.Payload.Synchronization.Events)
	}

	snapshot, sequence := decodeSnapshotScope(t, result.Payload.Synchronization.Snapshot)
	if sequence != boundary {
		t.Fatalf("snapshot sequence = %d, want current boundary %d", sequence, boundary)
	}
	if snapshot.Status != "active" {
		t.Fatalf("snapshot status = %q, want active", snapshot.Status)
	}

	currentPlayer := fixture.runtime().currentPlayer()
	teamsByID := make(map[domain.TeamID]snapshotTeamJSON, 2)
	for _, team := range snapshot.Teams {
		teamsByID[team.TeamID] = team
	}
	if len(teamsByID[domain.TeamA].PlayerIDs) != 1 || len(teamsByID[domain.TeamB].PlayerIDs) != 1 {
		t.Fatalf("team rosters = %+v / %+v, want one player each", teamsByID[domain.TeamA], teamsByID[domain.TeamB])
	}
	totalOrder := append(append([]domain.PlayerID(nil), teamsByID[domain.TeamA].TurnOrder...), teamsByID[domain.TeamB].TurnOrder...)
	if len(totalOrder) != 2 {
		t.Fatalf("turn order = %+v, want both players ordered", totalOrder)
	}

	playerSeen := false
	for _, participant := range snapshot.Participants {
		if participant.Role == RolePlayer && domain.PlayerID(participant.UserID) == currentPlayer {
			playerSeen = true
			if participant.CPUControl.Active {
				t.Fatalf("unsubstituted participant flagged CPU-controlled: %+v", participant)
			}
		}
	}
	if !playerSeen {
		t.Fatalf("acting player missing from participants: %+v", snapshot.Participants)
	}

	turn := snapshot.CurrentTurn
	if turn.PlayerID == nil || *turn.PlayerID != currentPlayer || turn.Timer.Phase != "throw" {
		t.Fatalf("current turn = %+v, want acting player with armed throw window", turn)
	}
	if len(snapshot.ResultQueue) != 0 || len(snapshot.Pieces) != 4 {
		t.Fatalf("fresh match snapshot queue/pieces = %+v / %d", snapshot.ResultQueue, len(snapshot.Pieces))
	}
	if snapshot.Buk.Enabled {
		t.Fatalf("buk state = %+v, want disabled default", snapshot.Buk)
	}

	if got := boundaryOf(t, fixture.registry, fixture.roomID); got != boundary {
		t.Fatalf("approved RECONNECT consumed a sequence: %d -> %d", boundary, got)
	}
}

// Duplicate RECONNECT replays the original bundle byte-for-byte.
func TestReconnectReplaysOriginalSnapshotAtOriginalBoundary(t *testing.T) {
	t.Parallel()

	fixture := newMatchFixture(t, nil)
	defer fixture.recorder.close()
	command := reconnectCommandFor(fixture.roomID, fixture.matchID, "cmd-reconnect-replay", 0)

	first, err := fixture.processor.Process(context.Background(), auth.User{ID: fixture.users[0]}, command)
	if err != nil {
		t.Fatalf("first Process() error = %v", err)
	}

	// Advance the live match so the boundary moves past the stored replay.
	rt := fixture.runtime()
	rt.throwResult = func(yut.Mode) (domain.YutResult, error) { return domain.YutGae, nil }
	fixture.driveUntilPlayerOrEnd(t, rt.currentPlayer())
	boundary := boundaryOf(t, fixture.registry, fixture.roomID)

	second, err := fixture.processor.Process(context.Background(), auth.User{ID: fixture.users[0]}, command)
	if err != nil {
		t.Fatalf("second Process() error = %v", err)
	}
	firstJSON, _ := json.Marshal(first)
	secondJSON, _ := json.Marshal(second)
	if string(firstJSON) != string(secondJSON) {
		t.Fatalf("accepted reconnect replay changed:\nfirst = %s\nsecond = %s", firstJSON, secondJSON)
	}
	_, sequence := decodeSnapshotScope(t, second.Payload.Synchronization.Snapshot)
	if sequence >= boundary {
		t.Fatalf("replayed boundary %d must predate current %d", sequence, boundary)
	}
}

// Scope, membership, and staleness rejections.
func TestReconnectRejectionsForRealRooms(t *testing.T) {
	t.Parallel()

	fixture := newMatchFixture(t, nil, func(f *matchFixture) {
		// Spectators are members; they must join before the match starts
		// because started rooms block membership changes.
		if _, err := f.registry.Join(JoinRoomInput{
			User: auth.UserID(reconnectSpectatorID), RoomID: f.roomID, Role: RoleSpectator,
		}); err != nil {
			t.Fatalf("Join(spectator) error = %v", err)
		}
	})
	defer fixture.recorder.close()

	wrongMatch := reconnectCommandFor(fixture.roomID, "other-match", "cmd-wrong-match", 0)
	result, err := fixture.processor.Process(context.Background(), auth.User{ID: fixture.users[0]}, wrongMatch)
	if err != nil {
		t.Fatalf("wrong match Process() error = %v", err)
	}
	assertReconnectRejection(t, result, protocol.ErrorCodeResyncRequired, true)

	ahead := reconnectCommandFor(fixture.roomID, fixture.matchID, "cmd-ahead", 999999)
	result, err = fixture.processor.Process(context.Background(), auth.User{ID: fixture.users[0]}, ahead)
	if err != nil {
		t.Fatalf("ahead Process() error = %v", err)
	}
	assertReconnectRejection(t, result, protocol.ErrorCodeResyncRequired, true)

	stranger := auth.UserID(startLobbyOutsiderID)
	outside := reconnectCommandFor(fixture.roomID, fixture.matchID, "cmd-stranger", 0)
	result, err = fixture.processor.Process(context.Background(), auth.User{ID: stranger}, outside)
	if err != nil {
		t.Fatalf("stranger Process() error = %v", err)
	}
	assertReconnectRejection(t, result, notMemberCode, false)

	missing := reconnectCommandFor("00000000000000000000000000000000", fixture.matchID, "cmd-missing", 0)
	result, err = fixture.processor.Process(context.Background(), auth.User{ID: fixture.users[0]}, missing)
	if err != nil {
		t.Fatalf("missing room Process() error = %v", err)
	}
	assertReconnectRejection(t, result, "ROOM_NOT_FOUND", true)

	// Spectators are members and may synchronize.
	spectator := auth.UserID(reconnectSpectatorID)
	command := reconnectCommandFor(fixture.roomID, fixture.matchID, "cmd-spectator", 0)
	result, err = fixture.processor.Process(context.Background(), auth.User{ID: spectator}, command)
	if err != nil || result.Payload.Status != protocol.CommandAccepted {
		t.Fatalf("spectator reconnect = %+v error = %v, want accepted", result, err)
	}
}

func boundaryOf(t *testing.T, registry *RoomRegistry, roomID domain.RoomID) uint64 {
	t.Helper()
	registry.mutex.Lock()
	defer registry.mutex.Unlock()
	boundary, err := registry.sequences.Boundary(roomID)
	if err != nil {
		t.Fatalf("Boundary(%s) error = %v", roomID, err)
	}
	return boundary
}
