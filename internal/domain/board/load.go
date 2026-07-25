package board

import (
	"errors"
	"fmt"
	"io"
	"os"

	"gopkg.in/yaml.v3"
)

// LoadFile loads and validates one board graph specification from path.
func LoadFile(path string) (*Graph, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open board graph: %w", err)
	}
	defer file.Close()

	graph, err := Load(file)
	if err != nil {
		return nil, fmt.Errorf("load board graph %q: %w", path, err)
	}
	return graph, nil
}

// Load strictly decodes and validates one board graph YAML document.
func Load(reader io.Reader) (*Graph, error) {
	decoder := yaml.NewDecoder(reader)
	decoder.KnownFields(true)

	var document boardDocument
	if err := decoder.Decode(&document); err != nil {
		return nil, fmt.Errorf("decode board graph: %w", err)
	}

	var extra yaml.Node
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err != nil {
			return nil, fmt.Errorf("decode trailing board graph document: %w", err)
		}
		return nil, errors.New("decode board graph: multiple YAML documents are not allowed")
	}

	graph, err := graphFromDocument(document)
	if err != nil {
		return nil, err
	}
	if err := graph.Validate(); err != nil {
		return nil, err
	}
	return graph, nil
}

func graphFromDocument(document boardDocument) (*Graph, error) {
	var structuralProblems []string
	edges := make([]edge, 0, len(document.ForwardEdges))
	for index, item := range document.ForwardEdges {
		if len(item) != 2 {
			structuralProblems = append(
				structuralProblems,
				fmt.Sprintf("forward edge %d has %d entries, want 2", index, len(item)),
			)
			continue
		}
		edges = append(edges, edge{from: item[0], to: item[1]})
	}

	if len(document.FinishDistance.ConceptualFinishEdge) != 2 {
		structuralProblems = append(
			structuralProblems,
			fmt.Sprintf(
				"conceptual finish edge has %d entries, want 2",
				len(document.FinishDistance.ConceptualFinishEdge),
			),
		)
	}

	nodes := make([]Node, 0, len(document.Nodes))
	nodeByID := make(map[SpaceID]Node, len(document.Nodes))
	for _, item := range document.Nodes {
		node := Node{
			id:   item.ID,
			name: item.Name,
			tags: append([]Tag(nil), item.Tags...),
		}
		nodes = append(nodes, node)
		if _, exists := nodeByID[node.id]; !exists {
			nodeByID[node.id] = node
		}
	}

	routeChoices := make(map[SpaceID]routeChoice, len(document.RouteChoices))
	for source, item := range document.RouteChoices {
		routeChoices[source] = routeChoice{
			normal:   item.Normal,
			shortcut: item.Shortcut,
		}
	}

	coordinates := make(map[SpaceID]coordinate, len(document.RenderReference.Coordinates))
	for id, item := range document.RenderReference.Coordinates {
		if len(item) != 2 {
			structuralProblems = append(
				structuralProblems,
				fmt.Sprintf("render coordinate %q has %d entries, want 2", id, len(item)),
			)
			continue
		}
		coordinates[id] = coordinate{x: item[0], y: item[1]}
	}

	if len(structuralProblems) > 0 {
		return nil, &ValidationError{Problems: structuralProblems}
	}

	finishEdge := [2]SpaceID{}
	copy(finishEdge[:], document.FinishDistance.ConceptualFinishEdge)
	graph := &Graph{
		version:                    document.Version,
		status:                     document.Status,
		startSpace:                 document.StartSpace,
		homeCheckpointSpace:        document.HomeCheckpointSpace,
		nodes:                      nodes,
		nodeByID:                   nodeByID,
		edges:                      edges,
		routeChoices:               routeChoices,
		forcedEdges:                cloneSpaceMap(document.ShortcutPolicy.Forced),
		selectableRule:             document.ShortcutPolicy.Selectable,
		fixedBukDestination:        document.Buk.FixedDestination,
		bukCandidates:              append([]SpaceID(nil), document.Buk.RandomCandidates...),
		finishEdge:                 finishEdge,
		homeCheckpointDistance:     document.FinishDistance.HomeCheckpointDistance,
		selectableDistanceRule:     document.FinishDistance.SelectableShortcut,
		forcedDistanceRule:         document.FinishDistance.ForcedShortcut,
		innerDistanceRule:          document.FinishDistance.InnerRoute,
		excludedDistanceStates:     clonePieceStates(document.FinishDistance.ExcludedStates),
		renderCoordinates:          coordinates,
		renderCoordinateEntryCount: len(document.RenderReference.Coordinates),
	}
	graph.rebuildAdjacency()
	return graph, nil
}

