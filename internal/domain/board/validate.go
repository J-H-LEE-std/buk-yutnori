package board

import (
	"fmt"
	"sort"
)

// Validate checks the static graph invariants required by the canonical spec.
func (g *Graph) Validate() error {
	if g == nil {
		return &ValidationError{Problems: []string{"graph is nil"}}
	}

	var problems []string
	if g.version != 1 {
		problems = append(problems, fmt.Sprintf("version = %d, want 1", g.version))
	}
	if g.status != "canonical" {
		problems = append(problems, fmt.Sprintf("status = %q, want canonical", g.status))
	}

	nodeCounts := make(map[SpaceID]int, len(g.nodes))
	for index, node := range g.nodes {
		nodeCounts[node.id]++
		if node.id == "" {
			problems = append(problems, fmt.Sprintf("node %d has empty ID", index))
		}
		if node.name == "" {
			problems = append(problems, fmt.Sprintf("node %q has empty name", node.id))
		}
		tagCounts := make(map[Tag]int, len(node.tags))
		for _, tag := range node.tags {
			tagCounts[tag]++
			if tag == "" {
				problems = append(problems, fmt.Sprintf("node %q has empty tag", node.id))
			}
		}
		for _, tag := range sortedDuplicateTags(tagCounts) {
			problems = append(
				problems,
				fmt.Sprintf("node %q has duplicate tag %q", node.id, tag),
			)
		}
	}
	for _, id := range sortedDuplicateSpaceIDs(nodeCounts) {
		problems = append(problems, fmt.Sprintf("duplicate node ID %q", id))
	}

	if _, ok := g.nodeByID[g.startSpace]; !ok {
		problems = append(problems, fmt.Sprintf("start space %q does not exist", g.startSpace))
	}
	if _, ok := g.nodeByID[g.homeCheckpointSpace]; !ok {
		problems = append(
			problems,
			fmt.Sprintf("home checkpoint space %q does not exist", g.homeCheckpointSpace),
		)
	}
	if g.startSpace != g.homeCheckpointSpace {
		problems = append(
			problems,
			fmt.Sprintf(
				"start space %q differs from home checkpoint space %q",
				g.startSpace,
				g.homeCheckpointSpace,
			),
		)
	}

	edgeCounts := make(map[edge]int, len(g.edges))
	for _, item := range g.edges {
		edgeCounts[item]++
		if _, ok := g.nodeByID[item.from]; !ok {
			problems = append(
				problems,
				fmt.Sprintf("edge references unknown source %q", item.from),
			)
		}
		if _, ok := g.nodeByID[item.to]; !ok {
			problems = append(
				problems,
				fmt.Sprintf("edge references unknown destination %q", item.to),
			)
		}
		if item.from == item.to {
			problems = append(problems, fmt.Sprintf("self edge %q -> %q", item.from, item.to))
		}
	}
	for _, item := range sortedDuplicateEdges(edgeCounts) {
		problems = append(
			problems,
			fmt.Sprintf("duplicate forward edge %q -> %q", item.from, item.to),
		)
	}

	problems = append(problems, g.validateRouteChoices()...)
	problems = append(problems, g.validateReachability()...)
	problems = append(problems, g.validateOuterRing()...)
	problems = append(problems, g.validateBukCandidates()...)
	problems = append(problems, g.validateFinishDistance()...)
	problems = append(problems, g.validateRenderCoordinates()...)

	if len(problems) > 0 {
		return &ValidationError{Problems: problems}
	}
	return nil
}

