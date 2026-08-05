// Package match applies deterministic piece movement and match rules.
//
// It is a pure domain package: callers supply the immutable board planner and
// room settings, while transport, persistence, and presentation remain
// outside the package.
package match

import (
	"errors"
	"fmt"
	"reflect"
	"sync"

	"buk-yutnori/internal/domain"
	"buk-yutnori/internal/domain/board"
	"buk-yutnori/internal/domain/room"
	"buk-yutnori/internal/domain/turn"
)

var (
	// ErrInvalidGameConfig identifies an invalid immutable game setup.
	ErrInvalidGameConfig = errors.New("invalid game configuration")

	// ErrUnknownPiece identifies a piece ID that does not belong to this game.
	ErrUnknownPiece = errors.New("unknown piece")

	// ErrPieceNotOwned identifies a piece selected by the opposing team.
	ErrPieceNotOwned = errors.New("piece is not owned by team")

	// ErrRouteSelectionRequired identifies a move with an unresolved route choice.
	ErrRouteSelectionRequired = errors.New("route selection is required")

	// ErrRouteSelectionNotAllowed identifies a route supplied for a deterministic move.
	ErrRouteSelectionNotAllowed = errors.New("route selection is not allowed")

	// ErrInvalidRouteSelection identifies a route that is not one of the current plans.
	ErrInvalidRouteSelection = errors.New("invalid route selection")

	// ErrMatchEnded identifies an attempted action after victory was decided.
	ErrMatchEnded = errors.New("match has ended")

	// ErrInvalidForwardPlan identifies a board planner result that cannot be applied.
	ErrInvalidForwardPlan = errors.New("invalid forward plan")
)

// TeamSetup declares the stable piece IDs owned by one canonical team.
type TeamSetup struct {
	TeamID   domain.TeamID
	PieceIDs []domain.PieceID
}

// Piece is the authoritative dynamic state of one piece.
type Piece struct {
	ID                  domain.PieceID
	TeamID              domain.TeamID
	State               domain.PieceState
	CurrentSpaceID      domain.SpaceID
	ActualPreviousSpace domain.SpaceID
}

// Snapshot is an atomic copy of all match state exposed by Game.
type Snapshot struct {
	Pieces       []Piece
	WinnerTeamID domain.TeamID
}

// OrdinaryMovePlan is one currently legal ordinary forward movement.
type OrdinaryMovePlan struct {
	Route               domain.Route
	DestinationState    domain.PieceState
	DestinationSpaceID  domain.SpaceID
	ActualPreviousSpace domain.SpaceID
	Traversed           []domain.SpaceID
	MovedPieceIDs       []domain.PieceID
}

// MoveOutcome describes an ordinary movement already committed to the game.
type MoveOutcome struct {
	Route             domain.Route
	MovementKind      domain.MovementKind
	FromSpaceID       domain.SpaceID
	ToSpaceID         domain.SpaceID
	MovedPieceIDs     []domain.PieceID
	StackedPieceIDs   []domain.PieceID
	CapturedPieceIDs  []domain.PieceID
	CaptureExtraThrow bool
	MatchEnded        bool
	WinnerTeamID      domain.TeamID
}

// TurnOutcome returns the decisions needed by the turn state machine.
func (outcome MoveOutcome) TurnOutcome() turn.MoveOutcome {
	return turn.MoveOutcome{
		CaptureExtraThrow: outcome.CaptureExtraThrow,
		MatchEnded:        outcome.MatchEnded,
	}
}

// Game owns authoritative piece positions and the winner for one match.
type Game struct {
	mutex sync.RWMutex

	planner      board.ForwardPlanner
	settings     room.Settings
	pieces       []Piece
	pieceIndex   map[domain.PieceID]int
	winnerTeamID domain.TeamID
}

