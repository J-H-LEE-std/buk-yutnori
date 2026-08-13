package cpu

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"

	"buk-yutnori/internal/domain"
	"buk-yutnori/internal/domain/board"
	"buk-yutnori/internal/domain/match"
	"buk-yutnori/internal/domain/room"
	"buk-yutnori/internal/domain/turn"
)

// Policy owns immutable canonical CPU settings and server-supplied randomness.
type Policy struct {
	mutex sync.Mutex

	finishPlanner board.FinishDistancePlanner
	settings      room.Settings
	randomSource  BoundedSource
}

// NewPolicy constructs a canonical automatic-decision policy.
func NewPolicy(
	planner board.FinishDistancePlanner,
	settings room.Settings,
	source BoundedSource,
) (*Policy, error) {
	if isNilInterface(planner) {
		return nil, fmt.Errorf("%w: finish-distance planner is required", ErrInvalidPolicyConfig)
	}
	if err := settings.Validate(); err != nil {
		return nil, fmt.Errorf("%w: room settings: %w", ErrInvalidPolicyConfig, err)
	}
	if isNilInterface(source) {
		return nil, fmt.Errorf("%w: random source is required", ErrInvalidPolicyConfig)
	}
	return &Policy{
		finishPlanner: planner,
		settings:      settings,
		randomSource:  source,
	}, nil
}

// NewSeededPolicy constructs a deterministic policy for tests.
func NewSeededPolicy(
	planner board.FinishDistancePlanner,
	settings room.Settings,
	seed1, seed2 uint64,
) (*Policy, error) {
	return NewPolicy(planner, settings, rand.New(rand.NewPCG(seed1, seed2)))
}

func isNilInterface(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}

// Decide returns the next canonical CPU action without changing game or turn state.
//
// ResultQueue is always read in FIFO order, regardless of the room movement-order
// setting. The returned decision remains uncommitted and must be revalidated by
// the authoritative engines when it is applied.
func (policy *Policy) Decide(
	game MatchReader,
	turnSnapshot turn.Snapshot,
	teamID domain.TeamID,
) (Decision, error) {
	if policy == nil {
		return Decision{}, fmt.Errorf("%w: policy is required", ErrInvalidDecisionInput)
	}
	if isNilInterface(game) {
		return Decision{}, fmt.Errorf("%w: match reader is required", ErrInvalidDecisionInput)
	}
	if err := teamID.Validate(); err != nil {
		return Decision{}, fmt.Errorf("%w: team_id: %w", ErrInvalidDecisionInput, err)
	}
	if len(turnSnapshot.ResultQueue) == 0 {
		return Decision{}, ErrNoResultToken
	}
	token := turnSnapshot.ResultQueue[0]
	if err := token.Validate(); err != nil {
		return Decision{}, fmt.Errorf("%w: head result token: %w", ErrInvalidDecisionInput, err)
	}
	snapshot := game.Snapshot()
	if snapshot.WinnerTeamID != "" {
		return Decision{}, match.ErrMatchEnded
	}
	base := Decision{TokenID: token.ID, Result: token.Result}
	if token.Result == domain.YutBuk {
		base.Action = ActionResolveBuk
		return base, nil
	}

	candidates, err := policy.moveCandidates(game, snapshot, teamID, token.Result)
	if err != nil {
		return Decision{}, err
	}
	if len(candidates) == 0 {
		base.Action = ActionDiscardResult
		return base, nil
	}
	best := bestCandidates(candidates)
	selected, err := policy.randomCandidate(best)
	if err != nil {
		return Decision{}, err
	}
	return Decision{
		Action:  ActionMovePiece,
		TokenID: token.ID,
		Result:  token.Result,
		PieceID: selected.piece.ID,
		Route:   selected.applyRoute,
	}, nil
}

type moveCandidate struct {
	piece       match.Piece
	movedIDs    []domain.PieceID
	applyRoute  domain.Route
	planRoute   domain.Route
	destination domain.PieceState
	space       domain.SpaceID

	immediateFinish bool
	captures        bool
	stacks          bool
	entersShortcut  bool
	hasDistance     bool
	distance        int
}

