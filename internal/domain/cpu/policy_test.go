package cpu

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	"buk-yutnori/internal/domain"
	"buk-yutnori/internal/domain/board"
	"buk-yutnori/internal/domain/match"
	"buk-yutnori/internal/domain/room"
	"buk-yutnori/internal/domain/turn"
)

func TestDecideAlwaysUsesFIFOHeadAndResolvesBukAutomatically(t *testing.T) {
	settings := room.DefaultSettings()
	settings.PieceCount = 2
	planner := &fakeDistancePlanner{distances: map[domain.SpaceID]int{"far": 10}}
	policy := mustPolicy(t, planner, settings, &sequenceSource{})
	game := &fakeMatch{
		snapshot: match.Snapshot{Pieces: []match.Piece{
			{ID: "A-1", TeamID: domain.TeamA, State: domain.PieceOnBoard, CurrentSpaceID: "far"},
		}},
		ordinary: map[moveRequest][]match.OrdinaryMovePlan{
			{pieceID: "A-1", result: domain.YutDo}: {{
				DestinationState:   domain.PieceOnBoard,
				DestinationSpaceID: "next",
				MovedPieceIDs:      []domain.PieceID{"A-1"},
			}},
		},
	}

	decision, err := policy.Decide(
		game,
		turnSnapshot(
			resultToken("token-do", domain.YutDo),
			resultToken("token-buk", domain.YutBuk),
		),
		domain.TeamA,
	)
	if err != nil {
		t.Fatalf("Decide() error = %v", err)
	}
	if decision != (Decision{
		Action:  ActionMovePiece,
		TokenID: "token-do",
		Result:  domain.YutDo,
		PieceID: "A-1",
	}) {
		t.Fatalf("Decision = %#v, want FIFO Do move", decision)
	}

	decision, err = policy.Decide(
		game,
		turnSnapshot(
			resultToken("token-buk", domain.YutBuk),
			resultToken("token-do", domain.YutDo),
		),
		domain.TeamA,
	)
	if err != nil {
		t.Fatalf("Decide(Buk) error = %v", err)
	}
	if decision != (Decision{
		Action:  ActionResolveBuk,
		TokenID: "token-buk",
		Result:  domain.YutBuk,
	}) {
		t.Fatalf("Buk decision = %#v", decision)
	}
}

