package application

import (
	"reflect"
	"testing"

	"buk-yutnori/internal/domain"
)

func TestSnapshotMovePreviewsMatchServerPlansWithoutMutation(t *testing.T) {
	fixture := newMatchFixture(t, nil)
	defer fixture.recorder.close()
	fixture.throwUntilResolved(t, fixture.runtime().currentPlayer())
	rt := fixture.runtime()
	state := rt.machine.Snapshot()
	request, err := rt.snapshotMoveRequest(state)
	if err != nil {
		t.Fatal(err)
	}
	if request == nil || len(request.Candidates) == 0 {
		t.Fatal("seeded throw must expose candidates")
	}
	before := rt.game.Snapshot()
	for _, candidate := range request.Candidates {
		if len(candidate.Previews) == 0 {
			t.Fatal("missing server preview")
		}
		for _, token := range state.ResultQueue {
			if token.ID != candidate.TokenID {
				continue
			}
			if token.Result == domain.YutBackdo {
				plan, err := rt.game.BackdoMovePlan(rt.currentTeam(), candidate.PieceID)
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(candidate.Previews[0].Traversed, plan.Traversed) {
					t.Fatal("backdo preview differs")
				}
			} else {
				plans, err := rt.game.OrdinaryMovePlans(rt.currentTeam(), candidate.PieceID, token.Result)
				if err != nil {
					t.Fatal(err)
				}
				if len(plans) != len(candidate.Previews) {
					t.Fatal("missing alternative route")
				}
				for index, plan := range plans {
					preview := candidate.Previews[index]
					if !reflect.DeepEqual(preview.Traversed, plan.Traversed) || preview.DestinationState != plan.DestinationState || preview.Route == nil || *preview.Route != plan.Route {
						t.Fatal("preview differs from server plan")
					}
					if plan.DestinationSpaceID == "" {
						if preview.DestinationSpaceID != nil {
							t.Fatal("finished preview must not invent a destination")
						}
					} else if preview.DestinationSpaceID == nil || *preview.DestinationSpaceID != plan.DestinationSpaceID {
						t.Fatal("preview destination differs from server plan")
					}
				}
			}
		}
	}
	if !reflect.DeepEqual(before, rt.game.Snapshot()) {
		t.Fatal("preview mutated game")
	}
}
