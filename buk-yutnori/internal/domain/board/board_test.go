package board

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestCanonicalBoardGraphInvariants(t *testing.T) {
	graph := loadCanonicalGraph(t)

	if got, want := graph.NodeCount(), 29; got != want {
		t.Fatalf("NodeCount() = %d, want %d", got, want)
	}
	if got, want := graph.EdgeCount(), 32; got != want {
		t.Fatalf("EdgeCount() = %d, want %d", got, want)
	}
	if got, want := graph.StartSpace(), SpaceID("chammeogi"); got != want {
		t.Fatalf("StartSpace() = %q, want %q", got, want)
	}
	if got, want := graph.HomeCheckpointSpace(), SpaceID("chammeogi"); got != want {
		t.Fatalf("HomeCheckpointSpace() = %q, want %q", got, want)
	}

	reachable, err := graph.ReachableFrom(graph.StartSpace(), SelectableShortcuts)
	if err != nil {
		t.Fatalf("ReachableFrom() error = %v", err)
	}
	if got, want := len(reachable), graph.NodeCount(); got != want {
		t.Fatalf("reachable nodes = %d, want %d", got, want)
	}

	canonicalPaths := [][]SpaceID{
		{
			"chammeogi",
			"do", "gae", "geol", "yut", "mo",
			"back_do", "back_gae", "back_geol", "back_yut", "back_mo",
			"jji_do", "jji_gae", "jji_geol", "jji_yut", "jji_mo",
			"nal_do", "nal_gae", "nal_geol", "nal_yut",
			"chammeogi",
		},
		{"mo", "mo_do", "mo_gae", "bang"},
		{"back_mo", "back_mo_do", "back_mo_gae", "bang"},
		{"bang", "sok_yut", "sok_mo", "jji_mo"},
		{"bang", "bangsugi", "anjji", "chammeogi"},
	}
	for _, path := range canonicalPaths {
		assertForwardPath(t, graph, path)
	}
}

func TestCanonicalBukCandidatesMatchTagsAndList(t *testing.T) {
	graph := loadCanonicalGraph(t)
	want := []SpaceID{
		"back_do",
		"back_gae",
		"back_geol",
		"back_yut",
		"jji_do",
		"jji_gae",
		"jji_geol",
		"jji_yut",
		"sok_yut",
		"sok_mo",
	}

	if got := graph.BukCandidates(); !reflect.DeepEqual(got, want) {
		t.Fatalf("BukCandidates() = %v, want %v", got, want)
	}
	for _, space := range want {
		node, ok := graph.Node(space)
		if !ok {
			t.Fatalf("buk candidate %q does not exist", space)
		}
		if !node.HasTag(TagBukCandidate) {
			t.Errorf("buk candidate %q is missing tag %q", space, TagBukCandidate)
		}
		if node.HasTag(TagRouteChoice) {
			t.Errorf("buk candidate %q must not require a route choice", space)
		}
	}

	if got, want := graph.FixedBukDestination(), SpaceID("jji_do"); got != want {
		t.Fatalf("FixedBukDestination() = %q, want %q", got, want)
	}
}

func TestLoadRejectsBrokenBoardInvariants(t *testing.T) {
	canonical := readCanonicalSpec(t)
	tests := []struct {
		name        string
		old         string
		replacement string
		wantError   string
	}{
		{
			name:        "duplicate node ID",
			old:         "  - {id: do, name: 도, tags: [outer]}",
			replacement: "  - {id: chammeogi, name: 도, tags: [outer]}",
			wantError:   `duplicate node ID "chammeogi"`,
		},
		{
			name:        "edge references unknown node",
			old:         "  - [chammeogi, do]",
			replacement: "  - [chammeogi, nowhere]",
			wantError:   `edge references unknown destination "nowhere"`,
		},
		{
			name:        "node is unreachable from start",
			old:         "  - [chammeogi, do]",
			replacement: "  - [chammeogi, chammeogi]",
			wantError:   "nodes unreachable from start",
		},
		{
			name:        "buk tag differs from explicit list",
			old:         "  - {id: back_do, name: 뒷도, tags: [outer, buk_candidate]}",
			replacement: "  - {id: back_do, name: 뒷도, tags: [outer]}",
			wantError:   "buk candidate tags differ from explicit list",
		},
		{
			name:        "undeclared graph branch",
			old:         "  - [do, gae]",
			replacement: "  - [do, gae]\n  - [do, geol]",
			wantError:   `node "do" branches without a route choice`,
		},
		{
			name:        "unsupported finish distance policy",
			old:         "  selectable_shortcut: minimum_over_legal_routes",
			replacement: "  selectable_shortcut: normal_route_only",
			wantError:   "selectable finish distance rule",
		},
		{
			name:        "unknown top-level field",
			old:         "version: 1",
			replacement: "version: 1\nunexpected: true",
			wantError:   "field unexpected not found",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			broken := replaceOnce(t, canonical, test.old, test.replacement)
			_, err := Load(strings.NewReader(broken))
			if err == nil {
				t.Fatal("Load() error = nil, want validation error")
			}
			if !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("Load() error = %q, want substring %q", err, test.wantError)
			}
		})
	}
}

