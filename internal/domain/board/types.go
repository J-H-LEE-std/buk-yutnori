// Package board loads and evaluates the canonical Yutnori board graph.
//
// The package contains only domain data and deterministic graph operations. It
// does not depend on transport, persistence, random generation, or turn state.
package board

import (
	"errors"
	"fmt"

	"buk-yutnori/internal/domain"
)

// SpaceID is the stable identifier of a logical board space.
type SpaceID = domain.SpaceID

// Tag describes a static property of a board node.
type Tag string

const (
	TagStart          Tag = "start"
	TagHomeCheckpoint Tag = "home_checkpoint"
	TagOuter          Tag = "outer"
	TagOuterCorner    Tag = "outer_corner"
	TagShortcutEntry  Tag = "shortcut_entry"
	TagInner          Tag = "inner"
	TagCenter         Tag = "center"
	TagRouteChoice    Tag = "route_choice"
	TagShortcut       Tag = "shortcut"
	TagBukCandidate   Tag = "buk_candidate"
)

// Node is an immutable logical board space.
type Node struct {
	id   SpaceID
	name string
	tags []Tag
}

// ID returns the stable node identifier.
func (n Node) ID() SpaceID {
	return n.id
}

// Name returns the display name from the canonical specification.
func (n Node) Name() string {
	return n.name
}

// Tags returns a copy of the node's tags.
func (n Node) Tags() []Tag {
	return append([]Tag(nil), n.tags...)
}

// HasTag reports whether the node carries tag.
func (n Node) HasTag(tag Tag) bool {
	for _, candidate := range n.tags {
		if candidate == tag {
			return true
		}
	}
	return false
}

// ShortcutPolicy determines which outgoing edges are legal at route choices.
type ShortcutPolicy string

const (
	SelectableShortcuts ShortcutPolicy = "selectable"
	ForcedShortcuts     ShortcutPolicy = "forced"
)

// PieceState is the subset of piece state needed by board calculations.
type PieceState = domain.PieceState

const (
	PieceWaiting        = domain.PieceWaiting
	PieceOnBoard        = domain.PieceOnBoard
	PieceHomeCheckpoint = domain.PieceHomeCheckpoint
	PieceFinished       = domain.PieceFinished
)

// Position identifies a piece state and, when applicable, its logical space.
type Position struct {
	State PieceState
	Space SpaceID
}

var (
	ErrFinishDistanceUnavailable = errors.New("finish distance is unavailable")
	ErrUnknownSpace              = errors.New("unknown board space")
	ErrInvalidShortcutPolicy     = errors.New("invalid shortcut policy")
	ErrInvalidPosition           = errors.New("invalid board position")
	ErrNoFinishPath              = errors.New("no legal forward path to finish")
)

// ValidationError reports every graph integrity problem found in one pass.
type ValidationError struct {
	Problems []string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("invalid board graph: %s", joinProblems(e.Problems))
}

func joinProblems(problems []string) string {
	if len(problems) == 0 {
		return ""
	}
	result := problems[0]
	for _, problem := range problems[1:] {
		result += "; " + problem
	}
	return result
}

type edge struct {
	from SpaceID
	to   SpaceID
}

type routeChoice struct {
	normal   SpaceID
	shortcut SpaceID
}

type coordinate struct {
	x float64
	y float64
}

// Graph is the immutable board topology built from board_graph.yaml.
type Graph struct {
	version                    int
	status                     string
	startSpace                 SpaceID
	homeCheckpointSpace        SpaceID
	nodes                      []Node
	nodeByID                   map[SpaceID]Node
	edges                      []edge
	adjacency                  map[SpaceID][]SpaceID
	routeChoices               map[SpaceID]routeChoice
	forcedEdges                map[SpaceID]SpaceID
	selectableRule             string
	fixedBukDestination        SpaceID
	bukCandidates              []SpaceID
	finishEdge                 [2]SpaceID
	homeCheckpointDistance     int
	selectableDistanceRule     string
	forcedDistanceRule         string
	innerDistanceRule          string
	excludedDistanceStates     []PieceState
	renderCoordinates          map[SpaceID]coordinate
	renderCoordinateEntryCount int
}

// NodeCount returns the number of declared nodes.
func (g *Graph) NodeCount() int {
	return len(g.nodes)
}

// EdgeCount returns the number of declared forward edges.
func (g *Graph) EdgeCount() int {
	return len(g.edges)
}

// StartSpace returns the canonical start space.
func (g *Graph) StartSpace() SpaceID {
	return g.startSpace
}

// HomeCheckpointSpace returns the logical home checkpoint space.
func (g *Graph) HomeCheckpointSpace() SpaceID {
	return g.homeCheckpointSpace
}

// Node returns a copy of the node identified by id.
func (g *Graph) Node(id SpaceID) (Node, bool) {
	node, ok := g.nodeByID[id]
	return node, ok
}

// Nodes returns the nodes in specification order.
func (g *Graph) Nodes() []Node {
	return append([]Node(nil), g.nodes...)
}

// ForwardSpaces returns all declared outgoing edges in specification order.
func (g *Graph) ForwardSpaces(id SpaceID) ([]SpaceID, error) {
	if _, ok := g.nodeByID[id]; !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownSpace, id)
	}
	return append([]SpaceID(nil), g.adjacency[id]...), nil
}

// HasForwardEdge reports whether the directed edge is declared.
func (g *Graph) HasForwardEdge(from, to SpaceID) bool {
	for _, destination := range g.adjacency[from] {
		if destination == to {
			return true
		}
	}
	return false
}

// FixedBukDestination returns the destination used when random Buk is off.
func (g *Graph) FixedBukDestination() SpaceID {
	return g.fixedBukDestination
}

// BukCandidates returns random Buk destinations in specification order.
func (g *Graph) BukCandidates() []SpaceID {
	return append([]SpaceID(nil), g.bukCandidates...)
}
