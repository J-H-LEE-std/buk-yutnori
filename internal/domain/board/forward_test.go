package board

import (
	"errors"
	"reflect"
	"testing"

	"buk-yutnori/internal/domain"
)

func TestGraphImplementsForwardPlanner(t *testing.T) {
	var _ ForwardPlanner = loadCanonicalGraph(t)
}

func TestForwardPlansDeployWaitingPieceWithoutCountingStart(t *testing.T) {
	graph := loadCanonicalGraph(t)
	tests := []struct {
		name        string
		spaces      int
		wantSpace   SpaceID
		wantPrev    SpaceID
		wantVisited []SpaceID
	}{
		{
			name:        "one space reaches first board space",
			spaces:      1,
			wantSpace:   "do",
			wantPrev:    "chammeogi",
			wantVisited: []SpaceID{"do"},
		},
		{
			name:        "five spaces stop on Mo without entering shortcut",
			spaces:      5,
			wantSpace:   "mo",
			wantPrev:    "yut",
			wantVisited: []SpaceID{"do", "gae", "geol", "yut", "mo"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			plans, err := graph.ForwardPlans(
				Position{State: PieceWaiting},
				test.spaces,
				SelectableShortcuts,
			)
			if err != nil {
				t.Fatalf("ForwardPlans() error = %v", err)
			}
			plan := requireSinglePlan(t, plans)
			assertForwardPlan(
				t,
				plan,
				domain.RouteNormal,
				Position{State: PieceOnBoard, Space: test.wantSpace},
				test.wantPrev,
				test.wantVisited,
			)
		})
	}
}

func TestForwardPlansOfferRoutesOnlyAtStartingChoice(t *testing.T) {
	graph := loadCanonicalGraph(t)
	plans, err := graph.ForwardPlans(
		Position{State: PieceOnBoard, Space: "mo"},
		3,
		SelectableShortcuts,
	)
	if err != nil {
		t.Fatalf("ForwardPlans() error = %v", err)
	}
	if len(plans) != 2 {
		t.Fatalf("len(ForwardPlans()) = %d, want 2", len(plans))
	}
	assertForwardPlan(
		t,
		plans[0],
		domain.RouteNormal,
		Position{State: PieceOnBoard, Space: "back_geol"},
		"back_gae",
		[]SpaceID{"back_do", "back_gae", "back_geol"},
	)
	assertForwardPlan(
		t,
		plans[1],
		domain.RouteShortcut,
		Position{State: PieceOnBoard, Space: "bang"},
		"mo_gae",
		[]SpaceID{"mo_do", "mo_gae", "bang"},
	)

	plans[0].Traversed[0] = "mutated"
	again, err := graph.ForwardPlans(
		Position{State: PieceOnBoard, Space: "mo"},
		3,
		SelectableShortcuts,
	)
	if err != nil {
		t.Fatalf("second ForwardPlans() error = %v", err)
	}
	if again[0].Traversed[0] != "back_do" {
		t.Fatalf("mutating returned plan changed graph result: %v", again[0].Traversed)
	}
}

func TestForwardPlansForceShortcutAtStartingChoice(t *testing.T) {
	graph := loadCanonicalGraph(t)
	plans, err := graph.ForwardPlans(
		Position{State: PieceOnBoard, Space: "back_mo"},
		3,
		ForcedShortcuts,
	)
	if err != nil {
		t.Fatalf("ForwardPlans() error = %v", err)
	}
	assertForwardPlan(
		t,
		requireSinglePlan(t, plans),
		domain.RouteShortcut,
		Position{State: PieceOnBoard, Space: "bang"},
		"back_mo_gae",
		[]SpaceID{"back_mo_do", "back_mo_gae", "bang"},
	)
}

