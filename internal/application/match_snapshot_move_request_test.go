package application

import (
	"testing"

	"buk-yutnori/internal/auth"
	"buk-yutnori/internal/domain"
	"buk-yutnori/internal/domain/room"
)

func TestGameSnapshotCarriesAuthoritativeRouteRequest(t *testing.T) {
	t.Parallel()

	fixture := newMatchFixture(t, func(settings *room.Settings) {
		settings.MovementOrder = room.MovementFIFO
		settings.ShortcutPolicy = room.ShortcutSelectable
	})
	defer fixture.recorder.close()
	player := fixture.runtime().currentPlayer()
	fixture.scriptThrowsFor(map[domain.PlayerID][]domain.YutResult{
		player: {domain.YutMo, domain.YutDo},
	})
	fixture.throwUntilResolved(t, player)

	rt := fixture.runtime()
	first := rt.machine.Snapshot()
	movable, err := rt.movablePieceIDs(first)
	if err != nil || len(movable) == 0 {
		t.Fatalf("first movable pieces = %v, error = %v", movable, err)
	}
	selectedPiece := movable[0]
	if err := fixture.registry.SelectPiece(auth.UserID(player), fixture.roomID, fixture.matchID, first.SelectedTokenID, selectedPiece); err != nil {
		t.Fatalf("first SELECT_PIECE error = %v", err)
	}

	rt = fixture.runtime()
	second := rt.machine.Snapshot()
	if second.RequiredInput != domain.InputSelectPiece {
		t.Fatalf("second required input = %q, want select_piece", second.RequiredInput)
	}
	if err := fixture.registry.SelectPiece(auth.UserID(player), fixture.roomID, fixture.matchID, second.SelectedTokenID, selectedPiece); err != nil {
		t.Fatalf("route-opening SELECT_PIECE error = %v", err)
	}

	entry := fixture.registry.rooms[fixture.roomID]
	snapshot, err := fixture.registry.assembleGameSnapshotLocked(
		entry, boundaryOf(t, fixture.registry, fixture.roomID),
	)
	if err != nil {
		t.Fatalf("assembleGameSnapshotLocked() error = %v", err)
	}
	request := snapshot.CurrentTurn.MoveRequest
	if snapshot.CurrentTurn.RequiredInput != string(domain.InputSelectRoute) || request == nil {
		t.Fatalf("current turn = %+v, want route request", snapshot.CurrentTurn)
	}
	if request.RequiredInput != domain.InputSelectRoute ||
		len(request.TokenIDs) != 1 || request.TokenIDs[0] != second.SelectedTokenID ||
		len(request.PieceIDs) != 1 || request.PieceIDs[0] != selectedPiece ||
		len(request.Routes) != 2 || request.Routes[0] != domain.RouteNormal ||
		request.Routes[1] != domain.RouteShortcut {
		t.Fatalf("move request = %+v", request)
	}
	moves := fixture.recorder.ofTypes("MOVE_REQUIRED")
	if len(moves) == 0 {
		t.Fatal("route MOVE_REQUIRED was not broadcast")
	}
	latest := moves[len(moves)-1].Payload
	if latest.RequiredInput != string(domain.InputSelectRoute) ||
		len(latest.Routes) != 2 || latest.Routes[0] != domain.RouteNormal ||
		latest.Routes[1] != domain.RouteShortcut {
		t.Fatalf("route MOVE_REQUIRED = %+v", latest)
	}
}