func TestDecideAppliesCanonicalPriorityOrder(t *testing.T) {
	tests := []struct {
		name      string
		pieces    []match.Piece
		plans     map[moveRequest][]match.OrdinaryMovePlan
		distances map[domain.SpaceID]int
		wantPiece domain.PieceID
	}{
		{
			name: "immediate finish before capture",
			pieces: []match.Piece{
				{ID: "A-finish", TeamID: domain.TeamA, State: domain.PieceHomeCheckpoint, CurrentSpaceID: "home"},
				{ID: "A-capture", TeamID: domain.TeamA, State: domain.PieceOnBoard, CurrentSpaceID: "far"},
				{ID: "B-target", TeamID: domain.TeamB, State: domain.PieceOnBoard, CurrentSpaceID: "target"},
			},
			plans: map[moveRequest][]match.OrdinaryMovePlan{
				{pieceID: "A-finish", result: domain.YutDo}: {{
					DestinationState: domain.PieceFinished,
					MovedPieceIDs:    []domain.PieceID{"A-finish"},
				}},
				{pieceID: "A-capture", result: domain.YutDo}: {{
					DestinationState:   domain.PieceOnBoard,
					DestinationSpaceID: "target",
					MovedPieceIDs:      []domain.PieceID{"A-capture"},
				}},
			},
			distances: map[domain.SpaceID]int{"home": 1, "far": 12},
			wantPiece: "A-finish",
		},
		{
			name: "capture before stacking",
			pieces: []match.Piece{
				{ID: "A-capture", TeamID: domain.TeamA, State: domain.PieceOnBoard, CurrentSpaceID: "far"},
				{ID: "A-stack", TeamID: domain.TeamA, State: domain.PieceOnBoard, CurrentSpaceID: "other"},
				{ID: "A-ally", TeamID: domain.TeamA, State: domain.PieceOnBoard, CurrentSpaceID: "ally"},
				{ID: "B-target", TeamID: domain.TeamB, State: domain.PieceOnBoard, CurrentSpaceID: "enemy"},
			},
			plans: map[moveRequest][]match.OrdinaryMovePlan{
				{pieceID: "A-capture", result: domain.YutDo}: {{DestinationState: domain.PieceOnBoard, DestinationSpaceID: "enemy", MovedPieceIDs: []domain.PieceID{"A-capture"}}},
				{pieceID: "A-stack", result: domain.YutDo}:   {{DestinationState: domain.PieceOnBoard, DestinationSpaceID: "ally", MovedPieceIDs: []domain.PieceID{"A-stack"}}},
				{pieceID: "A-ally", result: domain.YutDo}:    {{DestinationState: domain.PieceOnBoard, DestinationSpaceID: "plain", MovedPieceIDs: []domain.PieceID{"A-ally"}}},
			},
			distances: map[domain.SpaceID]int{"far": 12, "other": 8, "ally": 7},
			wantPiece: "A-capture",
		},
		{
			name: "stacking before entering shortcut",
			pieces: []match.Piece{
				{ID: "A-stack", TeamID: domain.TeamA, State: domain.PieceOnBoard, CurrentSpaceID: "far"},
				{ID: "A-shortcut", TeamID: domain.TeamA, State: domain.PieceOnBoard, CurrentSpaceID: "choice"},
				{ID: "A-ally", TeamID: domain.TeamA, State: domain.PieceOnBoard, CurrentSpaceID: "ally"},
			},
			plans: map[moveRequest][]match.OrdinaryMovePlan{
				{pieceID: "A-stack", result: domain.YutDo}:    {{DestinationState: domain.PieceOnBoard, DestinationSpaceID: "ally", MovedPieceIDs: []domain.PieceID{"A-stack"}}},
				{pieceID: "A-shortcut", result: domain.YutDo}: {{Route: domain.RouteShortcut, DestinationState: domain.PieceOnBoard, DestinationSpaceID: "inner", MovedPieceIDs: []domain.PieceID{"A-shortcut"}}},
				{pieceID: "A-ally", result: domain.YutDo}:     {{DestinationState: domain.PieceOnBoard, DestinationSpaceID: "plain", MovedPieceIDs: []domain.PieceID{"A-ally"}}},
			},
			distances: map[domain.SpaceID]int{"far": 12, "choice": 7, "ally": 5},
			wantPiece: "A-stack",
		},
		{
			name: "shortcut before closest to finish",
			pieces: []match.Piece{
				{ID: "A-shortcut", TeamID: domain.TeamA, State: domain.PieceOnBoard, CurrentSpaceID: "far"},
				{ID: "A-close", TeamID: domain.TeamA, State: domain.PieceOnBoard, CurrentSpaceID: "close"},
			},
			plans: map[moveRequest][]match.OrdinaryMovePlan{
				{pieceID: "A-shortcut", result: domain.YutDo}: {{Route: domain.RouteShortcut, DestinationState: domain.PieceOnBoard, DestinationSpaceID: "inner", MovedPieceIDs: []domain.PieceID{"A-shortcut"}}},
				{pieceID: "A-close", result: domain.YutDo}:    {{DestinationState: domain.PieceOnBoard, DestinationSpaceID: "next", MovedPieceIDs: []domain.PieceID{"A-close"}}},
			},
			distances: map[domain.SpaceID]int{"far": 12, "close": 2},
			wantPiece: "A-shortcut",
		},
		{
			name: "closest on-board piece before deploying waiting piece",
			pieces: []match.Piece{
				{ID: "A-board", TeamID: domain.TeamA, State: domain.PieceOnBoard, CurrentSpaceID: "far"},
				{ID: "A-waiting", TeamID: domain.TeamA, State: domain.PieceWaiting},
			},
			plans: map[moveRequest][]match.OrdinaryMovePlan{
				{pieceID: "A-board", result: domain.YutDo}:   {{DestinationState: domain.PieceOnBoard, DestinationSpaceID: "next", MovedPieceIDs: []domain.PieceID{"A-board"}}},
				{pieceID: "A-waiting", result: domain.YutDo}: {{DestinationState: domain.PieceOnBoard, DestinationSpaceID: "do", MovedPieceIDs: []domain.PieceID{"A-waiting"}}},
			},
			distances: map[domain.SpaceID]int{"far": 20},
			wantPiece: "A-board",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			settings := room.DefaultSettings()
			settings.PieceCount = 2
			policy := mustPolicy(
				t,
				&fakeDistancePlanner{distances: test.distances},
				settings,
				&sequenceSource{},
			)
			game := &fakeMatch{snapshot: match.Snapshot{Pieces: test.pieces}, ordinary: test.plans}

			decision, err := policy.Decide(
				game,
				turnSnapshot(resultToken("token", domain.YutDo)),
				domain.TeamA,
			)
			if err != nil {
				t.Fatalf("Decide() error = %v", err)
			}
			if decision.PieceID != test.wantPiece {
				t.Fatalf("PieceID = %q, want %q; decision = %#v", decision.PieceID, test.wantPiece, decision)
			}
		})
	}
}