func TestForwardPlansUseNormalRouteAtIntermediateChoices(t *testing.T) {
	graph := loadCanonicalGraph(t)
	tests := []struct {
		name        string
		position    Position
		spaces      int
		policy      ShortcutPolicy
		wantSpace   SpaceID
		wantPrev    SpaceID
		wantVisited []SpaceID
	}{
		{
			name:        "pass Mo under selectable policy",
			position:    Position{State: PieceOnBoard, Space: "yut"},
			spaces:      3,
			policy:      SelectableShortcuts,
			wantSpace:   "back_gae",
			wantPrev:    "back_do",
			wantVisited: []SpaceID{"mo", "back_do", "back_gae"},
		},
		{
			name:        "pass Mo under forced policy",
			position:    Position{State: PieceOnBoard, Space: "yut"},
			spaces:      3,
			policy:      ForcedShortcuts,
			wantSpace:   "back_gae",
			wantPrev:    "back_do",
			wantVisited: []SpaceID{"mo", "back_do", "back_gae"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			plans, err := graph.ForwardPlans(test.position, test.spaces, test.policy)
			if err != nil {
				t.Fatalf("ForwardPlans() error = %v", err)
			}
			plan := requireSinglePlan(t, plans)
			assertForwardPlan(
				t,
				plan,
				domain.RouteNormal,
				Position{State: PieceOnBoard, Space: test.wantSpace},
				test.wantPrev,
				test.wantVisited,
			)
		})
	}

	plans, err := graph.ForwardPlans(
		Position{State: PieceOnBoard, Space: "mo"},
		4,
		SelectableShortcuts,
	)
	if err != nil {
		t.Fatalf("ForwardPlans(Mo) error = %v", err)
	}
	assertForwardPlan(
		t,
		plans[1],
		domain.RouteShortcut,
		Position{State: PieceOnBoard, Space: "sok_yut"},
		"bang",
		[]SpaceID{"mo_do", "mo_gae", "bang", "sok_yut"},
	)
}

func TestForwardPlansApplyCheckpointAndFinishPerSelectedRoute(t *testing.T) {
	graph := loadCanonicalGraph(t)

	plans, err := graph.ForwardPlans(
		Position{State: PieceOnBoard, Space: "bang"},
		3,
		SelectableShortcuts,
	)
	if err != nil {
		t.Fatalf("ForwardPlans(checkpoint) error = %v", err)
	}
	assertForwardPlan(
		t,
		plans[0],
		domain.RouteNormal,
		Position{State: PieceOnBoard, Space: "jji_mo"},
		"sok_mo",
		[]SpaceID{"sok_yut", "sok_mo", "jji_mo"},
	)
	assertForwardPlan(
		t,
		plans[1],
		domain.RouteShortcut,
		Position{State: PieceHomeCheckpoint, Space: "chammeogi"},
		"anjji",
		[]SpaceID{"bangsugi", "anjji", "chammeogi"},
	)

	plans, err = graph.ForwardPlans(
		Position{State: PieceOnBoard, Space: "bang"},
		4,
		SelectableShortcuts,
	)
	if err != nil {
		t.Fatalf("ForwardPlans(finish) error = %v", err)
	}
	assertForwardPlan(
		t,
		plans[1],
		domain.RouteShortcut,
		Position{State: PieceFinished},
		"",
		[]SpaceID{"bangsugi", "anjji", "chammeogi"},
	)
}

