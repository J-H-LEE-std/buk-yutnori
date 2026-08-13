package match

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"reflect"
	"testing"

	"buk-yutnori/internal/domain"
	"buk-yutnori/internal/domain/board"
	"buk-yutnori/internal/domain/room"
	"buk-yutnori/internal/domain/turn"
)

func TestNewGameSelectsAndPublishesCanonicalBukDestination(t *testing.T) {
	tests := []struct {
		name       string
		random     bool
		tickets    []uint64
		want       domain.SpaceID
		wantLimits []uint64
	}{
		{name: "fixed", want: "jji_do"},
		{
			name:       "first random candidate selected once",
			random:     true,
			tickets:    []uint64{0},
			want:       "back_do",
			wantLimits: []uint64{10},
		},
		{
			name:       "last random candidate selected once",
			random:     true,
			tickets:    []uint64{9},
			want:       "sok_mo",
			wantLimits: []uint64{10},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			settings := room.DefaultSettings()
			settings.PieceCount = 2
			settings.BukModeEnabled = true
			settings.RandomBukDestination = test.random
			source := &sequenceSource{values: test.tickets}

			game := newCanonicalGameWithSource(t, settings, source)
			for attempt := 0; attempt < 2; attempt++ {
				if got := game.Snapshot().BukDestinationSpaceID; got != test.want {
					t.Fatalf("BukDestinationSpaceID = %q, want %q", got, test.want)
				}
			}
			if !reflect.DeepEqual(source.limits, test.wantLimits) {
				t.Fatalf("random limits = %v, want %v", source.limits, test.wantLimits)
			}
		})
	}

	if got := newCanonicalGame(t, room.DefaultSettings()).Snapshot().BukDestinationSpaceID; got != "" {
		t.Fatalf("disabled Buk destination = %q, want empty", got)
	}
}

func TestNewGameRequiresValidRandomSourceForBuk(t *testing.T) {
	settings := room.DefaultSettings()
	settings.PieceCount = 2
	settings.BukModeEnabled = true
	graph := loadCanonicalGraph(t)
	teams := canonicalTeamSetups(settings.PieceCount)

	if game, err := NewGame(graph, settings, teams); !errors.Is(err, ErrNilRandomSource) || game != nil {
		t.Fatalf("NewGame(no source) = (%#v, %v), want nil ErrNilRandomSource", game, err)
	}
	var typedNil *sequenceSource
	if game, err := NewGameWithRandomSource(graph, settings, teams, typedNil); !errors.Is(err, ErrNilRandomSource) || game != nil {
		t.Fatalf("NewGameWithRandomSource(typed nil) = (%#v, %v), want nil ErrNilRandomSource", game, err)
	}

	settings.RandomBukDestination = true
	source := &sequenceSource{values: []uint64{10}}
	if game, err := NewGameWithRandomSource(graph, settings, teams, source); !errors.Is(err, ErrRandomSourceOutOfRange) || game != nil {
		t.Fatalf("NewGameWithRandomSource(out of range) = (%#v, %v), want nil ErrRandomSourceOutOfRange", game, err)
	}
}

func TestNewGameRejectsInvalidBukPlannerContract(t *testing.T) {
	settings := bukSettings(2)
	teams := canonicalTeamSetups(2)
	source := &sequenceSource{}

	if game, err := NewGameWithRandomSource(
		stubForwardPlanner{},
		settings,
		teams,
		source,
	); !errors.Is(err, ErrInvalidBukPlanner) || game != nil {
		t.Fatalf("planner without Buk contract = (%#v, %v), want nil ErrInvalidBukPlanner", game, err)
	}

	graph := loadCanonicalGraph(t)
	invalidFixed := &invalidBukPlanner{Graph: graph, fixed: "do"}
	if game, err := NewGameWithRandomSource(
		invalidFixed,
		settings,
		teams,
		source,
	); !errors.Is(err, ErrInvalidBukPlanner) || game != nil {
		t.Fatalf("invalid fixed destination = (%#v, %v), want nil ErrInvalidBukPlanner", game, err)
	}

	settings.RandomBukDestination = true
	duplicateCandidates := graph.BukCandidates()
	duplicateCandidates[9] = duplicateCandidates[0]
	invalidRandom := &invalidBukPlanner{Graph: graph, candidates: duplicateCandidates}
	if game, err := NewGameWithRandomSource(
		invalidRandom,
		settings,
		teams,
		source,
	); !errors.Is(err, ErrInvalidBukPlanner) || game != nil {
		t.Fatalf("invalid random destinations = (%#v, %v), want nil ErrInvalidBukPlanner", game, err)
	}
}

