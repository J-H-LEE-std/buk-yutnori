package match

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"

	"buk-yutnori/internal/domain"
	"buk-yutnori/internal/domain/board"
	"buk-yutnori/internal/domain/room"
)

func TestNewGameRejectsMalformedTeamSetups(t *testing.T) {
	graph := loadCanonicalGraph(t)
	settings := room.DefaultSettings()
	settings.PieceCount = 2

	tests := []struct {
		name  string
		teams []TeamSetup
	}{
		{
			name: "duplicate team",
			teams: []TeamSetup{
				{TeamID: domain.TeamA, PieceIDs: []domain.PieceID{"A-1", "A-2"}},
				{TeamID: domain.TeamA, PieceIDs: []domain.PieceID{"B-1", "B-2"}},
			},
		},
		{
			name: "invalid team ID",
			teams: []TeamSetup{
				{TeamID: domain.TeamA, PieceIDs: []domain.PieceID{"A-1", "A-2"}},
				{TeamID: domain.TeamID("C"), PieceIDs: []domain.PieceID{"C-1", "C-2"}},
			},
		},
		{
			name: "empty piece ID",
			teams: []TeamSetup{
				{TeamID: domain.TeamA, PieceIDs: []domain.PieceID{"A-1", ""}},
				{TeamID: domain.TeamB, PieceIDs: []domain.PieceID{"B-1", "B-2"}},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			game, err := NewGame(graph, settings, test.teams)
			if !errors.Is(err, ErrInvalidGameConfig) {
				t.Fatalf("NewGame() error = %v, want ErrInvalidGameConfig", err)
			}
			if game != nil {
				t.Fatalf("NewGame() = %#v, want nil", game)
			}
		})
	}
}

func TestMalformedForwardPlannerResultsDoNotMutateGame(t *testing.T) {
	plannerError := errors.New("planner unavailable")
	validPlan := board.ForwardPlan{
		Route:               domain.RouteNormal,
		Destination:         board.Position{State: domain.PieceOnBoard, Space: "do"},
		ActualPreviousSpace: "chammeogi",
		Traversed:           []domain.SpaceID{"do"},
	}
	validShortcutPlan := board.ForwardPlan{
		Route:               domain.RouteShortcut,
		Destination:         board.Position{State: domain.PieceOnBoard, Space: "gae"},
		ActualPreviousSpace: "do",
		Traversed:           []domain.SpaceID{"gae"},
	}

	tests := []struct {
		name           string
		planner        board.MovementPlanner
		shortcutPolicy room.ShortcutPolicy
		wantError      error
	}{
		{
			name:      "planner error",
			planner:   stubForwardPlanner{err: plannerError},
			wantError: plannerError,
		},
		{
			name:      "empty plan list",
			planner:   stubForwardPlanner{},
			wantError: ErrInvalidForwardPlan,
		},
		{
			name: "invalid destination state",
			planner: stubForwardPlanner{plans: []board.ForwardPlan{{
				Route:       domain.RouteNormal,
				Destination: board.Position{State: domain.PieceWaiting},
			}}},
			wantError: ErrInvalidForwardPlan,
		},
		{
			name: "destination does not match traversal",
			planner: stubForwardPlanner{plans: []board.ForwardPlan{{
				Route:               domain.RouteNormal,
				Destination:         board.Position{State: domain.PieceOnBoard, Space: "do"},
				ActualPreviousSpace: "chammeogi",
				Traversed:           []domain.SpaceID{"gae"},
			}}},
			wantError: ErrInvalidForwardPlan,
		},
		{
			name: "actual previous space does not match multi-space traversal",
			planner: stubForwardPlanner{plans: []board.ForwardPlan{{
				Route:               domain.RouteNormal,
				Destination:         board.Position{State: domain.PieceOnBoard, Space: "gae"},
				ActualPreviousSpace: "mo",
				Traversed:           []domain.SpaceID{"do", "gae"},
			}}},
			wantError: ErrInvalidForwardPlan,
		},
		{
			name: "empty traversed space",
			planner: stubForwardPlanner{plans: []board.ForwardPlan{{
				Route:               domain.RouteNormal,
				Destination:         board.Position{State: domain.PieceOnBoard, Space: "do"},
				ActualPreviousSpace: "chammeogi",
				Traversed:           []domain.SpaceID{"", "do"},
			}}},
			wantError: ErrInvalidForwardPlan,
		},
		{
			name: "duplicate selectable routes",
			planner: stubForwardPlanner{plans: []board.ForwardPlan{
				validPlan,
				validPlan,
			}},
			wantError: ErrInvalidForwardPlan,
		},
		{
			name:      "single shortcut under selectable policy",
			planner:   stubForwardPlanner{plans: []board.ForwardPlan{validShortcutPlan}},
			wantError: ErrInvalidForwardPlan,
		},
		{
			name: "route choice under forced policy",
			planner: stubForwardPlanner{plans: []board.ForwardPlan{
				validPlan,
				validShortcutPlan,
			}},
			shortcutPolicy: room.ShortcutForced,
			wantError:      ErrInvalidForwardPlan,
		},
		{
			name: "too many plans",
			planner: stubForwardPlanner{plans: []board.ForwardPlan{
				validPlan,
				validShortcutPlan,
				validPlan,
			}},
			wantError: ErrInvalidForwardPlan,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			settings := room.DefaultSettings()
			settings.PieceCount = 2
			if test.shortcutPolicy != "" {
				settings.ShortcutPolicy = test.shortcutPolicy
			}
			game, err := NewGame(test.planner, settings, canonicalTeamSetups(settings.PieceCount))
			if err != nil {
				t.Fatalf("NewGame() error = %v", err)
			}
			before := game.Snapshot()
			_, err = game.ApplyOrdinaryMove(domain.TeamA, "A-1", domain.YutDo, "")
			if !errors.Is(err, test.wantError) {
				t.Fatalf("ApplyOrdinaryMove() error = %v, want %v", err, test.wantError)
			}
			if after := game.Snapshot(); !reflect.DeepEqual(after, before) {
				t.Fatalf("malformed planner result changed state:\nbefore=%#v\nafter=%#v", before, after)
			}
		})
	}
}

