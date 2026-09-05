package room

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"buk-yutnori/internal/domain"
)

func TestStartConfirmationWindowIsCanonical(t *testing.T) {
	if StartConfirmationWindow != 10*time.Second {
		t.Fatalf("StartConfirmationWindow = %v, want 10s", StartConfirmationWindow)
	}
}

func TestStartConfirmationBeginsFromEligibleLobby(t *testing.T) {
	lobby := readyTwoPlayerLobby(t)
	startedAt := startConfirmationTestTime()

	confirmation, err := NewStartConfirmation(lobby, "match-1", startedAt)
	if err != nil {
		t.Fatalf("NewStartConfirmation() error = %v", err)
	}

	snapshot := confirmation.Snapshot()
	if snapshot.MatchID != "match-1" {
		t.Fatalf("Snapshot().MatchID = %q, want match-1", snapshot.MatchID)
	}
	if snapshot.Status != StartConfirmationPending {
		t.Fatalf("Snapshot().Status = %q, want %q", snapshot.Status, StartConfirmationPending)
	}
	wantDeadline := startedAt.Add(StartConfirmationWindow)
	if !snapshot.DeadlineAt.Equal(wantDeadline) {
		t.Fatalf("Snapshot().DeadlineAt = %v, want %v", snapshot.DeadlineAt, wantDeadline)
	}
	wantPending := []domain.PlayerID{"player-a", "player-b"}
	if !reflect.DeepEqual(snapshot.PendingPlayerIDs, wantPending) {
		t.Fatalf("Snapshot().PendingPlayerIDs = %v, want %v", snapshot.PendingPlayerIDs, wantPending)
	}

	snapshot.PendingPlayerIDs[0] = "mutated"
	if got := confirmation.Snapshot().PendingPlayerIDs; !reflect.DeepEqual(got, wantPending) {
		t.Fatalf("pending players after snapshot mutation = %v, want %v", got, wantPending)
	}
	assertReady(t, lobby, "player-a", true)
	assertReady(t, lobby, "player-b", true)
}

func TestStartConfirmationCapturesDeterministicallyOrderedRoster(t *testing.T) {
	lobby := mustLobby(t)
	for _, player := range []struct {
		id   domain.PlayerID
		team domain.TeamID
	}{
		{id: "player-d", team: domain.TeamB},
		{id: "player-b", team: domain.TeamB},
		{id: "player-c", team: domain.TeamA},
		{id: "player-a", team: domain.TeamA},
	} {
		mustAddPlayer(t, lobby, player.id, player.team)
		mustSetReady(t, lobby, player.id, true)
	}
	confirmation := mustStartConfirmation(t, lobby)
	wantRoster := []domain.PlayerID{"player-a", "player-b", "player-c", "player-d"}
	if got := confirmation.Snapshot().PendingPlayerIDs; !reflect.DeepEqual(got, wantRoster) {
		t.Fatalf("Snapshot().PendingPlayerIDs = %v, want %v", got, wantRoster)
	}

	mustAddPlayer(t, lobby, "player-new", domain.TeamA)
	beforeDeadline := confirmation.Snapshot().DeadlineAt.Add(-time.Nanosecond)
	if _, err := confirmation.Confirm("player-new", beforeDeadline); !errors.Is(err, ErrStartConfirmationPlayerNotFound) {
		t.Fatalf("Confirm(new lobby player) error = %v, want ErrStartConfirmationPlayerNotFound", err)
	}
	if got := confirmation.Snapshot().PendingPlayerIDs; !reflect.DeepEqual(got, wantRoster) {
		t.Fatalf("captured roster after lobby join = %v, want %v", got, wantRoster)
	}
}

