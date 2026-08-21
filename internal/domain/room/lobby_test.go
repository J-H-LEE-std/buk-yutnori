package room

import (
	"errors"
	"testing"

	"buk-yutnori/internal/domain"
)

func TestLobbyNewPlayerStartsNotReady(t *testing.T) {
	lobby := mustLobby(t)
	mustAddPlayer(t, lobby, "player-a", domain.TeamA)

	player := mustPlayer(t, lobby, "player-a")
	if player.Ready {
		t.Fatal("new player Ready = true, want false")
	}
}

func TestLobbySettingChangePreservesReadyState(t *testing.T) {
	lobby := readyTwoPlayerLobby(t)

	settings := lobby.Settings()
	settings.MoveTimeoutSeconds = 120
	if err := lobby.UpdateSettings(settings); err != nil {
		t.Fatalf("UpdateSettings() error = %v", err)
	}

	assertReady(t, lobby, "player-a", true)
	assertReady(t, lobby, "player-b", true)
	if err := lobby.ValidateStart(); err != nil {
		t.Fatalf("ValidateStart() after setting change error = %v", err)
	}
}

func TestLobbyTeamCompositionChangesPreserveExistingReadyState(t *testing.T) {
	lobby := readyTwoPlayerLobby(t)

	mustAddPlayer(t, lobby, "player-c", domain.TeamA)
	assertReady(t, lobby, "player-a", true)
	assertReady(t, lobby, "player-b", true)
	assertReady(t, lobby, "player-c", false)

	if err := lobby.ChangeTeam("player-c", domain.TeamB); err != nil {
		t.Fatalf("ChangeTeam() error = %v", err)
	}
	assertReady(t, lobby, "player-a", true)
	assertReady(t, lobby, "player-b", true)
	assertReady(t, lobby, "player-c", false)

	if err := lobby.RemovePlayer("player-c"); err != nil {
		t.Fatalf("RemovePlayer() error = %v", err)
	}
	assertReady(t, lobby, "player-a", true)
	assertReady(t, lobby, "player-b", true)
}

func TestLobbyReadyPlayerCannotChangeTeamUntilNotReady(t *testing.T) {
	lobby := mustLobby(t)
	mustAddPlayer(t, lobby, "player-a", domain.TeamA)
	mustSetReady(t, lobby, "player-a", true)

	err := lobby.ChangeTeam("player-a", domain.TeamB)
	if !errors.Is(err, ErrReadyPlayerTeamChange) {
		t.Fatalf("ChangeTeam() error = %v, want ErrReadyPlayerTeamChange", err)
	}
	player := mustPlayer(t, lobby, "player-a")
	if player.Team != domain.TeamA || !player.Ready {
		t.Fatalf("player after rejected team change = %+v, want team A and ready", player)
	}

	if err := lobby.ChangeTeam("player-a", domain.TeamA); err != nil {
		t.Fatalf("idempotent ChangeTeam() error = %v", err)
	}
	mustSetReady(t, lobby, "player-a", false)
	if err := lobby.ChangeTeam("player-a", domain.TeamB); err != nil {
		t.Fatalf("ChangeTeam() after unready error = %v", err)
	}
	player = mustPlayer(t, lobby, "player-a")
	if player.Team != domain.TeamB || player.Ready {
		t.Fatalf("player after allowed team change = %+v, want team B and not ready", player)
	}
}

func TestLobbyValidateStartRechecksCurrentState(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(*testing.T, *Lobby)
		wantErr error
	}{
		{
			name: "not enough players",
			prepare: func(t *testing.T, lobby *Lobby) {
				mustAddPlayer(t, lobby, "player-a", domain.TeamA)
				mustSetReady(t, lobby, "player-a", true)
			},
			wantErr: ErrStartNotEnoughPlayers,
		},
		{
			name: "unbalanced teams",
			prepare: func(t *testing.T, lobby *Lobby) {
				mustAddPlayer(t, lobby, "player-a", domain.TeamA)
				mustAddPlayer(t, lobby, "player-b", domain.TeamA)
				mustSetReady(t, lobby, "player-a", true)
				mustSetReady(t, lobby, "player-b", true)
			},
			wantErr: ErrStartTeamsUnbalanced,
		},
		{
			name: "player not ready",
			prepare: func(t *testing.T, lobby *Lobby) {
				mustAddPlayer(t, lobby, "player-a", domain.TeamA)
				mustAddPlayer(t, lobby, "player-b", domain.TeamB)
				mustSetReady(t, lobby, "player-a", true)
			},
			wantErr: ErrStartPlayersNotReady,
		},
		{
			name: "eligible",
			prepare: func(t *testing.T, lobby *Lobby) {
				mustAddPlayer(t, lobby, "player-a", domain.TeamA)
				mustAddPlayer(t, lobby, "player-b", domain.TeamB)
				mustSetReady(t, lobby, "player-a", true)
				mustSetReady(t, lobby, "player-b", true)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			lobby := mustLobby(t)
			test.prepare(t, lobby)

			err := lobby.ValidateStart()
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("ValidateStart() error = %v, want %v", err, test.wantErr)
			}
		})
	}
}

