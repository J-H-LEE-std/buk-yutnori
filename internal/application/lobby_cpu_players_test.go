package application

import (
	"errors"
	"testing"
	"time"

	"buk-yutnori/internal/auth"
	"buk-yutnori/internal/domain"
	"buk-yutnori/internal/domain/room"
	"buk-yutnori/internal/protocol"
	"buk-yutnori/internal/storage"
)

func TestHostCanStartSoloMatchWithAutomaticallyConfirmedCPUPlayer(t *testing.T) {
	t.Parallel()
	clock := &manualClock{current: time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)}
	registry := newTestRegistryWithClock(t, clock.Now)
	summary, err := registry.Create(CreateRoomInput{Creator: lobbyCreatorID, Creation: room.Creation{Title: "CPU 테스트"}, Settings: room.DefaultSettings(), Team: domain.TeamA})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	cpuID, err := registry.AddCPUPlayer(lobbyCreatorID, summary.RoomID, domain.TeamB)
	if err != nil {
		t.Fatalf("AddCPUPlayer() error = %v", err)
	}
	if player, ok := registry.rooms[summary.RoomID].lobby.Player(cpuID); !ok || !player.CPU || !player.Ready || player.Team != domain.TeamB {
		t.Fatalf("CPU player = %+v, exists=%v", player, ok)
	}
	if err := registry.SetReady(lobbyCreatorID, summary.RoomID, true); err != nil {
		t.Fatalf("SetReady(host) error = %v", err)
	}
	if err := registry.RequestStart(lobbyCreatorID, summary.RoomID); err != nil {
		t.Fatalf("RequestStart() error = %v", err)
	}
	matchID := readActiveStart(t, registry, summary.RoomID).MatchID
	if err := registry.ConfirmStart(lobbyCreatorID, summary.RoomID, matchID); err != nil {
		t.Fatalf("ConfirmStart(host) error = %v", err)
	}
	if !registry.rooms[summary.RoomID].started || registry.rooms[summary.RoomID].runtime == nil {
		t.Fatal("human confirmation did not start the CPU match")
	}
	if !registry.rooms[summary.RoomID].runtime.cpuPlayers[cpuID] {
		t.Fatal("runtime lost CPU roster identity")
	}
	if err := registry.RemoveCPUPlayer(lobbyCreatorID, summary.RoomID, cpuID); !errors.Is(err, ErrRoomAlreadyStarted) {
		t.Fatalf("RemoveCPUPlayer(in match) error = %v, want ErrRoomAlreadyStarted", err)
	}
}

func TestIndependentCPUPlayersCanStartAndRunConsecutiveTurns(t *testing.T) {
	t.Parallel()

	var observed []matchEventEnvelope
	for seed := uint64(0); seed < 32 && observed == nil; seed++ {
		clock := &manualClock{current: time.Date(2026, 8, 30, 13, 0, 0, 0, time.UTC)}
		registry := newTestRegistryWithClock(t, clock.Now)
		registry.setMatchRandomSeed(func() (uint64, uint64, error) { return seed, seed + 1, nil })
		settings := room.DefaultSettings()
		settings.MaxPlayers = 4
		summary, err := registry.Create(CreateRoomInput{
			Creator: lobbyCreatorID, Creation: room.Creation{Title: "연속 CPU 턴"},
			Settings: settings, Team: domain.TeamA,
		})
		if err != nil {
			t.Fatalf("Create() error = %v", err)
		}
		cpuIDs := make(map[domain.PlayerID]bool)
		for _, team := range []domain.TeamID{domain.TeamB, domain.TeamA, domain.TeamB} {
			id, addErr := registry.AddCPUPlayer(lobbyCreatorID, summary.RoomID, team)
			if addErr != nil {
				t.Fatalf("AddCPUPlayer(%s) error = %v", team, addErr)
			}
			cpuIDs[id] = true
		}
		if err := registry.SetReady(lobbyCreatorID, summary.RoomID, true); err != nil {
			t.Fatalf("SetReady(host) error = %v", err)
		}
		recorder := newEventRecorder(t, registry, lobbyCreatorID)
		if err := registry.RequestStart(lobbyCreatorID, summary.RoomID); err != nil {
			t.Fatalf("RequestStart() error = %v", err)
		}
		detail, err := registry.Detail(lobbyCreatorID, summary.RoomID)
		if err != nil || detail.ActiveStart == nil {
			t.Fatalf("Detail(starting) = %+v error=%v", detail, err)
		}
		if err := registry.ConfirmStart(lobbyCreatorID, summary.RoomID, detail.ActiveStart.MatchID); err != nil {
			t.Fatalf("ConfirmStart(host) error = %v", err)
		}
		events := recorder.snapshotEvents()
		recorder.close()
		controls := make([]domain.PlayerID, 0, 3)
		for _, event := range events {
			if event.Type == protocol.EventCPUControlStarted && cpuIDs[event.Payload.PlayerID] {
				controls = append(controls, event.Payload.PlayerID)
			}
		}
		if len(controls) >= 2 && controls[0] != controls[1] {
			started := false
			for _, event := range events {
				if event.Type == protocol.EventGameStarted && event.Payload.FirstPlayerID == controls[0] {
					started = true
					break
				}
			}
			if started {
				observed = events
			}
		}
	}
	if observed == nil {
		t.Fatal("fixed seed search did not produce CPU first with consecutive independent CPU turns")
	}
	controls := make([]domain.PlayerID, 0, 3)
	for _, event := range observed {
		if event.Type == protocol.EventCPUControlStarted {
			controls = append(controls, event.Payload.PlayerID)
		}
	}
	if len(controls) < 2 || controls[0] == controls[1] {
		t.Fatalf("CPU_CONTROL_STARTED sequence = %v, want distinct consecutive CPU players", controls)
	}
	if throws := countMatchEvents(observed, protocol.EventYutResult); throws < 2 {
		t.Fatalf("YUT_RESULT count = %d, want each consecutive CPU to act", throws)
	}
}