func TestDecideAlwaysChoosesSelectableShortcutBeforeScoringMove(t *testing.T) {
	settings := room.DefaultSettings()
	settings.PieceCount = 2
	policy := mustPolicy(
		t,
		&fakeDistancePlanner{distances: map[domain.SpaceID]int{"choice": 5}},
		settings,
		&sequenceSource{},
	)
	game := &fakeMatch{
		snapshot: match.Snapshot{Pieces: []match.Piece{
			{ID: "A-1", TeamID: domain.TeamA, State: domain.PieceOnBoard, CurrentSpaceID: "choice"},
			{ID: "B-1", TeamID: domain.TeamB, State: domain.PieceOnBoard, CurrentSpaceID: "normal-target"},
		}},
		ordinary: map[moveRequest][]match.OrdinaryMovePlan{
			{pieceID: "A-1", result: domain.YutDo}: {
				{Route: domain.RouteNormal, DestinationState: domain.PieceOnBoard, DestinationSpaceID: "normal-target", MovedPieceIDs: []domain.PieceID{"A-1"}},
				{Route: domain.RouteShortcut, DestinationState: domain.PieceOnBoard, DestinationSpaceID: "shortcut-target", MovedPieceIDs: []domain.PieceID{"A-1"}},
			},
		},
	}

	decision, err := policy.Decide(
		game,
		turnSnapshot(resultToken("token", domain.YutDo)),
		domain.TeamA,
	)
	if err != nil {
		t.Fatalf("Decide() error = %v", err)
	}
	if decision.PieceID != "A-1" || decision.Route != domain.RouteShortcut {
		t.Fatalf("Decision = %#v, want selectable shortcut", decision)
	}
}

func TestDecideUsesEmptyApplyRouteForForcedShortcut(t *testing.T) {
	settings := room.DefaultSettings()
	settings.PieceCount = 2
	settings.ShortcutPolicy = room.ShortcutForced
	policy := mustPolicy(
		t,
		&fakeDistancePlanner{distances: map[domain.SpaceID]int{"choice": 5}},
		settings,
		&sequenceSource{},
	)
	game := &fakeMatch{
		snapshot: match.Snapshot{Pieces: []match.Piece{
			{ID: "A-1", TeamID: domain.TeamA, State: domain.PieceOnBoard, CurrentSpaceID: "choice"},
		}},
		ordinary: map[moveRequest][]match.OrdinaryMovePlan{
			{pieceID: "A-1", result: domain.YutDo}: {{
				Route:              domain.RouteShortcut,
				DestinationState:   domain.PieceOnBoard,
				DestinationSpaceID: "inner",
				MovedPieceIDs:      []domain.PieceID{"A-1"},
			}},
		},
	}

	decision, err := policy.Decide(
		game,
		turnSnapshot(resultToken("token", domain.YutDo)),
		domain.TeamA,
	)
	if err != nil {
		t.Fatalf("Decide() error = %v", err)
	}
	if decision.Route != "" {
		t.Fatalf("forced shortcut apply route = %q, want empty", decision.Route)
	}
}

func TestDecideSupportsBackdoAndDiscardsUnusableHead(t *testing.T) {
	settings := room.DefaultSettings()
	settings.PieceCount = 2
	policy := mustPolicy(
		t,
		&fakeDistancePlanner{distances: map[domain.SpaceID]int{"current": 4}},
		settings,
		&sequenceSource{},
	)
	game := &fakeMatch{
		snapshot: match.Snapshot{Pieces: []match.Piece{
			{ID: "A-1", TeamID: domain.TeamA, State: domain.PieceOnBoard, CurrentSpaceID: "current"},
			{ID: "A-2", TeamID: domain.TeamA, State: domain.PieceWaiting},
		}},
		backdo: map[domain.PieceID]match.BackdoMovePlan{
			"A-1": {DestinationState: domain.PieceOnBoard, DestinationSpaceID: "previous", MovedPieceIDs: []domain.PieceID{"A-1"}},
		},
		backdoErrors: map[domain.PieceID]error{
			"A-2": board.ErrBackdoMovementUnavailable,
		},
	}

	decision, err := policy.Decide(
		game,
		turnSnapshot(resultToken("token-backdo", domain.YutBackdo)),
		domain.TeamA,
	)
	if err != nil {
		t.Fatalf("Decide(Backdo) error = %v", err)
	}
	if decision.Action != ActionMovePiece || decision.PieceID != "A-1" ||
		decision.Result != domain.YutBackdo {
		t.Fatalf("Backdo decision = %#v", decision)
	}

	game.backdo = nil
	game.backdoErrors["A-1"] = board.ErrBackdoHistoryUnavailable
	decision, err = policy.Decide(
		game,
		turnSnapshot(resultToken("token-backdo", domain.YutBackdo)),
		domain.TeamA,
	)
	if err != nil {
		t.Fatalf("Decide(unusable Backdo) error = %v", err)
	}
	if decision != (Decision{
		Action:  ActionDiscardResult,
		TokenID: "token-backdo",
		Result:  domain.YutBackdo,
	}) {
		t.Fatalf("unusable Backdo decision = %#v", decision)
	}
}

