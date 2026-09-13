package protocol

import (
	"reflect"
	"testing"

	"buk-yutnori/internal/domain"
)

func TestMoveRequiredRouteOptionsAreAuthoritativeAndCloned(t *testing.T) {
	t.Parallel()

	routes := []domain.Route{domain.RouteNormal, domain.RouteShortcut}
	event, err := NewMoveRequiredEvent("room-1", "match-1", 1, MoveRequiredPayload{
		RequiredInput: domain.InputSelectRoute,
		Candidates:    []MoveCandidate{{TokenID: "token-1", PieceID: "A-1", Routes: routes}},
	})
	if err != nil {
		t.Fatalf("NewMoveRequiredEvent() error = %v", err)
	}
	routes[0] = domain.RouteShortcut
	if !reflect.DeepEqual(event.Payload.Candidates[0].Routes, []domain.Route{domain.RouteNormal, domain.RouteShortcut}) {
		t.Fatalf("event routes = %v", event.Payload.Candidates[0].Routes)
	}
}

func TestMoveRequiredRejectsContradictoryRouteOptions(t *testing.T) {
	t.Parallel()

	tests := []MoveRequiredPayload{
		{RequiredInput: domain.InputSelectRoute},
		{RequiredInput: domain.InputSelectRoute, Candidates: []MoveCandidate{{TokenID: "token-1", PieceID: "A-1", Routes: []domain.Route{domain.RouteNormal}}}},
		{RequiredInput: domain.InputSelectRoute, Candidates: []MoveCandidate{{TokenID: "token-1", PieceID: "A-1", Routes: []domain.Route{domain.RouteNormal, domain.RouteNormal}}}},
		{RequiredInput: domain.InputSelectRoute, Candidates: []MoveCandidate{{TokenID: "token-1", PieceID: "A-1", Routes: []domain.Route{domain.RouteNormal, domain.RouteShortcut}}, {TokenID: "token-2", PieceID: "A-1", Routes: []domain.Route{domain.RouteNormal, domain.RouteShortcut}}}},
		{RequiredInput: domain.InputNone, Candidates: []MoveCandidate{{TokenID: "token-1", PieceID: "A-1", Routes: []domain.Route{domain.RouteNormal, domain.RouteShortcut}}}},
	}
	for index, payload := range tests {
		if _, err := NewMoveRequiredEvent("room-1", "match-1", uint64(index+1), payload); err == nil {
			t.Fatalf("case %d accepted contradictory payload: %+v", index, payload)
		}
	}
}

func TestBukResolvedCarriesAllDistinctSourceSpaces(t *testing.T) {
	t.Parallel()
	event, err := NewBukResolvedEvent("room-1", "match-1", 1, BukResolvedPayload{
		TokenID:            "token-buk",
		DestinationSpaceID: "jji_do",
		MovedPieceIDs:      []domain.PieceID{"A-1", "A-2"},
		SourceSpaceID:      ptrSpace("mo_do"),
		SourceSpaceIDs:     []domain.SpaceID{"mo_do", "jji_mo"},
	})
	if err != nil {
		t.Fatalf("NewBukResolvedEvent() error = %v", err)
	}
	if !reflect.DeepEqual(event.Payload.SourceSpaceIDs, []domain.SpaceID{"mo_do", "jji_mo"}) {
		t.Fatalf("SourceSpaceIDs = %v", event.Payload.SourceSpaceIDs)
	}
	if _, err := NewBukResolvedEvent("room-1", "match-1", 2, BukResolvedPayload{
		TokenID:            "token-buk",
		DestinationSpaceID: "jji_do",
		MovedPieceIDs:      []domain.PieceID{"A-1"},
		SourceSpaceIDs:     []domain.SpaceID{"mo_do", "mo_do"},
	}); err == nil {
		t.Fatal("duplicate SourceSpaceIDs accepted")
	}
}

func ptrSpace(space domain.SpaceID) *domain.SpaceID { return &space }
