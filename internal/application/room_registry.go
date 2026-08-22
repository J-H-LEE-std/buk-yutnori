// In-memory room registry for the lobby lifecycle HTTP boundary.

package application

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"sync"

	"buk-yutnori/internal/auth"
	"buk-yutnori/internal/domain"
	"buk-yutnori/internal/domain/room"
)

var (
	// ErrRoomNotFound identifies registry operations on an absent room.
	ErrRoomNotFound = errors.New("room not found")
	// ErrAlreadyMember identifies a join request from an existing member.
	ErrAlreadyMember = errors.New("user is already a member of the room")
	// ErrPasswordRequired identifies a missing entry password.
	ErrPasswordRequired = errors.New("room password is required")
	// ErrInvalidRoomPassword identifies a wrong entry password.
	ErrInvalidRoomPassword = errors.New("invalid room password")
	// ErrCombinedCapacityFull identifies a join beyond the combined
	// player-plus-spectator limit.
	ErrCombinedCapacityFull = errors.New("player plus spectator count exceeds the room capacity")
)

const (
	// RolePlayer selects the participating member role.
	RolePlayer = "player"
	// RoleSpectator selects the observing member role.
	RoleSpectator = "spectator"

	// combinedMemberCapacity is the canonical player-plus-spectator limit
	// recorded in spec/room_settings.yaml constraints.
	combinedMemberCapacity = 20
)

// RoomSummary is one row of the public open-room list.
type RoomSummary struct {
	RoomID      domain.RoomID `json:"room_id"`
	Title       string        `json:"title"`
	HasPassword bool          `json:"has_password"`
	PlayerCount int           `json:"player_count"`
	MaxPlayers  int           `json:"max_players"`
}

// CreateRoomInput carries one authenticated create-room request.
type CreateRoomInput struct {
	Creator  auth.UserID
	Creation room.Creation
	Settings room.Settings
	Team     domain.TeamID
}

// JoinRoomInput carries one authenticated join-room request.
type JoinRoomInput struct {
	User     auth.UserID
	RoomID   domain.RoomID
	Role     string
	Team     domain.TeamID
	Password string
}

// RoomRegistry owns authoritative pre-match room membership. One mutex
// serializes every mutation; per-match execution keeps the ADR-0012 actor
// boundary and is out of scope here.
type RoomRegistry struct {
	mutex    sync.Mutex
	nextID   func() (string, error)
	rooms    map[domain.RoomID]*registeredRoom
	ordering []domain.RoomID
}

type registeredRoom struct {
	lobby      *room.Lobby
	password   []byte
	spectators map[auth.UserID]struct{}
	summary    RoomSummary
}

// NewRoomRegistry constructs an empty registry keyed by crypto-random room IDs.
func NewRoomRegistry() (*RoomRegistry, error) {
	return &RoomRegistry{
		nextID: newRoomIDGenerator(rand.Reader),
		rooms:  make(map[domain.RoomID]*registeredRoom),
	}, nil
}

func newRoomIDGenerator(source io.Reader) func() (string, error) {
	return func() (string, error) {
		buffer := make([]byte, 16)
		if _, err := io.ReadFull(source, buffer); err != nil {
			return "", fmt.Errorf("generate room id: %w", err)
		}
		id := domain.RoomID(hex.EncodeToString(buffer))
		if err := id.Validate(); err != nil {
			return "", err
		}
		return string(id), nil
	}
}

// Create validates the canonical contracts, admits the creator as the first
// player, and returns the created room summary.
func (registry *RoomRegistry) Create(input CreateRoomInput) (RoomSummary, error) {
	playerID, err := playerIDFromUser(input.Creator)
	if err != nil {
		return RoomSummary{}, err
	}
	if err := input.Creation.Validate(); err != nil {
		return RoomSummary{}, err
	}
	if err := input.Settings.Validate(); err != nil {
		return RoomSummary{}, err
	}
	if err := input.Team.Validate(); err != nil {
		return RoomSummary{}, err
	}

	registry.mutex.Lock()
	defer registry.mutex.Unlock()

	rawID, err := registry.nextID()
	if err != nil {
		return RoomSummary{}, err
	}
	roomID := domain.RoomID(rawID)
	lobby, err := room.NewLobby(input.Settings)
	if err != nil {
		return RoomSummary{}, err
	}
	if err := lobby.AddPlayer(playerID, input.Team); err != nil {
		return RoomSummary{}, err
	}

	entry := &registeredRoom{
		lobby:      lobby,
		spectators: make(map[auth.UserID]struct{}),
	}
	if input.Creation.Password != "" {
		sum := sha256.Sum256([]byte(input.Creation.Password))
		entry.password = sum[:]
	}
	entry.summary = RoomSummary{
		RoomID:      roomID,
		Title:       input.Creation.Title,
		HasPassword: len(entry.password) > 0,
		PlayerCount: 1,
		MaxPlayers:  input.Settings.MaxPlayers,
	}
	registry.rooms[roomID] = entry
	registry.ordering = append(registry.ordering, roomID)
	return entry.summary, nil
}

