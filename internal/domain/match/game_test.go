package match

import (
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	"buk-yutnori/internal/domain"
	"buk-yutnori/internal/domain/board"
	"buk-yutnori/internal/domain/room"
	"buk-yutnori/internal/domain/turn"
)

func TestNewGameCreatesCanonicalWaitingPieces(t *testing.T) {
	game := newCanonicalGame(t, room.DefaultSettings())
	snapshot := game.Snapshot()

	if snapshot.WinnerTeamID != "" {
		t.Fatalf("WinnerTeamID = %q, want empty", snapshot.WinnerTeamID)
	}
	if len(snapshot.Pieces) != 8 {
		t.Fatalf("len(Pieces) = %d, want 8", len(snapshot.Pieces))
	}
	for _, piece := range snapshot.Pieces {
		if piece.State != domain.PieceWaiting || piece.CurrentSpaceID != "" ||
			piece.ActualPreviousSpace != "" {
			t.Fatalf("initial piece = %#v, want waiting with no path state", piece)
		}
	}

	snapshot.Pieces[0].State = domain.PieceFinished
	if got := game.Snapshot().Pieces[0].State; got != domain.PieceWaiting {
		t.Fatalf("mutating snapshot changed game state to %q", got)
	}
}

func TestNewGameRejectsInvalidConfiguration(t *testing.T) {
	graph := loadCanonicalGraph(t)
	settings := room.DefaultSettings()
	valid := canonicalTeamSetups(settings.PieceCount)

	tests := []struct {
		name     string
		planner  board.MovementPlanner
		settings room.Settings
		teams    []TeamSetup
	}{
		{name: "nil planner", settings: settings, teams: valid},
		{name: "typed nil planner", planner: (*board.Graph)(nil), settings: settings, teams: valid},
		{
			name:     "invalid settings",
			planner:  graph,
			settings: func() room.Settings { value := settings; value.PieceCount = 1; return value }(),
			teams:    valid,
		},
		{
			name:     "missing team",
			planner:  graph,
			settings: settings,
			teams:    valid[:1],
		},
		{
			name:     "wrong piece count",
			planner:  graph,
			settings: settings,
			teams: []TeamSetup{
				{TeamID: domain.TeamA, PieceIDs: []domain.PieceID{"A-1"}},
				valid[1],
			},
		},
		{
			name:     "duplicate piece ID",
			planner:  graph,
			settings: settings,
			teams: []TeamSetup{
				valid[0],
				{TeamID: domain.TeamB, PieceIDs: []domain.PieceID{"A-1", "B-2", "B-3", "B-4"}},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			game, err := NewGame(test.planner, test.settings, test.teams)
			if !errors.Is(err, ErrInvalidGameConfig) {
				t.Fatalf("NewGame() error = %v, want ErrInvalidGameConfig", err)
			}
			if game != nil {
				t.Fatalf("NewGame() = %#v, want nil", game)
			}
		})
	}
}

func TestOrdinaryMovePlansUseResultDistancesAndCanonicalRoutes(t *testing.T) {
	tests := []struct {
		result    domain.YutResult
		wantSpace domain.SpaceID
	}{
		{result: domain.YutDo, wantSpace: "do"},
		{result: domain.YutGae, wantSpace: "gae"},
		{result: domain.YutGeol, wantSpace: "geol"},
		{result: domain.YutYut, wantSpace: "yut"},
		{result: domain.YutMo, wantSpace: "mo"},
	}

	for _, test := range tests {
		t.Run(test.result.String(), func(t *testing.T) {
			settings := room.DefaultSettings()
			settings.PieceCount = 2
			game := newCanonicalGame(t, settings)

			plans, err := game.OrdinaryMovePlans(domain.TeamA, "A-1", test.result)
			if err != nil {
				t.Fatalf("OrdinaryMovePlans() error = %v", err)
			}
			plan := requireSinglePlan(t, plans)
			if plan.Route != domain.RouteNormal || plan.DestinationState != domain.PieceOnBoard ||
				plan.DestinationSpaceID != test.wantSpace {
				t.Fatalf("plan = %#v, want normal on-board destination %q", plan, test.wantSpace)
			}
			if !reflect.DeepEqual(plan.MovedPieceIDs, []domain.PieceID{"A-1"}) {
				t.Fatalf("MovedPieceIDs = %v, want [A-1]", plan.MovedPieceIDs)
			}
		})
	}

	settings := room.DefaultSettings()
	settings.PieceCount = 2
	game := newCanonicalGame(t, settings)
	applyMove(t, game, domain.TeamA, "A-1", domain.YutMo, "")
	plans, err := game.OrdinaryMovePlans(domain.TeamA, "A-1", domain.YutGeol)
	if err != nil {
		t.Fatalf("OrdinaryMovePlans(Mo) error = %v", err)
	}
	if len(plans) != 2 || plans[0].Route != domain.RouteNormal ||
		plans[0].DestinationSpaceID != "back_geol" ||
		plans[1].Route != domain.RouteShortcut || plans[1].DestinationSpaceID != "bang" {
		t.Fatalf("route plans = %#v, want normal back_geol and shortcut bang", plans)
	}

	plans[0].MovedPieceIDs[0] = "mutated"
	plans[0].Traversed[0] = "mutated"
	again, err := game.OrdinaryMovePlans(domain.TeamA, "A-1", domain.YutGeol)
	if err != nil {
		t.Fatalf("second OrdinaryMovePlans() error = %v", err)
	}
	if again[0].MovedPieceIDs[0] != "A-1" || again[0].Traversed[0] != "back_do" {
		t.Fatalf("mutating plans changed game/planner result: %#v", again[0])
	}
}

