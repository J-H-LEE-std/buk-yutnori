package board

import "fmt"

// FinishDistancePlanner is the shared immutable distance dependency used by
// Buk target selection and the CPU closest-to-finish policy.
type FinishDistancePlanner interface {
	RemainingForwardDistance(position Position, policy ShortcutPolicy) (int, error)
}

var _ FinishDistancePlanner = (*Graph)(nil)

// ReachableFrom returns all spaces reachable through legal forward edges.
func (g *Graph) ReachableFrom(origin SpaceID, policy ShortcutPolicy) ([]SpaceID, error) {
	if _, ok := g.nodeByID[origin]; !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownSpace, origin)
	}
	if err := validateShortcutPolicy(policy); err != nil {
		return nil, err
	}

	visited := map[SpaceID]struct{}{origin: {}}
	queue := []SpaceID{origin}
	result := make([]SpaceID, 0, len(g.nodeByID))
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		result = append(result, current)
		for _, destination := range g.forwardForPolicy(current, policy) {
			if _, seen := visited[destination]; seen {
				continue
			}
			if _, exists := g.nodeByID[destination]; !exists {
				continue
			}
			visited[destination] = struct{}{}
			queue = append(queue, destination)
		}
	}
	return result, nil
}

// RemainingForwardDistance returns the minimum legal number of forward spaces
// from position to the conceptual finish node.
func (g *Graph) RemainingForwardDistance(
	position Position,
	policy ShortcutPolicy,
) (int, error) {
	if err := validateShortcutPolicy(policy); err != nil {
		return 0, err
	}

	switch position.State {
	case PieceWaiting, PieceFinished:
		return 0, fmt.Errorf("%w: piece state %q", ErrFinishDistanceUnavailable, position.State)
	case PieceHomeCheckpoint:
		if position.Space != g.homeCheckpointSpace {
			return 0, fmt.Errorf(
				"%w: home checkpoint must be %q, got %q",
				ErrInvalidPosition,
				g.homeCheckpointSpace,
				position.Space,
			)
		}
		return g.homeCheckpointDistance, nil
	case PieceOnBoard:
		if _, ok := g.nodeByID[position.Space]; !ok {
			return 0, fmt.Errorf("%w: %q", ErrUnknownSpace, position.Space)
		}
		if position.Space == g.homeCheckpointSpace {
			return 0, fmt.Errorf(
				"%w: %q must use home checkpoint state",
				ErrInvalidPosition,
				position.Space,
			)
		}
	default:
		return 0, fmt.Errorf("%w: unknown piece state %q", ErrInvalidPosition, position.State)
	}

	distance, found, err := g.shortestDistance(
		position.Space,
		g.homeCheckpointSpace,
		policy,
	)
	if err != nil {
		return 0, err
	}
	if !found {
		return 0, fmt.Errorf(
			"%w: from %q under %q policy",
			ErrNoFinishPath,
			position.Space,
			policy,
		)
	}
	return distance + g.homeCheckpointDistance, nil
}

func (g *Graph) shortestDistance(
	origin SpaceID,
	destination SpaceID,
	policy ShortcutPolicy,
) (int, bool, error) {
	if _, ok := g.nodeByID[origin]; !ok {
		return 0, false, fmt.Errorf("%w: %q", ErrUnknownSpace, origin)
	}
	if _, ok := g.nodeByID[destination]; !ok {
		return 0, false, fmt.Errorf("%w: %q", ErrUnknownSpace, destination)
	}
	if err := validateShortcutPolicy(policy); err != nil {
		return 0, false, err
	}
	if origin == destination {
		return 0, true, nil
	}

	type step struct {
		space    SpaceID
		distance int
	}
	visited := map[SpaceID]struct{}{origin: {}}
	queue := []step{{space: origin}}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for _, next := range g.forwardForPolicy(current.space, policy) {
			if _, exists := g.nodeByID[next]; !exists {
				continue
			}
			if _, seen := visited[next]; seen {
				continue
			}
			nextDistance := current.distance + 1
			if next == destination {
				return nextDistance, true, nil
			}
			visited[next] = struct{}{}
			queue = append(queue, step{space: next, distance: nextDistance})
		}
	}
	return 0, false, nil
}

func (g *Graph) forwardForPolicy(origin SpaceID, policy ShortcutPolicy) []SpaceID {
	if policy == ForcedShortcuts {
		if destination, ok := g.forcedEdges[origin]; ok {
			return []SpaceID{destination}
		}
	}
	return g.adjacency[origin]
}

func validateShortcutPolicy(policy ShortcutPolicy) error {
	switch policy {
	case SelectableShortcuts, ForcedShortcuts:
		return nil
	default:
		return fmt.Errorf("%w: %q", ErrInvalidShortcutPolicy, policy)
	}
}