func (g *Graph) validateRouteChoices() []string {
	var problems []string
	for _, source := range sortedRouteSources(g.routeChoices) {
		choice := g.routeChoices[source]
		if _, ok := g.nodeByID[source]; !ok {
			problems = append(
				problems,
				fmt.Sprintf("route choice references unknown source %q", source),
			)
		}
		if _, ok := g.nodeByID[choice.normal]; !ok {
			problems = append(
				problems,
				fmt.Sprintf("route choice %q has unknown normal destination %q", source, choice.normal),
			)
		}
		if _, ok := g.nodeByID[choice.shortcut]; !ok {
			problems = append(
				problems,
				fmt.Sprintf(
					"route choice %q has unknown shortcut destination %q",
					source,
					choice.shortcut,
				),
			)
		}
		if choice.normal == choice.shortcut {
			problems = append(
				problems,
				fmt.Sprintf("route choice %q uses the same destination twice", source),
			)
		}
		if !g.HasForwardEdge(source, choice.normal) {
			problems = append(
				problems,
				fmt.Sprintf("normal route is not an edge: %q -> %q", source, choice.normal),
			)
		}
		if !g.HasForwardEdge(source, choice.shortcut) {
			problems = append(
				problems,
				fmt.Sprintf("shortcut route is not an edge: %q -> %q", source, choice.shortcut),
			)
		}
		if len(g.adjacency[source]) != 2 {
			problems = append(
				problems,
				fmt.Sprintf(
					"route choice %q has %d outgoing edges, want 2",
					source,
					len(g.adjacency[source]),
				),
			)
		}
	}
	for _, source := range sortedNodeIDs(g.nodeByID) {
		if len(g.adjacency[source]) <= 1 {
			continue
		}
		if _, ok := g.routeChoices[source]; !ok {
			problems = append(
				problems,
				fmt.Sprintf("node %q branches without a route choice", source),
			)
		}
	}

	for _, source := range sortedSpaceMapKeys(g.forcedEdges) {
		destination := g.forcedEdges[source]
		choice, ok := g.routeChoices[source]
		if !ok {
			problems = append(
				problems,
				fmt.Sprintf("forced route references non-choice node %q", source),
			)
			continue
		}
		if destination != choice.shortcut {
			problems = append(
				problems,
				fmt.Sprintf(
					"forced route %q -> %q differs from shortcut %q",
					source,
					destination,
					choice.shortcut,
				),
			)
		}
	}
	for _, source := range sortedRouteSources(g.routeChoices) {
		if _, ok := g.forcedEdges[source]; !ok {
			problems = append(problems, fmt.Sprintf("route choice %q has no forced edge", source))
		}
	}

	if g.selectableRule != "player_chooses_at_next_forward_move" {
		problems = append(
			problems,
			fmt.Sprintf("unexpected selectable shortcut rule %q", g.selectableRule),
		)
	}
	return problems
}

func (g *Graph) validateReachability() []string {
	if _, ok := g.nodeByID[g.startSpace]; !ok {
		return nil
	}

	var problems []string
	reachable, err := g.ReachableFrom(g.startSpace, SelectableShortcuts)
	if err == nil && len(reachable) != len(g.nodeByID) {
		reached := make(map[SpaceID]struct{}, len(reachable))
		for _, id := range reachable {
			reached[id] = struct{}{}
		}
		problems = append(
			problems,
			fmt.Sprintf("nodes unreachable from start: %v", sortedMissingIDs(g.nodeByID, reached)),
		)
	}

	for _, policy := range []ShortcutPolicy{SelectableShortcuts, ForcedShortcuts} {
		for _, id := range sortedNodeIDs(g.nodeByID) {
			if _, found, pathErr := g.shortestDistance(id, g.homeCheckpointSpace, policy); pathErr == nil && !found {
				problems = append(
					problems,
					fmt.Sprintf("no %s forward path from %q to home checkpoint", policy, id),
				)
			}
		}
	}
	return problems
}

