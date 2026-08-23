// Canonical match runtime consuming started registry rooms (issue #82).
//
// One runtime owns the domain game, turn machine, seeded RNG, and turn
// timers for one started room. Every method runs while the owning
// RoomRegistry mutex is held, matching the ADR-0015 serialization boundary;
// per-room actors remain a future migration recorded in ADR-0015. Game-rule
// semantics stay inside internal/domain; these files only sequence them.

package application

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	mathrand "math/rand/v2"
	"sort"
	"time"

	"buk-yutnori/internal/domain"
	"buk-yutnori/internal/domain/board"
	"buk-yutnori/internal/domain/cpu"
	"buk-yutnori/internal/domain/match"
	"buk-yutnori/internal/domain/room"
	"buk-yutnori/internal/domain/turn"
	"buk-yutnori/internal/domain/yut"
	"buk-yutnori/internal/protocol"
)

var (
	// ErrMatchNotActive rejects match commands for a room without a live match.
	ErrMatchNotActive = errors.New("no active match in the room")
	// ErrNotTurnPlayer rejects a match command from anyone but the acting player.
	ErrNotTurnPlayer = errors.New("command sender is not the acting turn player")
	// ErrInvalidTurnAction rejects an action the current phase cannot accept.
	ErrInvalidTurnAction = errors.New("turn action is not valid in the current phase")
)

const (
	matchTimerKindThrow = "throw"
	matchTimerKindMove  = "move"

	cpuControlReasonTimeout    = "timeout"
	gameEndedReasonAllFinished = "all_pieces_finished"
)

type matchTimer interface {
	Stop() bool
}

// matchClock supplies the monotonic instant and cancellable timers used by
// turn deadlines. Production uses wall-clock AfterFunc; tests substitute a
// manual clock so deadline expiry stays deterministic (ADR-0003).
type matchClock interface {
	Now() time.Time
	AfterFunc(d time.Duration, f func()) matchTimer
}

type systemMatchClock struct{}

type systemMatchTimer struct {
	timer *time.Timer
}

func (systemMatchClock) Now() time.Time {
	return time.Now()
}

func (systemMatchClock) AfterFunc(d time.Duration, f func()) matchTimer {
	return systemMatchTimer{timer: time.AfterFunc(d, f)}
}

func (timer systemMatchTimer) Stop() bool {
	return timer.timer.Stop()
}

// AttachBoardGraph installs the canonical board graph consumed by every
// assembled match runtime. It must be called before the first start
// confirmation completes; otherwise CONFIRM_GAME_START fails closed.
func (registry *RoomRegistry) AttachBoardGraph(graph *board.Graph) error {
	if graph == nil {
		return fmt.Errorf("%w: board graph is required", ErrInvalidConfiguration)
	}
	registry.mutex.Lock()
	defer registry.mutex.Unlock()
	registry.boardGraph = graph
	return nil
}

// setMatchRandomSeed overrides the seed source for deterministic tests. The
// production default draws two uint64 values from crypto/rand.
func (registry *RoomRegistry) setMatchRandomSeed(seed func() (uint64, uint64, error)) {
	registry.mutex.Lock()
	defer registry.mutex.Unlock()
	registry.randomSeed = seed
}

// setMatchClock overrides the deadline clock for deterministic tests.
func (registry *RoomRegistry) setMatchClock(clock matchClock) {
	registry.mutex.Lock()
	defer registry.mutex.Unlock()
	registry.matchClock = clock
}

func defaultRandomSeed() (uint64, uint64, error) {
	var buffer [16]byte
	if _, err := rand.Read(buffer[:]); err != nil {
		return 0, 0, fmt.Errorf("draw match random seed: %w", err)
	}
	return binary.LittleEndian.Uint64(buffer[:8]), binary.LittleEndian.Uint64(buffer[8:]), nil
}