func TestBukRandomnessIsReproducibleWithServerSeed(t *testing.T) {
	settings := bukSettings(3)
	settings.RandomBukDestination = true
	settings.StackingEnabled = false

	first := newCanonicalGameWithSource(t, settings, rand.New(rand.NewPCG(17, 29)))
	second := newCanonicalGameWithSource(t, settings, rand.New(rand.NewPCG(17, 29)))
	if first.Snapshot().BukDestinationSpaceID != second.Snapshot().BukDestinationSpaceID {
		t.Fatalf(
			"same seed destinations differ: %q and %q",
			first.Snapshot().BukDestinationSpaceID,
			second.Snapshot().BukDestinationSpaceID,
		)
	}

	for _, game := range []*Game{first, second} {
		moveToMoDo(t, game, "A-1")
		moveToMoDo(t, game, "A-2")
		moveToJjiMo(t, game, domain.TeamA, "A-3")
	}
	firstOutcome, err := first.ResolveBuk(domain.TeamA)
	if err != nil {
		t.Fatalf("first ResolveBuk() error = %v", err)
	}
	secondOutcome, err := second.ResolveBuk(domain.TeamA)
	if err != nil {
		t.Fatalf("second ResolveBuk() error = %v", err)
	}
	if !reflect.DeepEqual(firstOutcome.SelectedPieceIDs, secondOutcome.SelectedPieceIDs) {
		t.Fatalf(
			"same seed selections differ: %v and %v",
			firstOutcome.SelectedPieceIDs,
			secondOutcome.SelectedPieceIDs,
		)
	}
}

func TestResolveBukUsesConfiguredShortcutPolicyForFinishDistance(t *testing.T) {
	for _, test := range []struct {
		name   string
		policy room.ShortcutPolicy
		want   board.ShortcutPolicy
	}{
		{name: "selectable", policy: room.ShortcutSelectable, want: board.SelectableShortcuts},
		{name: "forced", policy: room.ShortcutForced, want: board.ForcedShortcuts},
	} {
		t.Run(test.name, func(t *testing.T) {
			settings := bukSettings(2)
			settings.ShortcutPolicy = test.policy
			graph := loadCanonicalGraph(t)
			planner := &recordingDistancePlanner{Graph: graph}
			game, err := NewGameWithRandomSource(
				planner,
				settings,
				canonicalTeamSetups(2),
				&sequenceSource{},
			)
			if err != nil {
				t.Fatalf("NewGameWithRandomSource() error = %v", err)
			}
			applyMove(t, game, domain.TeamA, "A-1", domain.YutDo, "")
			if _, err := game.ResolveBuk(domain.TeamA); err != nil {
				t.Fatalf("ResolveBuk() error = %v", err)
			}
			if !reflect.DeepEqual(planner.policies, []board.ShortcutPolicy{test.want}) {
				t.Fatalf("distance policies = %v, want [%v]", planner.policies, test.want)
			}
		})
	}
}