func TestStartConfirmationRejectsInvalidOrIneligibleStartWithoutMutation(t *testing.T) {
	tests := []struct {
		name      string
		newLobby  func(*testing.T) *Lobby
		matchID   domain.MatchID
		wantError error
	}{
		{
			name: "nil lobby",
			newLobby: func(*testing.T) *Lobby {
				return nil
			},
			matchID:   "match-1",
			wantError: ErrInvalidStartConfirmation,
		},
		{
			name: "invalid match ID",
			newLobby: func(t *testing.T) *Lobby {
				return readyTwoPlayerLobby(t)
			},
			wantError: domain.ErrInvalidID,
		},
		{
			name: "not enough players",
			newLobby: func(t *testing.T) *Lobby {
				lobby := mustLobby(t)
				mustAddPlayer(t, lobby, "player-a", domain.TeamA)
				mustSetReady(t, lobby, "player-a", true)
				return lobby
			},
			matchID:   "match-1",
			wantError: ErrStartNotEnoughPlayers,
		},
		{
			name: "unbalanced teams",
			newLobby: func(t *testing.T) *Lobby {
				lobby := mustLobby(t)
				mustAddPlayer(t, lobby, "player-a", domain.TeamA)
				mustAddPlayer(t, lobby, "player-b", domain.TeamA)
				mustSetReady(t, lobby, "player-a", true)
				mustSetReady(t, lobby, "player-b", true)
				return lobby
			},
			matchID:   "match-1",
			wantError: ErrStartTeamsUnbalanced,
		},
		{
			name: "player not ready",
			newLobby: func(t *testing.T) *Lobby {
				lobby := mustLobby(t)
				mustAddPlayer(t, lobby, "player-a", domain.TeamA)
				mustAddPlayer(t, lobby, "player-b", domain.TeamB)
				mustSetReady(t, lobby, "player-a", true)
				return lobby
			},
			matchID:   "match-1",
			wantError: ErrStartPlayersNotReady,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			lobby := test.newLobby(t)
			before := lobbyReadyStates(lobby)

			confirmation, err := NewStartConfirmation(lobby, test.matchID, startConfirmationTestTime())
			if !errors.Is(err, test.wantError) {
				t.Fatalf("NewStartConfirmation() error = %v, want %v", err, test.wantError)
			}
			if confirmation != nil {
				t.Fatalf("NewStartConfirmation() = %+v, want nil", confirmation)
			}
			if got := lobbyReadyStates(lobby); !reflect.DeepEqual(got, before) {
				t.Fatalf("lobby ready states after rejected start = %v, want %v", got, before)
			}
		})
	}
}

func TestStartConfirmationConfirmsEveryPlayerBeforeDeadline(t *testing.T) {
	lobby := readyTwoPlayerLobby(t)
	confirmation := mustStartConfirmation(t, lobby)
	beforeDeadline := confirmation.Snapshot().DeadlineAt.Add(-time.Nanosecond)

	allConfirmed, err := confirmation.Confirm("player-a", beforeDeadline)
	if err != nil {
		t.Fatalf("Confirm(player-a) error = %v", err)
	}
	if allConfirmed {
		t.Fatal("Confirm(player-a) completed attempt with player-b still pending")
	}
	allConfirmed, err = confirmation.Confirm("player-a", beforeDeadline)
	if err != nil {
		t.Fatalf("duplicate Confirm(player-a) error = %v", err)
	}
	if allConfirmed {
		t.Fatal("duplicate Confirm(player-a) completed attempt with player-b still pending")
	}

	allConfirmed, err = confirmation.Confirm("player-b", beforeDeadline)
	if err != nil {
		t.Fatalf("Confirm(player-b) error = %v", err)
	}
	if !allConfirmed {
		t.Fatal("Confirm(player-b) did not complete fully confirmed attempt")
	}
	snapshot := confirmation.Snapshot()
	if snapshot.Status != StartConfirmationConfirmed {
		t.Fatalf("Snapshot().Status = %q, want %q", snapshot.Status, StartConfirmationConfirmed)
	}
	if len(snapshot.PendingPlayerIDs) != 0 {
		t.Fatalf("Snapshot().PendingPlayerIDs = %v, want empty", snapshot.PendingPlayerIDs)
	}
	assertReady(t, lobby, "player-a", true)
	assertReady(t, lobby, "player-b", true)

	if _, err := confirmation.Confirm("player-a", beforeDeadline); !errors.Is(err, ErrStartConfirmationClosed) {
		t.Fatalf("Confirm() after completion error = %v, want ErrStartConfirmationClosed", err)
	}
	if _, err := confirmation.Expire(lobby, snapshot.DeadlineAt); !errors.Is(err, ErrStartConfirmationClosed) {
		t.Fatalf("Expire() after completion error = %v, want ErrStartConfirmationClosed", err)
	}
}