func countMatchEvents(events []matchEventEnvelope, kind string) int {
	count := 0
	for _, event := range events {
		if event.Type == kind {
			count++
		}
	}
	return count
}

func TestMatchResultExcludesLobbyCPUPlayerButKeepsHumanOutcome(t *testing.T) {
	t.Parallel()
	human := domain.PlayerID(matchHostID)
	cpu := domain.PlayerID("cpu-1")
	result := matchResultForRuntime(&matchRuntime{
		order: []domain.PlayerID{human, cpu}, teamOf: map[domain.PlayerID]domain.TeamID{human: domain.TeamA, cpu: domain.TeamB},
		cpuPlayers: map[domain.PlayerID]bool{cpu: true},
	}, domain.TeamA)
	if len(result.Winners) != 1 || result.Winners[0] != auth.UserID(human) || len(result.Losers) != 0 {
		t.Fatalf("CPU-excluded result = %+v", result)
	}
	if err := result.Validate(); err != nil {
		t.Fatalf("solo human MatchResult.Validate() error = %v", err)
	}
	if err := (storage.MatchResult{}).Validate(); err == nil {
		t.Fatal("empty MatchResult.Validate() error = nil")
	}
}

func TestOnlyHostCanAlterCPULobbyPlayers(t *testing.T) {
	t.Parallel()
	registry := newTestRegistry(t)
	summary := createDefaultRoom(t, registry, auth.UserID("host"))
	if _, err := registry.AddCPUPlayer(auth.UserID("outsider"), summary.RoomID, domain.TeamB); !errors.Is(err, ErrNotRoomHost) {
		t.Fatalf("non-host AddCPUPlayer() error = %v, want ErrNotRoomHost", err)
	}
	cpuID, err := registry.AddCPUPlayer(auth.UserID("host"), summary.RoomID, domain.TeamB)
	if err != nil {
		t.Fatalf("host AddCPUPlayer() error = %v", err)
	}
	if err := registry.RemoveCPUPlayer(auth.UserID("outsider"), summary.RoomID, cpuID); !errors.Is(err, ErrNotRoomHost) {
		t.Fatalf("non-host RemoveCPUPlayer() error = %v, want ErrNotRoomHost", err)
	}
}

func TestLobbyLeaveReassignsHumanHostAndClosesWithoutHumans(t *testing.T) {
	t.Parallel()

	registry := newTestRegistry(t)
	registry.randomSeed = func() (uint64, uint64, error) { return 0, 1, nil }
	summary := createDefaultRoom(t, registry, lobbyCreatorID)
	if _, err := registry.Join(JoinRoomInput{
		User: lobbyOutsiderID, RoomID: summary.RoomID, Role: RolePlayer, Team: domain.TeamB,
	}); err != nil {
		t.Fatalf("Join() error = %v", err)
	}
	if err := registry.LeaveRoom(lobbyCreatorID, summary.RoomID); err != nil {
		t.Fatalf("LeaveRoom(host) error = %v", err)
	}
	detail, err := registry.Detail(lobbyOutsiderID, summary.RoomID)
	if err != nil || len(detail.Members) != 1 || !detail.Members[0].Host || detail.Members[0].UserID != lobbyOutsiderID {
		t.Fatalf("replacement host detail = %+v error=%v", detail, err)
	}
	if err := registry.LeaveRoom(lobbyOutsiderID, summary.RoomID); err != nil {
		t.Fatalf("LeaveRoom(last human) error = %v", err)
	}
	if _, err := registry.Detail(lobbyOutsiderID, summary.RoomID); !errors.Is(err, ErrRoomNotFound) {
		t.Fatalf("Detail(closed room) error = %v, want ErrRoomNotFound", err)
	}
	if rooms := registry.List(); len(rooms) != 0 {
		t.Fatalf("List() after last human left = %+v", rooms)
	}
}

