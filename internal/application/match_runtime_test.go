package application

import (
	"encoding/json"
	"sync"
	"testing"
	"time"

	"buk-yutnori/internal/auth"
	"buk-yutnori/internal/domain"
	"buk-yutnori/internal/domain/board"
	"buk-yutnori/internal/domain/room"
	"buk-yutnori/internal/storage"
)

// ---------------------------------------------------------------------------
// Shared fixtures

var (
	canonicalGraphOnce sync.Once
	canonicalGraph     *board.Graph
)

func mustAttachCanonicalBoardGraph(t *testing.T, registry *RoomRegistry) {
	t.Helper()
	canonicalGraphOnce.Do(func() {
		graph, err := board.LoadFile("../../spec/board_graph.yaml")
		if err != nil {
			t.Fatalf("LoadFile(canonical board graph) error = %v", err)
		}
		canonicalGraph = graph
	})
	if err := registry.AttachBoardGraph(canonicalGraph); err != nil {
		t.Fatalf("AttachBoardGraph() error = %v", err)
	}
}

// manualMatchClock records armed deadlines and fires them synchronously in
// deadline order during Advance, mirroring ADR-0003 monotonic semantics.
type manualMatchClock struct {
	now    time.Time
	timers []*manualMatchTimer
}

type manualMatchTimer struct {
	clock    *manualMatchClock
	deadline time.Time
	fire     func()
	stopped  bool
}

func (c *manualMatchClock) Now() time.Time { return c.now }

func (c *manualMatchClock) AfterFunc(d time.Duration, f func()) matchTimer {
	timer := &manualMatchTimer{clock: c, deadline: c.now.Add(d), fire: f}
	c.timers = append(c.timers, timer)
	return timer
}

func (timer *manualMatchTimer) Stop() bool {
	timer.stopped = true
	return true
}

func (c *manualMatchClock) Advance(d time.Duration) {
	target := c.now.Add(d)
	for {
		var next *manualMatchTimer
		for _, timer := range c.timers {
			if timer.stopped || !timer.deadline.After(c.now) || timer.deadline.After(target) {
				continue
			}
			if next == nil || timer.deadline.Before(next.deadline) {
				next = timer
			}
		}
		if next == nil {
			break
		}
		c.now = next.deadline
		next.fire()
	}
	c.now = target
}

type matchEventEnvelope struct {
	Raw      json.RawMessage  `json:"-"`
	Type     string           `json:"type"`
	Sequence uint64           `json:"sequence"`
	RoomID   domain.RoomID    `json:"room_id"`
	MatchID  *domain.MatchID  `json:"match_id"`
	Payload  matchEventFields `json:"payload"`
}

type matchEventFields struct {
	Status        string          `json:"status,omitempty"`
	PlayerID      domain.PlayerID `json:"player_id,omitempty"`
	FirstPlayerID domain.PlayerID `json:"first_player_id,omitempty"`
	RequiredInput string          `json:"required_input,omitempty"`
	Phase         string          `json:"phase,omitempty"`
	Reason        string          `json:"reason,omitempty"`
	WinnerTeamID  *domain.TeamID  `json:"winner_team_id,omitempty"`

	Token   *protocolTokenView   `json:"token,omitempty"`
	TokenID domain.ResultTokenID `json:"token_id,omitempty"`

	MovementKind       string                 `json:"movement_kind,omitempty"`
	PieceIDs           []domain.PieceID       `json:"piece_ids,omitempty"`
	CapturedPieceIDs   []domain.PieceID       `json:"captured_piece_ids,omitempty"`
	TokenIDs           []domain.ResultTokenID `json:"token_ids,omitempty"`
	SpaceID            domain.SpaceID         `json:"space_id,omitempty"`
	StackID            string                 `json:"stack_id,omitempty"`
	NoCandidate        bool                   `json:"no_candidate,omitempty"`
	DestinationSpaceID domain.SpaceID         `json:"destination_space_id,omitempty"`
}

type protocolTokenView struct {
	TokenID domain.ResultTokenID `json:"token_id"`
	Result  domain.YutResult     `json:"result"`
	Origin  domain.ResultOrigin  `json:"origin"`
}

// eventRecorder collects hub deliveries synchronously on the test
// goroutine. The fixture raises the subscription buffer so long matches
// never trip the production fail-closed drop.
type eventRecorder struct {
	subscription *RoomEventSubscription
	events       []matchEventEnvelope
}

// setEventBufferForTest raises the hub buffer so whole-match recordings fit
// without tripping the production fail-closed drop. Must run before any
// subscription.
func (registry *RoomRegistry) setEventBufferForTest(size int) {
	registry.mutex.Lock()
	defer registry.mutex.Unlock()
	registry.eventBufferSize = size
}

