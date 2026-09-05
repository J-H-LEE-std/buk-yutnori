package application

import (
	"errors"
	"testing"
	"time"

	"buk-yutnori/internal/auth"
	"buk-yutnori/internal/domain"
	"buk-yutnori/internal/domain/room"
)

func newPresenceMatchFixture(t *testing.T) *matchFixture {
	t.Helper()
	return newMatchFixture(t, nil, func(fixture *matchFixture) {
		for _, user := range fixture.users {
			if err := fixture.registry.ConnectionOpened(user); err != nil {
				t.Fatalf("ConnectionOpened(%s) error = %v", user, err)
			}
		}
	})
}

func TestNonCurrentDisconnectIsSubstitutedWhenItsTurnBegins(t *testing.T) {
	fixture := newPresenceMatchFixture(t)
	defer fixture.recorder.close()
	current := fixture.runtime().currentPlayer()
	next := fixture.otherPlayer(current)
	if err := fixture.registry.ConnectionClosed(auth.UserID(next)); err != nil {
		t.Fatalf("ConnectionClosed(next) error = %v", err)
	}
	if got := fixture.recorder.ofTypes("CPU_CONTROL_STARTED"); len(got) != 0 {
		t.Fatalf("non-current close started CPU early: %+v", got)
	}
	fixture.driveUntilPlayerOrEnd(t, current)
	cpu := fixture.recorder.ofTypes("CPU_CONTROL_STARTED")
	if len(cpu) == 0 || cpu[0].Payload.PlayerID != next || cpu[0].Payload.Reason != "disconnected" {
		t.Fatalf("next-turn CPU control = %+v", cpu)
	}
}

func TestPresenceUsesLastConnectionAndSnapshotReflectsIt(t *testing.T) {
	fixture := newPresenceMatchFixture(t)
	defer fixture.recorder.close()
	user := fixture.users[0]
	if err := fixture.registry.ConnectionOpened(user); err != nil {
		t.Fatalf("second ConnectionOpened() error = %v", err)
	}
	if err := fixture.registry.ConnectionClosed(user); err != nil {
		t.Fatalf("intermediate ConnectionClosed() error = %v", err)
	}
	if got := fixture.recorder.ofTypes("PLAYER_DISCONNECTED"); len(got) != 0 {
		t.Fatalf("intermediate close emitted disconnect = %+v", got)
	}
	if err := fixture.registry.ConnectionClosed(user); err != nil {
		t.Fatalf("last ConnectionClosed() error = %v", err)
	}
	if got := fixture.recorder.ofTypes("PLAYER_DISCONNECTED"); len(got) != 1 || got[0].Payload.PlayerID != domain.PlayerID(user) {
		t.Fatalf("disconnect events = %+v", got)
	}

	boundary := boundaryOf(t, fixture.registry, fixture.roomID)
	fixture.registry.mutex.Lock()
	snapshot, err := fixture.registry.assembleGameSnapshotLocked(fixture.registry.rooms[fixture.roomID], boundary)
	fixture.registry.mutex.Unlock()
	if err != nil {
		t.Fatalf("assembleGameSnapshotLocked() error = %v", err)
	}
	for _, participant := range snapshot.Participants {
		if participant.UserID == user && participant.Connected {
			t.Fatalf("disconnected participant = %+v", participant)
		}
	}
}

func TestCurrentPlayerDisconnectUsesCPUOnlyForCurrentTurn(t *testing.T) {
	fixture := newPresenceMatchFixture(t)
	defer fixture.recorder.close()
	current := fixture.runtime().currentPlayer()
	if err := fixture.registry.ConnectionClosed(auth.UserID(current)); err != nil {
		t.Fatalf("ConnectionClosed(current) error = %v", err)
	}
	cpu := fixture.recorder.ofTypes("CPU_CONTROL_STARTED")
	if len(cpu) != 1 || cpu[0].Payload.PlayerID != current || cpu[0].Payload.Reason != "disconnected" {
		t.Fatalf("CPU_CONTROL_STARTED = %+v", cpu)
	}
	rt := fixture.runtime()
	if rt == nil || rt.currentPlayer() == current || rt.cpuControlled {
		t.Fatalf("next turn leaked CPU control: runtime=%+v", rt)
	}
}

