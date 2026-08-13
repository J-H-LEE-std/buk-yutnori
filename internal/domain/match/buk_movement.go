package match

import (
	"fmt"
	"math"

	"buk-yutnori/internal/domain"
	"buk-yutnori/internal/domain/board"
	"buk-yutnori/internal/domain/room"
)

func initialBukDestination(
	planner board.BukPlanner,
	settings room.Settings,
	source BoundedSource,
) (domain.SpaceID, error) {
	if !settings.BukModeEnabled {
		return "", nil
	}
	if planner == nil {
		return "", fmt.Errorf("%w: planner is required", ErrInvalidBukPlanner)
	}
	if !settings.RandomBukDestination {
		destination := planner.FixedBukDestination()
		if err := validateBukDestination(planner, destination); err != nil {
			return "", err
		}
		return destination, nil
	}

	candidates := planner.BukCandidates()
	if len(candidates) != 10 {
		return "", fmt.Errorf(
			"%w: got %d random destinations, want 10",
			ErrInvalidBukPlanner,
			len(candidates),
		)
	}
	seen := make(map[domain.SpaceID]bool, len(candidates))
	for _, candidate := range candidates {
		if seen[candidate] {
			return "", fmt.Errorf(
				"%w: duplicate random destination %q",
				ErrInvalidBukPlanner,
				candidate,
			)
		}
		seen[candidate] = true
		if err := validateBukDestination(planner, candidate); err != nil {
			return "", err
		}
	}
	if !seen[planner.FixedBukDestination()] {
		return "", fmt.Errorf(
			"%w: fixed destination %q is not a random destination",
			ErrInvalidBukPlanner,
			planner.FixedBukDestination(),
		)
	}
	ticket, err := randomTicket(source, uint64(len(candidates)))
	if err != nil {
		return "", err
	}
	return candidates[ticket], nil
}

func validateBukDestination(
	planner board.BukPlanner,
	destination domain.SpaceID,
) error {
	if err := destination.Validate(); err != nil {
		return fmt.Errorf("%w: destination: %w", ErrInvalidBukPlanner, err)
	}
	node, ok := planner.Node(destination)
	if !ok {
		return fmt.Errorf("%w: unknown destination %q", ErrInvalidBukPlanner, destination)
	}
	if node.HasTag(board.TagCenter) || node.HasTag(board.TagRouteChoice) {
		return fmt.Errorf("%w: destination %q is a branch", ErrInvalidBukPlanner, destination)
	}
	if !node.HasTag(board.TagBukCandidate) {
		return fmt.Errorf(
			"%w: random destination %q lacks Buk candidate tag",
			ErrInvalidBukPlanner,
			destination,
		)
	}
	return nil
}

func randomTicket(source BoundedSource, limit uint64) (uint64, error) {
	if isNilBoundedSource(source) {
		return 0, ErrNilRandomSource
	}
	if limit == 0 {
		return 0, fmt.Errorf("%w: zero limit", ErrRandomSourceOutOfRange)
	}
	ticket := source.Uint64N(limit)
	if ticket >= limit {
		return 0, fmt.Errorf(
			"%w: got %d for limit %d",
			ErrRandomSourceOutOfRange,
			ticket,
			limit,
		)
	}
	return ticket, nil
}

type bukPositionGroup struct {
	position board.Position
	indices  []int
	distance int
}