func TestResolveBukMovesClosestPositionGroupToFixedDestination(t *testing.T) {
	settings := bukSettings(2)
	source := &sequenceSource{}
	game := newCanonicalGameWithSource(t, settings, source)
	moveToJjiYut(t, game, domain.TeamA, "A-1")
	applyMove(t, game, domain.TeamA, "A-2", domain.YutGae, "")

	outcome, err := game.ResolveBuk(domain.TeamA)
	if err != nil {
		t.Fatalf("ResolveBuk() error = %v", err)
	}
	if outcome.NoCandidate || !outcome.Moved || outcome.DestinationSpaceID != "jji_do" ||
		!reflect.DeepEqual(outcome.SelectedPieceIDs, []domain.PieceID{"A-1"}) {
		t.Fatalf("Buk outcome = %#v", outcome)
	}
	if outcome.Move.MovementKind != domain.MovementBuk || outcome.Move.FromSpaceID != "jji_yut" ||
		outcome.Move.ToSpaceID != "jji_do" ||
		!reflect.DeepEqual(outcome.Move.MovedPieceIDs, []domain.PieceID{"A-1"}) {
		t.Fatalf("Buk move = %#v", outcome.Move)
	}
	moved := requirePiece(t, game.Snapshot(), "A-1")
	if moved.CurrentSpaceID != "jji_do" || moved.ActualPreviousSpace != "jji_yut" {
		t.Fatalf("selected piece after Buk = %#v", moved)
	}
	if untouched := requirePiece(t, game.Snapshot(), "A-2"); untouched.CurrentSpaceID != "gae" {
		t.Fatalf("farther piece changed = %#v", untouched)
	}
	if len(source.limits) != 0 {
		t.Fatalf("unique closest group used random source with limits %v", source.limits)
	}
}

func TestResolveBukIncludesHomeCheckpointAndExcludesFinishedPieces(t *testing.T) {
	t.Run("home checkpoint has distance one", func(t *testing.T) {
		game := newCanonicalGameWithSource(t, bukSettings(2), &sequenceSource{})
		applyMove(t, game, domain.TeamA, "A-1", domain.YutDo, "")
		applyBackdo(t, game, domain.TeamA, "A-1")
		moveToNalYutForTeam(t, game, domain.TeamA, "A-2")

		outcome, err := game.ResolveBuk(domain.TeamA)
		if err != nil {
			t.Fatalf("ResolveBuk() error = %v", err)
		}
		if !reflect.DeepEqual(outcome.SelectedPieceIDs, []domain.PieceID{"A-1"}) {
			t.Fatalf("SelectedPieceIDs = %v, want home-checkpoint A-1", outcome.SelectedPieceIDs)
		}
		piece := requirePiece(t, game.Snapshot(), "A-1")
		if piece.CurrentSpaceID != "jji_do" || piece.ActualPreviousSpace != "chammeogi" {
			t.Fatalf("home-checkpoint piece after Buk = %#v", piece)
		}
	})

	t.Run("finished piece is excluded", func(t *testing.T) {
		game := newCanonicalGameWithSource(t, bukSettings(2), &sequenceSource{})
		moveToNalYutForTeam(t, game, domain.TeamA, "A-1")
		applyMove(t, game, domain.TeamA, "A-1", domain.YutGae, "")
		applyMove(t, game, domain.TeamA, "A-2", domain.YutDo, "")

		outcome, err := game.ResolveBuk(domain.TeamA)
		if err != nil {
			t.Fatalf("ResolveBuk() error = %v", err)
		}
		if !reflect.DeepEqual(outcome.SelectedPieceIDs, []domain.PieceID{"A-2"}) {
			t.Fatalf("SelectedPieceIDs = %v, want unfinished A-2", outcome.SelectedPieceIDs)
		}
		if piece := requirePiece(t, game.Snapshot(), "A-1"); piece.State != domain.PieceFinished {
			t.Fatalf("finished piece changed = %#v", piece)
		}
	})
}

func TestResolveBukWeightsEqualDistanceGroupsByCurrentPieceCount(t *testing.T) {
	tests := []struct {
		ticket uint64
		want   []domain.PieceID
	}{
		{ticket: 0, want: []domain.PieceID{"A-1", "A-2"}},
		{ticket: 1, want: []domain.PieceID{"A-1", "A-2"}},
		{ticket: 2, want: []domain.PieceID{"A-3"}},
	}

	for _, test := range tests {
		t.Run(fmt.Sprintf("ticket_%d", test.ticket), func(t *testing.T) {
			settings := bukSettings(3)
			settings.StackingEnabled = false
			source := &sequenceSource{values: []uint64{test.ticket}}
			game := newCanonicalGameWithSource(t, settings, source)
			moveToMoDo(t, game, "A-1")
			moveToMoDo(t, game, "A-2")
			moveToJjiMo(t, game, domain.TeamA, "A-3")

			outcome, err := game.ResolveBuk(domain.TeamA)
			if err != nil {
				t.Fatalf("ResolveBuk() error = %v", err)
			}
			if !reflect.DeepEqual(outcome.SelectedPieceIDs, test.want) {
				t.Fatalf("SelectedPieceIDs = %v, want %v", outcome.SelectedPieceIDs, test.want)
			}
			if !reflect.DeepEqual(source.limits, []uint64{3}) {
				t.Fatalf("random limits = %v, want [3]", source.limits)
			}
			for _, id := range test.want {
				if got := requirePiece(t, game.Snapshot(), id).CurrentSpaceID; got != "jji_do" {
					t.Fatalf("selected piece %q destination = %q, want jji_do", id, got)
				}
			}
		})
	}
}