func (g *Graph) validateOuterRing() []string {
	outer := make(map[SpaceID]struct{})
	for _, node := range g.nodes {
		if node.HasTag(TagStart) || node.HasTag(TagOuter) || node.HasTag(TagOuterCorner) {
			outer[node.id] = struct{}{}
		}
	}
	if _, ok := outer[g.startSpace]; !ok {
		return []string{fmt.Sprintf("start space %q is not tagged as outer", g.startSpace)}
	}

	visited := map[SpaceID]struct{}{g.startSpace: {}}
	current := g.startSpace
	for step := 0; step <= len(outer); step++ {
		var next []SpaceID
		if choice, ok := g.routeChoices[current]; ok {
			if _, isOuter := outer[choice.normal]; isOuter {
				next = append(next, choice.normal)
			}
		} else {
			for _, destination := range g.adjacency[current] {
				if _, isOuter := outer[destination]; isOuter {
					next = append(next, destination)
				}
			}
		}

		if len(next) != 1 {
			return []string{
				fmt.Sprintf("outer ring node %q has %d normal outer successors", current, len(next)),
			}
		}
		current = next[0]
		if current == g.startSpace {
			if len(visited) != len(outer) {
				return []string{
					fmt.Sprintf(
						"outer ring returned to start after %d of %d nodes",
						len(visited),
						len(outer),
					),
				}
			}
			return nil
		}
		if _, exists := visited[current]; exists {
			return []string{fmt.Sprintf("outer ring cycles at %q before start", current)}
		}
		visited[current] = struct{}{}
	}
	return []string{fmt.Sprintf("outer ring does not return to start %q", g.startSpace)}
}

func (g *Graph) validateBukCandidates() []string {
	var problems []string
	explicitCounts := make(map[SpaceID]int, len(g.bukCandidates))
	explicit := make(map[SpaceID]struct{}, len(g.bukCandidates))
	for _, id := range g.bukCandidates {
		explicitCounts[id]++
		explicit[id] = struct{}{}
		if _, ok := g.nodeByID[id]; !ok {
			problems = append(
				problems,
				fmt.Sprintf("buk candidate references unknown node %q", id),
			)
			continue
		}
		if _, routeChoiceNode := g.routeChoices[id]; routeChoiceNode {
			problems = append(
				problems,
				fmt.Sprintf("buk candidate %q requires a route choice", id),
			)
		}
		if g.nodeByID[id].HasTag(TagCenter) {
			problems = append(problems, fmt.Sprintf("buk candidate %q is a center node", id))
		}
	}
	for _, id := range sortedDuplicateSpaceIDs(explicitCounts) {
		problems = append(problems, fmt.Sprintf("duplicate buk candidate %q", id))
	}

	tagged := make(map[SpaceID]struct{})
	for _, node := range g.nodes {
		if node.HasTag(TagBukCandidate) {
			tagged[node.id] = struct{}{}
		}
	}
	if !sameSpaceSet(tagged, explicit) {
		problems = append(
			problems,
			fmt.Sprintf(
				"buk candidate tags differ from explicit list: tagged=%v explicit=%v",
				sortedSet(tagged),
				sortedSet(explicit),
			),
		)
	}
	if len(explicit) != 10 {
		problems = append(
			problems,
			fmt.Sprintf("buk candidate count = %d, want 10", len(explicit)),
		)
	}
	if _, ok := g.nodeByID[g.fixedBukDestination]; !ok {
		problems = append(
			problems,
			fmt.Sprintf("fixed buk destination %q does not exist", g.fixedBukDestination),
		)
	} else if _, ok := explicit[g.fixedBukDestination]; !ok {
		problems = append(
			problems,
			fmt.Sprintf(
				"fixed buk destination %q is not a random candidate",
				g.fixedBukDestination,
			),
		)
	}
	return problems
}

func (g *Graph) validateFinishDistance() []string {
	var problems []string
	if g.finishEdge[0] != g.homeCheckpointSpace || g.finishEdge[1] != "finished" {
		problems = append(
			problems,
			fmt.Sprintf(
				"conceptual finish edge = %q -> %q, want %q -> finished",
				g.finishEdge[0],
				g.finishEdge[1],
				g.homeCheckpointSpace,
			),
		)
	}
	if g.homeCheckpointDistance != 1 {
		problems = append(
			problems,
			fmt.Sprintf("home checkpoint distance = %d, want 1", g.homeCheckpointDistance),
		)
	}
	if g.selectableDistanceRule != "minimum_over_legal_routes" {
		problems = append(
			problems,
			fmt.Sprintf(
				"selectable finish distance rule = %q, want minimum_over_legal_routes",
				g.selectableDistanceRule,
			),
		)
	}
	if g.forcedDistanceRule != "forced_edges_only" {
		problems = append(
			problems,
			fmt.Sprintf(
				"forced finish distance rule = %q, want forced_edges_only",
				g.forcedDistanceRule,
			),
		)
	}
	if g.innerDistanceRule != "forward_edges_reachable_from_current_node_only" {
		problems = append(
			problems,
			fmt.Sprintf(
				"inner finish distance rule = %q, want forward_edges_reachable_from_current_node_only",
				g.innerDistanceRule,
			),
		)
	}
	wantExcludedStates := []PieceState{PieceWaiting, PieceFinished}
	if !samePieceStates(g.excludedDistanceStates, wantExcludedStates) {
		problems = append(
			problems,
			fmt.Sprintf(
				"excluded finish distance states = %v, want %v",
				g.excludedDistanceStates,
				wantExcludedStates,
			),
		)
	}
	return problems
}