func TestMalformedBackdoPlannerResultsDoNotMutateGame(t *testing.T) {
	plannerError := errors.New("planner unavailable")
	tests := []struct {
		name      string
		plan      board.BackdoPlan
		err       error
		wantError error
	}{
		{
			name:      "planner error",
			err:       plannerError,
			wantError: plannerError,
		},
		{
			name: "invalid destination state",
			plan: board.BackdoPlan{
				Destination:         board.Position{State: domain.PieceWaiting},
				ActualPreviousSpace: "do",
			},
			wantError: ErrInvalidBackdoPlan,
		},
		{
			name: "empty destination space",
			plan: board.BackdoPlan{
				Destination:         board.Position{State: domain.PieceOnBoard},
				ActualPreviousSpace: "do",
			},
			wantError: ErrInvalidBackdoPlan,
		},
		{
			name: "previous does not become source",
			plan: board.BackdoPlan{
				Destination:         board.Position{State: domain.PieceHomeCheckpoint, Space: "chammeogi"},
				ActualPreviousSpace: "gae",
				Traversed:           []domain.SpaceID{"chammeogi"},
			},
			wantError: ErrInvalidBackdoPlan,
		},
		{
			name: "empty traversal",
			plan: board.BackdoPlan{
				Destination:         board.Position{State: domain.PieceHomeCheckpoint, Space: "chammeogi"},
				ActualPreviousSpace: "do",
			},
			wantError: ErrInvalidBackdoPlan,
		},
		{
			name: "traversal destination mismatch",
			plan: board.BackdoPlan{
				Destination:         board.Position{State: domain.PieceHomeCheckpoint, Space: "chammeogi"},
				ActualPreviousSpace: "do",
				Traversed:           []domain.SpaceID{"gae"},
			},
			wantError: ErrInvalidBackdoPlan,
		},
		{
			name: "extra traversal",
			plan: board.BackdoPlan{
				Destination:         board.Position{State: domain.PieceHomeCheckpoint, Space: "chammeogi"},
				ActualPreviousSpace: "do",
				Traversed:           []domain.SpaceID{"gae", "chammeogi"},
			},
			wantError: ErrInvalidBackdoPlan,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			settings := room.DefaultSettings()
			settings.PieceCount = 2
			game := newCanonicalGame(t, settings)
			applyMove(t, game, domain.TeamA, "A-1", domain.YutDo, "")
			game.planner = stubForwardPlanner{backdoPlan: test.plan, backdoErr: test.err}

			before := game.Snapshot()
			_, err := game.ApplyBackdoMove(domain.TeamA, "A-1")
			if !errors.Is(err, test.wantError) {
				t.Fatalf("ApplyBackdoMove() error = %v, want %v", err, test.wantError)
			}
			if after := game.Snapshot(); !reflect.DeepEqual(after, before) {
				t.Fatalf("malformed Backdo plan changed state:\nbefore=%#v\nafter=%#v", before, after)
			}
		})
	}
}

func TestInvalidRouteEnumDoesNotMutateGame(t *testing.T) {
	settings := room.DefaultSettings()
	settings.PieceCount = 2
	game := newCanonicalGame(t, settings)
	applyMove(t, game, domain.TeamA, "A-1", domain.YutMo, "")

	before := game.Snapshot()
	_, err := game.ApplyOrdinaryMove(
		domain.TeamA,
		"A-1",
		domain.YutDo,
		domain.Route("detour"),
	)
	if !errors.Is(err, ErrInvalidRouteSelection) {
		t.Fatalf("ApplyOrdinaryMove() error = %v, want ErrInvalidRouteSelection", err)
	}
	if after := game.Snapshot(); !reflect.DeepEqual(after, before) {
		t.Fatal("invalid route enum changed state")
	}
}