func TestApplyOrdinaryMoveValidatesOwnershipSpecialResultsAndRoutesAtomically(t *testing.T) {
	settings := room.DefaultSettings()
	settings.PieceCount = 2
	game := newCanonicalGame(t, settings)

	tests := []struct {
		name      string
		teamID    domain.TeamID
		pieceID   domain.PieceID
		result    domain.YutResult
		route     domain.Route
		wantError error
	}{
		{name: "unknown piece", teamID: domain.TeamA, pieceID: "missing", result: domain.YutDo, wantError: ErrUnknownPiece},
		{name: "opponent piece", teamID: domain.TeamA, pieceID: "B-1", result: domain.YutDo, wantError: ErrPieceNotOwned},
		{name: "Backdo is special", teamID: domain.TeamA, pieceID: "A-1", result: domain.YutBackdo, wantError: domain.ErrNotOrdinaryYutResult},
		{name: "Buk is special", teamID: domain.TeamA, pieceID: "A-1", result: domain.YutBuk, wantError: domain.ErrNotOrdinaryYutResult},
		{name: "route supplied without choice", teamID: domain.TeamA, pieceID: "A-1", result: domain.YutDo, route: domain.RouteNormal, wantError: ErrRouteSelectionNotAllowed},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			before := game.Snapshot()
			_, err := game.ApplyOrdinaryMove(test.teamID, test.pieceID, test.result, test.route)
			if !errors.Is(err, test.wantError) {
				t.Fatalf("ApplyOrdinaryMove() error = %v, want %v", err, test.wantError)
			}
			if after := game.Snapshot(); !reflect.DeepEqual(after, before) {
				t.Fatalf("failed move changed state:\nbefore=%#v\nafter=%#v", before, after)
			}
		})
	}

	applyMove(t, game, domain.TeamA, "A-1", domain.YutMo, "")
	before := game.Snapshot()
	if _, err := game.ApplyOrdinaryMove(domain.TeamA, "A-1", domain.YutDo, ""); !errors.Is(err, ErrRouteSelectionRequired) {
		t.Fatalf("ApplyOrdinaryMove(route required) error = %v", err)
	}
	if after := game.Snapshot(); !reflect.DeepEqual(after, before) {
		t.Fatal("missing route changed state")
	}
	outcome := applyMove(t, game, domain.TeamA, "A-1", domain.YutDo, domain.RouteShortcut)
	if outcome.ToSpaceID != "mo_do" || outcome.Route != domain.RouteShortcut {
		t.Fatalf("shortcut outcome = %#v, want mo_do", outcome)
	}
}