func (g *Graph) validateRenderCoordinates() []string {
	var problems []string
	if g.renderCoordinateEntryCount != len(g.nodeByID) {
		problems = append(
			problems,
			fmt.Sprintf(
				"render coordinate count = %d, want %d",
				g.renderCoordinateEntryCount,
				len(g.nodeByID),
			),
		)
	}
	for _, id := range sortedNodeIDs(g.nodeByID) {
		value, ok := g.renderCoordinates[id]
		if !ok {
			problems = append(problems, fmt.Sprintf("node %q has no render coordinate", id))
			continue
		}
		if value.x < 0 || value.x > 1 || value.y < 0 || value.y > 1 {
			problems = append(
				problems,
				fmt.Sprintf("render coordinate for %q is outside normalized range", id),
			)
		}
	}
	for id := range g.renderCoordinates {
		if _, ok := g.nodeByID[id]; !ok {
			problems = append(
				problems,
				fmt.Sprintf("render coordinate references unknown node %q", id),
			)
		}
	}
	return problems
}

func sortedDuplicateTags(counts map[Tag]int) []Tag {
	var result []Tag
	for tag, count := range counts {
		if count > 1 {
			result = append(result, tag)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}

func sortedDuplicateSpaceIDs(counts map[SpaceID]int) []SpaceID {
	var result []SpaceID
	for id, count := range counts {
		if count > 1 {
			result = append(result, id)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}

func sortedDuplicateEdges(counts map[edge]int) []edge {
	var result []edge
	for item, count := range counts {
		if count > 1 {
			result = append(result, item)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].from == result[j].from {
			return result[i].to < result[j].to
		}
		return result[i].from < result[j].from
	})
	return result
}

func sortedRouteSources(routes map[SpaceID]routeChoice) []SpaceID {
	result := make([]SpaceID, 0, len(routes))
	for id := range routes {
		result = append(result, id)
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}

func sortedSpaceMapKeys(values map[SpaceID]SpaceID) []SpaceID {
	result := make([]SpaceID, 0, len(values))
	for id := range values {
		result = append(result, id)
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}

func sortedNodeIDs(nodes map[SpaceID]Node) []SpaceID {
	result := make([]SpaceID, 0, len(nodes))
	for id := range nodes {
		result = append(result, id)
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}

func sortedMissingIDs(nodes map[SpaceID]Node, present map[SpaceID]struct{}) []SpaceID {
	var result []SpaceID
	for id := range nodes {
		if _, ok := present[id]; !ok {
			result = append(result, id)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}

func sortedSet(values map[SpaceID]struct{}) []SpaceID {
	result := make([]SpaceID, 0, len(values))
	for id := range values {
		result = append(result, id)
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}

func sameSpaceSet(left, right map[SpaceID]struct{}) bool {
	if len(left) != len(right) {
		return false
	}
	for id := range left {
		if _, ok := right[id]; !ok {
			return false
		}
	}
	return true
}

func samePieceStates(left, right []PieceState) bool {
	if len(left) != len(right) {
		return false
	}
	counts := make(map[PieceState]int, len(left))
	for _, state := range left {
		counts[state]++
	}
	for _, state := range right {
		counts[state]--
	}
	for _, count := range counts {
		if count != 0 {
			return false
		}
	}
	return true
}
