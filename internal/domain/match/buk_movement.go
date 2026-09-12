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
	waitingFallback := false
	if len(groups) == 0 {
		// A team with no unfinished piece on the board still consumes Buk by
		// sending one waiting piece to the announced destination. Finished
		// pieces are never eligible; a match with only finished pieces has
		// already ended before this point.
		waiting := make([]int, 0, game.settings.PieceCount)
		for index, piece := range game.pieces {
			if piece.TeamID == teamID && piece.State == domain.PieceWaiting {
				waiting = append(waiting, index)
			}
		}
		if len(waiting) == 0 {
			return BukOutcome{
				NoCandidate:        true,
				DestinationSpaceID: game.bukDestinationSpaceID,
			}, nil
		}
		ticket, err := randomTicket(game.randomSource, uint64(len(waiting)))
		if err != nil {
			return BukOutcome{}, err
		}
		groups = []bukPositionGroup{{
			position: board.Position{State: board.PieceWaiting},
			indices:  []int{waiting[ticket]},
		}}
		waitingFallback = true
	}

	minimumDistance := math.MaxInt
	if waitingFallback {
		minimumDistance = 0
	}
	for index := range groups {
		if waitingFallback {
			groups[index].distance = 0
			continue
		}
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
	outcome := BukOutcome{
		DestinationSpaceID: game.bukDestinationSpaceID,
	}
	for _, selected := range closest {
		selectedPieceIDs := game.pieceIDsLocked(selected.indices)
		outcome.SelectedPieceIDs = append(outcome.SelectedPieceIDs, selectedPieceIDs...)
		if selected.position.State == board.PieceOnBoard && selected.position.Space == game.bukDestinationSpaceID {
			continue
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
		outcome.Moves = append(outcome.Moves, move)
	}
	if len(outcome.Moves) > 0 {
		outcome.Moved = true
		outcome.Move = outcome.Moves[0]
	}
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