func TestForcedShortcutDoesNotAcceptPlayerRouteSelection(t *testing.T) {
	settings := room.DefaultSettings()
	settings.PieceCount = 2
	settings.ShortcutPolicy = room.ShortcutForced
	game := newCanonicalGame(t, settings)
	applyMove(t, game, domain.TeamA, "A-1", domain.YutMo, "")

	plans, err := game.OrdinaryMovePlans(domain.TeamA, "A-1", domain.YutDo)
	if err != nil {
		t.Fatalf("OrdinaryMovePlans() error = %v", err)
	}
	plan := requireSinglePlan(t, plans)
	if plan.Route != domain.RouteShortcut || plan.DestinationSpaceID != "mo_do" {
		t.Fatalf("forced plan = %#v, want shortcut to mo_do", plan)
	}

	before := game.Snapshot()
	if _, err := game.ApplyOrdinaryMove(
		domain.TeamA,
		"A-1",
		domain.YutDo,
		domain.RouteShortcut,
	); !errors.Is(err, ErrRouteSelectionNotAllowed) {
		t.Fatalf("ApplyOrdinaryMove(explicit forced route) error = %v", err)
	}
	if after := game.Snapshot(); !reflect.DeepEqual(after, before) {
		t.Fatal("rejected forced-route selection changed state")
	}

	outcome := applyMove(t, game, domain.TeamA, "A-1", domain.YutDo, "")
	if outcome.ToSpaceID != "mo_do" {
		t.Fatalf("forced move destination = %q, want mo_do", outcome.ToSpaceID)
	}
}

func TestStackingEnabledMovesAlliedPiecesAsOneAndInheritsArrivingHistory(t *testing.T) {
	settings := room.DefaultSettings()
	settings.PieceCount = 2
	game := newCanonicalGame(t, settings)

	applyMove(t, game, domain.TeamA, "A-1", domain.YutDo, "")
	stacked := applyMove(t, game, domain.TeamA, "A-2", domain.YutDo, "")
	if !reflect.DeepEqual(stacked.StackedPieceIDs, []domain.PieceID{"A-1", "A-2"}) {
		t.Fatalf("StackedPieceIDs = %v, want [A-1 A-2]", stacked.StackedPieceIDs)
	}

	moved := applyMove(t, game, domain.TeamA, "A-1", domain.YutGae, "")
	if !reflect.DeepEqual(moved.MovedPieceIDs, []domain.PieceID{"A-1", "A-2"}) {
		t.Fatalf("MovedPieceIDs = %v, want whole stack", moved.MovedPieceIDs)
	}
	for _, id := range []domain.PieceID{"A-1", "A-2"} {
		piece := requirePiece(t, game.Snapshot(), id)
		if piece.CurrentSpaceID != "geol" || piece.ActualPreviousSpace != "gae" {
			t.Fatalf("piece %q = %#v, want geol with previous gae", id, piece)
		}
	}
}

func TestStackingDisabledKeepsAlliedPiecesIndependent(t *testing.T) {
	settings := room.DefaultSettings()
	settings.PieceCount = 2
	settings.StackingEnabled = false
	game := newCanonicalGame(t, settings)

	applyMove(t, game, domain.TeamA, "A-1", domain.YutDo, "")
	second := applyMove(t, game, domain.TeamA, "A-2", domain.YutDo, "")
	if second.StackedPieceIDs != nil {
		t.Fatalf("StackedPieceIDs = %v, want nil", second.StackedPieceIDs)
	}
	applyMove(t, game, domain.TeamA, "A-1", domain.YutGae, "")

	snapshot := game.Snapshot()
	first := requirePiece(t, snapshot, "A-1")
	secondPiece := requirePiece(t, snapshot, "A-2")
	if first.CurrentSpaceID != "geol" || first.ActualPreviousSpace != "gae" {
		t.Fatalf("A-1 = %#v, want geol with previous gae", first)
	}
	if secondPiece.CurrentSpaceID != "do" || secondPiece.ActualPreviousSpace != "chammeogi" {
		t.Fatalf("A-2 = %#v, want independent state on do", secondPiece)
	}
}