func TestStartConfirmationDisconnectMakesEarlierResponsePendingAgain(t *testing.T) {
	lobby := readyTwoPlayerLobby(t)
	confirmation := mustStartConfirmation(t, lobby)
	beforeDeadline := confirmation.Snapshot().DeadlineAt.Add(-time.Second)
	if complete, err := confirmation.Confirm("player-a", beforeDeadline); err != nil || complete {
		t.Fatalf("Confirm(player-a) = %v, %v", complete, err)
	}
	if err := confirmation.MarkDisconnected("player-a"); err != nil {
		t.Fatalf("MarkDisconnected(player-a) error = %v", err)
	}
	if got, want := confirmation.Snapshot().PendingPlayerIDs, []domain.PlayerID{"player-a", "player-b"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("pending after disconnect = %v, want %v", got, want)
	}
	if err := confirmation.MarkDisconnected("missing-player"); !errors.Is(err, ErrStartConfirmationPlayerNotFound) {
		t.Fatalf("MarkDisconnected(missing) error = %v", err)
	}
}

func TestStartConfirmationRejectsInvalidPlayerAndDeadlineWithoutMutation(t *testing.T) {
	lobby := readyTwoPlayerLobby(t)
	confirmation := mustStartConfirmation(t, lobby)
	deadline := confirmation.Snapshot().DeadlineAt
	wantPending := []domain.PlayerID{"player-a", "player-b"}

	if _, err := confirmation.Confirm("", deadline.Add(-time.Second)); !errors.Is(err, domain.ErrInvalidID) {
		t.Fatalf("Confirm(empty player) error = %v, want domain.ErrInvalidID", err)
	}
	if _, err := confirmation.Confirm("missing-player", deadline.Add(-time.Second)); !errors.Is(err, ErrStartConfirmationPlayerNotFound) {
		t.Fatalf("Confirm(missing player) error = %v, want ErrStartConfirmationPlayerNotFound", err)
	}
	if _, err := confirmation.Confirm("player-a", deadline); !errors.Is(err, ErrStartConfirmationExpired) {
		t.Fatalf("Confirm() at deadline error = %v, want ErrStartConfirmationExpired", err)
	}
	if _, err := confirmation.Confirm("player-a", deadline.Add(time.Nanosecond)); !errors.Is(err, ErrStartConfirmationExpired) {
		t.Fatalf("Confirm() after deadline error = %v, want ErrStartConfirmationExpired", err)
	}

	snapshot := confirmation.Snapshot()
	if snapshot.Status != StartConfirmationPending {
		t.Fatalf("Snapshot().Status = %q, want %q", snapshot.Status, StartConfirmationPending)
	}
	if !reflect.DeepEqual(snapshot.PendingPlayerIDs, wantPending) {
		t.Fatalf("Snapshot().PendingPlayerIDs = %v, want %v", snapshot.PendingPlayerIDs, wantPending)
	}
	assertReady(t, lobby, "player-a", true)
	assertReady(t, lobby, "player-b", true)
}

func TestStartConfirmationRejectsPrematureExpiryWithoutMutation(t *testing.T) {
	lobby := readyTwoPlayerLobby(t)
	confirmation := mustStartConfirmation(t, lobby)
	deadline := confirmation.Snapshot().DeadlineAt

	nonresponders, err := confirmation.Expire(lobby, deadline.Add(-time.Nanosecond))
	if !errors.Is(err, ErrStartConfirmationNotExpired) {
		t.Fatalf("Expire() before deadline error = %v, want ErrStartConfirmationNotExpired", err)
	}
	if nonresponders != nil {
		t.Fatalf("Expire() before deadline nonresponders = %v, want nil", nonresponders)
	}
	if confirmation.Snapshot().Status != StartConfirmationPending {
		t.Fatalf("Snapshot().Status after premature expiry = %q, want %q", confirmation.Snapshot().Status, StartConfirmationPending)
	}
	assertReady(t, lobby, "player-a", true)
	assertReady(t, lobby, "player-b", true)
}