func (policy *Policy) moveCandidates(
	game MatchReader,
	snapshot match.Snapshot,
	teamID domain.TeamID,
	result domain.YutResult,
) ([]moveCandidate, error) {
	if result == domain.YutBuk {
		return nil, fmt.Errorf("%w: Buk is not a piece-move candidate", ErrInvalidDecisionInput)
	}
	if err := result.Validate(); err != nil {
		return nil, fmt.Errorf("%w: result: %w", ErrInvalidDecisionInput, err)
	}

	seenGroups := make(map[string]bool, policy.settings.PieceCount)
	candidates := make([]moveCandidate, 0, policy.settings.PieceCount)
	for _, piece := range snapshot.Pieces {
		if piece.TeamID != teamID || piece.State == domain.PieceFinished {
			continue
		}
		candidate, available, err := policy.candidateForPiece(game, snapshot, teamID, piece, result)
		if err != nil {
			return nil, err
		}
		if !available {
			continue
		}
		groupKey, err := movedGroupKey(candidate.movedIDs)
		if err != nil {
			return nil, err
		}
		if seenGroups[groupKey] {
			continue
		}
		seenGroups[groupKey] = true
		candidates = append(candidates, candidate)
	}
	return candidates, nil
}

func (policy *Policy) candidateForPiece(
	game MatchReader,
	snapshot match.Snapshot,
	teamID domain.TeamID,
	piece match.Piece,
	result domain.YutResult,
) (moveCandidate, bool, error) {
	var candidate moveCandidate
	candidate.piece = piece

	switch result {
	case domain.YutDo, domain.YutGae, domain.YutGeol, domain.YutYut, domain.YutMo:
		plans, err := game.OrdinaryMovePlans(teamID, piece.ID, result)
		if err != nil {
			if errors.Is(err, board.ErrForwardMovementUnavailable) {
				return moveCandidate{}, false, nil
			}
			return moveCandidate{}, false, err
		}
		plan, applyRoute, err := automaticForwardPlan(plans)
		if err != nil {
			return moveCandidate{}, false, err
		}
		candidate.movedIDs = append([]domain.PieceID(nil), plan.MovedPieceIDs...)
		candidate.applyRoute = applyRoute
		candidate.planRoute = plan.Route
		candidate.destination = plan.DestinationState
		candidate.space = plan.DestinationSpaceID
	case domain.YutBackdo:
		plan, err := game.BackdoMovePlan(teamID, piece.ID)
		if err != nil {
			if errors.Is(err, board.ErrBackdoMovementUnavailable) ||
				errors.Is(err, board.ErrBackdoHistoryUnavailable) {
				return moveCandidate{}, false, nil
			}
			return moveCandidate{}, false, err
		}
		candidate.movedIDs = append([]domain.PieceID(nil), plan.MovedPieceIDs...)
		candidate.destination = plan.DestinationState
		candidate.space = plan.DestinationSpaceID
	default:
		return moveCandidate{}, false, fmt.Errorf(
			"%w: unsupported result %q",
			ErrInvalidDecisionInput,
			result,
		)
	}

	if len(candidate.movedIDs) == 0 {
		return moveCandidate{}, false, fmt.Errorf(
			"%w: piece %q plan has no moved pieces",
			ErrInvalidMovePlans,
			piece.ID,
		)
	}
	selectedIncluded := false
	for _, movedPieceID := range candidate.movedIDs {
		if movedPieceID == piece.ID {
			selectedIncluded = true
			break
		}
	}
	if !selectedIncluded {
		return moveCandidate{}, false, fmt.Errorf(
			"%w: piece %q is absent from its moved group",
			ErrInvalidMovePlans,
			piece.ID,
		)
	}
	candidate.immediateFinish = candidate.destination == domain.PieceFinished
	candidate.entersShortcut = candidate.planRoute == domain.RouteShortcut
	candidate.captures, candidate.stacks = policy.destinationEffects(snapshot, teamID, candidate)
	if piece.State != domain.PieceWaiting {
		distance, err := policy.finishPlanner.RemainingForwardDistance(
			board.Position{State: piece.State, Space: piece.CurrentSpaceID},
			boardShortcutPolicy(policy.settings.ShortcutPolicy),
		)
		if err != nil {
			return moveCandidate{}, false, err
		}
		if distance <= 0 {
			return moveCandidate{}, false, fmt.Errorf(
				"%w: non-positive finish distance %d for piece %q",
				ErrInvalidMovePlans,
				distance,
				piece.ID,
			)
		}
		candidate.hasDistance = true
		candidate.distance = distance
	}
	return candidate, true, nil
}