func TestResolveBukHandlesNoCandidateAndDestinationNoOp(t *testing.T) {
	t.Run("no candidate", func(t *testing.T) {
		game := newCanonicalGameWithSource(t, bukSettings(2), &sequenceSource{})
		before := game.Snapshot()
		outcome, err := game.ResolveBuk(domain.TeamA)
		if err != nil {
			t.Fatalf("ResolveBuk() error = %v", err)
		}
		if !outcome.NoCandidate || outcome.Moved || len(outcome.SelectedPieceIDs) != 0 {
			t.Fatalf("Buk outcome = %#v, want no candidate", outcome)
		}
		if got := outcome.TurnOutcome(); got != (turn.BukOutcome{NoCandidate: true}) {
			t.Fatalf("TurnOutcome() = %#v, want no candidate", got)
		}
		if after := game.Snapshot(); !reflect.DeepEqual(after, before) {
			t.Fatal("no-candidate Buk changed game state")
		}
	})

	t.Run("selected group already at destination", func(t *testing.T) {
		game := newCanonicalGameWithSource(t, bukSettings(2), &sequenceSource{})
		moveToJjiDo(t, game, domain.TeamA, "A-1")
		before := game.Snapshot()
		outcome, err := game.ResolveBuk(domain.TeamA)
		if err != nil {
			t.Fatalf("ResolveBuk() error = %v", err)
		}
		if outcome.NoCandidate || outcome.Moved || outcome.DestinationSpaceID != "jji_do" ||
			!reflect.DeepEqual(outcome.SelectedPieceIDs, []domain.PieceID{"A-1"}) {
			t.Fatalf("Buk outcome = %#v, want selected no-op", outcome)
		}
		if after := game.Snapshot(); !reflect.DeepEqual(after, before) {
			t.Fatal("destination no-op Buk changed game state")
		}
	})
}

func TestResolveBukMovesSameSpaceGroupWhenStackingDisabledAndCapturesAll(t *testing.T) {
	settings := bukSettings(2)
	settings.StackingEnabled = false
	settings.CaptureExtraThrow = room.CaptureExtraThrowDoToGeolPlusSpecial
	game := newCanonicalGameWithSource(t, settings, &sequenceSource{})
	moveToNalYutForTeam(t, game, domain.TeamA, "A-1")
	moveToNalYutForTeam(t, game, domain.TeamA, "A-2")
	moveToJjiDo(t, game, domain.TeamB, "B-1")
	moveToJjiDo(t, game, domain.TeamB, "B-2")

	outcome, err := game.ResolveBuk(domain.TeamA)
	if err != nil {
		t.Fatalf("ResolveBuk() error = %v", err)
	}
	if !outcome.Moved ||
		!reflect.DeepEqual(outcome.SelectedPieceIDs, []domain.PieceID{"A-1", "A-2"}) ||
		!reflect.DeepEqual(outcome.Move.CapturedPieceIDs, []domain.PieceID{"B-1", "B-2"}) ||
		!outcome.Move.CaptureExtraThrow || outcome.Move.StackedPieceIDs != nil {
		t.Fatalf("Buk capture outcome = %#v", outcome)
	}
	if got := outcome.TurnOutcome(); got != (turn.BukOutcome{CaptureExtraThrow: true}) {
		t.Fatalf("TurnOutcome() = %#v, want capture extra throw", got)
	}
	for _, id := range []domain.PieceID{"A-1", "A-2"} {
		piece := requirePiece(t, game.Snapshot(), id)
		if piece.CurrentSpaceID != "jji_do" || piece.ActualPreviousSpace != "nal_yut" {
			t.Fatalf("moved piece %q = %#v", id, piece)
		}
	}
	for _, id := range []domain.PieceID{"B-1", "B-2"} {
		piece := requirePiece(t, game.Snapshot(), id)
		if piece.State != domain.PieceWaiting || piece.CurrentSpaceID != "" ||
			piece.ActualPreviousSpace != "" {
			t.Fatalf("captured piece %q = %#v", id, piece)
		}
	}
}

