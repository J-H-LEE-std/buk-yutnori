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
	"sort"
	"sync"
	"time"

	"buk-yutnori/internal/auth"
	"buk-yutnori/internal/domain"
	"buk-yutnori/internal/domain/board"
	"buk-yutnori/internal/domain/room"
	"buk-yutnori/internal/protocol"
	"buk-yutnori/internal/storage"
)

var (
	// ErrRoomNotFound identifies registry operations on an absent room.
	ErrRoomNotFound = errors.New("room not found")
	// ErrNotMember identifies a valid room the caller does not belong to.
	ErrNotMember = errors.New("user is not a member of the room")
	// ErrAlreadyMember identifies a join request from an existing member.
	ErrAlreadyMember = errors.New("user is already a member of the room")
	// ErrPasswordRequired identifies a missing entry password.
	ErrPasswordRequired = errors.New("room password is required")
	// ErrInvalidRoomPassword identifies a wrong entry password.
	ErrInvalidRoomPassword = errors.New("invalid room password")
	// ErrCombinedCapacityFull identifies a join beyond the combined
	// player-plus-spectator limit.
	ErrCombinedCapacityFull = errors.New("player plus spectator count exceeds the room capacity")

	// ErrNotRoomHost rejects a start request from anyone but the room owner.
	ErrNotRoomHost = errors.New("only the room owner can request the match start")
	// ErrStartAlreadyRequested rejects commands while a start window is open.
	ErrStartAlreadyRequested = errors.New("start confirmation is already in progress")
	// ErrRoomAlreadyStarted rejects membership changes after a confirmed start.
	ErrRoomAlreadyStarted = errors.New("the match has already started")
	// ErrNoActiveStartConfirmation rejects confirmations without an open window.
	ErrNoActiveStartConfirmation = errors.New("no active start confirmation")
	// ErrMatchScopeMismatch rejects confirmations bound to another match scope.
	ErrMatchScopeMismatch = errors.New("start confirmation match scope does not match")
	// ErrEventStoreUnavailable reports a durable event-store failure or a
	// room fenced off by an earlier one. State transitions are refused until
	// operations intervene because the canonical store would otherwise
	// diverge from committed broadcasts (ADR-0017).
	ErrEventStoreUnavailable = errors.New("event store is unavailable")
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
// serializes every mutation, including the canonical match runtime that
// consumes started rooms (issue #82); per-room actors remain a future
// migration recorded in ADR-0015.
type RoomRegistry struct {
	mutex            sync.Mutex
	clock            func() time.Time
	randomID         func() (string, error)
	randomSeed       func() (uint64, uint64, error)
	matchClock       matchClock
	boardGraph       *board.Graph
	store            storage.EventStore
	sequences        *RoomEventSequences
	rooms            map[domain.RoomID]*registeredRoom
	ordering         []domain.RoomID
	eventSubscribers map[*RoomEventSubscription]auth.UserID
	eventBufferSize  int
}

type registeredRoom struct {
	lobby              *room.Lobby
	password           []byte
	spectators         map[auth.UserID]struct{}
	summary            RoomSummary
	host               auth.UserID
	confirmation       *room.StartConfirmation
	started            bool
	roomStatus         string
	runtime            *matchRuntime
	poisoned           bool
	expiryTimer        *time.Timer
	lastRoomUpdated    any
	activeGameStarting any
}

// NewRoomRegistry constructs an empty registry keyed by crypto-random room IDs.
// The clock drives start confirmation deadlines and must be monotonic in
// production (ADR-0003).
func NewRoomRegistry(clock func() time.Time) (*RoomRegistry, error) {
	if clock == nil {
		return nil, errors.New("invalid room registry configuration")
	}
	return &RoomRegistry{
		clock:            clock,
		randomID:         newRandomIDGenerator(rand.Reader),
		randomSeed:       defaultRandomSeed,
		matchClock:       systemMatchClock{},
		sequences:        NewRoomEventSequences(),
		rooms:            make(map[domain.RoomID]*registeredRoom),
		eventSubscribers: make(map[*RoomEventSubscription]auth.UserID),
	}, nil
}

func newRandomIDGenerator(source io.Reader) func() (string, error) {
	return func() (string, error) {
		buffer := make([]byte, 16)
		if _, err := io.ReadFull(source, buffer); err != nil {
			return "", fmt.Errorf("generate random id: %w", err)
		}
		return hex.EncodeToString(buffer), nil
	}
}

// AttachEventStore installs the durable canonical event store. Once a store
// is attached, every committed room event is persisted before its in-memory
// sequence and broadcast are allowed to commit (ADR-0014, ADR-0017). A nil
// argument is rejected; registries without an attached store keep the
// memory-only behavior used by tests.
func (registry *RoomRegistry) AttachEventStore(store storage.EventStore) error {
	if store == nil {
		return fmt.Errorf("%w: event store is required", ErrInvalidConfiguration)
	}
	registry.mutex.Lock()
	defer registry.mutex.Unlock()
	registry.store = store
	return nil
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

	rawID, err := registry.randomID()
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
		host:       input.Creator,
		roomStatus: protocol.RoomStatusLobby,
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
	tx := registry.newEventTx(roomID)
	tx.emit(func(sequence uint64) (any, error) {
		return protocol.NewRoomUpdatedEvent(roomID, sequence, protocol.RoomStatusLobby)
	})
	if err := tx.flush(); err != nil {
		return RoomSummary{}, err
	}
	return entry.summary, nil
}

// List returns the current open rooms in creation order.
func (registry *RoomRegistry) List() []RoomSummary {
	registry.mutex.Lock()
	defer registry.mutex.Unlock()

	summaries := make([]RoomSummary, 0, len(registry.ordering))
	for _, roomID := range registry.ordering {
		entry := registry.rooms[roomID]
		if entry.poisoned {
			// A fenced room rejects every mutation (ADR-0017), so it must
			// not advertise itself to joining users either.
			continue
		}
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
	if err := registry.guardLobbyMutation(entry); err != nil {
		return RoomSummary{}, err
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
	tx := registry.newEventTx(input.RoomID)
	tx.emit(func(sequence uint64) (any, error) {
		return protocol.NewRoomUpdatedEvent(input.RoomID, sequence, entry.roomStatus)
	})
	if err := tx.flush(); err != nil {
		return RoomSummary{}, err
	}
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
		return Membership{}, ErrNotMember
	}
	return Membership{Role: RolePlayer, Team: player.Team, Ready: player.Ready}, nil
}

// RoomDetailSnapshot is the authoritative member view of one room.
type RoomDetailSnapshot struct {
	Summary     RoomSummary          `json:"summary"`
	Members     []RoomMemberView     `json:"members"`
	ActiveStart *ActiveStartSnapshot `json:"active_start,omitempty"`
}

// RoomMemberView is one member's visible lobby state.
type RoomMemberView struct {
	UserID auth.UserID   `json:"user_id"`
	Role   string        `json:"role"`
	Team   domain.TeamID `json:"team,omitempty"`
	Ready  bool          `json:"ready"`
}

// ActiveStartSnapshot describes the open confirmation window.
type ActiveStartSnapshot struct {
	MatchID                domain.MatchID `json:"match_id"`
	ConfirmationDeadlineAt string         `json:"confirmation_deadline_at"`
}

// Detail returns the member-only view of a room's current lobby state and,
// while a start window is open, its active confirmation contract (ADR-0015).
func (registry *RoomRegistry) Detail(user auth.UserID, roomID domain.RoomID) (RoomDetailSnapshot, error) {
	playerID, err := playerIDFromUser(user)
	if err != nil {
		return RoomDetailSnapshot{}, err
	}

	registry.mutex.Lock()
	defer registry.mutex.Unlock()

	entry, exists := registry.rooms[roomID]
	if !exists {
		return RoomDetailSnapshot{}, ErrRoomNotFound
	}
	if _, spectator := entry.spectators[user]; !spectator {
		if _, player := entry.lobby.Player(playerID); !player {
			return RoomDetailSnapshot{}, ErrNotMember
		}
	}

	detail := RoomDetailSnapshot{Summary: entry.summary}
	spectatorIDs := make([]auth.UserID, 0, len(entry.spectators))
	for id := range entry.spectators {
		spectatorIDs = append(spectatorIDs, id)
	}
	sort.Slice(spectatorIDs, func(left, right int) bool { return spectatorIDs[left] < spectatorIDs[right] })
	for _, id := range spectatorIDs {
		detail.Members = append(detail.Members, RoomMemberView{UserID: id, Role: RoleSpectator})
	}
	players := entry.lobby.Players()
	ids := make([]domain.PlayerID, 0, len(players))
	for id := range players {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(left, right int) bool { return ids[left] < ids[right] })
	for _, id := range ids {
		player := players[id]
		detail.Members = append(detail.Members, RoomMemberView{
			UserID: auth.UserID(id), Role: RolePlayer,
			Team: player.Team, Ready: player.Ready,
		})
	}
	if entry.confirmation != nil && !entry.started {
		snapshot := entry.confirmation.Snapshot()
		if snapshot.Status == room.StartConfirmationPending {
			detail.ActiveStart = &ActiveStartSnapshot{
				MatchID:                snapshot.MatchID,
				ConfirmationDeadlineAt: snapshot.DeadlineAt.UTC().Format(time.RFC3339),
			}
		}
	}
	return detail, nil
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
	if err := registry.guardLobbyMutation(entry); err != nil {
		return err
	}
	if err := entry.lobby.ChangeTeam(playerID, team); err != nil {
		return err
	}
	tx := registry.newEventTx(roomID)
	tx.emit(func(sequence uint64) (any, error) {
		return protocol.NewRoomUpdatedEvent(roomID, sequence, entry.roomStatus)
	})
	return tx.flush()
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
	if err := registry.guardLobbyMutation(entry); err != nil {
		return err
	}
	if err := entry.lobby.SetReady(playerID, ready); err != nil {
		return err
	}
	tx := registry.newEventTx(roomID)
	tx.emit(func(sequence uint64) (any, error) {
		return protocol.NewRoomUpdatedEvent(roomID, sequence, entry.roomStatus)
	})
	return tx.flush()
}

func playerIDFromUser(user auth.UserID) (domain.PlayerID, error) {
	playerID := domain.PlayerID(user)
	if err := playerID.Validate(); err != nil {
		return "", fmt.Errorf("%w: %v", auth.ErrUnauthenticated, err)
	}
	return playerID, nil
}
