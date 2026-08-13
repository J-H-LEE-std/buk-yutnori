package boardtest

import (
	"fmt"
	"reflect"
	"slices"

	"buk-yutnori/internal/domain"
	"buk-yutnori/internal/domain/board"
)

var conformancePolicies = []board.ShortcutPolicy{
	board.SelectableShortcuts,
	board.ForcedShortcuts,
}

// CheckForwardPlanner verifies candidate against the canonical reference graph.
//
// The contract covers every canonical piece position, both shortcut policies,
// and every movement distance through one space beyond the longest legal
// finish distance. This checks route selection, board topology, checkpoint and
// finish transitions, and the exact stable plan order expected by match.Game.
func CheckForwardPlanner(reference *board.Graph, candidate board.ForwardPlanner) error {
	if reference == nil {
		return fmt.Errorf("forward planner conformance: canonical reference is required")
	}
	if isNilForwardPlanner(candidate) {
		return fmt.Errorf("forward planner conformance: candidate is required")
	}

	positions := conformancePositions(reference)
	maxSpaces, err := conformanceMaxSpaces(reference, positions)
	if err != nil {
		return err
	}
	for _, position := range positions {
		for _, policy := range conformancePolicies {
			for spaces := 1; spaces <= maxSpaces; spaces++ {
				if err := checkForwardRequest(reference, candidate, position, spaces, policy); err != nil {
					return fmt.Errorf(
						"forward planner conformance: position=%s spaces=%d policy=%q: %w",
						formatPosition(position),
						spaces,
						policy,
						err,
					)
				}
			}
		}
	}
	return nil
}

func conformancePositions(reference *board.Graph) []board.Position {
	positions := []board.Position{{State: domain.PieceWaiting}}
	for _, node := range reference.Nodes() {
		if node.ID() == reference.HomeCheckpointSpace() {
			continue
		}
		positions = append(positions, board.Position{
			State: domain.PieceOnBoard,
			Space: node.ID(),
		})
	}
	return append(positions, board.Position{
		State: domain.PieceHomeCheckpoint,
		Space: reference.HomeCheckpointSpace(),
	})
}

func conformanceMaxSpaces(
	reference *board.Graph,
	positions []board.Position,
) (int, error) {
	maxFinishSpaces := 1
	for _, position := range positions {
		for _, policy := range conformancePolicies {
			finishSpaces, err := forwardFinishSpaces(reference, position, policy)
			if err != nil {
				return 0, err
			}
			if finishSpaces > maxFinishSpaces {
				maxFinishSpaces = finishSpaces
			}
		}
	}
	return maxFinishSpaces + 1, nil
}

func forwardFinishSpaces(
	reference *board.Graph,
	position board.Position,
	policy board.ShortcutPolicy,
) (int, error) {
	limit := reference.NodeCount() + 1
	for spaces := 1; spaces <= limit; spaces++ {
		plans, err := reference.ForwardPlans(position, spaces, policy)
		if err != nil {
			return 0, fmt.Errorf(
				"forward planner conformance: reference plan for position=%s spaces=%d policy=%q: %w",
				formatPosition(position),
				spaces,
				policy,
				err,
			)
		}
		allFinished := len(plans) > 0
		for _, plan := range plans {
			if plan.Destination.State != domain.PieceFinished {
				allFinished = false
				break
			}
		}
		if allFinished {
			return spaces, nil
		}
	}
	return 0, fmt.Errorf(
		"forward planner conformance: reference does not finish position=%s policy=%q within %d spaces",
		formatPosition(position),
		policy,
		limit,
	)
}

func checkForwardRequest(
	reference *board.Graph,
	candidate board.ForwardPlanner,
	position board.Position,
	spaces int,
	policy board.ShortcutPolicy,
) error {
	want, err := reference.ForwardPlans(position, spaces, policy)
	if err != nil {
		return fmt.Errorf("canonical reference failed: %w", err)
	}
	got, err := candidate.ForwardPlans(position, spaces, policy)
	if err != nil {
		return fmt.Errorf("candidate failed valid request: %w", err)
	}
	if len(got) != len(want) {
		return fmt.Errorf("plan count = %d, want %d", len(got), len(want))
	}
	for index := range got {
		if got[index].Route != want[index].Route {
			return fmt.Errorf(
				"plan[%d] route = %q, want %q",
				index,
				got[index].Route,
				want[index].Route,
			)
		}
		if err := validatePlanTopology(reference, position, got[index]); err != nil {
			return fmt.Errorf("plan[%d]: %w", index, err)
		}
		if !sameForwardPlan(got[index], want[index]) {
			return fmt.Errorf("plan[%d] = %#v, want %#v", index, got[index], want[index])
		}
	}
	return nil
}