func TestLobbyFailedStartConfirmationIsAtomic(t *testing.T) {
	lobby := readyTwoPlayerLobby(t)
	mustAddPlayer(t, lobby, "player-c", domain.TeamA)
	mustSetReady(t, lobby, "player-c", true)

	err := lobby.FailStartConfirmation(nil)
	if !errors.Is(err, ErrNoStartConfirmationFailure) {
		t.Fatalf("FailStartConfirmation() empty error = %v, want ErrNoStartConfirmationFailure", err)
	}
	assertReady(t, lobby, "player-a", true)
	assertReady(t, lobby, "player-b", true)
	assertReady(t, lobby, "player-c", true)

	err = lobby.FailStartConfirmation([]domain.PlayerID{"missing-player"})
	if !errors.Is(err, ErrPlayerNotFound) {
		t.Fatalf("FailStartConfirmation() invalid player error = %v, want ErrPlayerNotFound", err)
	}
	assertReady(t, lobby, "player-a", true)
	assertReady(t, lobby, "player-b", true)
	assertReady(t, lobby, "player-c", true)

	if err := lobby.FailStartConfirmation([]domain.PlayerID{"player-c"}); err != nil {
		t.Fatalf("FailStartConfirmation() error = %v", err)
	}
	if _, ok := lobby.Player("player-c"); ok {
		t.Fatal("nonresponding player still exists after failed start confirmation")
	}
	assertReady(t, lobby, "player-a", false)
	assertReady(t, lobby, "player-b", false)
}

func TestLobbyRejectsInvalidMutationsWithoutChangingState(t *testing.T) {
	lobby := mustLobby(t)
	mustAddPlayer(t, lobby, "player-a", domain.TeamA)
	mustSetReady(t, lobby, "player-a", true)

	invalidSettings := lobby.Settings()
	invalidSettings.MaxPlayers = 3
	if err := lobby.UpdateSettings(invalidSettings); !errors.Is(err, ErrInvalidSettings) {
		t.Fatalf("UpdateSettings() error = %v, want ErrInvalidSettings", err)
	}
	if got := lobby.Settings(); got != DefaultSettings() {
		t.Fatalf("Settings() after rejected update = %+v, want defaults", got)
	}
	assertReady(t, lobby, "player-a", true)

	if err := lobby.SetReady("missing-player", true); !errors.Is(err, ErrPlayerNotFound) {
		t.Fatalf("SetReady() error = %v, want ErrPlayerNotFound", err)
	}
	if err := lobby.RemovePlayer("missing-player"); !errors.Is(err, ErrPlayerNotFound) {
		t.Fatalf("RemovePlayer() error = %v, want ErrPlayerNotFound", err)
	}
}

func TestLobbyRejectsDuplicatePlayerWithoutChangingState(t *testing.T) {
	lobby := mustLobby(t)
	mustAddPlayer(t, lobby, "player-a", domain.TeamA)
	mustSetReady(t, lobby, "player-a", true)

	err := lobby.AddPlayer("player-a", domain.TeamB)
	if !errors.Is(err, ErrPlayerAlreadyExists) {
		t.Fatalf("AddPlayer() duplicate error = %v, want ErrPlayerAlreadyExists", err)
	}
	player := mustPlayer(t, lobby, "player-a")
	if player.Team != domain.TeamA || !player.Ready {
		t.Fatalf("player after rejected duplicate = %+v, want team A and ready", player)
	}
}

func TestLobbyRejectsEmptyPlayerIDWithoutChangingState(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Lobby) error
	}{
		{
			name: "add player",
			mutate: func(lobby *Lobby) error {
				return lobby.AddPlayer("", domain.TeamB)
			},
		},
		{
			name: "remove player",
			mutate: func(lobby *Lobby) error {
				return lobby.RemovePlayer("")
			},
		},
		{
			name: "set ready",
			mutate: func(lobby *Lobby) error {
				return lobby.SetReady("", false)
			},
		},
		{
			name: "change team",
			mutate: func(lobby *Lobby) error {
				return lobby.ChangeTeam("", domain.TeamB)
			},
		},
		{
			name: "fail start confirmation",
			mutate: func(lobby *Lobby) error {
				return lobby.FailStartConfirmation([]domain.PlayerID{""})
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			lobby := mustLobby(t)
			mustAddPlayer(t, lobby, "player-a", domain.TeamA)
			mustSetReady(t, lobby, "player-a", true)

			if err := test.mutate(lobby); !errors.Is(err, domain.ErrInvalidID) {
				t.Fatalf("mutation error = %v, want domain.ErrInvalidID", err)
			}
			player := mustPlayer(t, lobby, "player-a")
			if player.Team != domain.TeamA || !player.Ready {
				t.Fatalf("player after rejected mutation = %+v, want team A and ready", player)
			}
			if _, ok := lobby.Player(""); ok {
				t.Fatal("empty player ID exists after rejected mutation")
			}
		})
	}
}

