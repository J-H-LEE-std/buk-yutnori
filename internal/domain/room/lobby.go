package room

import (
	"errors"
	"fmt"

	"buk-yutnori/internal/domain"
)

var (
	// ErrPlayerAlreadyExists identifies a duplicate player admission.
	ErrPlayerAlreadyExists = errors.New("player already exists in lobby")

	// ErrPlayerNotFound identifies a lobby command for an absent player.
	ErrPlayerNotFound = errors.New("player not found in lobby")
	// ErrCPUPlayerRequired identifies an attempt to remove a human through the
	// server-owned CPU seat operation.
	ErrCPUPlayerRequired = errors.New("player is not a CPU player")

	// ErrLobbyFull identifies admission beyond the room's configured capacity.
	ErrLobbyFull = errors.New("lobby is full")

	// ErrReadyPlayerTeamChange requires a player to unset ready before moving teams.
	ErrReadyPlayerTeamChange = errors.New("ready player cannot change team")

	// ErrSettingsBelowPlayerCount prevents a room maximum below current occupancy.
	ErrSettingsBelowPlayerCount = errors.New("room settings maximum is below current player count")

	// ErrStartNotEnoughPlayers identifies a start request with fewer than two players.
	ErrStartNotEnoughPlayers = errors.New("not enough players to start")

	// ErrStartTeamsUnbalanced identifies a start request without equal non-empty teams.
	ErrStartTeamsUnbalanced = errors.New("teams are not balanced for start")

	// ErrStartPlayersNotReady identifies a start request before every player is ready.
	ErrStartPlayersNotReady = errors.New("not every player is ready")

	// ErrNoStartConfirmationFailure prevents an empty failure transition from clearing ready state.
	ErrNoStartConfirmationFailure = errors.New("failed start confirmation requires a nonresponding player")
)

// Player is one player's authoritative lobby state.
type Player struct {
	ID    domain.PlayerID
	Team  domain.TeamID
	Ready bool
	CPU   bool
}

// Lobby contains pure pre-match room state. Its application actor owns
// serialization; Lobby itself does not synchronize concurrent callers.
type Lobby struct {
	settings Settings
	players  map[domain.PlayerID]Player
}

// NewLobby creates an empty lobby with validated canonical settings.
func NewLobby(settings Settings) (*Lobby, error) {
	if err := settings.Validate(); err != nil {
		return nil, err
	}

	return &Lobby{
		settings: settings,
		players:  make(map[domain.PlayerID]Player),
	}, nil
}

// Settings returns the current room settings by value.
func (lobby *Lobby) Settings() Settings {
	return lobby.settings
}

// Player returns one player's state by value.
func (lobby *Lobby) Player(id domain.PlayerID) (Player, bool) {
	player, ok := lobby.players[id]
	return player, ok
}

// Players returns a copy of every player's current state keyed by ID.
func (lobby *Lobby) Players() map[domain.PlayerID]Player {
	players := make(map[domain.PlayerID]Player, len(lobby.players))
	for id, player := range lobby.players {
		players[id] = player
	}
	return players
}

// AddPlayer admits a new, initially not-ready player to one team.
func (lobby *Lobby) AddPlayer(id domain.PlayerID, team domain.TeamID) error {
	if err := id.Validate(); err != nil {
		return err
	}
	if err := team.Validate(); err != nil {
		return err
	}
	if _, exists := lobby.players[id]; exists {
		return fmt.Errorf("%w: %s", ErrPlayerAlreadyExists, id)
	}
	if len(lobby.players) >= lobby.settings.MaxPlayers {
		return fmt.Errorf("%w: maximum %d players", ErrLobbyFull, lobby.settings.MaxPlayers)
	}

	lobby.players[id] = Player{ID: id, Team: team, Ready: false}
	return nil
}

// AddCPUPlayer admits a server-owned player. CPU players are always ready;
// they have no account and never answer a start confirmation.
func (lobby *Lobby) AddCPUPlayer(id domain.PlayerID, team domain.TeamID) error {
	if err := lobby.AddPlayer(id, team); err != nil {
		return err
	}
	player := lobby.players[id]
	player.CPU = true
	player.Ready = true
	lobby.players[id] = player
	return nil
}

