package application

import (
	"errors"
	"testing"
	"time"

	"buk-yutnori/internal/auth"
	"buk-yutnori/internal/domain"
	"buk-yutnori/internal/domain/room"
)

func newTestRegistryWithClock(t *testing.T, clock func() time.Time) *RoomRegistry {
	t.Helper()

	registry, err := NewRoomRegistry(clock)
	if err != nil {
		t.Fatalf("NewRoomRegistry(clock) error = %v", err)
	}
	return registry
}

type startFixture struct {
	registry *RoomRegistry
	roomID   domain.RoomID
	matchID  func() domain.MatchID
	clock    *manualClock
}

type manualClock struct{ current time.Time }

func (c *manualClock) Now() time.Time          { return c.current }
func (c *manualClock) Advance(d time.Duration) { c.current = c.current.Add(d) }

func newStartFixture(t *testing.T, players int) startFixture {
	t.Helper()

	clock := &manualClock{current: time.Date(2026, 8, 23, 9, 0, 0, 0, time.UTC)}
	registry := newTestRegistryWithClock(t, clock.Now)

	summary, err := registry.Create(CreateRoomInput{
		Creator:  lobbyCreatorID,
		Creation: room.Creation{Title: "시작 방"},
		Settings: room.DefaultSettings(),
		Team:     domain.TeamA,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	for index := 1; index < players; index++ {
		user := auth.UserID(startRosterIDs[index%len(startRosterIDs)])
		team := domain.TeamB
		if index%2 == 0 {
			team = domain.TeamA
		}
		if _, err := registry.Join(JoinRoomInput{
			User:   user,
			RoomID: summary.RoomID,
			Role:   RolePlayer,
			Team:   team,
		}); err != nil {
			t.Fatalf("Join(player %d) error = %v", index, err)
		}
	}
	for _, id := range startRosterIDs[:players] {
		if err := registry.SetReady(auth.UserID(id), summary.RoomID, true); err != nil {
			t.Fatalf("SetReady(%s) error = %v", id, err)
		}
	}
	return startFixture{
		registry: registry,
		roomID:   summary.RoomID,
		matchID: func() domain.MatchID {
			_, _, matchID := readStartState(registry, summary.RoomID)
			return matchID
		},
		clock: clock,
	}
}

var startRosterIDs = []string{
	"usr_MzMzMzMzMzMzMzMzMzMzMw",
	"usr_RERERERERERERERERERERA",
	"usr_VVVVVVVVVVVVVVVVVVVVVQ",
	"usr_ZmZmZmZmZmZmZmZmZmZmZg",
	"usr_EREREREREREREREREREREQ",
	"usr_IiIiIiIiIiIiIiIiIiIiIg",
	"usr_GhoaGhoaGhoaGhoaGhoaGg",
	"usr_Hx8fHx8fHx8fHx8fHx8fHw",
}

const startLobbyOutsiderID = "usr_7u7u7u7u7u7u7u7u7u7u7g"

func TestRequestStartRequiresHostAndEligibility(t *testing.T) {
	fixture := newStartFixture(t, 2)

	if err := fixture.registry.RequestStart(auth.UserID(startRosterIDs[1]), fixture.roomID); !errors.Is(err, ErrNotRoomHost) {
		t.Fatalf("RequestStart(non-host) error = %v, want ErrNotRoomHost", err)
	}
	if err := fixture.registry.RequestStart(auth.UserID(startLobbyOutsiderID), fixture.roomID); !errors.Is(err, ErrNotRoomHost) {
		t.Fatalf("RequestStart(outsider) error = %v, want ErrNotRoomHost", err)
	}
	if err := fixture.registry.RequestStart(lobbyCreatorID, domain.RoomID("00000000000000000000000000000000")); !errors.Is(err, ErrRoomNotFound) {
		t.Fatalf("RequestStart(unknown room) error = %v, want ErrRoomNotFound", err)
	}

	if err := fixture.registry.SetReady(lobbyCreatorID, fixture.roomID, false); err != nil {
		t.Fatalf("SetReady(false precondition) error = %v", err)
	}
	if err := fixture.registry.RequestStart(lobbyCreatorID, fixture.roomID); !errors.Is(err, room.ErrStartPlayersNotReady) {
		t.Fatalf("RequestStart(unready) error = %v, want ErrStartPlayersNotReady", err)
	}
	if err := fixture.registry.SetReady(lobbyCreatorID, fixture.roomID, true); err != nil {
		t.Fatalf("SetReady(true restore) error = %v", err)
	}
	if err := fixture.registry.RequestStart(lobbyCreatorID, fixture.roomID); err != nil {
		t.Fatalf("RequestStart(eligible) error = %v", err)
	}
	if fixture.matchID() == "" {
		t.Fatal("match id was not generated for the confirmation window")
	}
	resolveWindow(t, fixture)
}

func TestStartWindowBlocksLobbyMutationsAndRepeatStarts(t *testing.T) {
	fixture := newStartFixture(t, 2)
	if err := fixture.registry.RequestStart(lobbyCreatorID, fixture.roomID); err != nil {
		t.Fatalf("RequestStart() error = %v", err)
	}

	if err := fixture.registry.ChangeTeam(lobbyCreatorID, fixture.roomID, domain.TeamB); !errors.Is(err, ErrStartAlreadyRequested) {
		t.Fatalf("ChangeTeam(during window) error = %v, want ErrStartAlreadyRequested", err)
	}
	if err := fixture.registry.SetReady(lobbyCreatorID, fixture.roomID, false); !errors.Is(err, ErrStartAlreadyRequested) {
		t.Fatalf("SetReady(during window) error = %v, want ErrStartAlreadyRequested", err)
	}
	if _, err := fixture.registry.Join(JoinRoomInput{
		User:   auth.UserID(startLobbyOutsiderID),
		RoomID: fixture.roomID,
		Role:   RoleSpectator,
	}); !errors.Is(err, ErrStartAlreadyRequested) {
		t.Fatalf("Join(during window) error = %v, want ErrStartAlreadyRequested", err)
	}
	if err := fixture.registry.RequestStart(lobbyCreatorID, fixture.roomID); !errors.Is(err, ErrStartAlreadyRequested) {
		t.Fatalf("repeat RequestStart error = %v, want ErrStartAlreadyRequested", err)
	}

	membership, err := fixture.registry.Membership(lobbyCreatorID, fixture.roomID)
	if err != nil || !membership.Ready {
		t.Fatalf("Membership(during window) = %+v error = %v, want readable ready state", membership, err)
	}
	resolveWindow(t, fixture)
}

func TestConfirmStartLifecycleReachesStartedState(t *testing.T) {
	fixture := newStartFixture(t, 2)
	second := startRosterIDs[1]

	if err := fixture.registry.RequestStart(lobbyCreatorID, fixture.roomID); err != nil {
		t.Fatalf("RequestStart() error = %v", err)
	}
	activeMatchID := fixture.matchID()

	wrongScope := activeMatchID + "00"
	if err := fixture.registry.ConfirmStart(auth.UserID(second), fixture.roomID, domain.MatchID(wrongScope)); !errors.Is(err, ErrMatchScopeMismatch) {
		t.Fatalf("ConfirmStart(wrong scope) error = %v, want ErrMatchScopeMismatch", err)
	}

	fixture.clock.Advance(3 * time.Second)
	if err := fixture.registry.ConfirmStart(auth.UserID(second), fixture.roomID, activeMatchID); err != nil {
		t.Fatalf("ConfirmStart(second) error = %v", err)
	}
	if len(fixture.registry.List()) != 1 {
		t.Fatal("room must stay listed during confirmation")
	}

	if err := fixture.registry.ConfirmStart(auth.UserID(second), fixture.roomID, activeMatchID); err != nil {
		t.Fatalf("repeat ConfirmStart before completion error = %v", err)
	}

	if err := fixture.registry.ConfirmStart(lobbyCreatorID, fixture.roomID, activeMatchID); err != nil {
		t.Fatalf("ConfirmStart(host completes) error = %v", err)
	}
	if entry := fixture.registry.rooms[fixture.roomID]; !entry.started {
		t.Fatal("room did not enter started state after full confirmation")
	}

	if err := fixture.registry.ConfirmStart(auth.UserID(second), fixture.roomID, activeMatchID); !errors.Is(err, ErrNoActiveStartConfirmation) {
		t.Fatalf("ConfirmStart(after success) error = %v, want ErrNoActiveStartConfirmation", err)
	}
	if err := fixture.registry.SetReady(lobbyCreatorID, fixture.roomID, false); !errors.Is(err, ErrRoomAlreadyStarted) {
		t.Fatalf("SetReady(after started) error = %v, want ErrRoomAlreadyStarted", err)
	}
	if err := fixture.registry.RequestStart(lobbyCreatorID, fixture.roomID); !errors.Is(err, ErrRoomAlreadyStarted) {
		t.Fatalf("RequestStart(after started) error = %v, want ErrRoomAlreadyStarted", err)
	}
}

func TestExpireAppliesCanonicalSanctionsAndAllowsRestart(t *testing.T) {
	fixture := newStartFixture(t, 4)
	third := auth.UserID(startRosterIDs[2])

	if err := fixture.registry.RequestStart(lobbyCreatorID, fixture.roomID); err != nil {
		t.Fatalf("RequestStart() error = %v", err)
	}
	activeMatchID := fixture.matchID()
	fixture.clock.Advance(4 * time.Second)
	if err := fixture.registry.ConfirmStart(third, fixture.roomID, activeMatchID); err != nil {
		t.Fatalf("ConfirmStart(one responder) error = %v", err)
	}

	fixture.clock.Advance(room.StartConfirmationWindow)
	if err := fixture.registry.ExpireStartConfirmation(fixture.roomID); err != nil {
		t.Fatalf("ExpireStartConfirmation() error = %v", err)
	}

	if _, err := fixture.registry.Membership(auth.UserID(startRosterIDs[0]), fixture.roomID); !errors.Is(err, ErrRoomNotFound) {
		t.Fatalf("Membership(nonresponder 1) error = %v, want removed", err)
	}
	if _, err := fixture.registry.Membership(third, fixture.roomID); err != nil {
		t.Fatalf("Membership(responder) error = %v, want retained", err)
	}
	retained, err := fixture.registry.Membership(third, fixture.roomID)
	if err != nil || retained.Ready {
		t.Fatalf("retained membership = %+v error = %v, want ready reset", retained, err)
	}

	if err := fixture.registry.SetReady(third, fixture.roomID, true); err != nil {
		t.Fatalf("SetReady(after expiry sanctions) error = %v", err)
	}
	if err := fixture.registry.RequestStart(lobbyCreatorID, fixture.roomID); !errors.Is(err, room.ErrStartNotEnoughPlayers) {
		t.Fatalf("RequestStart(restart without rebalance) error = %v, want ErrStartNotEnoughPlayers", err)
	}
}

func TestExpireBeforeDeadlineIsRejectedWithoutMutation(t *testing.T) {
	fixture := newStartFixture(t, 2)
	if err := fixture.registry.RequestStart(lobbyCreatorID, fixture.roomID); err != nil {
		t.Fatalf("RequestStart() error = %v", err)
	}

	fixture.clock.Advance(2 * time.Second)
	before := fixture.registry.Membership
	if err := fixture.registry.ExpireStartConfirmation(fixture.roomID); !errors.Is(err, room.ErrStartConfirmationNotExpired) {
		t.Fatalf("ExpireStartConfirmation(premature) error = %v, want ErrStartConfirmationNotExpired", err)
	}
	if _, err := before(lobbyCreatorID, fixture.roomID); err != nil {
		t.Fatalf("premature expiry mutated roster: %v", err)
	}
	resolveWindow(t, fixture)
}

func TestLateConfirmationDoesNotResurrectCancelledStart(t *testing.T) {
	fixture := newStartFixture(t, 2)

	if err := fixture.registry.RequestStart(lobbyCreatorID, fixture.roomID); err != nil {
		t.Fatalf("RequestStart() error = %v", err)
	}
	fixture.clock.Advance(room.StartConfirmationWindow + time.Second)

	if err := fixture.registry.ConfirmStart(auth.UserID(startRosterIDs[1]), fixture.roomID, fixture.matchID()); !errors.Is(err, room.ErrStartConfirmationExpired) {
		t.Fatalf("late ConfirmStart error = %v, want ErrStartConfirmationExpired", err)
	}
	resolveWindow(t, fixture)
}

// readStartState reads the room's confirmation internals under the registry
// lock so tests never race the background expiry timer.
func readStartState(registry *RoomRegistry, roomID domain.RoomID) (bool, bool, domain.MatchID) {
	registry.mutex.Lock()
	defer registry.mutex.Unlock()

	entry := registry.rooms[roomID]
	if entry == nil {
		return false, false, ""
	}
	if entry.confirmation == nil {
		return false, entry.started, ""
	}
	snapshot := entry.confirmation.Snapshot()
	return snapshot.Status == room.StartConfirmationPending, entry.started, snapshot.MatchID
}

// resolveWindow deterministically closes an open start confirmation window so
// no real 10-second background timer outlives the test.
func resolveWindow(t *testing.T, fixture startFixture) {
	t.Helper()

	fixture.clock.Advance(room.StartConfirmationWindow + time.Second)
	if err := fixture.registry.ExpireStartConfirmation(fixture.roomID); err != nil {
		t.Fatalf("resolveWindow() error = %v", err)
	}
}

// TestStartConfirmationRealTimerExpiresAutomatically seals the production
// expiry path: time.AfterFunc scheduling, premature-fire re-arm, and the
// mutex-serialized transition. It uses the real clock and therefore takes
// slightly more than the canonical 10-second window.
func TestStartConfirmationRealTimerExpiresAutomatically(t *testing.T) {
	registry := newTestRegistryWithClock(t, time.Now)

	summary, err := registry.Create(CreateRoomInput{
		Creator:  lobbyCreatorID,
		Creation: room.Creation{Title: "실시계 방"},
		Settings: room.DefaultSettings(),
		Team:     domain.TeamA,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	responder := auth.UserID(startRosterIDs[1])
	if _, err := registry.Join(JoinRoomInput{
		User:   responder,
		RoomID: summary.RoomID,
		Role:   RolePlayer,
		Team:   domain.TeamB,
	}); err != nil {
		t.Fatalf("Join(responder) error = %v", err)
	}
	for _, user := range []auth.UserID{lobbyCreatorID, responder} {
		if err := registry.SetReady(user, summary.RoomID, true); err != nil {
			t.Fatalf("SetReady(%s) error = %v", user, err)
		}
	}
	if err := registry.RequestStart(lobbyCreatorID, summary.RoomID); err != nil {
		t.Fatalf("RequestStart() error = %v", err)
	}
	_, _, matchID := readStartState(registry, summary.RoomID)
	if err := registry.ConfirmStart(responder, summary.RoomID, matchID); err != nil {
		t.Fatalf("ConfirmStart(responder) error = %v", err)
	}

	deadline := time.Now().Add(15 * time.Second)
	for {
		pending, _, _ := readStartState(registry, summary.RoomID)
		if !pending {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("background timer did not clear the confirmation window in time")
		}
		time.Sleep(100 * time.Millisecond)
	}

	if _, err := registry.Membership(lobbyCreatorID, summary.RoomID); !errors.Is(err, ErrRoomNotFound) {
		t.Fatalf("Membership(nonresponder host) error = %v, want removed by sanctions", err)
	}
	retained, err := registry.Membership(responder, summary.RoomID)
	if err != nil || retained.Ready {
		t.Fatalf("retained membership = %+v error = %v, want ready reset only", retained, err)
	}
}