func TestCaptureReturnsEveryOpponentOnDestinationToWaiting(t *testing.T) {
	for _, stacking := range []bool{false, true} {
		t.Run(map[bool]string{false: "independent opponents", true: "opponent stack"}[stacking], func(t *testing.T) {
			settings := room.DefaultSettings()
			settings.PieceCount = 2
			settings.StackingEnabled = stacking
			game := newCanonicalGame(t, settings)

			applyMove(t, game, domain.TeamB, "B-1", domain.YutDo, "")
			applyMove(t, game, domain.TeamB, "B-2", domain.YutDo, "")
			outcome := applyMove(t, game, domain.TeamA, "A-1", domain.YutDo, "")

			if !reflect.DeepEqual(outcome.CapturedPieceIDs, []domain.PieceID{"B-1", "B-2"}) {
				t.Fatalf("CapturedPieceIDs = %v, want [B-1 B-2]", outcome.CapturedPieceIDs)
			}
			if !outcome.CaptureExtraThrow {
				t.Fatal("CaptureExtraThrow = false under always policy")
			}
			for _, id := range outcome.CapturedPieceIDs {
				piece := requirePiece(t, game.Snapshot(), id)
				if piece.State != domain.PieceWaiting || piece.CurrentSpaceID != "" ||
					piece.ActualPreviousSpace != "" {
					t.Fatalf("captured piece %q = %#v, want cleared waiting state", id, piece)
				}
			}
		})
	}
}

func TestCaptureExtraThrowUsesRoomPolicyAndOrdinaryResult(t *testing.T) {
	tests := []struct {
		name   string
		policy room.CaptureExtraThrowPolicy
		result domain.YutResult
		want   bool
	}{
		{name: "always on Yut", policy: room.CaptureExtraThrowAlways, result: domain.YutYut, want: true},
		{name: "limited on Do", policy: room.CaptureExtraThrowDoToGeolPlusSpecial, result: domain.YutDo, want: true},
		{name: "limited excludes Yut", policy: room.CaptureExtraThrowDoToGeolPlusSpecial, result: domain.YutYut, want: false},
		{name: "none on Do", policy: room.CaptureExtraThrowNone, result: domain.YutDo, want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			settings := room.DefaultSettings()
			settings.PieceCount = 2
			settings.CaptureExtraThrow = test.policy
			game := newCanonicalGame(t, settings)
			applyMove(t, game, domain.TeamB, "B-1", test.result, "")
			outcome := applyMove(t, game, domain.TeamA, "A-1", test.result, "")
			if outcome.CaptureExtraThrow != test.want {
				t.Fatalf("CaptureExtraThrow = %v, want %v", outcome.CaptureExtraThrow, test.want)
			}
			if got := outcome.TurnOutcome(); got != (turn.MoveOutcome{CaptureExtraThrow: test.want}) {
				t.Fatalf("TurnOutcome() = %#v, want capture extra %v", got, test.want)
			}
		})
	}
}

func TestHomeCheckpointFinishStackAndVictory(t *testing.T) {
	settings := room.DefaultSettings()
	settings.PieceCount = 2
	game := newCanonicalGame(t, settings)

	moveToNalYut(t, game, "A-1")
	exact := applyMove(t, game, domain.TeamA, "A-1", domain.YutDo, "")
	if exact.MovementKind != domain.MovementForward || exact.ToSpaceID != "chammeogi" {
		t.Fatalf("checkpoint outcome = %#v", exact)
	}
	piece := requirePiece(t, game.Snapshot(), "A-1")
	if piece.State != domain.PieceHomeCheckpoint || piece.ActualPreviousSpace != "nal_yut" {
		t.Fatalf("checkpoint piece = %#v, want previous nal_yut", piece)
	}
	finished := applyMove(t, game, domain.TeamA, "A-1", domain.YutDo, "")
	if finished.MovementKind != domain.MovementFinish || finished.MatchEnded {
		t.Fatalf("first finish outcome = %#v, want non-terminal finish", finished)
	}
	piece = requirePiece(t, game.Snapshot(), "A-1")
	if piece.State != domain.PieceFinished || piece.CurrentSpaceID != "" || piece.ActualPreviousSpace != "" {
		t.Fatalf("finished piece = %#v, want cleared path state", piece)
	}

	moveToNalYut(t, game, "A-2")
	winning := applyMove(t, game, domain.TeamA, "A-2", domain.YutGae, "")
	if !winning.MatchEnded || winning.WinnerTeamID != domain.TeamA ||
		winning.MovementKind != domain.MovementFinish {
		t.Fatalf("winning outcome = %#v", winning)
	}
	if got := winning.TurnOutcome(); got != (turn.MoveOutcome{MatchEnded: true}) {
		t.Fatalf("TurnOutcome() = %#v, want match ended", got)
	}
	if game.Snapshot().WinnerTeamID != domain.TeamA {
		t.Fatalf("snapshot winner = %q, want A", game.Snapshot().WinnerTeamID)
	}

	before := game.Snapshot()
	if _, err := game.ApplyOrdinaryMove(domain.TeamB, "B-1", domain.YutDo, ""); !errors.Is(err, ErrMatchEnded) {
		t.Fatalf("move after victory error = %v, want ErrMatchEnded", err)
	}
	if after := game.Snapshot(); !reflect.DeepEqual(after, before) {
		t.Fatal("move after victory changed state")
	}
}