func TestKickAndRemoveCPUCommandsKeepTargetsSeparated(t *testing.T) {
	t.Parallel()

	registry := newTestRegistry(t)
	summary := createDefaultRoom(t, registry, lobbyCreatorID)
	cpuID, err := registry.AddCPUPlayer(lobbyCreatorID, summary.RoomID, domain.TeamB)
	if err != nil {
		t.Fatalf("AddCPUPlayer() error = %v", err)
	}
	if err := registry.KickPlayer(lobbyCreatorID, summary.RoomID, cpuID); !errors.Is(err, room.ErrHumanPlayerRequired) {
		t.Fatalf("KickPlayer(CPU) error = %v", err)
	}
	if err := registry.RemoveCPUPlayer(lobbyCreatorID, summary.RoomID, domain.PlayerID(lobbyCreatorID)); !errors.Is(err, room.ErrCPUPlayerRequired) {
		t.Fatalf("RemoveCPUPlayer(human) error = %v", err)
	}
	if err := registry.KickPlayer(lobbyOutsiderID, summary.RoomID, domain.PlayerID(lobbyCreatorID)); !errors.Is(err, ErrNotRoomHost) {
		t.Fatalf("KickPlayer(non-host) error = %v", err)
	}
	if err := registry.KickPlayer(lobbyCreatorID, summary.RoomID, domain.PlayerID(lobbyCreatorID)); !errors.Is(err, ErrCannotKickRoomHost) {
		t.Fatalf("KickPlayer(host) error = %v", err)
	}
}

func TestKickedPlayerReceivesCommittedSignalAfterMembershipRemoval(t *testing.T) {
	t.Parallel()

	registry := newTestRegistry(t)
	summary := createDefaultRoom(t, registry, lobbyCreatorID)
	if _, err := registry.Join(JoinRoomInput{
		User: lobbyOutsiderID, RoomID: summary.RoomID, Role: RolePlayer, Team: domain.TeamB,
	}); err != nil {
		t.Fatalf("Join() error = %v", err)
	}
	subscription, err := registry.SubscribeEvents(lobbyOutsiderID)
	if err != nil {
		t.Fatalf("SubscribeEvents() error = %v", err)
	}
	defer subscription.Close()
	select {
	case <-subscription.Events(): // cached ROOM_UPDATED
	default:
		t.Fatal("cached ROOM_UPDATED missing")
	}
	if err := registry.KickPlayer(lobbyCreatorID, summary.RoomID, domain.PlayerID(lobbyOutsiderID)); err != nil {
		t.Fatalf("KickPlayer() error = %v", err)
	}
	select {
	case delivered := <-subscription.Events():
		event, ok := delivered.Message.(protocol.PlayerKickedEvent)
		if !ok || event.Payload.PlayerID != domain.PlayerID(lobbyOutsiderID) {
			t.Fatalf("delivered kick event = %#v", delivered.Message)
		}
	default:
		t.Fatal("PLAYER_KICKED was not delivered to removed player")
	}
	if _, err := registry.Detail(lobbyOutsiderID, summary.RoomID); !errors.Is(err, ErrNotMember) {
		t.Fatalf("Detail(kicked player) error = %v, want ErrNotMember", err)
	}
}

func TestLobbyCPUFailureCodesAreExplicit(t *testing.T) {
	t.Parallel()

	executor := &LobbyCommandExecutor{}
	tests := []struct {
		err  error
		code string
	}{
		{room.ErrLobbyFull, "ROOM_FULL"},
		{room.ErrCPUPlayerRequired, "CPU_PLAYER_REQUIRED"},
		{room.ErrHumanPlayerRequired, "HUMAN_PLAYER_REQUIRED"},
		{ErrCannotKickRoomHost, "CANNOT_KICK_ROOM_HOST"},
		{ErrNotMember, "ROOM_MEMBER_REQUIRED"},
	}
	for _, test := range tests {
		outcome := executor.rejectLobbyError(test.err)
		if outcome.Error == nil || outcome.Error.Code != test.code || outcome.Error.Retriable {
			t.Fatalf("rejectLobbyError(%v) = %+v, want %s", test.err, outcome, test.code)
		}
	}
}