// NewGame creates a game with every configured piece waiting off the board.
func NewGame(
	planner board.ForwardPlanner,
	settings room.Settings,
	teams []TeamSetup,
) (*Game, error) {
	if isNilForwardPlanner(planner) {
		return nil, fmt.Errorf("%w: board planner is required", ErrInvalidGameConfig)
	}
	if err := settings.Validate(); err != nil {
		return nil, fmt.Errorf("%w: room settings: %w", ErrInvalidGameConfig, err)
	}
	if len(teams) != 2 {
		return nil, fmt.Errorf("%w: got %d teams, want 2", ErrInvalidGameConfig, len(teams))
	}

	pieceIndex := make(map[domain.PieceID]int, settings.PieceCount*2)
	pieces := make([]Piece, 0, settings.PieceCount*2)
	seenTeams := make(map[domain.TeamID]bool, 2)
	for _, setup := range teams {
		if err := setup.TeamID.Validate(); err != nil {
			return nil, fmt.Errorf("%w: team_id: %w", ErrInvalidGameConfig, err)
		}
		if seenTeams[setup.TeamID] {
			return nil, fmt.Errorf("%w: duplicate team %q", ErrInvalidGameConfig, setup.TeamID)
		}
		seenTeams[setup.TeamID] = true
		if len(setup.PieceIDs) != settings.PieceCount {
			return nil, fmt.Errorf(
				"%w: team %q has %d pieces, want %d",
				ErrInvalidGameConfig,
				setup.TeamID,
				len(setup.PieceIDs),
				settings.PieceCount,
			)
		}
		for _, pieceID := range setup.PieceIDs {
			if err := pieceID.Validate(); err != nil {
				return nil, fmt.Errorf("%w: piece_id: %w", ErrInvalidGameConfig, err)
			}
			if _, exists := pieceIndex[pieceID]; exists {
				return nil, fmt.Errorf("%w: duplicate piece %q", ErrInvalidGameConfig, pieceID)
			}
			pieceIndex[pieceID] = len(pieces)
			pieces = append(pieces, Piece{
				ID:     pieceID,
				TeamID: setup.TeamID,
				State:  domain.PieceWaiting,
			})
		}
	}
	if !seenTeams[domain.TeamA] || !seenTeams[domain.TeamB] {
		return nil, fmt.Errorf("%w: teams A and B are required", ErrInvalidGameConfig)
	}

	return &Game{
		planner:    planner,
		settings:   settings,
		pieces:     pieces,
		pieceIndex: pieceIndex,
	}, nil
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

// Snapshot returns a copy that callers may mutate without changing the game.
func (game *Game) Snapshot() Snapshot {
	game.mutex.RLock()
	defer game.mutex.RUnlock()
	return Snapshot{
		Pieces:       append([]Piece(nil), game.pieces...),
		WinnerTeamID: game.winnerTeamID,
	}
}

// OrdinaryMovePlans returns the currently legal forward plans without mutation.
func (game *Game) OrdinaryMovePlans(
	teamID domain.TeamID,
	pieceID domain.PieceID,
	result domain.YutResult,
) ([]OrdinaryMovePlan, error) {
	game.mutex.RLock()
	defer game.mutex.RUnlock()

	plans, _, err := game.ordinaryMovePlansLocked(teamID, pieceID, result)
	if err != nil {
		return nil, err
	}
	return cloneOrdinaryMovePlans(plans), nil
}

func (game *Game) ordinaryMovePlansLocked(
	teamID domain.TeamID,
	pieceID domain.PieceID,
	result domain.YutResult,
) ([]OrdinaryMovePlan, []int, error) {
	if game.winnerTeamID != "" {
		return nil, nil, ErrMatchEnded
	}
	selectedIndex, err := game.ownedPieceIndexLocked(teamID, pieceID)
	if err != nil {
		return nil, nil, err
	}
	spaces, err := result.OrdinaryMovementSpaces()
	if err != nil {
		return nil, nil, err
	}

	movingIndices := game.movingPieceIndicesLocked(selectedIndex)
	movedPieceIDs := game.pieceIDsLocked(movingIndices)
	selected := game.pieces[selectedIndex]
	plannerPlans, err := game.planner.ForwardPlans(
		board.Position{State: selected.State, Space: selected.CurrentSpaceID},
		spaces,
		boardShortcutPolicy(game.settings.ShortcutPolicy),
	)
	if err != nil {
		return nil, nil, err
	}
	if len(plannerPlans) == 0 {
		return nil, nil, fmt.Errorf("%w: planner returned no plans", ErrInvalidForwardPlan)
	}

	plans := make([]OrdinaryMovePlan, len(plannerPlans))
	for index, plannerPlan := range plannerPlans {
		if err := validateForwardPlan(plannerPlan); err != nil {
			return nil, nil, err
		}
		plans[index] = OrdinaryMovePlan{
			Route:               plannerPlan.Route,
			DestinationState:    plannerPlan.Destination.State,
			DestinationSpaceID:  plannerPlan.Destination.Space,
			ActualPreviousSpace: plannerPlan.ActualPreviousSpace,
			Traversed:           append([]domain.SpaceID(nil), plannerPlan.Traversed...),
			MovedPieceIDs:       append([]domain.PieceID(nil), movedPieceIDs...),
		}
	}
	return plans, movingIndices, nil
}

func boardShortcutPolicy(policy room.ShortcutPolicy) board.ShortcutPolicy {
	if policy == room.ShortcutForced {
		return board.ForcedShortcuts
	}
	return board.SelectableShortcuts
}

func validateForwardPlan(plan board.ForwardPlan) error {
	if err := plan.Route.Validate(); err != nil {
		return fmt.Errorf("%w: route: %w", ErrInvalidForwardPlan, err)
	}
	switch plan.Destination.State {
	case domain.PieceOnBoard, domain.PieceHomeCheckpoint:
		if plan.Destination.Space == "" || plan.ActualPreviousSpace == "" {
			return fmt.Errorf(
				"%w: destination %q requires current and previous spaces",
				ErrInvalidForwardPlan,
				plan.Destination.State,
			)
		}
	case domain.PieceFinished:
		if plan.Destination.Space != "" || plan.ActualPreviousSpace != "" {
			return fmt.Errorf("%w: finished destination retains path state", ErrInvalidForwardPlan)
		}
	default:
		return fmt.Errorf(
			"%w: destination state %q",
			ErrInvalidForwardPlan,
			plan.Destination.State,
		)
	}
	return nil
}

func cloneOrdinaryMovePlans(plans []OrdinaryMovePlan) []OrdinaryMovePlan {
	cloned := make([]OrdinaryMovePlan, len(plans))
	for index, plan := range plans {
		cloned[index] = plan
		cloned[index].Traversed = append([]domain.SpaceID(nil), plan.Traversed...)
		cloned[index].MovedPieceIDs = append([]domain.PieceID(nil), plan.MovedPieceIDs...)
	}
	return cloned
}

func (game *Game) ownedPieceIndexLocked(
	teamID domain.TeamID,
	pieceID domain.PieceID,
) (int, error) {
	if err := teamID.Validate(); err != nil {
		return 0, err
	}
	index, ok := game.pieceIndex[pieceID]
	if !ok {
		return 0, fmt.Errorf("%w: %q", ErrUnknownPiece, pieceID)
	}
	if game.pieces[index].TeamID != teamID {
		return 0, fmt.Errorf("%w: piece %q", ErrPieceNotOwned, pieceID)
	}
	return index, nil
}

func (game *Game) movingPieceIndicesLocked(selectedIndex int) []int {
	selected := game.pieces[selectedIndex]
	if !game.settings.StackingEnabled || selected.State == domain.PieceWaiting {
		return []int{selectedIndex}
	}

	indices := make([]int, 0, game.settings.PieceCount)
	for index, piece := range game.pieces {
		if piece.TeamID == selected.TeamID && piece.State == selected.State &&
			piece.CurrentSpaceID == selected.CurrentSpaceID {
			indices = append(indices, index)
		}
	}
	return indices
}

func (game *Game) pieceIDsLocked(indices []int) []domain.PieceID {
	ids := make([]domain.PieceID, len(indices))
	for index, pieceIndex := range indices {
		ids[index] = game.pieces[pieceIndex].ID
	}
	return ids
}

// ApplyOrdinaryMove validates and atomically commits one ordinary forward move.
//
// Plans are recalculated while holding the game lock so a previously displayed
// plan cannot be applied after authoritative state changes.
func (game *Game) ApplyOrdinaryMove(
	teamID domain.TeamID,
	pieceID domain.PieceID,
	result domain.YutResult,
	selectedRoute domain.Route,
) (MoveOutcome, error) {
	game.mutex.Lock()
	defer game.mutex.Unlock()

	plans, movingIndices, err := game.ordinaryMovePlansLocked(teamID, pieceID, result)
	if err != nil {
		return MoveOutcome{}, err
	}
	plan, err := selectOrdinaryMovePlan(plans, selectedRoute)
	if err != nil {
		return MoveOutcome{}, err
	}

	selectedIndex := game.pieceIndex[pieceID]
	fromSpaceID := game.pieces[selectedIndex].CurrentSpaceID
	movedPieceIDs := game.pieceIDsLocked(movingIndices)
	destinationAllies := game.destinationAlliesLocked(teamID, plan, movingIndices)
	capturedIndices := game.capturedPieceIndicesLocked(teamID, plan)
	capturedPieceIDs := game.pieceIDsLocked(capturedIndices)

	for _, index := range capturedIndices {
		game.pieces[index].State = domain.PieceWaiting
		game.pieces[index].CurrentSpaceID = ""
		game.pieces[index].ActualPreviousSpace = ""
	}
	finalGroup := append(append([]int(nil), movingIndices...), destinationAllies...)
	for _, index := range finalGroup {
		game.pieces[index].State = plan.DestinationState
		game.pieces[index].CurrentSpaceID = plan.DestinationSpaceID
		game.pieces[index].ActualPreviousSpace = plan.ActualPreviousSpace
		if plan.DestinationState == domain.PieceFinished {
			game.pieces[index].CurrentSpaceID = ""
			game.pieces[index].ActualPreviousSpace = ""
		}
	}

	matchEnded := game.allTeamPiecesFinishedLocked(teamID)
	if matchEnded {
		game.winnerTeamID = teamID
	}
	movementKind := domain.MovementForward
	if plan.DestinationState == domain.PieceFinished {
		movementKind = domain.MovementFinish
	}

	var stackedPieceIDs []domain.PieceID
	if game.settings.StackingEnabled && plan.DestinationState != domain.PieceFinished &&
		len(finalGroup) > 1 {
		stackedPieceIDs = game.teamPieceIDsAtDestinationLocked(teamID, plan)
	}
	captureExtraThrow := len(capturedPieceIDs) > 0 &&
		game.captureGrantsExtraThrowLocked(result)
	if matchEnded {
		captureExtraThrow = false
	}

	return MoveOutcome{
		Route:             plan.Route,
		MovementKind:      movementKind,
		FromSpaceID:       fromSpaceID,
		ToSpaceID:         plan.DestinationSpaceID,
		MovedPieceIDs:     movedPieceIDs,
		StackedPieceIDs:   stackedPieceIDs,
		CapturedPieceIDs:  capturedPieceIDs,
		CaptureExtraThrow: captureExtraThrow,
		MatchEnded:        matchEnded,
		WinnerTeamID:      game.winnerTeamID,
	}, nil
}

func selectOrdinaryMovePlan(
	plans []OrdinaryMovePlan,
	selectedRoute domain.Route,
) (OrdinaryMovePlan, error) {
	if len(plans) == 1 {
		if selectedRoute != "" {
			return OrdinaryMovePlan{}, ErrRouteSelectionNotAllowed
		}
		return plans[0], nil
	}
	if selectedRoute == "" {
		return OrdinaryMovePlan{}, ErrRouteSelectionRequired
	}
	if err := selectedRoute.Validate(); err != nil {
		return OrdinaryMovePlan{}, fmt.Errorf("%w: %w", ErrInvalidRouteSelection, err)
	}
	for _, plan := range plans {
		if plan.Route == selectedRoute {
			return plan, nil
		}
	}
	return OrdinaryMovePlan{}, fmt.Errorf("%w: %q", ErrInvalidRouteSelection, selectedRoute)
}

func (game *Game) destinationAlliesLocked(
	teamID domain.TeamID,
	plan OrdinaryMovePlan,
	movingIndices []int,
) []int {
	if !game.settings.StackingEnabled || plan.DestinationState == domain.PieceFinished {
		return nil
	}
	moving := make(map[int]bool, len(movingIndices))
	for _, index := range movingIndices {
		moving[index] = true
	}
	var indices []int
	for index, piece := range game.pieces {
		if !moving[index] && piece.TeamID == teamID && piece.State == plan.DestinationState &&
			piece.CurrentSpaceID == plan.DestinationSpaceID {
			indices = append(indices, index)
		}
	}
	return indices
}

func (game *Game) capturedPieceIndicesLocked(
	teamID domain.TeamID,
	plan OrdinaryMovePlan,
) []int {
	if plan.DestinationState == domain.PieceFinished {
		return nil
	}
	var indices []int
	for index, piece := range game.pieces {
		if piece.TeamID != teamID && piece.State == plan.DestinationState &&
			piece.CurrentSpaceID == plan.DestinationSpaceID {
			indices = append(indices, index)
		}
	}
	return indices
}

func (game *Game) teamPieceIDsAtDestinationLocked(
	teamID domain.TeamID,
	plan OrdinaryMovePlan,
) []domain.PieceID {
	var ids []domain.PieceID
	for _, piece := range game.pieces {
		if piece.TeamID == teamID && piece.State == plan.DestinationState &&
			piece.CurrentSpaceID == plan.DestinationSpaceID {
			ids = append(ids, piece.ID)
		}
	}
	return ids
}

func (game *Game) allTeamPiecesFinishedLocked(teamID domain.TeamID) bool {
	for _, piece := range game.pieces {
		if piece.TeamID == teamID && piece.State != domain.PieceFinished {
			return false
		}
	}
	return true
}

func (game *Game) captureGrantsExtraThrowLocked(result domain.YutResult) bool {
	switch game.settings.CaptureExtraThrow {
	case room.CaptureExtraThrowAlways:
		return true
	case room.CaptureExtraThrowDoToGeolPlusSpecial:
		return result == domain.YutDo || result == domain.YutGae || result == domain.YutGeol
	case room.CaptureExtraThrowNone:
		return false
	default:
		return false
	}
}