func TestRemainingForwardDistanceOnCanonicalBoard(t *testing.T) {
	graph := loadCanonicalGraph(t)
	tests := []struct {
		name     string
		position Position
		policy   ShortcutPolicy
		want     int
	}{
		{
			name:     "home checkpoint",
			position: Position{State: PieceHomeCheckpoint, Space: "chammeogi"},
			policy:   SelectableShortcuts,
			want:     1,
		},
		{
			name:     "last outer space",
			position: Position{State: PieceOnBoard, Space: "nal_yut"},
			policy:   SelectableShortcuts,
			want:     2,
		},
		{
			name:     "center selectable",
			position: Position{State: PieceOnBoard, Space: "bang"},
			policy:   SelectableShortcuts,
			want:     4,
		},
		{
			name:     "center forced",
			position: Position{State: PieceOnBoard, Space: "bang"},
			policy:   ForcedShortcuts,
			want:     4,
		},
		{
			name:     "inner route",
			position: Position{State: PieceOnBoard, Space: "sok_yut"},
			policy:   SelectableShortcuts,
			want:     8,
		},
		{
			name:     "outer shortcut entry",
			position: Position{State: PieceOnBoard, Space: "mo"},
			policy:   SelectableShortcuts,
			want:     7,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := graph.RemainingForwardDistance(test.position, test.policy)
			if err != nil {
				t.Fatalf("RemainingForwardDistance() error = %v", err)
			}
			if got != test.want {
				t.Fatalf("RemainingForwardDistance() = %d, want %d", got, test.want)
			}
		})
	}
}

func TestRemainingForwardDistanceUsesForcedEdgesOnly(t *testing.T) {
	spec := readCanonicalSpec(t)
	spec = replaceOnce(
		t,
		spec,
		"  bang:\n    normal: sok_yut\n    shortcut: bangsugi",
		"  bang:\n    normal: bangsugi\n    shortcut: sok_yut",
	)
	spec = replaceOnce(t, spec, "    bang: bangsugi", "    bang: sok_yut")

	graph, err := Load(strings.NewReader(spec))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	position := Position{State: PieceOnBoard, Space: "bang"}

	selectable, err := graph.RemainingForwardDistance(position, SelectableShortcuts)
	if err != nil {
		t.Fatalf("selectable RemainingForwardDistance() error = %v", err)
	}
	forced, err := graph.RemainingForwardDistance(position, ForcedShortcuts)
	if err != nil {
		t.Fatalf("forced RemainingForwardDistance() error = %v", err)
	}
	if selectable != 4 {
		t.Fatalf("selectable distance = %d, want 4", selectable)
	}
	if forced != 9 {
		t.Fatalf("forced distance = %d, want 9", forced)
	}
}

func TestRemainingForwardDistanceRejectsExcludedAndInvalidPositions(t *testing.T) {
	graph := loadCanonicalGraph(t)
	tests := []struct {
		name      string
		position  Position
		policy    ShortcutPolicy
		wantError error
	}{
		{
			name:      "waiting",
			position:  Position{State: PieceWaiting},
			policy:    SelectableShortcuts,
			wantError: ErrFinishDistanceUnavailable,
		},
		{
			name:      "finished",
			position:  Position{State: PieceFinished},
			policy:    SelectableShortcuts,
			wantError: ErrFinishDistanceUnavailable,
		},
		{
			name:      "unknown space",
			position:  Position{State: PieceOnBoard, Space: "nowhere"},
			policy:    SelectableShortcuts,
			wantError: ErrUnknownSpace,
		},
		{
			name:      "invalid shortcut policy",
			position:  Position{State: PieceOnBoard, Space: "do"},
			policy:    ShortcutPolicy("invalid"),
			wantError: ErrInvalidShortcutPolicy,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := graph.RemainingForwardDistance(test.position, test.policy)
			if !errors.Is(err, test.wantError) {
				t.Fatalf("RemainingForwardDistance() error = %v, want %v", err, test.wantError)
			}
		})
	}
}

func loadCanonicalGraph(t *testing.T) *Graph {
	t.Helper()
	graph, err := LoadFile(canonicalSpecPath(t))
	if err != nil {
		t.Fatalf("LoadFile() error = %v", err)
	}
	return graph
}

func readCanonicalSpec(t *testing.T) string {
	t.Helper()
	content, err := os.ReadFile(canonicalSpecPath(t))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	return string(content)
}

func canonicalSpecPath(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller() could not locate test file")
	}
	return filepath.Join(filepath.Dir(filename), "..", "..", "..", "spec", "board_graph.yaml")
}

func replaceOnce(t *testing.T, input, old, replacement string) string {
	t.Helper()
	if got := strings.Count(input, old); got != 1 {
		t.Fatalf("fixture marker %q occurs %d times, want 1", old, got)
	}
	return strings.Replace(input, old, replacement, 1)
}

func assertForwardPath(t *testing.T, graph *Graph, path []SpaceID) {
	t.Helper()
	for index := 1; index < len(path); index++ {
		from := path[index-1]
		to := path[index]
		if !graph.HasForwardEdge(from, to) {
			t.Errorf("missing canonical edge %s -> %s", from, to)
		}
	}
}