// RemoveCPUPlayer removes only a server-owned player.
func (lobby *Lobby) RemoveCPUPlayer(id domain.PlayerID) error {
	player, err := lobby.player(id)
	if err != nil {
		return err
	}
	if !player.CPU {
		return ErrCPUPlayerRequired
	}
	delete(lobby.players, id)
	return nil
}

// RemovePlayer removes one player without changing any remaining ready state.
func (lobby *Lobby) RemovePlayer(id domain.PlayerID) error {
	if err := id.Validate(); err != nil {
		return err
	}
	if _, exists := lobby.players[id]; !exists {
		return fmt.Errorf("%w: %s", ErrPlayerNotFound, id)
	}

	delete(lobby.players, id)
	return nil
}

// SetReady changes only the selected player's ready state.
func (lobby *Lobby) SetReady(id domain.PlayerID, ready bool) error {
	player, err := lobby.player(id)
	if err != nil {
		return err
	}

	player.Ready = ready
	lobby.players[id] = player
	return nil
}

// ChangeTeam moves a not-ready player without changing any other ready state.
// Repeating the already-current team is an idempotent no-op.
func (lobby *Lobby) ChangeTeam(id domain.PlayerID, team domain.TeamID) error {
	if err := team.Validate(); err != nil {
		return err
	}
	player, err := lobby.player(id)
	if err != nil {
		return err
	}
	if player.Team == team {
		return nil
	}
	if player.Ready {
		return fmt.Errorf("%w: %s", ErrReadyPlayerTeamChange, id)
	}

	player.Team = team
	lobby.players[id] = player
	return nil
}

// UpdateSettings replaces valid room settings without changing ready states.
func (lobby *Lobby) UpdateSettings(settings Settings) error {
	if err := settings.Validate(); err != nil {
		return err
	}
	if settings.MaxPlayers < len(lobby.players) {
		return fmt.Errorf(
			"%w: maximum %d, current %d",
			ErrSettingsBelowPlayerCount,
			settings.MaxPlayers,
			len(lobby.players),
		)
	}

	lobby.settings = settings
	return nil
}

// ValidateStart rechecks the current roster at the instant start is requested.
func (lobby *Lobby) ValidateStart() error {
	if len(lobby.players) < 2 {
		return ErrStartNotEnoughPlayers
	}

	teamCounts := map[domain.TeamID]int{
		domain.TeamA: 0,
		domain.TeamB: 0,
	}
	for _, player := range lobby.players {
		teamCounts[player.Team]++
	}
	if teamCounts[domain.TeamA] == 0 || teamCounts[domain.TeamA] != teamCounts[domain.TeamB] {
		return ErrStartTeamsUnbalanced
	}

	for _, player := range lobby.players {
		if !player.CPU && !player.Ready {
			return ErrStartPlayersNotReady
		}
	}
	return nil
}

// FailStartConfirmation atomically removes known nonresponders and clears the
// ready state of every remaining player. It validates all targets before the
// first mutation so an invalid request leaves the lobby unchanged.
func (lobby *Lobby) FailStartConfirmation(nonresponders []domain.PlayerID) error {
	if len(nonresponders) == 0 {
		return ErrNoStartConfirmationFailure
	}

	unique := make(map[domain.PlayerID]struct{}, len(nonresponders))
	for _, id := range nonresponders {
		if err := id.Validate(); err != nil {
			return err
		}
		if _, exists := lobby.players[id]; !exists {
			return fmt.Errorf("%w: %s", ErrPlayerNotFound, id)
		}
		unique[id] = struct{}{}
	}

	for id := range unique {
		delete(lobby.players, id)
	}
	for id, player := range lobby.players {
		// CPU seats are server-owned and have no client command that could make
		// them ready again after a failed confirmation.
		player.Ready = player.CPU
		lobby.players[id] = player
	}
	return nil
}

func (lobby *Lobby) player(id domain.PlayerID) (Player, error) {
	if err := id.Validate(); err != nil {
		return Player{}, err
	}
	player, exists := lobby.players[id]
	if !exists {
		return Player{}, fmt.Errorf("%w: %s", ErrPlayerNotFound, id)
	}
	return player, nil
}
