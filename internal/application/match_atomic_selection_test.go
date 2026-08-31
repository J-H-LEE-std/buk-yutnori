package application

import (
	"testing"

	"buk-yutnori/internal/auth"
	"buk-yutnori/internal/domain"
	"buk-yutnori/internal/domain/room"
)

func TestSelectMovePersistsConsecutiveAtomicSelectionEvents(t *testing.T) {
	t.Parallel()
	fixture := newMatchFixture(t, func(settings *room.Settings) {
		settings.MovementOrder = room.MovementFree
	})
	defer fixture.recorder.close()
	player := fixture.runtime().currentPlayer()
	fixture.scriptThrowsFor(map[domain.PlayerID][]domain.YutResult{player: {domain.YutDo}})
	fixture.throwUntilResolved(t, player)

	runtime := fixture.runtime()
	candidates, _, err := runtime.moveCandidates(runtime.machine.Snapshot())
	if err != nil || len(candidates) == 0 {
		t.Fatalf("moveCandidates() = %v, %v", candidates, err)
	}
	selected := candidates[0]
	if err := fixture.registry.SelectMove(auth.UserID(player), fixture.roomID, fixture.matchID, selected.TokenID, selected.PieceID); err != nil {
		t.Fatalf("SELECT_MOVE error = %v", err)
	}

	events := fixture.recorder.snapshotEvents()
	for index := range events {
		if events[index].Type != "RESULT_SELECTED" {
			continue
		}
		if index+1 >= len(events) || events[index+1].Type != "PIECE_SELECTED" ||
			events[index+1].Sequence != events[index].Sequence+1 ||
			events[index].Payload.TokenID != selected.TokenID ||
			events[index+1].Payload.TokenID != selected.TokenID ||
			events[index+1].Payload.PieceID != selected.PieceID {
			t.Fatalf("atomic selection event pair = %#v %#v", events[index], events[index+1])
		}
		return
	}
	t.Fatal("RESULT_SELECTED was not emitted")
}