func TestOrdinaryMovePlansRejectsQueriesAfterMatchEnd(t *testing.T) {
	settings := room.DefaultSettings()
	settings.PieceCount = 2
	game := newCanonicalGame(t, settings)

	moveToNalYut(t, game, "A-1")
	applyMove(t, game, domain.TeamA, "A-1", domain.YutGae, "")
	moveToNalYut(t, game, "A-2")
	applyMove(t, game, domain.TeamA, "A-2", domain.YutGae, "")

	before := game.Snapshot()
	if _, err := game.OrdinaryMovePlans(domain.TeamB, "B-1", domain.YutDo); !errors.Is(err, ErrMatchEnded) {
		t.Fatalf("OrdinaryMovePlans() error = %v, want ErrMatchEnded", err)
	}
	if after := game.Snapshot(); !reflect.DeepEqual(after, before) {
		t.Fatal("planning after match end changed state")
	}
}

func TestGameSupportsConcurrentPlanningSnapshotsAndApplication(t *testing.T) {
	settings := room.DefaultSettings()
	settings.PieceCount = 2
	settings.ShortcutPolicy = room.ShortcutForced
	game := newCanonicalGame(t, settings)

	const attempts = 128
	errorsFound := make(chan error, 3)
	var waitGroup sync.WaitGroup
	waitGroup.Add(3)

	go func() {
		defer waitGroup.Done()
		for range attempts {
			if err := validateSnapshotPieceStates(game.Snapshot()); err != nil {
				errorsFound <- err
				return
			}
		}
	}()
	go func() {
		defer waitGroup.Done()
		for range attempts {
			plans, err := game.OrdinaryMovePlans(domain.TeamB, "B-1", domain.YutDo)
			if err != nil {
				errorsFound <- fmt.Errorf("planning: %w", err)
				return
			}
			if len(plans) != 1 || plans[0].DestinationSpaceID != "do" {
				errorsFound <- fmt.Errorf("unexpected waiting-piece plan: %#v", plans)
				return
			}
		}
	}()
	go func() {
		defer waitGroup.Done()
		for range attempts {
			_, err := game.ApplyOrdinaryMove(domain.TeamA, "A-1", domain.YutDo, "")
			if err != nil && !errors.Is(err, board.ErrForwardMovementUnavailable) {
				errorsFound <- fmt.Errorf("applying move: %w", err)
				return
			}
		}
	}()

	waitGroup.Wait()
	close(errorsFound)
	for err := range errorsFound {
		t.Error(err)
	}
	if err := validateSnapshotPieceStates(game.Snapshot()); err != nil {
		t.Error(err)
	}
}

type stubForwardPlanner struct {
	plans      []board.ForwardPlan
	err        error
	backdoPlan board.BackdoPlan
	backdoErr  error
}

func (planner stubForwardPlanner) ForwardPlans(
	board.Position,
	int,
	board.ShortcutPolicy,
) ([]board.ForwardPlan, error) {
	return planner.plans, planner.err
}

func (planner stubForwardPlanner) BackdoPlan(
	board.Position,
	board.SpaceID,
) (board.BackdoPlan, error) {
	return planner.backdoPlan, planner.backdoErr
}

func validateSnapshotPieceStates(snapshot Snapshot) error {
	if len(snapshot.Pieces) != 4 {
		return fmt.Errorf("snapshot has %d pieces, want 4", len(snapshot.Pieces))
	}
	seen := make(map[domain.PieceID]bool, len(snapshot.Pieces))
	for _, piece := range snapshot.Pieces {
		if seen[piece.ID] {
			return fmt.Errorf("snapshot contains duplicate piece %q", piece.ID)
		}
		seen[piece.ID] = true
		if err := piece.TeamID.Validate(); err != nil {
			return fmt.Errorf("piece %q team: %w", piece.ID, err)
		}
		switch piece.State {
		case domain.PieceWaiting, domain.PieceFinished:
			if piece.CurrentSpaceID != "" || piece.ActualPreviousSpace != "" {
				return fmt.Errorf("piece %q in state %q retains path state", piece.ID, piece.State)
			}
		case domain.PieceOnBoard, domain.PieceHomeCheckpoint:
			if piece.CurrentSpaceID == "" || piece.ActualPreviousSpace == "" {
				return fmt.Errorf("piece %q in state %q lacks path state", piece.ID, piece.State)
			}
		default:
			return fmt.Errorf("piece %q has invalid state %q", piece.ID, piece.State)
		}
	}
	return nil
}