func TestHostDisconnectResumesPause(t *testing.T) {
	fixture := newPresenceMatchFixture(t)
	defer fixture.recorder.close()
	host := fixture.users[0]
	if err := fixture.registry.PauseGame(host, fixture.roomID, fixture.matchID, 5); err != nil {
		t.Fatalf("PauseGame() error = %v", err)
	}
	if err := fixture.registry.ConnectionClosed(host); err != nil {
		t.Fatalf("ConnectionClosed(host) error = %v", err)
	}
	resumed := fixture.recorder.ofTypes("GAME_RESUMED")
	if len(resumed) != 1 || resumed[0].Payload.Reason != "host_disconnected" {
		t.Fatalf("GAME_RESUMED = %+v", resumed)
	}
	if fixture.runtime().paused {
		t.Fatal("host pause remained active after disconnect")
	}
	events := fixture.recorder.snapshotEvents()
	disconnectedIndex, resumedIndex := -1, -1
	for index, event := range events {
		switch event.Type {
		case "PLAYER_DISCONNECTED":
			if event.Payload.PlayerID == domain.PlayerID(host) {
				disconnectedIndex = index
			}
		case "GAME_RESUMED":
			if event.Payload.Reason == "host_disconnected" {
				resumedIndex = index
			}
		}
	}
	if disconnectedIndex < 0 || resumedIndex != disconnectedIndex+1 {
		t.Fatalf("host disconnect/resume order = disconnect %d, resume %d, events=%+v", disconnectedIndex, resumedIndex, events)
	}
}

func TestAllPlayersDisconnectedWatchdogRecoversAt29Seconds(t *testing.T) {
	fixture := newPresenceMatchFixture(t)
	defer fixture.recorder.close()
	for _, user := range fixture.users {
		if err := fixture.registry.ConnectionClosed(user); err != nil {
			t.Fatalf("ConnectionClosed(%s) error = %v", user, err)
		}
	}
	if fixture.runtime() == nil || !fixture.runtime().allPlayersDisconnected {
		t.Fatal("all-player disconnect did not suspend the match")
	}
	fixture.clock.Advance(29 * time.Second)
	if fixture.runtime() == nil {
		t.Fatal("match closed before the 30-second deadline")
	}
	if err := fixture.registry.ConnectionOpened(fixture.users[0]); err != nil {
		t.Fatalf("ConnectionOpened() error = %v", err)
	}
	if fixture.runtime() == nil || fixture.runtime().allPlayersDisconnected {
		t.Fatal("reconnect did not cancel all-player suspension")
	}
	reconnected := fixture.recorder.ofTypes("PLAYER_RECONNECTED")
	if len(reconnected) != 1 || reconnected[0].Payload.PlayerID != domain.PlayerID(fixture.users[0]) || !reconnected[0].Payload.ControlRestored {
		t.Fatalf("PLAYER_RECONNECTED = %+v", reconnected)
	}
	fixture.clock.Advance(time.Second)
	if fixture.runtime() == nil {
		t.Fatal("stale watchdog closed the recovered match")
	}
}

func TestNonCurrentReconnectCancelsWatchdogAndRunsAbsentCurrentPlayer(t *testing.T) {
	fixture := newPresenceMatchFixture(t)
	defer fixture.recorder.close()
	current := fixture.runtime().currentPlayer()
	nonCurrent := fixture.otherPlayer(current)
	for _, user := range fixture.users {
		if err := fixture.registry.ConnectionClosed(user); err != nil {
			t.Fatalf("ConnectionClosed(%s) error = %v", user, err)
		}
	}
	if err := fixture.registry.ConnectionOpened(auth.UserID(nonCurrent)); err != nil {
		t.Fatalf("ConnectionOpened(non-current) error = %v", err)
	}
	rt := fixture.runtime()
	if rt == nil || rt.allPlayersDisconnected || rt.currentPlayer() != nonCurrent || rt.cpuControlled {
		t.Fatalf("non-current recovery state = %+v", rt)
	}
	cpu := fixture.recorder.ofTypes("CPU_CONTROL_STARTED")
	if len(cpu) == 0 || cpu[len(cpu)-1].Payload.PlayerID != current || cpu[len(cpu)-1].Payload.Reason != "disconnected" {
		t.Fatalf("absent-current CPU control = %+v", cpu)
	}
}