func TestLobbyRejectsInvalidTeamWithoutChangingState(t *testing.T) {
	invalidTeam := domain.TeamID("invalid")
	tests := []struct {
		name   string
		mutate func(*Lobby) error
	}{
		{
			name: "add player",
			mutate: func(lobby *Lobby) error {
				return lobby.AddPlayer("player-b", invalidTeam)
			},
		},
		{
			name: "change team",
			mutate: func(lobby *Lobby) error {
				return lobby.ChangeTeam("player-a", invalidTeam)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			lobby := mustLobby(t)
			mustAddPlayer(t, lobby, "player-a", domain.TeamA)
			mustSetReady(t, lobby, "player-a", true)

			if err := test.mutate(lobby); !errors.Is(err, domain.ErrInvalidEnumValue) {
				t.Fatalf("mutation error = %v, want domain.ErrInvalidEnumValue", err)
			}
			player := mustPlayer(t, lobby, "player-a")
			if player.Team != domain.TeamA || !player.Ready {
				t.Fatalf("player after rejected mutation = %+v, want team A and ready", player)
			}
			if _, ok := lobby.Player("player-b"); ok {
				t.Fatal("player with invalid team exists after rejected mutation")
			}
		})
	}
}

func TestLobbyEnforcesConfiguredPlayerCapacity(t *testing.T) {
	settings := DefaultSettings()
	settings.MaxPlayers = 2
	lobby, err := NewLobby(settings)
	if err != nil {
		t.Fatalf("NewLobby() error = %v", err)
	}
	mustAddPlayer(t, lobby, "player-a", domain.TeamA)
	mustAddPlayer(t, lobby, "player-b", domain.TeamB)

	err = lobby.AddPlayer("player-c", domain.TeamA)
	if !errors.Is(err, ErrLobbyFull) {
		t.Fatalf("AddPlayer() beyond capacity error = %v, want ErrLobbyFull", err)
	}
	if _, ok := lobby.Player("player-c"); ok {
		t.Fatal("player admitted beyond configured capacity")
	}
}

func TestLobbyRejectsMaximumBelowCurrentPlayerCount(t *testing.T) {
	lobby := mustLobby(t)
	mustAddPlayer(t, lobby, "player-a", domain.TeamA)
	mustAddPlayer(t, lobby, "player-b", domain.TeamB)
	mustAddPlayer(t, lobby, "player-c", domain.TeamA)
	mustSetReady(t, lobby, "player-a", true)

	settings := lobby.Settings()
	settings.MaxPlayers = 2
	err := lobby.UpdateSettings(settings)
	if !errors.Is(err, ErrSettingsBelowPlayerCount) {
		t.Fatalf("UpdateSettings() error = %v, want ErrSettingsBelowPlayerCount", err)
	}
	if got := lobby.Settings(); got != DefaultSettings() {
		t.Fatalf("Settings() after rejected capacity update = %+v, want defaults", got)
	}
	assertReady(t, lobby, "player-a", true)
}

func readyTwoPlayerLobby(t *testing.T) *Lobby {
	t.Helper()

	lobby := mustLobby(t)
	mustAddPlayer(t, lobby, "player-a", domain.TeamA)
	mustAddPlayer(t, lobby, "player-b", domain.TeamB)
	mustSetReady(t, lobby, "player-a", true)
	mustSetReady(t, lobby, "player-b", true)
	return lobby
}

func mustLobby(t *testing.T) *Lobby {
	t.Helper()

	lobby, err := NewLobby(DefaultSettings())
	if err != nil {
		t.Fatalf("NewLobby() error = %v", err)
	}
	return lobby
}

func mustAddPlayer(t *testing.T, lobby *Lobby, id domain.PlayerID, team domain.TeamID) {
	t.Helper()

	if err := lobby.AddPlayer(id, team); err != nil {
		t.Fatalf("AddPlayer(%q, %q) error = %v", id, team, err)
	}
}

func mustSetReady(t *testing.T, lobby *Lobby, id domain.PlayerID, ready bool) {
	t.Helper()

	if err := lobby.SetReady(id, ready); err != nil {
		t.Fatalf("SetReady(%q, %t) error = %v", id, ready, err)
	}
}

func mustPlayer(t *testing.T, lobby *Lobby, id domain.PlayerID) Player {
	t.Helper()

	player, ok := lobby.Player(id)
	if !ok {
		t.Fatalf("Player(%q) not found", id)
	}
	return player
}

func assertReady(t *testing.T, lobby *Lobby, id domain.PlayerID, want bool) {
	t.Helper()

	player := mustPlayer(t, lobby, id)
	if player.Ready != want {
		t.Fatalf("Player(%q).Ready = %t, want %t", id, player.Ready, want)
	}
}