func TestStackedPiecesFinishTogether(t *testing.T) {
	settings := room.DefaultSettings()
	settings.PieceCount = 2
	game := newCanonicalGame(t, settings)
	moveToNalYut(t, game, "A-1")
	moveToNalYut(t, game, "A-2")

	before := game.Snapshot()
	for _, id := range []domain.PieceID{"A-1", "A-2"} {
		piece := requirePiece(t, before, id)
		if piece.CurrentSpaceID != "nal_yut" {
			t.Fatalf("piece %q before finish = %#v", id, piece)
		}
	}
	outcome := applyMove(t, game, domain.TeamA, "A-2", domain.YutGae, "")
	if !reflect.DeepEqual(outcome.MovedPieceIDs, []domain.PieceID{"A-1", "A-2"}) ||
		!outcome.MatchEnded {
		t.Fatalf("stack finish outcome = %#v", outcome)
	}
	for _, id := range outcome.MovedPieceIDs {
		if piece := requirePiece(t, game.Snapshot(), id); piece.State != domain.PieceFinished {
			t.Fatalf("piece %q state = %q, want finished", id, piece.State)
		}
	}
}

func newCanonicalGame(t *testing.T, settings room.Settings) *Game {
	t.Helper()
	game, err := NewGame(loadCanonicalGraph(t), settings, canonicalTeamSetups(settings.PieceCount))
	if err != nil {
		t.Fatalf("NewGame() error = %v", err)
	}
	return game
}

func canonicalTeamSetups(pieceCount int) []TeamSetup {
	all := map[domain.TeamID][]domain.PieceID{
		domain.TeamA: {"A-1", "A-2", "A-3", "A-4"},
		domain.TeamB: {"B-1", "B-2", "B-3", "B-4"},
	}
	return []TeamSetup{
		{TeamID: domain.TeamA, PieceIDs: append([]domain.PieceID(nil), all[domain.TeamA][:pieceCount]...)},
		{TeamID: domain.TeamB, PieceIDs: append([]domain.PieceID(nil), all[domain.TeamB][:pieceCount]...)},
	}
}

func loadCanonicalGraph(t *testing.T) *board.Graph {
	t.Helper()
	path := filepath.Join("..", "..", "..", "spec", "board_graph.yaml")
	graph, err := board.LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile(%q) error = %v", path, err)
	}
	return graph
}

func applyMove(
	t *testing.T,
	game *Game,
	teamID domain.TeamID,
	pieceID domain.PieceID,
	result domain.YutResult,
	route domain.Route,
) MoveOutcome {
	t.Helper()
	outcome, err := game.ApplyOrdinaryMove(teamID, pieceID, result, route)
	if err != nil {
		t.Fatalf(
			"ApplyOrdinaryMove(%q, %q, %q, %q) error = %v",
			teamID,
			pieceID,
			result,
			route,
			err,
		)
	}
	return outcome
}

func moveToNalYut(t *testing.T, game *Game, pieceID domain.PieceID) {
	t.Helper()
	applyMove(t, game, domain.TeamA, pieceID, domain.YutMo, "")
	applyMove(t, game, domain.TeamA, pieceID, domain.YutMo, domain.RouteNormal)
	applyMove(t, game, domain.TeamA, pieceID, domain.YutMo, domain.RouteNormal)
	applyMove(t, game, domain.TeamA, pieceID, domain.YutYut, "")
}

func requireSinglePlan(t *testing.T, plans []OrdinaryMovePlan) OrdinaryMovePlan {
	t.Helper()
	if len(plans) != 1 {
		t.Fatalf("len(plans) = %d, want 1", len(plans))
	}
	return plans[0]
}

func requirePiece(t *testing.T, snapshot Snapshot, id domain.PieceID) Piece {
	t.Helper()
	for _, piece := range snapshot.Pieces {
		if piece.ID == id {
			return piece
		}
	}
	t.Fatalf("piece %q is missing from snapshot", id)
	return Piece{}
}