func TestSpectatorPresenceDoesNotEmitPlayerEventsOrKeepMatchAlive(t *testing.T) {
	spectator := auth.UserID(reconnectSpectatorID)
	fixture := newMatchFixture(t, nil, func(fixture *matchFixture) {
		for _, user := range fixture.users {
			if err := fixture.registry.ConnectionOpened(user); err != nil {
				t.Fatalf("ConnectionOpened(%s) error = %v", user, err)
			}
		}
		if err := fixture.registry.ConnectionOpened(spectator); err != nil {
			t.Fatalf("ConnectionOpened(spectator) error = %v", err)
		}
		if _, err := fixture.registry.Join(JoinRoomInput{User: spectator, RoomID: fixture.roomID, Role: RoleSpectator}); err != nil {
			t.Fatalf("Join(spectator) error = %v", err)
		}
	})
	defer fixture.recorder.close()
	if err := fixture.registry.ConnectionClosed(spectator); err != nil {
		t.Fatalf("ConnectionClosed(spectator) error = %v", err)
	}
	if got := fixture.recorder.ofTypes("PLAYER_DISCONNECTED"); len(got) != 0 {
		t.Fatalf("spectator emitted player disconnect = %+v", got)
	}
	if err := fixture.registry.ConnectionOpened(spectator); err != nil {
		t.Fatalf("ConnectionOpened(spectator) error = %v", err)
	}
	for _, user := range fixture.users {
		if err := fixture.registry.ConnectionClosed(user); err != nil {
			t.Fatalf("ConnectionClosed(%s) error = %v", user, err)
		}
	}
	fixture.clock.Advance(allPlayersDisconnectedGrace)
	if fixture.runtime() != nil {
		t.Fatal("connected spectator kept abandoned match alive")
	}
}

func TestSoloHumanDisconnectSuspendsMatchWithCPUSeat(t *testing.T) {
	wallClock := &manualClock{current: time.Date(2026, 9, 5, 16, 0, 0, 0, time.UTC)}
	registry := newTestRegistryWithClock(t, wallClock.Now)
	matchClock := &manualMatchClock{now: wallClock.current}
	registry.setMatchClock(matchClock)
	registry.setMatchRandomSeed(func() (uint64, uint64, error) { return 0xA11CE, 0xB0B, nil })
	host := auth.UserID(matchHostID)
	settings := room.DefaultSettings()
	settings.MaxPlayers = 2
	summary, err := registry.Create(CreateRoomInput{Creator: host, Creation: room.Creation{Title: "presence CPU 방"}, Settings: settings, Team: domain.TeamA})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if _, err := registry.AddCPUPlayer(host, summary.RoomID, domain.TeamB); err != nil {
		t.Fatalf("AddCPUPlayer() error = %v", err)
	}
	if err := registry.ConnectionOpened(host); err != nil {
		t.Fatalf("ConnectionOpened(host) error = %v", err)
	}
	if err := registry.SetReady(host, summary.RoomID, true); err != nil {
		t.Fatalf("SetReady(host) error = %v", err)
	}
	recorder := newEventRecorder(t, registry, host)
	defer recorder.close()
	if err := registry.RequestStart(host, summary.RoomID); err != nil {
		t.Fatalf("RequestStart() error = %v", err)
	}
	matchID := readActiveStart(t, registry, summary.RoomID).MatchID
	if err := registry.ConfirmStart(host, summary.RoomID, matchID); err != nil {
		t.Fatalf("ConfirmStart(host) error = %v", err)
	}
	cpuBefore := len(recorder.ofTypes("CPU_CONTROL_STARTED"))
	if err := registry.ConnectionClosed(host); err != nil {
		t.Fatalf("ConnectionClosed(host) error = %v", err)
	}
	rt := registry.rooms[summary.RoomID].runtime
	if rt == nil || !rt.allPlayersDisconnected {
		t.Fatalf("solo-human disconnect did not suspend match: %+v", rt)
	}
	matchClock.Advance(29 * time.Second)
	if got := len(recorder.ofTypes("CPU_CONTROL_STARTED")); got != cpuBefore {
		t.Fatalf("CPU advanced abandoned match: before=%d after=%d", cpuBefore, got)
	}
	matchClock.Advance(time.Second)
	if _, err := registry.Detail(host, summary.RoomID); !errors.Is(err, ErrRoomNotFound) {
		t.Fatalf("abandoned CPU room survived deadline: %v", err)
	}
}

