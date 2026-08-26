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
		TokenIDs:      []domain.ResultTokenID{"token-1"},
		PieceIDs:      []domain.PieceID{"A-1"},
		Routes:        routes,
	})
	if err != nil {
		t.Fatalf("NewMoveRequiredEvent() error = %v", err)
	}
	routes[0] = domain.RouteShortcut
	if !reflect.DeepEqual(event.Payload.Routes, []domain.Route{domain.RouteNormal, domain.RouteShortcut}) {
		t.Fatalf("event routes = %v", event.Payload.Routes)
	}
}

func TestMoveRequiredRejectsContradictoryRouteOptions(t *testing.T) {
	t.Parallel()

	tests := []MoveRequiredPayload{
		{RequiredInput: domain.InputSelectRoute, TokenIDs: []domain.ResultTokenID{"token-1"}, PieceIDs: []domain.PieceID{"A-1"}},
		{RequiredInput: domain.InputSelectRoute, TokenIDs: []domain.ResultTokenID{"token-1"}, PieceIDs: []domain.PieceID{"A-1"}, Routes: []domain.Route{domain.RouteNormal}},
		{RequiredInput: domain.InputSelectRoute, TokenIDs: []domain.ResultTokenID{"token-1"}, PieceIDs: []domain.PieceID{"A-1"}, Routes: []domain.Route{domain.RouteNormal, domain.RouteNormal}},
		{RequiredInput: domain.InputSelectRoute, TokenIDs: []domain.ResultTokenID{"token-1", "token-2"}, PieceIDs: []domain.PieceID{"A-1"}, Routes: []domain.Route{domain.RouteNormal, domain.RouteShortcut}},
		{RequiredInput: domain.InputSelectPiece, TokenIDs: []domain.ResultTokenID{"token-1"}, PieceIDs: []domain.PieceID{"A-1"}, Routes: []domain.Route{domain.RouteNormal}},
	}
	for index, payload := range tests {
		if _, err := NewMoveRequiredEvent("room-1", "match-1", uint64(index+1), payload); err == nil {
			t.Fatalf("case %d accepted contradictory payload: %+v", index, payload)
		}
	}
}