func cloneSpaceMap(source map[SpaceID]SpaceID) map[SpaceID]SpaceID {
	result := make(map[SpaceID]SpaceID, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

func clonePieceStates(source []string) []PieceState {
	result := make([]PieceState, len(source))
	for index, value := range source {
		result[index] = PieceState(value)
	}
	return result
}

func (g *Graph) rebuildAdjacency() {
	g.adjacency = make(map[SpaceID][]SpaceID, len(g.nodeByID))
	for id := range g.nodeByID {
		g.adjacency[id] = nil
	}
	for _, item := range g.edges {
		g.adjacency[item.from] = append(g.adjacency[item.from], item.to)
	}
}

type boardDocument struct {
	Version             int                       `yaml:"version"`
	Status              string                    `yaml:"status"`
	Reference           referenceDocument         `yaml:"reference"`
	StartSpace          SpaceID                   `yaml:"start_space"`
	HomeCheckpointSpace SpaceID                   `yaml:"home_checkpoint_space"`
	FinishRule          string                    `yaml:"finish_rule"`
	Orientation         orientationDocument       `yaml:"orientation"`
	Nodes               []nodeDocument            `yaml:"nodes"`
	ForwardEdges        [][]SpaceID               `yaml:"forward_edges"`
	RouteChoices        map[SpaceID]routeDocument `yaml:"route_choices"`
	ShortcutPolicy      shortcutPolicyDocument    `yaml:"shortcut_policy"`
	ReversePolicy       reversePolicyDocument     `yaml:"reverse_policy"`
	LogicalSpaceRules   []string                  `yaml:"logical_space_rules"`
	Buk                 bukDocument               `yaml:"buk"`
	FinishDistance      finishDistanceDocument    `yaml:"finish_distance"`
	RenderReference     renderReferenceDocument   `yaml:"render_reference"`
}

type referenceDocument struct {
	Image string `yaml:"image"`
	Note  string `yaml:"note"`
}

type orientationDocument struct {
	CanonicalForward string `yaml:"canonical_forward"`
}

type nodeDocument struct {
	ID   SpaceID `yaml:"id"`
	Name string  `yaml:"name"`
	Tags []Tag   `yaml:"tags"`
}

type routeDocument struct {
	Normal   SpaceID `yaml:"normal"`
	Shortcut SpaceID `yaml:"shortcut"`
}

type shortcutPolicyDocument struct {
	Selectable string              `yaml:"selectable"`
	Forced     map[SpaceID]SpaceID `yaml:"forced"`
}

type reversePolicyDocument struct {
	Primary                      string   `yaml:"primary"`
	StateModel                   string   `yaml:"state_model"`
	AfterForwardMove             string   `yaml:"after_forward_move"`
	AfterBackdo                  string   `yaml:"after_backdo"`
	AfterBuk                     string   `yaml:"after_buk"`
	OnStackHistoryConflict       string   `yaml:"on_stack_history_conflict"`
	IndependentAlliesKeepHistory bool     `yaml:"independent_allies_keep_separate_history"`
	ClearOn                      []string `yaml:"clear_on"`
	HomeCheckpointBackdo         string   `yaml:"home_checkpoint_backdo"`
	CenterRule                   string   `yaml:"center_rule"`
	HistoryRequired              bool     `yaml:"history_required"`
}

type bukDocument struct {
	FixedDestination SpaceID   `yaml:"fixed_destination"`
	RandomCandidates []SpaceID `yaml:"random_candidates"`
	Constraints      []string  `yaml:"constraints"`
}

type finishDistanceDocument struct {
	Definition             string    `yaml:"definition"`
	ConceptualFinishEdge   []SpaceID `yaml:"conceptual_finish_edge"`
	HomeCheckpointDistance int       `yaml:"home_checkpoint_distance"`
	SelectableShortcut     string    `yaml:"selectable_shortcut"`
	ForcedShortcut         string    `yaml:"forced_shortcut"`
	InnerRoute             string    `yaml:"inner_route"`
	ExcludedStates         []string  `yaml:"excluded_states"`
	SharedBy               []string  `yaml:"shared_by"`
}

type renderReferenceDocument struct {
	CoordinateSystem  string                `yaml:"coordinate_system"`
	CoordinatesStatus string                `yaml:"coordinates_status"`
	Coordinates       map[SpaceID][]float64 `yaml:"coordinates"`
}