func TestResolveBukStacksAtDestinationAndBackdoReturnsToBukOrigin(t *testing.T) {
	game := newCanonicalGameWithSource(t, bukSettings(2), &sequenceSource{})
	moveToNalYutForTeam(t, game, domain.TeamA, "A-1")
	moveToJjiDo(t, game, domain.TeamA, "A-2")

	outcome, err := game.ResolveBuk(domain.TeamA)
	if err != nil {
		t.Fatalf("ResolveBuk() error = %v", err)
	}
	if !reflect.DeepEqual(outcome.SelectedPieceIDs, []domain.PieceID{"A-1"}) ||
		!reflect.DeepEqual(outcome.Move.StackedPieceIDs, []domain.PieceID{"A-1", "A-2"}) {
		t.Fatalf("Buk stacking outcome = %#v", outcome)
	}
	for _, id := range []domain.PieceID{"A-1", "A-2"} {
		piece := requirePiece(t, game.Snapshot(), id)
		if piece.CurrentSpaceID != "jji_do" || piece.ActualPreviousSpace != "nal_yut" {
			t.Fatalf("stacked piece %q = %#v", id, piece)
		}
	}

	backdo := applyBackdo(t, game, domain.TeamA, "A-1")
	if backdo.ToSpaceID != "nal_yut" ||
		!reflect.DeepEqual(backdo.MovedPieceIDs, []domain.PieceID{"A-1", "A-2"}) {
		t.Fatalf("Backdo after Buk = %#v", backdo)
	}
}

func TestResolveBukRejectsDisabledModeInvalidRandomAndDistanceFailureAtomically(t *testing.T) {
	t.Run("disabled mode", func(t *testing.T) {
		game := newCanonicalGame(t, room.DefaultSettings())
		before := game.Snapshot()
		if _, err := game.ResolveBuk(domain.TeamA); !errors.Is(err, ErrBukModeDisabled) {
			t.Fatalf("ResolveBuk() error = %v, want ErrBukModeDisabled", err)
		}
		if after := game.Snapshot(); !reflect.DeepEqual(after, before) {
			t.Fatal("disabled Buk changed game state")
		}
	})

	t.Run("weighted ticket out of range", func(t *testing.T) {
		settings := bukSettings(3)
		settings.StackingEnabled = false
		game := newCanonicalGameWithSource(t, settings, &sequenceSource{values: []uint64{3}})
		moveToMoDo(t, game, "A-1")
		moveToMoDo(t, game, "A-2")
		moveToJjiMo(t, game, domain.TeamA, "A-3")
		before := game.Snapshot()
		if _, err := game.ResolveBuk(domain.TeamA); !errors.Is(err, ErrRandomSourceOutOfRange) {
			t.Fatalf("ResolveBuk() error = %v, want ErrRandomSourceOutOfRange", err)
		}
		if after := game.Snapshot(); !reflect.DeepEqual(after, before) {
			t.Fatal("rejected weighted selection changed game state")
		}
	})

	t.Run("distance planner failure", func(t *testing.T) {
		settings := bukSettings(2)
		graph := loadCanonicalGraph(t)
		planner := failingDistancePlanner{Graph: graph, err: errors.New("distance failed")}
		game, err := NewGameWithRandomSource(
			planner,
			settings,
			canonicalTeamSetups(2),
			&sequenceSource{},
		)
		if err != nil {
			t.Fatalf("NewGame() error = %v", err)
		}
		applyMove(t, game, domain.TeamA, "A-1", domain.YutDo, "")
		before := game.Snapshot()
		if _, err := game.ResolveBuk(domain.TeamA); err == nil || err.Error() != "distance failed" {
			t.Fatalf("ResolveBuk() error = %v, want distance failed", err)
		}
		if after := game.Snapshot(); !reflect.DeepEqual(after, before) {
			t.Fatal("distance failure changed game state")
		}
	})
}

