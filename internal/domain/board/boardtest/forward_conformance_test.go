package boardtest

import (
	"path/filepath"
	"strings"
	"testing"

	"buk-yutnori/internal/domain"
	"buk-yutnori/internal/domain/board"
)

func TestCanonicalGraphSatisfiesForwardPlannerContract(t *testing.T) {
	reference := loadCanonicalGraph(t)
	if err := CheckForwardPlanner(reference, reference); err != nil {
		t.Fatalf("CheckForwardPlanner() error = %v", err)
	}
}

func TestForwardPlannerContractRejectsTopologyViolations(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func([]board.ForwardPlan) []board.ForwardPlan
		wantError string
	}{
		{
			name: "unknown destination",
			mutate: func(plans []board.ForwardPlan) []board.ForwardPlan {
				plans[0].Destination.Space = "missing"
				return plans
			},
			wantError: "unknown destination",
		},
		{
			name: "unknown traversed space",
			mutate: func(plans []board.ForwardPlan) []board.ForwardPlan {
				plans[0].Traversed[0] = "missing"
				return plans
			},
			wantError: "unknown traversed space",
		},
		{
			name: "illegal forward edge",
			mutate: func(plans []board.ForwardPlan) []board.ForwardPlan {
				plans[0].Destination.Space = "mo"
				plans[0].Traversed[0] = "mo"
				return plans
			},
			wantError: "illegal forward edge",
		},
		{
			name: "destination differs from traversal",
			mutate: func(plans []board.ForwardPlan) []board.ForwardPlan {
				plans[0].Destination.Space = "gae"
				return plans
			},
			wantError: "destination differs from final traversed space",
		},
		{
			name: "actual previous differs from traversal",
			mutate: func(plans []board.ForwardPlan) []board.ForwardPlan {
				plans[0].ActualPreviousSpace = "yut"
				return plans
			},
			wantError: "actual previous space",
		},
		{
			name: "unknown actual previous space",
			mutate: func(plans []board.ForwardPlan) []board.ForwardPlan {
				plans[0].ActualPreviousSpace = "missing"
				return plans
			},
			wantError: "unknown actual previous space",
		},
		{
			name: "wrong route count",
			mutate: func(plans []board.ForwardPlan) []board.ForwardPlan {
				return append(plans, plans[0])
			},
			wantError: "plan count",
		},
		{
			name: "wrong route order",
			mutate: func(plans []board.ForwardPlan) []board.ForwardPlan {
				plans[0].Route = domain.RouteShortcut
				return plans
			},
			wantError: "route",
		},
		{
			name: "invalid home checkpoint",
			mutate: func(plans []board.ForwardPlan) []board.ForwardPlan {
				plans[0].Destination.State = domain.PieceHomeCheckpoint
				return plans
			},
			wantError: "home checkpoint destination",
		},
		{
			name: "premature finish",
			mutate: func(plans []board.ForwardPlan) []board.ForwardPlan {
				plans[0].Destination = board.Position{State: domain.PieceFinished}
				plans[0].ActualPreviousSpace = ""
				plans[0].Traversed = nil
				return plans
			},
			wantError: "finished before traversing home checkpoint",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			reference := loadCanonicalGraph(t)
			candidate := mutatingForwardPlanner{base: reference, mutate: test.mutate}
			err := CheckForwardPlanner(reference, candidate)
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("CheckForwardPlanner() error = %v, want containing %q", err, test.wantError)
			}
		})
	}
}

type mutatingForwardPlanner struct {
	base   board.ForwardPlanner
	mutate func([]board.ForwardPlan) []board.ForwardPlan
}

func (planner mutatingForwardPlanner) ForwardPlans(
	position board.Position,
	spaces int,
	policy board.ShortcutPolicy,
) ([]board.ForwardPlan, error) {
	plans, err := planner.base.ForwardPlans(position, spaces, policy)
	if err != nil {
		return nil, err
	}
	cloned := cloneForwardPlans(plans)
	return planner.mutate(cloned), nil
}

func cloneForwardPlans(plans []board.ForwardPlan) []board.ForwardPlan {
	cloned := append([]board.ForwardPlan(nil), plans...)
	for index := range cloned {
		cloned[index].Traversed = append([]board.SpaceID(nil), cloned[index].Traversed...)
	}
	return cloned
}

func loadCanonicalGraph(t *testing.T) *board.Graph {
	t.Helper()
	path := filepath.Join("..", "..", "..", "..", "spec", "board_graph.yaml")
	graph, err := board.LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile(%q) error = %v", path, err)
	}
	return graph
}