// List returns the current open rooms in creation order.
func (registry *RoomRegistry) List() []RoomSummary {
	registry.mutex.Lock()
	defer registry.mutex.Unlock()

	summaries := make([]RoomSummary, 0, len(registry.ordering))
	for _, roomID := range registry.ordering {
		entry := registry.rooms[roomID]
		entry.summary.PlayerCount = len(entry.lobby.Players())
		summaries = append(summaries, entry.summary)
	}
	return summaries
}

// Join admits an authenticated user as a player or spectator. Passwords are
// compared in constant time against the stored digest only.
func (registry *RoomRegistry) Join(input JoinRoomInput) (RoomSummary, error) {
	playerID, err := playerIDFromUser(input.User)
	if err != nil {
		return RoomSummary{}, err
	}

	registry.mutex.Lock()
	defer registry.mutex.Unlock()

	entry, exists := registry.rooms[input.RoomID]
	if !exists {
		return RoomSummary{}, ErrRoomNotFound
	}
	switch input.Role {
	case RolePlayer:
	case RoleSpectator:
	default:
		return RoomSummary{}, fmt.Errorf("invalid role %q", input.Role)
	}
	if _, already := entry.spectators[input.User]; already {
		return RoomSummary{}, ErrAlreadyMember
	}
	if _, already := entry.lobby.Player(playerID); already {
		return RoomSummary{}, ErrAlreadyMember
	}
	if len(entry.password) > 0 {
		if input.Password == "" {
			return RoomSummary{}, ErrPasswordRequired
		}
		sum := sha256.Sum256([]byte(input.Password))
		if subtle.ConstantTimeCompare(sum[:], entry.password) != 1 {
			return RoomSummary{}, ErrInvalidRoomPassword
		}
	}

	playerCount := len(entry.lobby.Players())
	// Both entry paths share the canonical combined member limit; the lobby
	// additionally enforces MaxPlayers for players.
	if playerCount+len(entry.spectators) >= combinedMemberCapacity {
		return RoomSummary{}, ErrCombinedCapacityFull
	}
	if input.Role == RolePlayer {
		if err := entry.lobby.AddPlayer(playerID, input.Team); err != nil {
			return RoomSummary{}, err
		}
		playerCount++
	} else {
		entry.spectators[input.User] = struct{}{}
	}
	entry.summary.PlayerCount = playerCount
	return entry.summary, nil
}

// Membership describes one user's authoritative position in a room.
type Membership struct {
	Role  string
	Team  domain.TeamID
	Ready bool
}

// Membership returns the authenticated user's current role, team, and ready
// state inside the room.
func (registry *RoomRegistry) Membership(user auth.UserID, roomID domain.RoomID) (Membership, error) {
	playerID, err := playerIDFromUser(user)
	if err != nil {
		return Membership{}, err
	}

	registry.mutex.Lock()
	defer registry.mutex.Unlock()

	entry, exists := registry.rooms[roomID]
	if !exists {
		return Membership{}, ErrRoomNotFound
	}
	if _, spectator := entry.spectators[user]; spectator {
		return Membership{Role: RoleSpectator}, nil
	}
	player, ok := entry.lobby.Player(playerID)
	if !ok {
		return Membership{}, ErrRoomNotFound
	}
	return Membership{Role: RolePlayer, Team: player.Team, Ready: player.Ready}, nil
}

// ChangeTeam moves the authenticated player to the requested team through the
// canonical Lobby rules. Spectators and non-members are rejected.
func (registry *RoomRegistry) ChangeTeam(user auth.UserID, roomID domain.RoomID, team domain.TeamID) error {
	playerID, err := playerIDFromUser(user)
	if err != nil {
		return err
	}
	if err := team.Validate(); err != nil {
		return err
	}

	registry.mutex.Lock()
	defer registry.mutex.Unlock()

	entry, exists := registry.rooms[roomID]
	if !exists {
		return ErrRoomNotFound
	}
	return entry.lobby.ChangeTeam(playerID, team)
}

// SetReady changes only the authenticated player's ready state through the
// canonical Lobby rules. Spectators and non-members are rejected.
func (registry *RoomRegistry) SetReady(user auth.UserID, roomID domain.RoomID, ready bool) error {
	playerID, err := playerIDFromUser(user)
	if err != nil {
		return err
	}

	registry.mutex.Lock()
	defer registry.mutex.Unlock()

	entry, exists := registry.rooms[roomID]
	if !exists {
		return ErrRoomNotFound
	}
	return entry.lobby.SetReady(playerID, ready)
}

func playerIDFromUser(user auth.UserID) (domain.PlayerID, error) {
	playerID := domain.PlayerID(user)
	if err := playerID.Validate(); err != nil {
		return "", fmt.Errorf("%w: %v", auth.ErrUnauthenticated, err)
	}
	return playerID, nil
}