func TestForwardPlansHandleHomeCheckpointAndFinish(t *testing.T) {
	graph := loadCanonicalGraph(t)
	tests := []struct {
		name        string
		position    Position
		spaces      int
		want        Position
		wantPrev    SpaceID
		wantVisited []SpaceID
	}{
		{
			name:        "exactly reach home checkpoint",
			position:    Position{State: PieceOnBoard, Space: "nal_yut"},
			spaces:      1,
			want:        Position{State: PieceHomeCheckpoint, Space: "chammeogi"},
			wantPrev:    "nal_yut",
			wantVisited: []SpaceID{"chammeogi"},
		},
		{
			name:        "exact finish distance",
			position:    Position{State: PieceOnBoard, Space: "nal_yut"},
			spaces:      2,
			want:        Position{State: PieceFinished},
			wantVisited: []SpaceID{"chammeogi"},
		},
		{
			name:        "overshoot finish distance",
			position:    Position{State: PieceOnBoard, Space: "nal_yut"},
			spaces:      5,
			want:        Position{State: PieceFinished},
			wantVisited: []SpaceID{"chammeogi"},
		},
		{
			name:     "home checkpoint finishes on next positive movement",
			position: Position{State: PieceHomeCheckpoint, Space: "chammeogi"},
			spaces:   1,
			want:     Position{State: PieceFinished},
		},
		{
			name:     "home checkpoint also finishes on oversized movement",
			position: Position{State: PieceHomeCheckpoint, Space: "chammeogi"},
			spaces:   5,
			want:     Position{State: PieceFinished},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			plans, err := graph.ForwardPlans(test.position, test.spaces, SelectableShortcuts)
			if err != nil {
				t.Fatalf("ForwardPlans() error = %v", err)
			}
			assertForwardPlan(
				t,
				requireSinglePlan(t, plans),
				domain.RouteNormal,
				test.want,
				test.wantPrev,
				test.wantVisited,
			)
		})
	}
}

func TestForwardPlansTraverseCanonicalOuterBoard(t *testing.T) {
	graph := loadCanonicalGraph(t)
	outer := []SpaceID{
		"do", "gae", "geol", "yut", "mo",
		"back_do", "back_gae", "back_geol", "back_yut", "back_mo",
		"jji_do", "jji_gae", "jji_geol", "jji_yut", "jji_mo",
		"nal_do", "nal_gae", "nal_geol", "nal_yut", "chammeogi",
	}

	plans, err := graph.ForwardPlans(
		Position{State: PieceWaiting},
		len(outer),
		SelectableShortcuts,
	)
	if err != nil {
		t.Fatalf("ForwardPlans() error = %v", err)
	}
	assertForwardPlan(
		t,
		requireSinglePlan(t, plans),
		domain.RouteNormal,
		Position{State: PieceHomeCheckpoint, Space: "chammeogi"},
		"nal_yut",
		outer,
	)

	plans, err = graph.ForwardPlans(
		Position{State: PieceWaiting},
		len(outer)+1,
		SelectableShortcuts,
	)
	if err != nil {
		t.Fatalf("ForwardPlans(finish) error = %v", err)
	}
	assertForwardPlan(
		t,
		requireSinglePlan(t, plans),
		domain.RouteNormal,
		Position{State: PieceFinished},
		"",
		outer,
	)
}

func TestForwardPlansCoverEveryCanonicalRouteChoice(t *testing.T) {
	graph := loadCanonicalGraph(t)
	tests := []struct {
		start        SpaceID
		normal       SpaceID
		shortcut     SpaceID
		forcedResult SpaceID
	}{
		{start: "mo", normal: "back_do", shortcut: "mo_do", forcedResult: "mo_do"},
		{start: "back_mo", normal: "jji_do", shortcut: "back_mo_do", forcedResult: "back_mo_do"},
		{start: "bang", normal: "sok_yut", shortcut: "bangsugi", forcedResult: "bangsugi"},
	}

	for _, test := range tests {
		t.Run(string(test.start), func(t *testing.T) {
			position := Position{State: PieceOnBoard, Space: test.start}
			selectable, err := graph.ForwardPlans(position, 1, SelectableShortcuts)
			if err != nil {
				t.Fatalf("selectable ForwardPlans() error = %v", err)
			}
			if len(selectable) != 2 {
				t.Fatalf("len(selectable plans) = %d, want 2", len(selectable))
			}
			if selectable[0].Destination.Space != test.normal {
				t.Errorf("normal destination = %q, want %q", selectable[0].Destination.Space, test.normal)
			}
			if selectable[1].Destination.Space != test.shortcut {
				t.Errorf("shortcut destination = %q, want %q", selectable[1].Destination.Space, test.shortcut)
			}

			forced, err := graph.ForwardPlans(position, 1, ForcedShortcuts)
			if err != nil {
				t.Fatalf("forced ForwardPlans() error = %v", err)
			}
			if got := requireSinglePlan(t, forced).Destination.Space; got != test.forcedResult {
				t.Errorf("forced destination = %q, want %q", got, test.forcedResult)
			}
		})
	}
}