func newEventRecorder(t *testing.T, registry *RoomRegistry, user auth.UserID) *eventRecorder {
	t.Helper()
	registry.setEventBufferForTest(1 << 20)
	subscription, err := registry.SubscribeEvents(user)
	if err != nil {
		t.Fatalf("SubscribeEvents(%s) error = %v", user, err)
	}
	return &eventRecorder{subscription: subscription}
}

// flush drains everything delivered so far into the local slice.
func (recorder *eventRecorder) flush() {
	for {
		select {
		case event, ok := <-recorder.subscription.Events():
			if !ok {
				return
			}
			raw, err := json.Marshal(event.Message)
			if err != nil {
				continue
			}
			var envelope matchEventEnvelope
			if json.Unmarshal(raw, &envelope) != nil {
				continue
			}
			envelope.Raw = json.RawMessage(append([]byte(nil), raw...))
			recorder.events = append(recorder.events, envelope)
		default:
			return
		}
	}
}

func (recorder *eventRecorder) snapshotEvents() []matchEventEnvelope {
	recorder.flush()
	return append([]matchEventEnvelope(nil), recorder.events...)
}

// close unsubscribes the connection.
func (recorder *eventRecorder) close() {
	recorder.subscription.Close()
}

func (recorder *eventRecorder) ofTypes(types ...string) []matchEventEnvelope {
	wanted := make(map[string]struct{}, len(types))
	for _, kind := range types {
		wanted[kind] = struct{}{}
	}
	filtered := make([]matchEventEnvelope, 0)
	for _, envelope := range recorder.snapshotEvents() {
		if _, ok := wanted[envelope.Type]; ok {
			filtered = append(filtered, envelope)
		}
	}
	return filtered
}

type matchFixture struct {
	registry  *RoomRegistry
	roomID    domain.RoomID
	matchID   domain.MatchID
	users     []auth.UserID
	clock     *manualMatchClock
	recorder  *eventRecorder
	processor *Processor
	store     *fakeEventStore
}

const reconnectSpectatorID = "usr_VVVVVVVVVVVVVVVVVVVVVQ"

const (
	matchHostID  = "usr_MzMzMzMzMzMzMzMzMzMzMw"
	matchGuestID = "usr_RERERERERERERERERERERA"
)