func TestDecideDeduplicatesStacksButKeepsIndependentAllies(t *testing.T) {
	for _, test := range []struct {
		name       string
		stacking   bool
		movedIDs   map[domain.PieceID][]domain.PieceID
		tickets    []uint64
		wantPiece  domain.PieceID
		wantLimits []uint64
	}{
		{
			name:     "stack is one candidate",
			stacking: true,
			movedIDs: map[domain.PieceID][]domain.PieceID{
				"A-1": {"A-1", "A-2"},
				"A-2": {"A-2", "A-1"},
			},
			wantPiece: "A-1",
		},
		{
			name:     "independent allies remain separate candidates",
			stacking: false,
			movedIDs: map[domain.PieceID][]domain.PieceID{
				"A-1": {"A-1"},
				"A-2": {"A-2"},
			},
			tickets:    []uint64{1},
			wantPiece:  "A-2",
			wantLimits: []uint64{2},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			settings := room.DefaultSettings()
			settings.PieceCount = 2
			settings.StackingEnabled = test.stacking
			source := &sequenceSource{values: test.tickets}
			policy := mustPolicy(
				t,
				&fakeDistancePlanner{distances: map[domain.SpaceID]int{"same": 5}},
				settings,
				source,
			)
			plans := make(map[moveRequest][]match.OrdinaryMovePlan)
			for pieceID, movedIDs := range test.movedIDs {
				plans[moveRequest{pieceID: pieceID, result: domain.YutDo}] = []match.OrdinaryMovePlan{{
					DestinationState:   domain.PieceOnBoard,
					DestinationSpaceID: "plain",
					MovedPieceIDs:      movedIDs,
				}}
			}
			game := &fakeMatch{
				snapshot: match.Snapshot{Pieces: []match.Piece{
					{ID: "A-1", TeamID: domain.TeamA, State: domain.PieceOnBoard, CurrentSpaceID: "same"},
					{ID: "A-2", TeamID: domain.TeamA, State: domain.PieceOnBoard, CurrentSpaceID: "same"},
				}},
				ordinary: plans,
			}

			decision, err := policy.Decide(
				game,
				turnSnapshot(resultToken("token", domain.YutDo)),
				domain.TeamA,
			)
			if err != nil {
				t.Fatalf("Decide() error = %v", err)
			}
			if decision.PieceID != test.wantPiece {
				t.Fatalf("PieceID = %q, want %q", decision.PieceID, test.wantPiece)
			}
			if !reflect.DeepEqual(source.limits, test.wantLimits) {
				t.Fatalf("random limits = %v, want %v", source.limits, test.wantLimits)
			}
		})
	}
}

func TestDecideUsesUniformRandomTiebreakAndRejectsOutOfRangeAtomically(t *testing.T) {
	for _, test := range []struct {
		ticket    uint64
		wantPiece domain.PieceID
		wantError error
	}{
		{ticket: 0, wantPiece: "A-1"},
		{ticket: 1, wantPiece: "A-2"},
		{ticket: 2, wantPiece: "A-3"},
		{ticket: 3, wantError: ErrRandomSourceOutOfRange},
	} {
		t.Run(fmt.Sprintf("ticket_%d", test.ticket), func(t *testing.T) {
			settings := room.DefaultSettings()
			settings.PieceCount = 3
			source := &sequenceSource{values: []uint64{test.ticket}}
			policy := mustPolicy(
				t,
				&fakeDistancePlanner{distances: map[domain.SpaceID]int{"same-distance": 5}},
				settings,
				source,
			)
			game := equalCandidateMatch()
			before := game.Snapshot()

			decision, err := policy.Decide(
				game,
				turnSnapshot(resultToken("token", domain.YutDo)),
				domain.TeamA,
			)
			if !errors.Is(err, test.wantError) {
				t.Fatalf("Decide() error = %v, want %v", err, test.wantError)
			}
			if err == nil && decision.PieceID != test.wantPiece {
				t.Fatalf("PieceID = %q, want %q", decision.PieceID, test.wantPiece)
			}
			if after := game.Snapshot(); !reflect.DeepEqual(after, before) {
				t.Fatal("CPU decision changed match snapshot")
			}
			if !reflect.DeepEqual(source.limits, []uint64{3}) {
				t.Fatalf("random limits = %v, want [3]", source.limits)
			}
		})
	}
}