func TestStartConfirmationExpiryRemovesNonrespondersAndClearsRemainingReadyState(t *testing.T) {
	lobby := mustLobby(t)
	mustAddPlayer(t, lobby, "player-d", domain.TeamB)
	mustAddPlayer(t, lobby, "player-b", domain.TeamB)
	mustAddPlayer(t, lobby, "player-c", domain.TeamA)
	mustAddPlayer(t, lobby, "player-a", domain.TeamA)
	for _, id := range []domain.PlayerID{"player-a", "player-b", "player-c", "player-d"} {
		mustSetReady(t, lobby, id, true)
	}
	confirmation := mustStartConfirmation(t, lobby)
	beforeDeadline := confirmation.Snapshot().DeadlineAt.Add(-time.Nanosecond)
	for _, id := range []domain.PlayerID{"player-a", "player-c", "player-d"} {
		if _, err := confirmation.Confirm(id, beforeDeadline); err != nil {
			t.Fatalf("Confirm(%q) error = %v", id, err)
		}
	}

	nonresponders, err := confirmation.Expire(lobby, confirmation.Snapshot().DeadlineAt)
	if err != nil {
		t.Fatalf("Expire() error = %v", err)
	}
	wantNonresponders := []domain.PlayerID{"player-b"}
	if !reflect.DeepEqual(nonresponders, wantNonresponders) {
		t.Fatalf("Expire() nonresponders = %v, want %v", nonresponders, wantNonresponders)
	}
	if _, ok := lobby.Player("player-b"); ok {
		t.Fatal("nonresponding player remains in lobby after expiry")
	}
	for _, id := range []domain.PlayerID{"player-a", "player-c", "player-d"} {
		assertReady(t, lobby, id, false)
	}
	snapshot := confirmation.Snapshot()
	if snapshot.Status != StartConfirmationFailed {
		t.Fatalf("Snapshot().Status = %q, want %q", snapshot.Status, StartConfirmationFailed)
	}
	if !reflect.DeepEqual(snapshot.PendingPlayerIDs, wantNonresponders) {
		t.Fatalf("Snapshot().PendingPlayerIDs = %v, want %v", snapshot.PendingPlayerIDs, wantNonresponders)
	}
	nonresponders[0] = "mutated"
	if got := confirmation.Snapshot().PendingPlayerIDs; !reflect.DeepEqual(got, wantNonresponders) {
		t.Fatalf("pending players after result mutation = %v, want %v", got, wantNonresponders)
	}
	if _, err := confirmation.Confirm("player-b", beforeDeadline); !errors.Is(err, ErrStartConfirmationExpired) {
		t.Fatalf("late Confirm() after failed attempt error = %v, want ErrStartConfirmationExpired", err)
	}
	if _, err := confirmation.Expire(lobby, confirmation.Snapshot().DeadlineAt); !errors.Is(err, ErrStartConfirmationClosed) {
		t.Fatalf("second Expire() error = %v, want ErrStartConfirmationClosed", err)
	}
}

func TestStartConfirmationExpiryMayRemoveEntireRoster(t *testing.T) {
	lobby := readyTwoPlayerLobby(t)
	confirmation := mustStartConfirmation(t, lobby)

	nonresponders, err := confirmation.Expire(lobby, confirmation.Snapshot().DeadlineAt)
	if err != nil {
		t.Fatalf("Expire() error = %v", err)
	}
	wantNonresponders := []domain.PlayerID{"player-a", "player-b"}
	if !reflect.DeepEqual(nonresponders, wantNonresponders) {
		t.Fatalf("Expire() nonresponders = %v, want %v", nonresponders, wantNonresponders)
	}
	for _, id := range wantNonresponders {
		if _, ok := lobby.Player(id); ok {
			t.Fatalf("nonresponding player %q remains after expiry", id)
		}
	}
	if confirmation.Snapshot().Status != StartConfirmationFailed {
		t.Fatalf("Snapshot().Status = %q, want %q", confirmation.Snapshot().Status, StartConfirmationFailed)
	}
}