// matchRuntime owns one live match inside a started registry room.
type matchRuntime struct {
	roomID   domain.RoomID
	matchID  domain.MatchID
	settings room.Settings

	order  []domain.PlayerID
	teamOf map[domain.PlayerID]domain.TeamID

	game        *match.Game
	cpu         *cpu.Policy
	throwResult func(yut.Mode) (domain.YutResult, error)

	machine   *turn.Machine
	turnIndex int
	tokenSeq  uint64

	timerGeneration  uint64
	timerKind        string
	timerDeadline    time.Time
	activeTimer      matchTimer
	cpuControlled    bool
	pendingMovePiece domain.PieceID
}

func (rt *matchRuntime) currentPlayer() domain.PlayerID {
	return rt.order[rt.turnIndex]
}

func oppositeTeam(team domain.TeamID) domain.TeamID {
	if team == domain.TeamA {
		return domain.TeamB
	}
	return domain.TeamA
}

func (rt *matchRuntime) currentTeam() domain.TeamID {
	return rt.teamOf[rt.currentPlayer()]
}

func (rt *matchRuntime) nextTokenID() domain.ResultTokenID {
	rt.tokenSeq++
	return domain.ResultTokenID(fmt.Sprintf("token-%06d", rt.tokenSeq))
}

// newMatchRuntime assembles the canonical runtime from the confirmed roster:
// piece setups, drawn team-internal order plus first player (docs/05), and
// the seeded server-owned RNG shared by throws, Buk weighting, and CPU
// tiebreaks. It performs no emission and mutates no lobby state, so a
// construction failure leaves the open start window untouched.
func (registry *RoomRegistry) newMatchRuntime(entry *registeredRoom, roomID domain.RoomID, matchID domain.MatchID) (*matchRuntime, error) {
	if registry.boardGraph == nil {
		return nil, fmt.Errorf("%w: board graph is not attached", ErrInvalidConfiguration)
	}
	settings := entry.lobby.Settings()
	if err := settings.Validate(); err != nil {
		return nil, fmt.Errorf("%w: room settings: %v", ErrInvalidConfiguration, err)
	}
	seed1, seed2, err := registry.randomSeed()
	if err != nil {
		return nil, err
	}
	draw := mathrand.New(mathrand.NewPCG(seed1, seed2))

	players := entry.lobby.Players()
	teamPlayers := map[domain.TeamID][]domain.PlayerID{
		domain.TeamA: {},
		domain.TeamB: {},
	}
	for id := range players {
		player := players[id]
		if err := player.Team.Validate(); err != nil {
			return nil, fmt.Errorf("%w: roster team: %v", ErrInvalidConfiguration, err)
		}
		teamPlayers[player.Team] = append(teamPlayers[player.Team], id)
	}
	for _, team := range []domain.TeamID{domain.TeamA, domain.TeamB} {
		ids := teamPlayers[team]
		sort.Slice(ids, func(left, right int) bool { return ids[left] < ids[right] })
		draw.Shuffle(len(ids), func(left, right int) { ids[left], ids[right] = ids[right], ids[left] })
	}
	startingTeam := domain.TeamA
	if draw.Uint64N(2) == 1 {
		startingTeam = domain.TeamB
	}
	first := teamPlayers[startingTeam]
	second := teamPlayers[oppositeTeam(startingTeam)]
	order := make([]domain.PlayerID, 0, len(first)+len(second))
	for index := 0; index < len(first) || index < len(second); index++ {
		if index < len(first) {
			order = append(order, first[index])
		}
		if index < len(second) {
			order = append(order, second[index])
		}
	}

	teamOf := make(map[domain.PlayerID]domain.TeamID, len(order))
	setups := make([]match.TeamSetup, 0, 2)
	for _, team := range []domain.TeamID{domain.TeamA, domain.TeamB} {
		pieceIDs := make([]domain.PieceID, 0, settings.PieceCount)
		for number := 1; number <= settings.PieceCount; number++ {
			pieceIDs = append(pieceIDs, domain.PieceID(fmt.Sprintf("%s-%d", team, number)))
		}
		setups = append(setups, match.TeamSetup{TeamID: team, PieceIDs: pieceIDs})
		for _, id := range teamPlayers[team] {
			teamOf[id] = team
		}
	}

	source := mathrand.New(mathrand.NewPCG(seed1, seed2^0x9E3779B97F4A7C15))
	sampler, err := yut.NewSampler(source)
	if err != nil {
		return nil, fmt.Errorf("%w: assemble yut sampler: %v", ErrInvalidConfiguration, err)
	}
	game, err := match.NewGameWithRandomSource(registry.boardGraph, settings, setups, source)
	if err != nil {
		return nil, fmt.Errorf("%w: assemble match game: %v", ErrInvalidConfiguration, err)
	}
	policy, err := cpu.NewPolicy(registry.boardGraph, settings, source)
	if err != nil {
		return nil, fmt.Errorf("%w: assemble CPU policy: %v", ErrInvalidConfiguration, err)
	}

	return &matchRuntime{
		roomID:      roomID,
		matchID:     matchID,
		settings:    settings,
		order:       order,
		teamOf:      teamOf,
		game:        game,
		cpu:         policy,
		throwResult: sampler.Throw,
	}, nil
}