func TestSeededPolicyReproducesEqualCandidateDecision(t *testing.T) {
	settings := room.DefaultSettings()
	settings.PieceCount = 3
	planner := &fakeDistancePlanner{distances: map[domain.SpaceID]int{"same-distance": 5}}
	first, err := NewSeededPolicy(planner, settings, 17, 29)
	if err != nil {
		t.Fatalf("NewSeededPolicy(first) error = %v", err)
	}
	second, err := NewSeededPolicy(planner, settings, 17, 29)
	if err != nil {
		t.Fatalf("NewSeededPolicy(second) error = %v", err)
	}
	game := equalCandidateMatch()
	snapshot := turnSnapshot(resultToken("token", domain.YutDo))

	firstDecision, err := first.Decide(game, snapshot, domain.TeamA)
	if err != nil {
		t.Fatalf("first Decide() error = %v", err)
	}
	secondDecision, err := second.Decide(game, snapshot, domain.TeamA)
	if err != nil {
		t.Fatalf("second Decide() error = %v", err)
	}
	if firstDecision != secondDecision {
		t.Fatalf("same seed decisions differ: %#v and %#v", firstDecision, secondDecision)
	}
}

func TestDecideUsesConfiguredShortcutPolicyForSharedFinishDistance(t *testing.T) {
	for _, test := range []struct {
		name   string
		policy room.ShortcutPolicy
		want   board.ShortcutPolicy
	}{
		{name: "selectable", policy: room.ShortcutSelectable, want: board.SelectableShortcuts},
		{name: "forced", policy: room.ShortcutForced, want: board.ForcedShortcuts},
	} {
		t.Run(test.name, func(t *testing.T) {
			settings := room.DefaultSettings()
			settings.PieceCount = 2
			settings.ShortcutPolicy = test.policy
			planner := &fakeDistancePlanner{distances: map[domain.SpaceID]int{"space": 5}}
			policy := mustPolicy(t, planner, settings, &sequenceSource{})
			game := &fakeMatch{
				snapshot: match.Snapshot{Pieces: []match.Piece{{
					ID: "A-1", TeamID: domain.TeamA, State: domain.PieceOnBoard, CurrentSpaceID: "space",
				}}},
				ordinary: map[moveRequest][]match.OrdinaryMovePlan{
					{pieceID: "A-1", result: domain.YutDo}: {{DestinationState: domain.PieceOnBoard, DestinationSpaceID: "next", MovedPieceIDs: []domain.PieceID{"A-1"}}},
				},
			}

			if _, err := policy.Decide(game, turnSnapshot(resultToken("token", domain.YutDo)), domain.TeamA); err != nil {
				t.Fatalf("Decide() error = %v", err)
			}
			if !reflect.DeepEqual(planner.policies, []board.ShortcutPolicy{test.want}) {
				t.Fatalf("distance policies = %v, want [%v]", planner.policies, test.want)
			}
		})
	}
}

