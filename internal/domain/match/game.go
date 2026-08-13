// Package match applies deterministic piece movement and match rules.
//
// It is a pure domain package: callers supply the immutable board planner and
// room settings, while transport, persistence, and presentation remain
// outside the package.
package match

import (
	"fmt"
	"reflect"
	"sync"

	"buk-yutnori/internal/domain"
	"buk-yutnori/internal/domain/board"
	"buk-yutnori/internal/domain/room"
)

// Game owns authoritative piece positions and the winner for one match.
type Game struct {
	mutex sync.RWMutex

	planner      board.MovementPlanner
	settings     room.Settings
	pieces       []Piece
	pieceIndex   map[domain.PieceID]int
	winnerTeamID domain.TeamID
}

// NewGame creates a game with every configured piece waiting off the board.
func NewGame(
	planner board.MovementPlanner,
	settings room.Settings,
	teams []TeamSetup,
) (*Game, error) {
	if isNilMovementPlanner(planner) {
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

func isNilMovementPlanner(planner board.MovementPlanner) bool {
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