func TestAllPlayersDisconnectedFor30SecondsInvalidatesAndClosesRoom(t *testing.T) {
	fixture := newPresenceMatchFixture(t)
	defer fixture.recorder.close()
	for _, user := range fixture.users {
		if err := fixture.registry.ConnectionClosed(user); err != nil {
			t.Fatalf("ConnectionClosed(%s) error = %v", user, err)
		}
	}
	fixture.clock.Advance(30 * time.Second)
	if fixture.runtime() != nil {
		t.Fatal("match survived all-player disconnect deadline")
	}
	if _, err := fixture.registry.Detail(fixture.users[0], fixture.roomID); err != ErrRoomNotFound {
		t.Fatalf("Detail() error = %v, want %v", err, ErrRoomNotFound)
	}
	ended := fixture.recorder.ofTypes("GAME_ENDED")
	if len(ended) != 1 || ended[0].Payload.Status != "invalid" || ended[0].Payload.Reason != "all_players_disconnected" {
		t.Fatalf("GAME_ENDED = %+v", ended)
	}
	fixture.store.mu.Lock()
	defer fixture.store.mu.Unlock()
	if len(fixture.store.results) != 0 {
		t.Fatalf("invalid match updated statistics: %+v", fixture.store.results)
	}
	if len(fixture.store.rows) < 2 {
		t.Fatalf("stored rows = %d, want terminal pair", len(fixture.store.rows))
	}
	last := fixture.store.rows[len(fixture.store.rows)-2:]
	if last[0].EventType != "GAME_ENDED" || last[1].EventType != "ROOM_UPDATED" || last[1].Sequence != last[0].Sequence+1 {
		t.Fatalf("terminal rows = %+v", last)
	}
}

func TestAllPlayersDisconnectedWatchdogClosesDuringStorageFailure(t *testing.T) {
	fixture := newPresenceMatchFixture(t)
	defer fixture.recorder.close()
	failing := &fakeEventStore{}
	failing.setFailure(errors.New("disk unavailable"))
	fixture.registry.setEventStoreForTest(failing)
	for _, user := range fixture.users {
		_ = fixture.registry.ConnectionClosed(user)
	}
	if fixture.runtime() == nil || !fixture.runtime().storagePaused || !fixture.runtime().allPlayersDisconnected {
		t.Fatalf("combined outage state = %+v", fixture.runtime())
	}
	fixture.clock.Advance(30 * time.Second)
	if _, err := fixture.registry.Detail(fixture.users[0], fixture.roomID); err != ErrRoomNotFound {
		t.Fatalf("room survived emergency deadline: %v", err)
	}
	ended := fixture.recorder.ofTypes("GAME_ENDED")
	if len(ended) != 1 || ended[0].Payload.Reason != gameEndedReasonDisconnected {
		t.Fatalf("emergency GAME_ENDED = %+v", ended)
	}
}

