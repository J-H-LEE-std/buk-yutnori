package application

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"buk-yutnori/internal/auth"
	"buk-yutnori/internal/domain"
	"buk-yutnori/internal/domain/yut"
	"buk-yutnori/internal/profile"
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
	if turn.MoveRequest != nil {
		t.Fatalf("throw input exposed move request: %+v", turn.MoveRequest)
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

func TestReconnectSnapshotResolvesPersistentParticipantNicknames(t *testing.T) {
	t.Parallel()

	fixture := newMatchFixture(t, nil)
	defer fixture.recorder.close()
	profiles := &snapshotProfileStore{values: map[auth.UserID]profile.Profile{
		fixture.users[0]: {UserID: fixture.users[0], Nickname: "가나다", Public: true},
		// A valid record for another identity is not allowed to be displayed
		// as this participant or to make RECONNECT fail.
		fixture.users[1]: {UserID: fixture.users[0], Nickname: "나다라", Public: false},
	}}
	if err := fixture.registry.AttachProfileStore(profiles); err != nil {
		t.Fatalf("AttachProfileStore() error = %v", err)
	}

	command := reconnectCommandFor(fixture.roomID, fixture.matchID, "cmd-reconnect-nicknames", 0)
	result, err := fixture.processor.Process(context.Background(), auth.User{ID: fixture.users[0]}, command)
	if err != nil || result.Payload.Synchronization == nil {
		t.Fatalf("RECONNECT = %+v error = %v", result, err)
	}
	snapshot, _ := decodeSnapshotScope(t, result.Payload.Synchronization.Snapshot)
	byUser := make(map[auth.UserID]string, len(snapshot.Participants))
	for _, participant := range snapshot.Participants {
		byUser[participant.UserID] = participant.Nickname
	}
	if byUser[fixture.users[0]] != "가나다" {
		t.Fatalf("configured nickname = %q, want 가나다", byUser[fixture.users[0]])
	}
	if byUser[fixture.users[1]] != string(fixture.users[1]) {
		t.Fatalf("mismatched profile fallback = %q, want %q", byUser[fixture.users[1]], fixture.users[1])
	}
}

func TestReconnectRevalidatesMatchScopeAfterProfileLookup(t *testing.T) {
	t.Parallel()

	fixture := newMatchFixture(t, nil)
	defer fixture.recorder.close()
	profiles := &blockingSnapshotProfileStore{started: make(chan struct{}), release: make(chan struct{})}
	if err := fixture.registry.AttachProfileStore(profiles); err != nil {
		t.Fatalf("AttachProfileStore() error = %v", err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := fixture.registry.ReconnectBundle(fixture.users[0], fixture.roomID, fixture.matchID, 0)
		done <- err
	}()
	<-profiles.started
	fixture.registry.mutex.Lock()
	fixture.registry.rooms[fixture.roomID].runtime = nil
	fixture.registry.mutex.Unlock()
	close(profiles.release)
	if err := <-done; !errors.Is(err, ErrMatchNotActive) {
		t.Fatalf("ReconnectBundle() after match changed = %v, want %v", err, ErrMatchNotActive)
	}
}

func TestResolveSnapshotNicknamesFallsBackForMissingAndReadFailure(t *testing.T) {
	t.Parallel()

	configured := auth.UserID(matchHostID)
	missing := auth.UserID(matchGuestID)
	failed := auth.UserID(reconnectSpectatorID)
	profiles := &snapshotProfileStore{
		values: map[auth.UserID]profile.Profile{
			configured: {UserID: configured, Nickname: "나다라", Public: false},
		},
		errors: map[auth.UserID]error{failed: errors.New("profile database unavailable")},
	}
	nicknames := resolveSnapshotNicknames(context.Background(), profiles, []auth.UserID{configured, missing, failed})
	if nicknames[configured] != "나다라" {
		t.Fatalf("configured nickname = %q", nicknames[configured])
	}
	if _, ok := nicknames[missing]; ok {
		t.Fatalf("missing profile unexpectedly resolved = %q", nicknames[missing])
	}
	if _, ok := nicknames[failed]; ok {
		t.Fatalf("failed profile unexpectedly resolved = %q", nicknames[failed])
	}
}

// A user who joins as a spectator after the match has started receives the
// same server-authoritative live scope and snapshot as an earlier spectator,
// but never receives a player control permission.
func TestLateSpectatorAdmissionSynchronizesActiveMatch(t *testing.T) {
	t.Parallel()

	fixture := newMatchFixture(t, nil)
	defer fixture.recorder.close()
	lateSpectator := auth.UserID(startRosterIDs[2])

	if _, err := fixture.registry.Join(JoinRoomInput{
		User: lateSpectator, RoomID: fixture.roomID, Role: RoleSpectator,
	}); err != nil {
		t.Fatalf("Join(late spectator) error = %v", err)
	}
	membership, err := fixture.registry.Membership(lateSpectator, fixture.roomID)
	if err != nil || membership.Role != RoleSpectator || membership.Team != "" || membership.Ready {
		t.Fatalf("late spectator membership = %+v error = %v", membership, err)
	}
	detail, err := fixture.registry.Detail(lateSpectator, fixture.roomID)
	if err != nil || detail.ActiveMatch == nil || detail.ActiveMatch.MatchID != fixture.matchID {
		t.Fatalf("late spectator detail = %+v error = %v, want active match scope", detail, err)
	}

	command := reconnectCommandFor(fixture.roomID, fixture.matchID, "cmd-late-spectator", 0)
	result, err := fixture.processor.Process(context.Background(), auth.User{ID: lateSpectator}, command)
	if err != nil || result.Payload.Status != protocol.CommandAccepted || result.Payload.Synchronization == nil {
		t.Fatalf("late spectator reconnect = %+v error = %v, want accepted snapshot", result, err)
	}
	snapshot, _ := decodeSnapshotScope(t, result.Payload.Synchronization.Snapshot)
	var participant *snapshotParticipant
	for index := range snapshot.Participants {
		if snapshot.Participants[index].UserID == lateSpectator {
			participant = &snapshot.Participants[index]
			break
		}
	}
	if participant == nil || participant.Role != RoleSpectator || participant.TeamID != nil ||
		len(participant.Permissions) != 1 || participant.Permissions[0] != participantPermissionChat {
		t.Fatalf("late spectator participant = %+v, want chat-only observer", participant)
	}

	scope := fixture.matchID
	throw := protocol.ClientCommand{
		Version: protocol.Version1, Direction: protocol.DirectionClientCommand,
		Type: protocol.CommandThrowYut, CommandID: "cmd-late-spectator-throw",
		RoomID: fixture.roomID, MatchID: &scope, Payload: protocol.EmptyPayload{},
	}
	result, err = fixture.processor.Process(context.Background(), auth.User{ID: lateSpectator}, throw)
	if err != nil || result.Payload.Status != protocol.CommandRejected || result.Payload.Error == nil ||
		result.Payload.Error.Code != notMemberCode || result.Payload.Error.Retriable {
		t.Fatalf("late spectator throw = %+v error = %v, want non-retriable control rejection", result, err)
	}
}

func TestLateSpectatorAdmissionPreservesStartedRoomBoundaries(t *testing.T) {
	t.Parallel()

	fixture := newMatchFixture(t, nil)
	defer fixture.recorder.close()
	password := "pass1234"
	digest := sha256.Sum256([]byte(password))
	fixture.registry.mutex.Lock()
	fixture.registry.rooms[fixture.roomID].password = digest[:]
	fixture.registry.mutex.Unlock()

	if _, err := fixture.registry.Join(JoinRoomInput{
		User: auth.UserID(startRosterIDs[2]), RoomID: fixture.roomID, Role: RolePlayer, Team: domain.TeamA, Password: password,
	}); !errors.Is(err, ErrRoomAlreadyStarted) {
		t.Fatalf("Join(player during match) error = %v, want ErrRoomAlreadyStarted", err)
	}
	if _, err := fixture.registry.Join(JoinRoomInput{
		User: auth.UserID(startRosterIDs[2]), RoomID: fixture.roomID, Role: RoleSpectator,
	}); !errors.Is(err, ErrPasswordRequired) {
		t.Fatalf("Join(late spectator without password) error = %v, want ErrPasswordRequired", err)
	}
	if _, err := fixture.registry.Join(JoinRoomInput{
		User: auth.UserID(startRosterIDs[2]), RoomID: fixture.roomID, Role: RoleSpectator, Password: "wrong999",
	}); !errors.Is(err, ErrInvalidRoomPassword) {
		t.Fatalf("Join(late spectator wrong password) error = %v, want ErrInvalidRoomPassword", err)
	}
	if _, err := fixture.registry.Join(JoinRoomInput{
		User: auth.UserID(startRosterIDs[2]), RoomID: fixture.roomID, Role: RoleSpectator, Password: password,
	}); err != nil {
		t.Fatalf("Join(password-protected late spectator) error = %v", err)
	}
	if _, err := fixture.registry.Join(JoinRoomInput{
		User: auth.UserID(startRosterIDs[2]), RoomID: fixture.roomID, Role: RoleSpectator, Password: password,
	}); !errors.Is(err, ErrAlreadyMember) {
		t.Fatalf("Join(duplicate late spectator) error = %v, want ErrAlreadyMember", err)
	}
	for index := 0; index < combinedMemberCapacity-3; index++ {
		user := auth.UserID("late-spectator-" + string(rune('a'+index%26)) + string(rune('0'+index/26)))
		if _, err := fixture.registry.Join(JoinRoomInput{
			User: user, RoomID: fixture.roomID, Role: RoleSpectator, Password: password,
		}); err != nil {
			t.Fatalf("Join(late spectator %d) error = %v", index, err)
		}
	}
	if _, err := fixture.registry.Join(JoinRoomInput{
		User: auth.UserID("late-spectator-over-capacity"), RoomID: fixture.roomID, Role: RoleSpectator, Password: password,
	}); !errors.Is(err, ErrCombinedCapacityFull) {
		t.Fatalf("Join(late spectator over capacity) error = %v, want ErrCombinedCapacityFull", err)
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
		// This covers a spectator who was already in the room before the
		// match; TestLateSpectatorAdmissionSynchronizesActiveMatch covers
		// the equally valid in-match admission path.
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

type snapshotProfileStore struct {
	values map[auth.UserID]profile.Profile
	errors map[auth.UserID]error
}

type blockingSnapshotProfileStore struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (store *blockingSnapshotProfileStore) Save(context.Context, profile.Profile) error {
	return errors.New("unexpected profile save")
}

func (store *blockingSnapshotProfileStore) Lookup(ctx context.Context, _ auth.UserID) (profile.Profile, error) {
	store.once.Do(func() { close(store.started) })
	select {
	case <-store.release:
		return profile.Profile{}, profile.ErrNotFound
	case <-ctx.Done():
		return profile.Profile{}, ctx.Err()
	}
}

func (store *snapshotProfileStore) Save(context.Context, profile.Profile) error {
	return errors.New("unexpected profile save")
}

func (store *snapshotProfileStore) Lookup(_ context.Context, userID auth.UserID) (profile.Profile, error) {
	if err := store.errors[userID]; err != nil {
		return profile.Profile{}, err
	}
	if value, ok := store.values[userID]; ok {
		return value, nil
	}
	return profile.Profile{}, profile.ErrNotFound
}