// startLocked attaches the assembled runtime and emits GAME_STARTED followed
// by the first TURN_STARTED broadcast. The caller already flipped the room
// into its in-match status.
func (registry *RoomRegistry) startLocked(entry *registeredRoom, rt *matchRuntime) error {
	entry.runtime = rt
	bukDestination := rt.game.Snapshot().BukDestinationSpaceID
	var destinationPointer *domain.SpaceID
	if bukDestination != "" {
		value := bukDestination
		destinationPointer = &value
	}
	if err := registry.emitLocked(rt.roomID, func(sequence uint64) (any, error) {
		return protocol.NewGameStartedEvent(rt.roomID, rt.matchID, sequence, protocol.GameStartedPayload{
			FirstPlayerID:       rt.order[0],
			BukDestinationSpace: destinationPointer,
		})
	}); err != nil {
		return err
	}
	return registry.beginTurnLocked(entry, rt)
}

func (registry *RoomRegistry) beginTurnLocked(entry *registeredRoom, rt *matchRuntime) error {
	machine, err := turn.NewMachine(rt.currentPlayer(), rt.settings)
	if err != nil {
		return fmt.Errorf("%w: assemble turn machine: %v", ErrInvalidConfiguration, err)
	}
	rt.machine = machine
	rt.pendingMovePiece = ""
	rt.cpuControlled = false
	if err := machine.Start(); err != nil {
		return fmt.Errorf("%w: start turn: %v", ErrInvalidConfiguration, err)
	}
	registry.scheduleThrowTimerLocked(rt)
	return registry.emitTurnStartedLocked(entry, rt)
}

// endTurnLocked closes the finished turn and opens the next player's turn.
// CPU substitution never leaks across turns: control resets before the next
// window opens (docs/03 시간 초과).
func (registry *RoomRegistry) endTurnLocked(entry *registeredRoom, rt *matchRuntime) error {
	registry.cancelTimerLocked(rt)
	rt.turnIndex = (rt.turnIndex + 1) % len(rt.order)
	return registry.beginTurnLocked(entry, rt)
}

func (registry *RoomRegistry) finishMatchLocked(entry *registeredRoom, rt *matchRuntime) error {
	winner := rt.game.Snapshot().WinnerTeamID
	registry.cancelTimerLocked(rt)
	if err := registry.emitLocked(rt.roomID, func(sequence uint64) (any, error) {
		return protocol.NewFinishedGameEndedEvent(
			rt.roomID, rt.matchID, sequence, winner, gameEndedReasonAllFinished,
		)
	}); err != nil {
		return err
	}

	// Canonical end-of-match return to the same waiting room (docs/05):
	// release started, close the consumed confirmation, keep teams and
	// settings, and reset ready states so membership mutations resume.
	entry.started = false
	entry.confirmation = nil
	entry.runtime = nil
	entry.roomStatus = protocol.RoomStatusPostMatch
	if err := resetReadyStatesLocked(entry); err != nil {
		return err
	}
	return registry.emitLocked(rt.roomID, func(sequence uint64) (any, error) {
		return protocol.NewRoomUpdatedEvent(rt.roomID, sequence, entry.roomStatus)
	})
}