func sameForwardPlan(left, right board.ForwardPlan) bool {
	return left.Route == right.Route &&
		left.Destination == right.Destination &&
		left.ActualPreviousSpace == right.ActualPreviousSpace &&
		slices.Equal(left.Traversed, right.Traversed)
}

func validatePlanTopology(
	reference *board.Graph,
	position board.Position,
	plan board.ForwardPlan,
) error {
	if err := plan.Route.Validate(); err != nil {
		return fmt.Errorf("invalid route: %w", err)
	}
	if plan.ActualPreviousSpace != "" {
		if _, ok := reference.Node(plan.ActualPreviousSpace); !ok {
			return fmt.Errorf("unknown actual previous space %q", plan.ActualPreviousSpace)
		}
	}
	for index, spaceID := range plan.Traversed {
		if _, ok := reference.Node(spaceID); !ok {
			return fmt.Errorf("unknown traversed space at index %d: %q", index, spaceID)
		}
	}

	switch plan.Destination.State {
	case domain.PieceOnBoard:
		if _, ok := reference.Node(plan.Destination.Space); !ok {
			return fmt.Errorf("unknown destination %q", plan.Destination.Space)
		}
		if plan.Destination.Space == reference.HomeCheckpointSpace() {
			return fmt.Errorf("on-board destination uses home checkpoint %q", plan.Destination.Space)
		}
		if err := validateUnfinishedPlan(reference, position, plan); err != nil {
			return err
		}
	case domain.PieceHomeCheckpoint:
		if plan.Destination.Space != reference.HomeCheckpointSpace() {
			return fmt.Errorf(
				"home checkpoint destination = %q, want %q",
				plan.Destination.Space,
				reference.HomeCheckpointSpace(),
			)
		}
		if err := validateUnfinishedPlan(reference, position, plan); err != nil {
			return err
		}
	case domain.PieceFinished:
		if plan.Destination.Space != "" || plan.ActualPreviousSpace != "" {
			return fmt.Errorf("finished destination retains board or path state")
		}
		if position.State == domain.PieceHomeCheckpoint {
			if len(plan.Traversed) != 0 {
				return fmt.Errorf("home checkpoint finish traversed spaces %v", plan.Traversed)
			}
			return nil
		}
		if len(plan.Traversed) == 0 ||
			plan.Traversed[len(plan.Traversed)-1] != reference.HomeCheckpointSpace() {
			return fmt.Errorf("finished before traversing home checkpoint")
		}
	default:
		return fmt.Errorf("invalid destination state %q", plan.Destination.State)
	}

	return validateTraversedEdges(reference, position, plan.Traversed)
}

func validateUnfinishedPlan(
	reference *board.Graph,
	position board.Position,
	plan board.ForwardPlan,
) error {
	if len(plan.Traversed) == 0 {
		return fmt.Errorf("unfinished plan has empty traversal")
	}
	finalSpace := plan.Traversed[len(plan.Traversed)-1]
	if plan.Destination.Space != finalSpace {
		return fmt.Errorf(
			"destination differs from final traversed space: got %q, want %q",
			plan.Destination.Space,
			finalSpace,
		)
	}

	wantPrevious := forwardOrigin(reference, position)
	if len(plan.Traversed) > 1 {
		wantPrevious = plan.Traversed[len(plan.Traversed)-2]
	}
	if plan.ActualPreviousSpace != wantPrevious {
		return fmt.Errorf(
			"actual previous space = %q, want %q",
			plan.ActualPreviousSpace,
			wantPrevious,
		)
	}
	return nil
}

func validateTraversedEdges(
	reference *board.Graph,
	position board.Position,
	traversed []board.SpaceID,
) error {
	current := forwardOrigin(reference, position)
	for index, next := range traversed {
		if !reference.HasForwardEdge(current, next) {
			return fmt.Errorf(
				"illegal forward edge at index %d: %q -> %q",
				index,
				current,
				next,
			)
		}
		current = next
	}
	return nil
}

func forwardOrigin(reference *board.Graph, position board.Position) board.SpaceID {
	if position.State == domain.PieceWaiting {
		return reference.StartSpace()
	}
	return position.Space
}

func formatPosition(position board.Position) string {
	if position.Space == "" {
		return string(position.State)
	}
	return fmt.Sprintf("%s@%s", position.State, position.Space)
}

func isNilForwardPlanner(planner board.ForwardPlanner) bool {
	if planner == nil {
		return true
	}
	value := reflect.ValueOf(planner)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}