// newMatchFixture builds a fully started two-player room whose runtime uses
// a manual clock and fixed seeds, so every scenario stays deterministic.
// Hooks run after the roster is ready and before the start request, e.g. to
// admit spectators while the lobby still accepts membership changes.
func newMatchFixture(t *testing.T, mutate func(*room.Settings), hooks ...func(*matchFixture)) *matchFixture {
	t.Helper()

	wallClock := &manualClock{current: time.Date(2026, 8, 23, 9, 0, 0, 0, time.UTC)}
	registry := newTestRegistryWithClock(t, wallClock.Now)
	matchClock := &manualMatchClock{now: wallClock.current}
	// The durable store attaches before any room exists, mirroring
	// production wiring, so every committed event is persisted.
	store := &fakeEventStore{}
	if err := registry.AttachEventStore(store); err != nil {
		t.Fatalf("AttachEventStore() error = %v", err)
	}
	registry.setMatchClock(matchClock)
	registry.setMatchRandomSeed(func() (uint64, uint64, error) { return 0xA11CE, 0xB0B, nil })

	settings := room.DefaultSettings()
	settings.MaxPlayers = 2
	settings.PieceCount = 2
	settings.ThrowTimeoutSeconds = 10
	settings.MoveTimeoutSeconds = 30
	if mutate != nil {
		mutate(&settings)
	}

	users := []auth.UserID{matchHostID, matchGuestID}
	summary, err := registry.Create(CreateRoomInput{
		Creator:  users[0],
		Creation: room.Creation{Title: "경기 방"},
		Settings: settings,
		Team:     domain.TeamA,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if _, err := registry.Join(JoinRoomInput{
		User: users[1], RoomID: summary.RoomID, Role: RolePlayer, Team: domain.TeamB,
	}); err != nil {
		t.Fatalf("Join(guest) error = %v", err)
	}
	for _, user := range users {
		if err := registry.SetReady(user, summary.RoomID, true); err != nil {
			t.Fatalf("SetReady(%s) error = %v", user, err)
		}
	}

	recorder := newEventRecorder(t, registry, users[0])
	fixture := &matchFixture{
		registry: registry,
		roomID:   summary.RoomID,
		users:    users,
		clock:    matchClock,
		recorder: recorder,
	}
	for _, hook := range hooks {
		hook(fixture)
	}
	if err := registry.RequestStart(users[0], summary.RoomID); err != nil {
		t.Fatalf("RequestStart() error = %v", err)
	}
	fixture.matchID = readActiveStart(t, registry, summary.RoomID).MatchID
	for _, user := range users {
		if err := registry.ConfirmStart(user, summary.RoomID, fixture.matchID); err != nil {
			t.Fatalf("ConfirmStart(%s) error = %v", user, err)
		}
	}
	matchExecutor, err := NewMatchCommandExecutor(registry)
	if err != nil {
		t.Fatalf("NewMatchCommandExecutor() error = %v", err)
	}
	processor, err := NewProcessor(matchExecutor)
	if err != nil {
		t.Fatalf("NewProcessor() error = %v", err)
	}
	fixture.processor = processor
	fixture.store = store
	return fixture
}

// setEventStoreForTest swaps the durable store mid-test for failure injection.
func (registry *RoomRegistry) setEventStoreForTest(store storage.EventStore) {
	registry.mutex.Lock()
	defer registry.mutex.Unlock()
	registry.store = store
}

func readActiveStart(t *testing.T, registry *RoomRegistry, roomID domain.RoomID) ActiveStartSnapshot {
	t.Helper()
	detail, err := registry.Detail(auth.UserID(matchHostID), roomID)
	if err != nil {
		t.Fatalf("Detail() error = %v", err)
	}
	if detail.ActiveStart == nil {
		t.Fatal("no active start confirmation while window is open")
	}
	return *detail.ActiveStart
}

func (fixture *matchFixture) runtime() *matchRuntime {
	fixture.registry.mutex.Lock()
	defer fixture.registry.mutex.Unlock()
	entry, exists := fixture.registry.rooms[fixture.roomID]
	if !exists {
		return nil
	}
	return entry.runtime
}

func (fixture *matchFixture) otherPlayer(player domain.PlayerID) domain.PlayerID {
	for _, user := range fixture.users {
		if domain.PlayerID(user) != player {
			return domain.PlayerID(user)
		}
	}
	return player
}

// throwUntilResolved issues THROW_YUT while the same player still owes one
// (yut/mo extra chain), stopping when a selection input opens or the turn
// passes on.
func (fixture *matchFixture) throwUntilResolved(t *testing.T, player domain.PlayerID) {
	t.Helper()
	for step := 0; step < 16; step++ {
		rt := fixture.runtime()
		if rt == nil {
			return
		}
		snapshot := rt.machine.Snapshot()
		if snapshot.RequiredInput != domain.InputThrow || rt.currentPlayer() != player {
			return
		}
		if err := fixture.registry.ThrowYut(auth.UserID(player), fixture.roomID, fixture.matchID); err != nil {
			t.Fatalf("THROW_YUT(%s) error = %v", player, err)
		}
	}
	t.Fatal("throw chain exceeded step cap")
}

func (fixture *matchFixture) throwTimeout() time.Duration {
	return time.Duration(fixture.runtime().settings.ThrowTimeoutSeconds) * time.Second
}

func (fixture *matchFixture) moveTimeout() time.Duration {
	return time.Duration(fixture.runtime().settings.MoveTimeoutSeconds) * time.Second
}

// driveUntilPlayerOrEnd executes the acting player's required inputs until
// control passes to another player or the match ends.
func (fixture *matchFixture) driveUntilPlayerOrEnd(t *testing.T, player domain.PlayerID) {
	t.Helper()
	for step := 0; step < 128; step++ {
		rt := fixture.runtime()
		if rt == nil {
			return
		}
		snapshot := rt.machine.Snapshot()
		if snapshot.Phase == domain.TurnWaitThrow && rt.currentPlayer() != player {
			return
		}
		current := rt.currentPlayer()
		var err error
		switch snapshot.RequiredInput {
		case domain.InputThrow:
			err = fixture.registry.ThrowYut(auth.UserID(current), fixture.roomID, fixture.matchID)
		case domain.InputSelectResult:
			err = fixture.registry.SelectResult(auth.UserID(current), fixture.roomID, fixture.matchID, snapshot.ResultQueue[0].ID)
		case domain.InputSelectPiece:
			movable, movableErr := rt.movablePieceIDs(snapshot)
			if movableErr != nil {
				t.Fatalf("movablePieceIDs() error = %v", movableErr)
			}
			if len(movable) == 0 {
				t.Fatal("piece selection offered without movable pieces")
			}
			err = fixture.registry.SelectPiece(auth.UserID(current), fixture.roomID, fixture.matchID, snapshot.SelectedTokenID, movable[0])
		case domain.InputSelectRoute:
			err = fixture.registry.SelectRoute(auth.UserID(current), fixture.roomID, fixture.matchID, snapshot.SelectedTokenID, rt.pendingMovePiece, domain.RouteNormal)
		default:
			return
		}
		if err != nil {
			t.Fatalf("drive step for %s failed: %v", current, err)
		}
	}
	t.Fatal("drive loop exceeded step cap")
}

// playWholeMatch drives both players until the runtime detaches at GAME_ENDED.
func (fixture *matchFixture) playWholeMatch(t *testing.T) {
	t.Helper()
	for turn := 0; turn < 4096; turn++ {
		rt := fixture.runtime()
		if rt == nil {
			return
		}
		player := rt.currentPlayer()
		fixture.driveUntilPlayerOrEnd(t, player)
	}
	t.Fatal("match did not end within the turn cap")
}