func TestHostDisconnectDuringStoragePauseDefersOrderedResumeUntilRecovery(t *testing.T) {
	fixture := newPresenceMatchFixture(t)
	defer fixture.recorder.close()
	host := fixture.users[0]
	failing := &fakeEventStore{}
	failing.setFailure(errors.New("temporary disk failure"))
	fixture.registry.setEventStoreForTest(failing)
	if err := fixture.registry.PauseGame(host, fixture.roomID, fixture.matchID, 5); !errors.Is(err, ErrEventStoreUnavailable) {
		t.Fatalf("PauseGame() error = %v, want ErrEventStoreUnavailable", err)
	}
	if err := fixture.registry.ConnectionClosed(host); err != nil {
		t.Fatalf("ConnectionClosed(host) error = %v", err)
	}
	if fixture.runtime().paused {
		t.Fatal("host pause state remained active during storage outage")
	}
	fixture.registry.setEventStoreForTest(&fakeEventStore{})
	fixture.clock.Advance(storageRetryDelays[0])
	if fixture.runtime() == nil || fixture.runtime().storagePaused || fixture.runtime().paused {
		t.Fatalf("post-recovery runtime = %+v", fixture.runtime())
	}
	resumed := fixture.recorder.ofTypes("GAME_RESUMED")
	if len(resumed) < 2 || resumed[0].Payload.Reason != "host_disconnected" || resumed[1].Payload.Reason != "storage_recovered" {
		t.Fatalf("ordered resumes = %+v", resumed)
	}
}

func TestAllDisconnectedStorageRecoveryKeepsGraceAndRestoresWindow(t *testing.T) {
	fixture := newPresenceMatchFixture(t)
	defer fixture.recorder.close()
	failing := &fakeEventStore{}
	failing.setFailure(errors.New("temporary disk failure"))
	fixture.registry.setEventStoreForTest(failing)
	for _, user := range fixture.users {
		_ = fixture.registry.ConnectionClosed(user)
	}
	fixture.registry.setEventStoreForTest(&fakeEventStore{})
	fixture.clock.Advance(storageRetryDelays[0])
	rt := fixture.runtime()
	if rt == nil || rt.storagePaused || !rt.allPlayersDisconnected {
		t.Fatalf("post-recovery grace state = %+v", rt)
	}
	fixture.clock.Advance(28 * time.Second)
	if err := fixture.registry.ConnectionOpened(fixture.users[0]); err != nil {
		t.Fatalf("ConnectionOpened() error = %v", err)
	}
	rt = fixture.runtime()
	if rt == nil || rt.allPlayersDisconnected || (rt.timerKind == "" && !rt.cpuControlled) {
		t.Fatalf("restored presence window = %+v", rt)
	}
	fixture.clock.Advance(time.Second)
	if fixture.runtime() == nil {
		t.Fatal("stale all-disconnected watchdog closed recovered room")
	}
}

func TestReconnectAfterStorageRetriesExhaustedRestartsPersistence(t *testing.T) {
	fixture := newPresenceMatchFixture(t)
	defer fixture.recorder.close()
	failing := &fakeEventStore{}
	failing.setFailure(errors.New("temporary disk failure"))
	fixture.registry.setEventStoreForTest(failing)
	for _, user := range fixture.users {
		_ = fixture.registry.ConnectionClosed(user)
	}
	fixture.clock.Advance(storageRetryDelays[0] + storageRetryDelays[1] + storageRetryDelays[2])
	rt := fixture.runtime()
	if rt == nil || !rt.storagePaused || !rt.allPlayersDisconnected || rt.activeTimer != nil {
		t.Fatalf("exhausted grace state = %+v", rt)
	}

	fixture.registry.setEventStoreForTest(&fakeEventStore{})
	if err := fixture.registry.ConnectionOpened(fixture.users[0]); err != nil {
		t.Fatalf("ConnectionOpened() error = %v", err)
	}
	rt = fixture.runtime()
	if rt == nil || rt.activeTimer == nil || rt.retryAttempt != 0 {
		t.Fatalf("reconnect did not restart storage retry = %+v", rt)
	}
	fixture.clock.Advance(storageRetryDelays[0])
	rt = fixture.runtime()
	if rt == nil || rt.storagePaused || rt.allPlayersDisconnected {
		t.Fatalf("reconnect recovery state = %+v", rt)
	}
}