func TestStartConfirmationExpiryFailureLeavesAttemptAndLobbyUnchanged(t *testing.T) {
	lobby := readyTwoPlayerLobby(t)
	confirmation := mustStartConfirmation(t, lobby)
	beforeDeadline := confirmation.Snapshot().DeadlineAt.Add(-time.Nanosecond)
	if _, err := confirmation.Confirm("player-a", beforeDeadline); err != nil {
		t.Fatalf("Confirm(player-a) error = %v", err)
	}
	if err := lobby.RemovePlayer("player-b"); err != nil {
		t.Fatalf("RemovePlayer(player-b) error = %v", err)
	}

	nonresponders, err := confirmation.Expire(lobby, confirmation.Snapshot().DeadlineAt)
	if !errors.Is(err, ErrPlayerNotFound) {
		t.Fatalf("Expire() with drifted lobby error = %v, want ErrPlayerNotFound", err)
	}
	if nonresponders != nil {
		t.Fatalf("Expire() with drifted lobby nonresponders = %v, want nil", nonresponders)
	}
	if confirmation.Snapshot().Status != StartConfirmationPending {
		t.Fatalf("Snapshot().Status after failed expiry = %q, want %q", confirmation.Snapshot().Status, StartConfirmationPending)
	}
	assertReady(t, lobby, "player-a", true)
}

func TestStartConfirmationRejectsInvalidAttemptUsage(t *testing.T) {
	var confirmation *StartConfirmation
	if snapshot := confirmation.Snapshot(); !reflect.DeepEqual(snapshot, StartConfirmationSnapshot{}) {
		t.Fatalf("nil Snapshot() = %+v, want zero value", snapshot)
	}
	if _, err := confirmation.Confirm("player-a", startConfirmationTestTime()); !errors.Is(err, ErrInvalidStartConfirmation) {
		t.Fatalf("nil Confirm() error = %v, want ErrInvalidStartConfirmation", err)
	}
	lobby := readyTwoPlayerLobby(t)
	if _, err := confirmation.Expire(lobby, startConfirmationTestTime()); !errors.Is(err, ErrInvalidStartConfirmation) {
		t.Fatalf("nil Expire() error = %v, want ErrInvalidStartConfirmation", err)
	}

	confirmation = mustStartConfirmation(t, lobby)
	if _, err := confirmation.Expire(nil, confirmation.Snapshot().DeadlineAt); !errors.Is(err, ErrInvalidStartConfirmation) {
		t.Fatalf("Expire(nil lobby) error = %v, want ErrInvalidStartConfirmation", err)
	}
	if confirmation.Snapshot().Status != StartConfirmationPending {
		t.Fatalf("Snapshot().Status after nil lobby = %q, want %q", confirmation.Snapshot().Status, StartConfirmationPending)
	}
	assertReady(t, lobby, "player-a", true)
	assertReady(t, lobby, "player-b", true)
}

func mustStartConfirmation(t *testing.T, lobby *Lobby) *StartConfirmation {
	t.Helper()

	confirmation, err := NewStartConfirmation(lobby, "match-1", startConfirmationTestTime())
	if err != nil {
		t.Fatalf("NewStartConfirmation() error = %v", err)
	}
	return confirmation
}

func startConfirmationTestTime() time.Time {
	return time.Date(2026, time.August, 21, 12, 0, 0, 0, time.UTC)
}

func lobbyReadyStates(lobby *Lobby) map[domain.PlayerID]bool {
	if lobby == nil {
		return nil
	}
	states := make(map[domain.PlayerID]bool)
	for id, player := range lobby.players {
		states[id] = player.Ready
	}
	return states
}