// ResolveBuk automatically selects and applies the canonical Buk position group.
func (game *Game) ResolveBuk(teamID domain.TeamID) (BukOutcome, error) {
	game.mutex.Lock()
	defer game.mutex.Unlock()

	if game.winnerTeamID != "" {
		return BukOutcome{}, ErrMatchEnded
	}
	if !game.settings.BukModeEnabled {
		return BukOutcome{}, ErrBukModeDisabled
	}
	if err := teamID.Validate(); err != nil {
		return BukOutcome{}, err
	}

	groups := game.bukPositionGroupsLocked(teamID)
	if len(groups) == 0 {
		return BukOutcome{
			NoCandidate:        true,
			DestinationSpaceID: game.bukDestinationSpaceID,
		}, nil
	}

	minimumDistance := math.MaxInt
	for index := range groups {
		distance, err := game.bukPlanner.RemainingForwardDistance(
			groups[index].position,
			boardShortcutPolicy(game.settings.ShortcutPolicy),
		)
		if err != nil {
			return BukOutcome{}, err
		}
		if distance <= 0 {
			return BukOutcome{}, fmt.Errorf(
				"%w: non-positive finish distance %d at %q",
				ErrInvalidBukPlanner,
				distance,
				groups[index].position.Space,
			)
		}
		groups[index].distance = distance
		if distance < minimumDistance {
			minimumDistance = distance
		}
	}

	closest := make([]bukPositionGroup, 0, len(groups))
	for _, group := range groups {
		if group.distance == minimumDistance {
			closest = append(closest, group)
		}
	}
	selected, err := game.selectBukGroupLocked(closest)
	if err != nil {
		return BukOutcome{}, err
	}
	selectedPieceIDs := game.pieceIDsLocked(selected.indices)
	outcome := BukOutcome{
		DestinationSpaceID: game.bukDestinationSpaceID,
		SelectedPieceIDs:   selectedPieceIDs,
	}
	if selected.position.Space == game.bukDestinationSpaceID {
		return outcome, nil
	}

	move := game.applyMoveResolutionLocked(
		teamID,
		selectedPieceIDs[0],
		domain.YutBuk,
		moveResolutionPlan{
			MovementKind:        domain.MovementBuk,
			DestinationState:    domain.PieceOnBoard,
			DestinationSpaceID:  game.bukDestinationSpaceID,
			ActualPreviousSpace: selected.position.Space,
			MovingIndices:       selected.indices,
		},
	)
	outcome.Moved = true
	outcome.Move = move
	return outcome, nil
}

func (game *Game) bukPositionGroupsLocked(teamID domain.TeamID) []bukPositionGroup {
	groupBySpace := make(map[domain.SpaceID]int, game.settings.PieceCount)
	groups := make([]bukPositionGroup, 0, game.settings.PieceCount)
	for index, piece := range game.pieces {
		if piece.TeamID != teamID ||
			(piece.State != domain.PieceOnBoard && piece.State != domain.PieceHomeCheckpoint) {
			continue
		}
		if groupIndex, ok := groupBySpace[piece.CurrentSpaceID]; ok {
			groups[groupIndex].indices = append(groups[groupIndex].indices, index)
			continue
		}
		groupBySpace[piece.CurrentSpaceID] = len(groups)
		groups = append(groups, bukPositionGroup{
			position: board.Position{State: piece.State, Space: piece.CurrentSpaceID},
			indices:  []int{index},
		})
	}
	return groups
}

func (game *Game) selectBukGroupLocked(
	groups []bukPositionGroup,
) (bukPositionGroup, error) {
	if len(groups) == 1 {
		return groups[0], nil
	}
	var totalWeight uint64
	for _, group := range groups {
		weight := uint64(len(group.indices))
		if math.MaxUint64-totalWeight < weight {
			return bukPositionGroup{}, fmt.Errorf(
				"%w: position group weight overflow",
				ErrInvalidBukPlanner,
			)
		}
		totalWeight += weight
	}
	ticket, err := randomTicket(game.randomSource, totalWeight)
	if err != nil {
		return bukPositionGroup{}, err
	}
	for _, group := range groups {
		weight := uint64(len(group.indices))
		if ticket < weight {
			return group, nil
		}
		ticket -= weight
	}
	return bukPositionGroup{}, fmt.Errorf(
		"%w: no group for weighted ticket",
		ErrRandomSourceOutOfRange,
	)
}
