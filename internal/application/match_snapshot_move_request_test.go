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
	candidates, _, err := rt.moveCandidates(first)
	if err != nil || len(candidates) == 0 { t.Fatalf("first candidates = %v, error = %v", candidates, err) }
	selected := candidates[0]
	if err := fixture.registry.SelectMove(auth.UserID(player), fixture.roomID, fixture.matchID, selected.TokenID, selected.PieceID); err != nil {
		t.Fatalf("SELECT_MOVE error = %v", err)
	}

	entry := fixture.registry.rooms[fixture.roomID]
	snapshot, err := fixture.registry.assembleGameSnapshotLocked(
		entry, boundaryOf(t, fixture.registry, fixture.roomID),
	)
	if err != nil {
		t.Fatalf("assembleGameSnapshotLocked() error = %v", err)
	}
	request := snapshot.CurrentTurn.MoveRequest
	if request == nil { t.Fatalf("current turn = %+v, want move request", snapshot.CurrentTurn) }
	if snapshot.CurrentTurn.RequiredInput != string(request.RequiredInput) {
		t.Fatalf("current turn input = %q, request = %q", snapshot.CurrentTurn.RequiredInput, request.RequiredInput)
	}
	if len(request.Candidates) == 0 {
		t.Fatalf("move request = %+v", request)
	}
	moves := fixture.recorder.ofTypes("MOVE_REQUIRED")
	if len(moves) == 0 {
		t.Fatal("route MOVE_REQUIRED was not broadcast")
	}
	latest := moves[len(moves)-1].Payload
	if len(latest.Candidates) == 0 {
		t.Fatalf("route MOVE_REQUIRED = %+v", latest)
	}
}