func TestPolicyValidatesDependenciesAndDecisionInput(t *testing.T) {
	settings := room.DefaultSettings()
	settings.PieceCount = 2
	planner := &fakeDistancePlanner{}
	source := &sequenceSource{}

	if policy, err := NewPolicy(nil, settings, source); !errors.Is(err, ErrInvalidPolicyConfig) || policy != nil {
		t.Fatalf("NewPolicy(nil planner) = (%#v, %v)", policy, err)
	}
	var typedNilPlanner *fakeDistancePlanner
	if policy, err := NewPolicy(typedNilPlanner, settings, source); !errors.Is(err, ErrInvalidPolicyConfig) || policy != nil {
		t.Fatalf("NewPolicy(typed nil planner) = (%#v, %v)", policy, err)
	}
	if policy, err := NewPolicy(planner, settings, nil); !errors.Is(err, ErrInvalidPolicyConfig) || policy != nil {
		t.Fatalf("NewPolicy(nil source) = (%#v, %v)", policy, err)
	}
	var typedNilSource *sequenceSource
	if policy, err := NewPolicy(planner, settings, typedNilSource); !errors.Is(err, ErrInvalidPolicyConfig) || policy != nil {
		t.Fatalf("NewPolicy(typed nil source) = (%#v, %v)", policy, err)
	}
	invalidSettings := settings
	invalidSettings.PieceCount = 1
	if policy, err := NewPolicy(planner, invalidSettings, source); !errors.Is(err, ErrInvalidPolicyConfig) || policy != nil {
		t.Fatalf("NewPolicy(invalid settings) = (%#v, %v)", policy, err)
	}

	policy := mustPolicy(t, planner, settings, source)
	validGame := &fakeMatch{}
	var nilPolicy *Policy
	if _, err := nilPolicy.Decide(validGame, turn.Snapshot{}, domain.TeamA); !errors.Is(err, ErrInvalidDecisionInput) {
		t.Fatalf("nil Policy.Decide() error = %v", err)
	}
	if _, err := policy.Decide(nil, turn.Snapshot{}, domain.TeamA); !errors.Is(err, ErrInvalidDecisionInput) {
		t.Fatalf("Decide(nil game) error = %v", err)
	}
	var typedNilGame *fakeMatch
	if _, err := policy.Decide(typedNilGame, turn.Snapshot{}, domain.TeamA); !errors.Is(err, ErrInvalidDecisionInput) {
		t.Fatalf("Decide(typed nil game) error = %v", err)
	}
	if _, err := policy.Decide(validGame, turn.Snapshot{}, "C"); !errors.Is(err, ErrInvalidDecisionInput) {
		t.Fatalf("Decide(invalid team) error = %v", err)
	}
	if _, err := policy.Decide(validGame, turn.Snapshot{}, domain.TeamA); !errors.Is(err, ErrNoResultToken) {
		t.Fatalf("Decide(empty queue) error = %v", err)
	}
	for _, result := range []domain.YutResult{domain.YutDo, domain.YutBuk} {
		endedGame := &fakeMatch{snapshot: match.Snapshot{WinnerTeamID: domain.TeamA}}
		if _, err := policy.Decide(
			endedGame,
			turnSnapshot(resultToken("token", result)),
			domain.TeamA,
		); !errors.Is(err, match.ErrMatchEnded) {
			t.Fatalf("Decide(ended match, %q) error = %v", result, err)
		}
	}
}

func TestDecidePropagatesUnexpectedPlannerFailureWithoutMutation(t *testing.T) {
	settings := room.DefaultSettings()
	settings.PieceCount = 2

	t.Run("movement planner", func(t *testing.T) {
		plannerFailure := errors.New("movement planner failed")
		policy := mustPolicy(t, &fakeDistancePlanner{}, settings, &sequenceSource{})
		game := &fakeMatch{
			snapshot: match.Snapshot{Pieces: []match.Piece{{
				ID: "A-1", TeamID: domain.TeamA, State: domain.PieceWaiting,
			}}},
			ordinaryErrors: map[moveRequest]error{
				{pieceID: "A-1", result: domain.YutDo}: plannerFailure,
			},
		}
		assertDecisionFailureLeavesSnapshot(t, policy, game, plannerFailure)
	})

	t.Run("finish distance planner", func(t *testing.T) {
		plannerFailure := errors.New("distance planner failed")
		policy := mustPolicy(
			t,
			&fakeDistancePlanner{err: plannerFailure},
			settings,
			&sequenceSource{},
		)
		game := &fakeMatch{
			snapshot: match.Snapshot{Pieces: []match.Piece{{
				ID: "A-1", TeamID: domain.TeamA, State: domain.PieceOnBoard, CurrentSpaceID: "space",
			}}},
			ordinary: map[moveRequest][]match.OrdinaryMovePlan{
				{pieceID: "A-1", result: domain.YutDo}: {{DestinationState: domain.PieceOnBoard, DestinationSpaceID: "next", MovedPieceIDs: []domain.PieceID{"A-1"}}},
			},
		}
		assertDecisionFailureLeavesSnapshot(t, policy, game, plannerFailure)
	})

	t.Run("selected piece absent from moved group", func(t *testing.T) {
		policy := mustPolicy(t, &fakeDistancePlanner{}, settings, &sequenceSource{})
		game := &fakeMatch{
			snapshot: match.Snapshot{Pieces: []match.Piece{{
				ID: "A-1", TeamID: domain.TeamA, State: domain.PieceWaiting,
			}}},
			ordinary: map[moveRequest][]match.OrdinaryMovePlan{
				{pieceID: "A-1", result: domain.YutDo}: {{DestinationState: domain.PieceOnBoard, DestinationSpaceID: "do", MovedPieceIDs: []domain.PieceID{"A-2"}}},
			},
		}
		assertDecisionFailureLeavesSnapshot(t, policy, game, ErrInvalidMovePlans)
	})

	for _, test := range []struct {
		name  string
		plans []match.OrdinaryMovePlan
	}{
		{name: "no forward plans"},
		{
			name: "route choice without shortcut",
			plans: []match.OrdinaryMovePlan{
				{Route: domain.RouteNormal, MovedPieceIDs: []domain.PieceID{"A-1"}},
				{Route: domain.RouteNormal, MovedPieceIDs: []domain.PieceID{"A-1"}},
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			policy := mustPolicy(t, &fakeDistancePlanner{}, settings, &sequenceSource{})
			game := &fakeMatch{
				snapshot: match.Snapshot{Pieces: []match.Piece{{
					ID: "A-1", TeamID: domain.TeamA, State: domain.PieceWaiting,
				}}},
				ordinary: map[moveRequest][]match.OrdinaryMovePlan{
					{pieceID: "A-1", result: domain.YutDo}: test.plans,
				},
			}
			assertDecisionFailureLeavesSnapshot(t, policy, game, ErrInvalidMovePlans)
		})
	}
}