func bukSettings(pieceCount int) room.Settings {
	settings := room.DefaultSettings()
	settings.PieceCount = pieceCount
	settings.BukModeEnabled = true
	return settings
}

func newCanonicalGameWithSource(
	t *testing.T,
	settings room.Settings,
	source BoundedSource,
) *Game {
	t.Helper()
	game, err := NewGameWithRandomSource(
		loadCanonicalGraph(t),
		settings,
		canonicalTeamSetups(settings.PieceCount),
		source,
	)
	if err != nil {
		t.Fatalf("NewGame() error = %v", err)
	}
	return game
}

func moveToMoDo(t *testing.T, game *Game, pieceID domain.PieceID) {
	t.Helper()
	applyMove(t, game, domain.TeamA, pieceID, domain.YutMo, "")
	applyMove(t, game, domain.TeamA, pieceID, domain.YutDo, domain.RouteShortcut)
}

func moveToJjiDo(t *testing.T, game *Game, teamID domain.TeamID, pieceID domain.PieceID) {
	t.Helper()
	applyMove(t, game, teamID, pieceID, domain.YutMo, "")
	applyMove(t, game, teamID, pieceID, domain.YutMo, domain.RouteNormal)
	applyMove(t, game, teamID, pieceID, domain.YutDo, domain.RouteNormal)
}

func moveToJjiYut(t *testing.T, game *Game, teamID domain.TeamID, pieceID domain.PieceID) {
	t.Helper()
	applyMove(t, game, teamID, pieceID, domain.YutMo, "")
	applyMove(t, game, teamID, pieceID, domain.YutMo, domain.RouteNormal)
	applyMove(t, game, teamID, pieceID, domain.YutYut, domain.RouteNormal)
}

func moveToJjiMo(t *testing.T, game *Game, teamID domain.TeamID, pieceID domain.PieceID) {
	t.Helper()
	applyMove(t, game, teamID, pieceID, domain.YutMo, "")
	applyMove(t, game, teamID, pieceID, domain.YutMo, domain.RouteNormal)
	applyMove(t, game, teamID, pieceID, domain.YutMo, domain.RouteNormal)
}

func moveToNalYutForTeam(t *testing.T, game *Game, teamID domain.TeamID, pieceID domain.PieceID) {
	t.Helper()
	applyMove(t, game, teamID, pieceID, domain.YutMo, "")
	applyMove(t, game, teamID, pieceID, domain.YutMo, domain.RouteNormal)
	applyMove(t, game, teamID, pieceID, domain.YutMo, domain.RouteNormal)
	applyMove(t, game, teamID, pieceID, domain.YutYut, "")
}

type sequenceSource struct {
	values []uint64
	limits []uint64
	next   int
}

func (source *sequenceSource) Uint64N(limit uint64) uint64 {
	source.limits = append(source.limits, limit)
	if source.next >= len(source.values) {
		return limit
	}
	value := source.values[source.next]
	source.next++
	return value
}

type failingDistancePlanner struct {
	*board.Graph
	err error
}

func (planner failingDistancePlanner) RemainingForwardDistance(
	board.Position,
	board.ShortcutPolicy,
) (int, error) {
	return 0, planner.err
}

type recordingDistancePlanner struct {
	*board.Graph
	policies []board.ShortcutPolicy
}

func (planner *recordingDistancePlanner) RemainingForwardDistance(
	position board.Position,
	policy board.ShortcutPolicy,
) (int, error) {
	planner.policies = append(planner.policies, policy)
	return planner.Graph.RemainingForwardDistance(position, policy)
}

type invalidBukPlanner struct {
	*board.Graph
	fixed      domain.SpaceID
	candidates []domain.SpaceID
}

func (planner *invalidBukPlanner) FixedBukDestination() domain.SpaceID {
	if planner.fixed != "" {
		return planner.fixed
	}
	return planner.Graph.FixedBukDestination()
}

func (planner *invalidBukPlanner) BukCandidates() []domain.SpaceID {
	if planner.candidates != nil {
		return append([]domain.SpaceID(nil), planner.candidates...)
	}
	return planner.Graph.BukCandidates()
}