func TestForwardPlansRejectInvalidRequests(t *testing.T) {
	graph := loadCanonicalGraph(t)
	tests := []struct {
		name      string
		position  Position
		spaces    int
		policy    ShortcutPolicy
		wantError error
	}{
		{
			name:      "zero spaces",
			position:  Position{State: PieceWaiting},
			spaces:    0,
			policy:    SelectableShortcuts,
			wantError: ErrInvalidForwardSpaces,
		},
		{
			name:      "negative spaces",
			position:  Position{State: PieceWaiting},
			spaces:    -1,
			policy:    SelectableShortcuts,
			wantError: ErrInvalidForwardSpaces,
		},
		{
			name:      "waiting piece has a space",
			position:  Position{State: PieceWaiting, Space: "do"},
			spaces:    1,
			policy:    SelectableShortcuts,
			wantError: ErrInvalidPosition,
		},
		{
			name:      "finished piece",
			position:  Position{State: PieceFinished},
			spaces:    1,
			policy:    SelectableShortcuts,
			wantError: ErrForwardMovementUnavailable,
		},
		{
			name:      "finished piece has a space",
			position:  Position{State: PieceFinished, Space: "do"},
			spaces:    1,
			policy:    SelectableShortcuts,
			wantError: ErrInvalidPosition,
		},
		{
			name:      "home checkpoint has wrong space",
			position:  Position{State: PieceHomeCheckpoint, Space: "do"},
			spaces:    1,
			policy:    SelectableShortcuts,
			wantError: ErrInvalidPosition,
		},
		{
			name:      "on-board piece has unknown space",
			position:  Position{State: PieceOnBoard, Space: "nowhere"},
			spaces:    1,
			policy:    SelectableShortcuts,
			wantError: ErrUnknownSpace,
		},
		{
			name:      "on-board piece uses checkpoint as ordinary space",
			position:  Position{State: PieceOnBoard, Space: "chammeogi"},
			spaces:    1,
			policy:    SelectableShortcuts,
			wantError: ErrInvalidPosition,
		},
		{
			name:      "invalid piece state",
			position:  Position{State: PieceState("invalid")},
			spaces:    1,
			policy:    SelectableShortcuts,
			wantError: ErrInvalidPosition,
		},
		{
			name:      "invalid shortcut policy",
			position:  Position{State: PieceWaiting},
			spaces:    1,
			policy:    ShortcutPolicy("invalid"),
			wantError: ErrInvalidShortcutPolicy,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			plans, err := graph.ForwardPlans(test.position, test.spaces, test.policy)
			if !errors.Is(err, test.wantError) {
				t.Fatalf("ForwardPlans() error = %v, want %v", err, test.wantError)
			}
			if plans != nil {
				t.Fatalf("ForwardPlans() plans = %v, want nil", plans)
			}
		})
	}
}

func requireSinglePlan(t *testing.T, plans []ForwardPlan) ForwardPlan {
	t.Helper()
	if len(plans) != 1 {
		t.Fatalf("len(plans) = %d, want 1", len(plans))
	}
	return plans[0]
}

func assertForwardPlan(
	t *testing.T,
	plan ForwardPlan,
	route domain.Route,
	destination Position,
	actualPrevious SpaceID,
	traversed []SpaceID,
) {
	t.Helper()
	if plan.Route != route {
		t.Errorf("Route = %q, want %q", plan.Route, route)
	}
	if plan.Destination != destination {
		t.Errorf("Destination = %#v, want %#v", plan.Destination, destination)
	}
	if plan.ActualPreviousSpace != actualPrevious {
		t.Errorf("ActualPreviousSpace = %q, want %q", plan.ActualPreviousSpace, actualPrevious)
	}
	if !reflect.DeepEqual(plan.Traversed, traversed) {
		t.Errorf("Traversed = %v, want %v", plan.Traversed, traversed)
	}
}