func assertDecisionFailureLeavesSnapshot(
	t *testing.T,
	policy *Policy,
	game *fakeMatch,
	wantError error,
) {
	t.Helper()
	before := game.Snapshot()
	if _, err := policy.Decide(
		game,
		turnSnapshot(resultToken("token", domain.YutDo)),
		domain.TeamA,
	); !errors.Is(err, wantError) {
		t.Fatalf("Decide() error = %v, want %v", err, wantError)
	}
	if after := game.Snapshot(); !reflect.DeepEqual(after, before) {
		t.Fatal("failed CPU decision changed match state")
	}
}

func TestPolicyIntegratesWithAuthoritativeGameWithoutMutation(t *testing.T) {
	graph, err := board.LoadFile(filepath.Join("..", "..", "..", "spec", "board_graph.yaml"))
	if err != nil {
		t.Fatalf("LoadFile() error = %v", err)
	}
	settings := room.DefaultSettings()
	settings.PieceCount = 2
	game, err := match.NewGame(graph, settings, []match.TeamSetup{
		{TeamID: domain.TeamA, PieceIDs: []domain.PieceID{"A-1", "A-2"}},
		{TeamID: domain.TeamB, PieceIDs: []domain.PieceID{"B-1", "B-2"}},
	})
	if err != nil {
		t.Fatalf("NewGame() error = %v", err)
	}
	policy := mustPolicy(t, graph, settings, &sequenceSource{values: []uint64{0}})
	before := game.Snapshot()

	decision, err := policy.Decide(
		game,
		turnSnapshot(resultToken("token-do", domain.YutDo)),
		domain.TeamA,
	)
	if err != nil {
		t.Fatalf("Decide() error = %v", err)
	}
	if decision.Action != ActionMovePiece || decision.PieceID != "A-1" || decision.Route != "" {
		t.Fatalf("Decision = %#v", decision)
	}
	if after := game.Snapshot(); !reflect.DeepEqual(after, before) {
		t.Fatal("CPU policy mutated authoritative game")
	}
	if _, err := game.ApplyOrdinaryMove(
		domain.TeamA,
		decision.PieceID,
		decision.Result,
		decision.Route,
	); err != nil {
		t.Fatalf("authoritative apply rejected CPU decision: %v", err)
	}
}

func TestPolicySerializesConcurrentUseOfServerRandomSource(t *testing.T) {
	graph, err := board.LoadFile(filepath.Join("..", "..", "..", "spec", "board_graph.yaml"))
	if err != nil {
		t.Fatalf("LoadFile() error = %v", err)
	}
	settings := room.DefaultSettings()
	settings.PieceCount = 2
	game, err := match.NewGame(graph, settings, []match.TeamSetup{
		{TeamID: domain.TeamA, PieceIDs: []domain.PieceID{"A-1", "A-2"}},
		{TeamID: domain.TeamB, PieceIDs: []domain.PieceID{"B-1", "B-2"}},
	})
	if err != nil {
		t.Fatalf("NewGame() error = %v", err)
	}
	policy := mustPolicy(t, graph, settings, rand.New(rand.NewPCG(41, 43)))
	before := game.Snapshot()
	snapshot := turnSnapshot(resultToken("token-do", domain.YutDo))

	const attempts = 100
	errorsFound := make(chan error, attempts)
	var waitGroup sync.WaitGroup
	waitGroup.Add(attempts)
	for range attempts {
		go func() {
			defer waitGroup.Done()
			decision, err := policy.Decide(game, snapshot, domain.TeamA)
			if err != nil {
				errorsFound <- err
				return
			}
			if decision.PieceID != "A-1" && decision.PieceID != "A-2" {
				errorsFound <- fmt.Errorf("unexpected decision %#v", decision)
			}
		}()
	}
	waitGroup.Wait()
	close(errorsFound)
	for err := range errorsFound {
		t.Error(err)
	}
	if after := game.Snapshot(); !reflect.DeepEqual(after, before) {
		t.Fatal("concurrent CPU decisions changed authoritative match")
	}
}

