// Canonical match runtime consuming started registry rooms (issue #82).
//
// One runtime owns the domain game, turn machine, seeded RNG, and turn
// timers for one started room. Every method runs while the owning
// RoomRegistry mutex is held, matching the ADR-0015 serialization boundary;
// per-room actors remain a future migration recorded in ADR-0015. Game-rule
// semantics stay inside internal/domain; these files only sequence them.

package application

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	mathrand "math/rand/v2"
	"sort"
	"time"

	"buk-yutnori/internal/auth"
	"buk-yutnori/internal/domain"
	"buk-yutnori/internal/domain/board"
	"buk-yutnori/internal/domain/cpu"
	"buk-yutnori/internal/domain/match"
	"buk-yutnori/internal/domain/room"
	"buk-yutnori/internal/domain/turn"
	"buk-yutnori/internal/domain/yut"
	"buk-yutnori/internal/protocol"
	"buk-yutnori/internal/storage"
)

var (
	// ErrMatchNotActive rejects match commands for a room without a live match.
	ErrMatchNotActive = errors.New("no active match in the room")
	// ErrNotTurnPlayer rejects a match command from anyone but the acting player.
	ErrNotTurnPlayer = errors.New("command sender is not the acting turn player")
	// ErrInvalidTurnAction rejects an action the current phase cannot accept.
	ErrInvalidTurnAction = errors.New("turn action is not valid in the current phase")
	// ErrMatchPaused rejects commands while the match is paused; retrying
	// after resume is meaningful, so the rejection is retriable.
	ErrMatchPaused = errors.New("the match is paused")
	// ErrMatchPauseUsed rejects a second host pause in one match (docs/03:
	// 경기당 1회).
	ErrMatchPauseUsed = errors.New("the per-match pause was already used")
	// ErrMatchNotPaused rejects a resume without an active pause.
	ErrMatchNotPaused = errors.New("the match is not paused")
)

const (
	// storageFailureReason and friends mirror schema enums for the
	// storage-failure operational pause (#87).
	matchPauseKindUser    = "user"
	matchPauseKindStorage = "storage"

	gameEndedReasonStorageRetryExhausted = "storage_retry_exhausted"
)

// storageRetryDelays is the canonical retry schedule after an initial
// persistence failure (spec/turn_state_machine.yaml: retry_delays_seconds).
var storageRetryDelays = [...]time.Duration{time.Second, 2 * time.Second, 5 * time.Second}