func automaticForwardPlan(
	plans []match.OrdinaryMovePlan,
) (match.OrdinaryMovePlan, domain.Route, error) {
	switch len(plans) {
	case 1:
		return plans[0], "", nil
	case 2:
		for _, plan := range plans {
			if plan.Route == domain.RouteShortcut {
				return plan, domain.RouteShortcut, nil
			}
		}
		return match.OrdinaryMovePlan{}, "", fmt.Errorf(
			"%w: selectable plans have no shortcut",
			ErrInvalidMovePlans,
		)
	default:
		return match.OrdinaryMovePlan{}, "", fmt.Errorf(
			"%w: got %d forward plans, want 1 or 2",
			ErrInvalidMovePlans,
			len(plans),
		)
	}
}

func (policy *Policy) destinationEffects(
	snapshot match.Snapshot,
	teamID domain.TeamID,
	candidate moveCandidate,
) (captures, stacks bool) {
	if candidate.destination == domain.PieceFinished {
		return false, false
	}
	moving := make(map[domain.PieceID]bool, len(candidate.movedIDs))
	for _, pieceID := range candidate.movedIDs {
		moving[pieceID] = true
	}
	for _, piece := range snapshot.Pieces {
		if piece.State != candidate.destination || piece.CurrentSpaceID != candidate.space {
			continue
		}
		if piece.TeamID != teamID {
			captures = true
			continue
		}
		if policy.settings.StackingEnabled && !moving[piece.ID] {
			stacks = true
		}
	}
	return captures, stacks
}

func movedGroupKey(pieceIDs []domain.PieceID) (string, error) {
	canonical := append([]domain.PieceID(nil), pieceIDs...)
	sort.Slice(canonical, func(left, right int) bool {
		return canonical[left] < canonical[right]
	})
	var builder strings.Builder
	for index, pieceID := range canonical {
		if err := pieceID.Validate(); err != nil {
			return "", fmt.Errorf(
				"%w: moved piece %d: %w",
				ErrInvalidMovePlans,
				index,
				err,
			)
		}
		value := string(pieceID)
		builder.WriteString(strconv.Itoa(len(value)))
		builder.WriteByte(':')
		builder.WriteString(value)
	}
	return builder.String(), nil
}

func boardShortcutPolicy(policy room.ShortcutPolicy) board.ShortcutPolicy {
	if policy == room.ShortcutForced {
		return board.ForcedShortcuts
	}
	return board.SelectableShortcuts
}

func bestCandidates(candidates []moveCandidate) []moveCandidate {
	best := []moveCandidate{candidates[0]}
	for _, candidate := range candidates[1:] {
		comparison := compareCandidates(candidate, best[0])
		switch {
		case comparison > 0:
			best = []moveCandidate{candidate}
		case comparison == 0:
			best = append(best, candidate)
		}
	}
	return best
}

func compareCandidates(left, right moveCandidate) int {
	for _, comparison := range []int{
		compareBool(left.immediateFinish, right.immediateFinish),
		compareBool(left.captures, right.captures),
		compareBool(left.stacks, right.stacks),
		compareBool(left.entersShortcut, right.entersShortcut),
		compareBool(left.hasDistance, right.hasDistance),
	} {
		if comparison != 0 {
			return comparison
		}
	}
	if left.hasDistance && left.distance != right.distance {
		if left.distance < right.distance {
			return 1
		}
		return -1
	}
	return 0
}

func compareBool(left, right bool) int {
	switch {
	case left == right:
		return 0
	case left:
		return 1
	default:
		return -1
	}
}

func (policy *Policy) randomCandidate(candidates []moveCandidate) (moveCandidate, error) {
	if len(candidates) == 1 {
		return candidates[0], nil
	}
	limit := uint64(len(candidates))
	policy.mutex.Lock()
	ticket := policy.randomSource.Uint64N(limit)
	policy.mutex.Unlock()
	if ticket >= limit {
		return moveCandidate{}, fmt.Errorf(
			"%w: got %d for limit %d",
			ErrRandomSourceOutOfRange,
			ticket,
			limit,
		)
	}
	return candidates[ticket], nil
}