func equalCandidateMatch() *fakeMatch {
	pieces := make([]match.Piece, 0, 3)
	plans := make(map[moveRequest][]match.OrdinaryMovePlan, 3)
	for _, pieceID := range []domain.PieceID{"A-1", "A-2", "A-3"} {
		pieces = append(pieces, match.Piece{
			ID: pieceID, TeamID: domain.TeamA, State: domain.PieceOnBoard, CurrentSpaceID: "same-distance",
		})
		plans[moveRequest{pieceID: pieceID, result: domain.YutDo}] = []match.OrdinaryMovePlan{{
			DestinationState:   domain.PieceOnBoard,
			DestinationSpaceID: "plain",
			MovedPieceIDs:      []domain.PieceID{pieceID},
		}}
	}
	return &fakeMatch{snapshot: match.Snapshot{Pieces: pieces}, ordinary: plans}
}

func mustPolicy(
	t *testing.T,
	planner board.FinishDistancePlanner,
	settings room.Settings,
	source BoundedSource,
) *Policy {
	t.Helper()
	policy, err := NewPolicy(planner, settings, source)
	if err != nil {
		t.Fatalf("NewPolicy() error = %v", err)
	}
	return policy
}

func turnSnapshot(tokens ...turn.ResultToken) turn.Snapshot {
	return turn.Snapshot{ResultQueue: tokens}
}

func resultToken(id domain.ResultTokenID, result domain.YutResult) turn.ResultToken {
	return turn.ResultToken{
		ID:                  id,
		Result:              result,
		Origin:              domain.ResultOriginInitialThrow,
		GeneratedByPlayerID: "player-A",
	}
}

type moveRequest struct {
	pieceID domain.PieceID
	result  domain.YutResult
}

type fakeMatch struct {
	snapshot       match.Snapshot
	ordinary       map[moveRequest][]match.OrdinaryMovePlan
	ordinaryErrors map[moveRequest]error
	backdo         map[domain.PieceID]match.BackdoMovePlan
	backdoErrors   map[domain.PieceID]error
}

func (game *fakeMatch) Snapshot() match.Snapshot {
	snapshot := game.snapshot
	snapshot.Pieces = append([]match.Piece(nil), game.snapshot.Pieces...)
	return snapshot
}

func (game *fakeMatch) OrdinaryMovePlans(
	_ domain.TeamID,
	pieceID domain.PieceID,
	result domain.YutResult,
) ([]match.OrdinaryMovePlan, error) {
	request := moveRequest{pieceID: pieceID, result: result}
	if err := game.ordinaryErrors[request]; err != nil {
		return nil, err
	}
	plans := game.ordinary[request]
	cloned := make([]match.OrdinaryMovePlan, len(plans))
	for index, plan := range plans {
		cloned[index] = plan
		cloned[index].Traversed = append([]domain.SpaceID(nil), plan.Traversed...)
		cloned[index].MovedPieceIDs = append([]domain.PieceID(nil), plan.MovedPieceIDs...)
	}
	return cloned, nil
}

func (game *fakeMatch) BackdoMovePlan(
	_ domain.TeamID,
	pieceID domain.PieceID,
) (match.BackdoMovePlan, error) {
	if err := game.backdoErrors[pieceID]; err != nil {
		return match.BackdoMovePlan{}, err
	}
	plan, ok := game.backdo[pieceID]
	if !ok {
		return match.BackdoMovePlan{}, board.ErrBackdoMovementUnavailable
	}
	plan.Traversed = append([]domain.SpaceID(nil), plan.Traversed...)
	plan.MovedPieceIDs = append([]domain.PieceID(nil), plan.MovedPieceIDs...)
	return plan, nil
}

type fakeDistancePlanner struct {
	distances map[domain.SpaceID]int
	policies  []board.ShortcutPolicy
	err       error
}

func (planner *fakeDistancePlanner) RemainingForwardDistance(
	position board.Position,
	policy board.ShortcutPolicy,
) (int, error) {
	planner.policies = append(planner.policies, policy)
	if planner.err != nil {
		return 0, planner.err
	}
	distance, ok := planner.distances[position.Space]
	if !ok {
		return 0, board.ErrFinishDistanceUnavailable
	}
	return distance, nil
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

var _ BoundedSource = rand.New(rand.NewPCG(1, 2))