const (
	matchTimerKindThrow = "throw"
	matchTimerKindMove  = "move"

	cpuControlReasonTimeout     = "timeout"
	cpuControlReasonLobbyPlayer = "lobby_player"
	gameEndedReasonAllFinished  = "all_pieces_finished"
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

	order      []domain.PlayerID
	teamOf     map[domain.PlayerID]domain.TeamID
	cpuPlayers map[domain.PlayerID]bool

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

	pauseUsed          bool
	paused             bool
	pauseEndsAt        time.Time
	pauseExpiryTimer   matchTimer
	preservedTimerKind string
	preservedRemaining time.Duration

	// Operational storage-failure pause (#87). Mutually exclusive with the
	// host pause in practice because every command path is fenced while a
	// pause is active.
	storagePaused   bool
	retryAttempt    int
	pendingMessages []any
	pendingRows     []storage.EventRow
	pendingResult   *storage.MatchResult
	finishAfterSave bool
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
	cpuPlayers := make(map[domain.PlayerID]bool)
	setups := make([]match.TeamSetup, 0, 2)
	for _, team := range []domain.TeamID{domain.TeamA, domain.TeamB} {
		pieceIDs := make([]domain.PieceID, 0, settings.PieceCount)
		for number := 1; number <= settings.PieceCount; number++ {
			pieceIDs = append(pieceIDs, domain.PieceID(fmt.Sprintf("%s-%d", team, number)))
		}
		setups = append(setups, match.TeamSetup{TeamID: team, PieceIDs: pieceIDs})
		for _, id := range teamPlayers[team] {
			teamOf[id] = team
			if players[id].CPU {
				cpuPlayers[id] = true
			}
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
		cpuPlayers:  cpuPlayers,
		game:        game,
		cpu:         policy,
		throwResult: sampler.Throw,
	}, nil
}

// startMatchBroadcastsLocked stages GAME_STARTED and the first TURN_STARTED
// on the caller's transaction after the runtime was attached. The caller
// already flipped the room into its in-match status.
func (registry *RoomRegistry) startMatchBroadcastsLocked(tx *eventTx, entry *registeredRoom, rt *matchRuntime) error {
	entry.runtime = rt
	bukDestination := rt.game.Snapshot().BukDestinationSpaceID
	var destinationPointer *domain.SpaceID
	if bukDestination != "" {
		value := bukDestination
		destinationPointer = &value
	}
	tx.emit(func(sequence uint64) (any, error) {
		return protocol.NewGameStartedEvent(rt.roomID, rt.matchID, sequence, protocol.GameStartedPayload{
			FirstPlayerID:       rt.order[0],
			BukDestinationSpace: destinationPointer,
		})
	})
	return registry.beginTurnLocked(entry, rt, tx)
}

func (registry *RoomRegistry) beginTurnLocked(entry *registeredRoom, rt *matchRuntime, tx *eventTx) error {
	machine, err := turn.NewMachine(rt.currentPlayer(), rt.settings)
	if err != nil {
		return fmt.Errorf("%w: assemble turn machine: %v", ErrInvalidConfiguration, err)
	}
	rt.machine = machine
	rt.pendingMovePiece = ""
	rt.cpuControlled = rt.cpuPlayers[rt.currentPlayer()]
	if err := machine.Start(); err != nil {
		return fmt.Errorf("%w: start turn: %v", ErrInvalidConfiguration, err)
	}
	registry.stageTurnStarted(tx, rt)
	if rt.cpuControlled {
		player := rt.currentPlayer()
		tx.emit(func(sequence uint64) (any, error) {
			return protocol.NewCPUControlStartedEvent(rt.roomID, rt.matchID, sequence, protocol.CPUControlStartedPayload{
				PlayerID: player, Reason: cpuControlReasonLobbyPlayer,
			})
		})
		registry.runCpuTurnLocked(entry, rt, tx)
		return nil
	}
	registry.scheduleThrowTimerLocked(rt)
	return nil
}

// endTurnLocked closes the finished turn and opens the next player's turn.
// CPU substitution never leaks across turns: control resets before the next
// window opens (docs/03 시간 초과).
func (registry *RoomRegistry) endTurnLocked(entry *registeredRoom, rt *matchRuntime, tx *eventTx) error {
	registry.cancelTimerLocked(rt)
	rt.turnIndex = (rt.turnIndex + 1) % len(rt.order)
	return registry.beginTurnLocked(entry, rt, tx)
}

// PauseGame starts the canonical per-match host pause: the active turn
// window is cancelled and its kind plus remaining milliseconds are preserved
// for resume (docs/03 일시 정지, ADR-0003).
func (registry *RoomRegistry) PauseGame(user auth.UserID, roomID domain.RoomID, matchID domain.MatchID, minutes int) error {
	registry.mutex.Lock()
	defer registry.mutex.Unlock()

	entry, rt, err := registry.liveMatchLocked(user, roomID, matchID)
	if err != nil {
		return err
	}
	if entry.host != user {
		return ErrNotRoomHost
	}
	if minutes < protocol.MinPauseDurationMinutes || minutes > protocol.MaxPauseDurationMinutes {
		return fmt.Errorf("%w: pause duration %d", ErrInvalidCommand, minutes)
	}
	if rt.paused {
		return ErrMatchPaused
	}
	if rt.pauseUsed {
		return ErrMatchPauseUsed
	}

	now := registry.matchClock.Now()
	preservedKind := rt.timerKind
	preservedRemaining := time.Duration(rt.remainingMS(now)) * time.Millisecond
	rt.pauseUsed = true
	rt.paused = true
	rt.preservedTimerKind = preservedKind
	rt.preservedRemaining = preservedRemaining
	registry.cancelTimerLocked(rt)

	nowCopy := now
	rt.pauseEndsAt = nowCopy.Add(time.Duration(minutes) * time.Minute)
	pauseDeadline := rt.pauseEndsAt
	// The expiry timer lives outside activeTimer so an operational storage
	// pause entering later cannot cancel the host's auto-resume (#87).
	rt.pauseExpiryTimer = registry.matchClock.AfterFunc(
		time.Duration(minutes)*time.Minute,
		func() { registry.firePauseExpiry(roomID, pauseDeadline) },
	)

	tx := registry.newEventTx(roomID)
	endsAt := rt.pauseEndsAt.UTC().Format(time.RFC3339)
	pausedBy := domain.PlayerID(user)
	tx.emit(func(sequence uint64) (any, error) {
		return protocol.NewGamePausedEvent(rt.roomID, rt.matchID, sequence, protocol.GamePausedPayload{
			Reason:           protocol.PauseReasonHostRequest,
			PausedByPlayerID: &pausedBy,
			EndsAt:           &endsAt,
			PreservedTimerMS: uint64(preservedRemaining.Milliseconds()),
		})
	})
	return tx.flush()
}

// ResumeGame ends an active host pause early, restoring the preserved turn
// window (docs/03: 재개 시 보존한 남은 시간부터 같은 타이머를 계속 진행).
func (registry *RoomRegistry) ResumeGame(user auth.UserID, roomID domain.RoomID, matchID domain.MatchID) error {
	registry.mutex.Lock()
	defer registry.mutex.Unlock()

	entry, rt, err := registry.liveMatchLocked(user, roomID, matchID)
	if err != nil {
		return err
	}
	if entry.host != user {
		return ErrNotRoomHost
	}
	if !rt.paused {
		return ErrMatchNotPaused
	}
	if rt.pauseExpiryTimer != nil {
		rt.pauseExpiryTimer.Stop()
		rt.pauseExpiryTimer = nil
	}
	tx := registry.newEventTx(roomID)
	if err := registry.resumeMatchLocked(tx, rt, protocol.ResumeReasonHostRequest); err != nil {
		return err
	}
	return tx.flush()
}

// firePauseExpiry auto-resumes the match when the scheduled pause window
// elapses. A stale deadline (resumed and re-paused meanwhile) is ignored.
func (registry *RoomRegistry) firePauseExpiry(roomID domain.RoomID, deadline time.Time) {
	registry.mutex.Lock()
	defer registry.mutex.Unlock()

	entry, exists := registry.rooms[roomID]
	if !exists || entry.poisoned {
		return
	}
	rt := entry.runtime
	if rt == nil {
		return
	}
	if rt.pauseExpiryTimer != nil {
		rt.pauseExpiryTimer.Stop()
		rt.pauseExpiryTimer = nil
	}
	// While the operational pause fences broadcasts, recovery settles the
	// expired host pause afterwards (settleUserPauseAfterRecoveryLocked).
	if rt.storagePaused {
		return
	}
	if !rt.paused || !rt.pauseEndsAt.Equal(deadline) {
		return
	}
	tx := registry.newEventTx(roomID)
	if err := registry.resumeMatchLocked(tx, rt, protocol.ResumeReasonPauseExpired); err != nil {
		return
	}
	_ = tx.flush()
}

// resumeMatchLocked restores the preserved turn window and stages the
// GAME_RESUMED broadcast.
func (registry *RoomRegistry) resumeMatchLocked(tx *eventTx, rt *matchRuntime, reason string) error {
	rt.paused = false
	if rt.activeTimer != nil {
		rt.activeTimer.Stop()
		rt.activeTimer = nil
	}
	if rt.preservedTimerKind != "" && rt.preservedRemaining > 0 && !rt.cpuControlled {
		switch rt.preservedTimerKind {
		case matchTimerKindThrow:
			registry.armTimerLocked(rt, matchTimerKindThrow, rt.preservedRemaining)
		case matchTimerKindMove:
			registry.armTimerLocked(rt, matchTimerKindMove, rt.preservedRemaining)
		}
	}
	rt.preservedTimerKind = ""
	rt.preservedRemaining = 0
	tx.emit(func(sequence uint64) (any, error) {
		return protocol.NewGameResumedEvent(rt.roomID, rt.matchID, sequence, protocol.GameResumedPayload{Reason: reason})
	})
	return nil
}

func (registry *RoomRegistry) armTimerLocked(rt *matchRuntime, kind string, duration time.Duration) {
	rt.timerGeneration++
	rt.timerKind = kind
	now := registry.matchClock.Now()
	rt.timerDeadline = now.Add(duration)
	generation := rt.timerGeneration
	roomID := rt.roomID
	rt.activeTimer = registry.matchClock.AfterFunc(duration, func() {
		registry.fireTurnTimeout(roomID, generation)
	})
}

func (registry *RoomRegistry) finishMatchLocked(entry *registeredRoom, rt *matchRuntime, tx *eventTx) error {
	winner := rt.game.Snapshot().WinnerTeamID
	result := matchResultForRuntime(rt, winner)
	registry.cancelTimerLocked(rt)
	tx.emit(func(sequence uint64) (any, error) {
		return protocol.NewFinishedGameEndedEvent(
			rt.roomID, rt.matchID, sequence, winner, gameEndedReasonAllFinished,
		)
	})

	// Canonical end-of-match return to the same waiting room (docs/05): keep
	// the runtime attached until its terminal event and statistics transaction
	// commits, then release started state in detachFinishedMatchLocked.
	entry.confirmation = nil
	entry.roomStatus = protocol.RoomStatusPostMatch
	resetReadyStatesLocked(entry)
	status := entry.roomStatus
	tx.emit(func(sequence uint64) (any, error) {
		return protocol.NewRoomUpdatedEvent(rt.roomID, sequence, status)
	})
	tx.markMatchFinished()
	// A future lifecycle feature may allow every human to leave while lobby
	// CPUs finish the match. There is no user statistic to persist in that
	// case, but the terminal event must still commit normally.
	if len(result.Winners) != 0 || len(result.Losers) != 0 {
		tx.recordMatchResult(result)
	}
	return nil
}

func (registry *RoomRegistry) detachFinishedMatchLocked(entry *registeredRoom) {
	entry.started = false
	entry.confirmation = nil
	entry.runtime = nil
}

func matchResultForRuntime(rt *matchRuntime, winner domain.TeamID) storage.MatchResult {
	result := storage.MatchResult{
		Winners: make([]auth.UserID, 0, len(rt.order)),
		Losers:  make([]auth.UserID, 0, len(rt.order)),
	}
	// Player IDs originate only at playerIDFromUser before start, and order /
	// teamOf are the immutable started roster. Finish must therefore never
	// depend on mutable lobby membership or current connection state.
	for _, playerID := range rt.order {
		if rt.cpuPlayers[playerID] {
			continue
		}
		userID := auth.UserID(playerID)
		if rt.teamOf[playerID] == winner {
			result.Winners = append(result.Winners, userID)
		} else {
			result.Losers = append(result.Losers, userID)
		}
	}
	sort.Slice(result.Winners, func(left, right int) bool { return result.Winners[left] < result.Winners[right] })
	sort.Slice(result.Losers, func(left, right int) bool { return result.Losers[left] < result.Losers[right] })
	return result
}

// ---------------------------------------------------------------------------
// Storage-failure operational pause (#87)

// enterStoragePauseLocked replaces poison fencing for started rooms: the
// failed batch is preserved verbatim, the active turn window is preserved
// like a host pause, and canonical retries run on the match clock. The
// deferred GAME_PAUSED marker rides the same batch so the store and the
// broadcast stream stay byte-identical (ADR-0017).
func (registry *RoomRegistry) enterStoragePauseLocked(entry *registeredRoom, rt *matchRuntime, messages []any, rows []storage.EventRow, result *storage.MatchResult, finishAfterSave bool) {
	now := registry.matchClock.Now()
	rt.storagePaused = true
	rt.retryAttempt = 0
	rt.pendingMessages = messages
	rt.pendingRows = rows
	rt.pendingResult = result
	rt.finishAfterSave = finishAfterSave
	if finishAfterSave {
		roomID := rt.roomID
		rt.activeTimer = registry.matchClock.AfterFunc(storageRetryDelays[0], func() {
			registry.fireStorageRetry(roomID, 0)
		})
		return
	}
	if rt.preservedTimerKind == "" && rt.timerKind != "" {
		rt.preservedTimerKind = rt.timerKind
		rt.preservedRemaining = time.Duration(rt.remainingMS(now)) * time.Millisecond
	}
	registry.cancelTimerLocked(rt)

	// Stage the deferred GAME_PAUSED(storage_failure) marker behind the
	// failed batch; it commits only when recovery persists them together.
	// The marker reports the preserved window value directly - cancelTimer
	// has already cleared the live deadline by this point.
	preservedMS := uint64(rt.preservedRemaining.Milliseconds())
	boundary, err := registry.sequences.Boundary(rt.roomID)
	if err != nil {
		slog.Error("storage pause boundary read failed", "room_id", string(rt.roomID), "error", err)
		return
	}
	sequence := boundary + uint64(len(rows)) + 1
	pausedEvent, buildErr := protocol.NewGamePausedEvent(rt.roomID, rt.matchID, sequence, protocol.GamePausedPayload{
		Reason:           protocol.PauseReasonStorageFailure,
		PreservedTimerMS: preservedMS,
	})
	if buildErr != nil {
		slog.Error("storage pause marker build failed", "room_id", string(rt.roomID), "error", buildErr)
		return
	}
	encoded, err := json.Marshal(pausedEvent)
	if err != nil {
		slog.Error("storage pause marker encode failed", "room_id", string(rt.roomID), "error", err)
		return
	}
	rt.pendingMessages = append(rt.pendingMessages, pausedEvent)
	rt.pendingRows = append(rt.pendingRows, storage.EventRow{
		RoomID:      rt.roomID,
		Sequence:    sequence,
		EventType:   protocol.EventGamePaused,
		PayloadJSON: encoded,
	})

	expected := 0
	roomID := rt.roomID
	rt.activeTimer = registry.matchClock.AfterFunc(storageRetryDelays[0], func() {
		registry.fireStorageRetry(roomID, expected)
	})
}

// fireStorageRetry re-attempts the pending batch on the canonical schedule.
// Every row in rt.pendingRows is appended exactly once: success commits and
// delivers them before the resume marker becomes its own minimal operation,
// so a duplicate-key failure against a healthy store is impossible by
// construction. Exhausting the schedule invalidates the match.
func (registry *RoomRegistry) fireStorageRetry(roomID domain.RoomID, expectedAttempt int) {
	registry.mutex.Lock()
	defer registry.mutex.Unlock()

	entry, exists := registry.rooms[roomID]
	if !exists || entry.poisoned {
		return
	}
	rt := entry.runtime
	if rt == nil || !rt.storagePaused || rt.retryAttempt != expectedAttempt {
		return
	}

	ctx, cancel := eventStoreContext()
	defer cancel()
	appendEvents := registry.store.AppendRoomEvents
	if rt.pendingResult != nil {
		resultStore, ok := registry.store.(storage.MatchResultStore)
		if !ok {
			appendErr := fmt.Errorf("event store cannot persist match results")
			registry.invalidateMatchLocked(entry, rt, appendErr)
			return
		}
		appendEvents = func(ctx context.Context, rows []storage.EventRow) error {
			return resultStore.AppendMatchFinalization(ctx, rows, *rt.pendingResult)
		}
	}
	appendErr := appendEvents(ctx, rt.pendingRows)
	if appendErr != nil {
		rt.retryAttempt++
		next := rt.retryAttempt
		slog.Error("event store retry failed",
			"room_id", string(rt.roomID),
			"attempt", next,
			"error", appendErr,
		)
		if next < len(storageRetryDelays) {
			roomIDCopy := roomID
			expected := next
			delay := storageRetryDelays[next]
			rt.activeTimer = registry.matchClock.AfterFunc(delay, func() {
				registry.fireStorageRetry(roomIDCopy, expected)
			})
			return
		}
		registry.invalidateMatchLocked(entry, rt, appendErr)
		return
	}

	// The pending batch is durably stored; commit and deliver it now so the
	// resume marker appends after rows that are already canonical.
	for range rt.pendingRows {
		if _, err := registry.sequences.CommitNext(rt.roomID); err != nil {
			slog.Error("storage recovery sequence commit failed", "room_id", string(rt.roomID), "error", err)
			return
		}
	}
	registry.publishCommittedLocked(entry, rt.pendingMessages)
	finishAfterSave := rt.finishAfterSave
	rt.pendingMessages = nil
	rt.pendingRows = nil
	rt.pendingResult = nil
	rt.finishAfterSave = false
	if finishAfterSave {
		registry.detachFinishedMatchLocked(entry)
		return
	}

	resumedEvent, err := protocol.NewGameResumedEvent(
		rt.roomID, rt.matchID,
		boundaryOfLocked(registry, rt.roomID)+1,
		protocol.GameResumedPayload{Reason: protocol.ResumeReasonStorageRecovered},
	)
	if err != nil {
		slog.Error("storage resume marker build failed", "room_id", string(rt.roomID), "error", err)
		entry.poisoned = true
		return
	}
	encoded, err := json.Marshal(resumedEvent)
	if err != nil {
		slog.Error("storage resume marker encode failed", "room_id", string(rt.roomID), "error", err)
		entry.poisoned = true
		return
	}

	ctx2, cancel2 := eventStoreContext()
	defer cancel2()
	resumedRow := storage.EventRow{
		RoomID:      rt.roomID,
		Sequence:    resumedEvent.Sequence,
		EventType:   protocol.EventGameResumed,
		PayloadJSON: encoded,
	}
	if appendErr := registry.store.AppendRoomEvents(ctx2, []storage.EventRow{resumedRow}); appendErr != nil {
		// The store flapped between the two appends: the resume marker
		// becomes the whole pending batch and the retry schedule restarts.
		// Rows already committed are never re-appended, so no duplicate can
		// occur.
		slog.Error("storage resume marker append failed", "room_id", string(rt.roomID), "error", appendErr)
		rt.pendingMessages = []any{resumedEvent}
		rt.pendingRows = []storage.EventRow{resumedRow}
		rt.retryAttempt = 0
		roomIDCopy := roomID
		rt.activeTimer = registry.matchClock.AfterFunc(storageRetryDelays[0], func() {
			registry.fireStorageRetry(roomIDCopy, 0)
		})
		return
	}
	if _, err := registry.sequences.CommitNext(rt.roomID); err != nil {
		slog.Error("storage resume sequence commit failed", "room_id", string(rt.roomID), "error", err)
		entry.poisoned = true
		return
	}
	registry.publishCommittedLocked(entry, []any{resumedEvent})

	rt.storagePaused = false
	rt.retryAttempt = 0

	// A host pause that was already active when the storage pause entered
	// survives this recovery untouched: settle its expiry instead of
	// touching the turn window it is preserving.
	if rt.paused {
		registry.settleUserPauseAfterRecoveryLocked(entry, rt)
		return
	}
	switch rt.preservedTimerKind {
	case matchTimerKindThrow:
		registry.armTimerLocked(rt, matchTimerKindThrow, rt.preservedRemaining)
	case matchTimerKindMove:
		registry.armTimerLocked(rt, matchTimerKindMove, rt.preservedRemaining)
	}
	rt.preservedTimerKind = ""
	rt.preservedRemaining = 0
}

// settleUserPauseAfterRecoveryLocked fires or reschedules the surviving host
// pause once broadcasts work again. An already-elapsed window auto-resumes
// through the normal persisted path; otherwise only the expiry timer is
// rearmed for the remainder.
func (registry *RoomRegistry) settleUserPauseAfterRecoveryLocked(entry *registeredRoom, rt *matchRuntime) {
	now := registry.matchClock.Now()
	remaining := rt.pauseEndsAt.Sub(now)
	if remaining <= 0 {
		tx := registry.newEventTx(rt.roomID)
		if err := registry.resumeMatchLocked(tx, rt, protocol.ResumeReasonPauseExpired); err != nil {
			slog.Error("post-recovery pause expiry failed", "room_id", string(rt.roomID), "error", err)
			return
		}
		if err := tx.flush(); err != nil {
			slog.Error("post-recovery pause flush failed", "room_id", string(rt.roomID), "error", err)
		}
		return
	}
	roomIDCopy := rt.roomID
	deadline := rt.pauseEndsAt
	rt.pauseExpiryTimer = registry.matchClock.AfterFunc(remaining, func() {
		registry.firePauseExpiry(roomIDCopy, deadline)
	})
}

func boundaryOfLocked(registry *RoomRegistry, roomID domain.RoomID) uint64 {
	boundary, err := registry.sequences.Boundary(roomID)
	if err != nil {
		return 0
	}
	return boundary
}

func (registry *RoomRegistry) invalidateMatchLocked(entry *registeredRoom, rt *matchRuntime, lastErr error) {
	slog.Error("match invalidated after storage retries exhausted",
		"room_id", string(rt.roomID),
		"match_id", string(rt.matchID),
		"pending_rows", len(rt.pendingRows),
		"last_error", lastErr,
	)

	lastSequence := rt.pendingRows[len(rt.pendingRows)-1].Sequence
	invalidEnd, endErr := protocol.NewInvalidGameEndedEvent(
		rt.roomID, rt.matchID, lastSequence+1,
		gameEndedReasonStorageRetryExhausted,
	)
	if endErr != nil {
		slog.Error("invalidation event build failed", "room_id", string(rt.roomID), "error", endErr)
		return
	}
	endEncoded, err := json.Marshal(invalidEnd)
	if err != nil {
		slog.Error("invalidation event encode failed", "room_id", string(rt.roomID), "error", err)
		return
	}

	// The post-match waiting-room transition rides the terminal batch so a
	// still-broken store cannot fence the room out of rematches forever.
	entry.roomStatus = protocol.RoomStatusPostMatch
	resetReadyStatesLocked(entry)
	postMatch, err := protocol.NewRoomUpdatedEvent(rt.roomID, lastSequence+2, entry.roomStatus)
	if err != nil {
		slog.Error("post-match event build failed", "room_id", string(rt.roomID), "error", err)
		return
	}
	postEncoded, err := json.Marshal(postMatch)
	if err != nil {
		slog.Error("post-match event encode failed", "room_id", string(rt.roomID), "error", err)
		return
	}

	rows := append(rt.pendingRows, storage.EventRow{
		RoomID:      rt.roomID,
		Sequence:    invalidEnd.Sequence,
		EventType:   protocol.EventGameEnded,
		PayloadJSON: endEncoded,
	}, storage.EventRow{
		RoomID:      rt.roomID,
		Sequence:    postMatch.Sequence,
		EventType:   protocol.EventRoomUpdated,
		PayloadJSON: postEncoded,
	})
	messages := append(rt.pendingMessages, invalidEnd, postMatch)

	// Best-effort persist: clients are notified either way (spec
	// invalidation_persist_failure). A failure leaves a sequence gap in the
	// canonical store, recorded in docs/12.
	ctx, cancel := eventStoreContext()
	appendErr := registry.store.AppendRoomEvents(ctx, rows)
	cancel()
	if appendErr == nil {
		for range rows {
			if _, err := registry.sequences.CommitNext(rt.roomID); err != nil {
				slog.Error("invalidation sequence commit failed", "room_id", string(rt.roomID), "error", err)
				break
			}
		}
	} else {
		slog.Error("invalidation persist failed; sequence gap possible",
			"room_id", string(rt.roomID),
			"error", appendErr,
		)
	}

	registry.publishCommittedLocked(entry, messages)

	// Terminal teardown mirrors finishMatchLocked minus the winner flow.
	rt.pendingMessages = nil
	rt.pendingRows = nil
	rt.storagePaused = false
	rt.retryAttempt = 0
	rt.paused = false
	rt.pauseUsed = false
	entry.started = false
	entry.confirmation = nil
	entry.runtime = nil
}
