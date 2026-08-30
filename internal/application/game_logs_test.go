package application

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"

	"buk-yutnori/internal/domain"
	"buk-yutnori/internal/protocol"
	"buk-yutnori/internal/storage"
)

func TestRoomRegistryGameLogsRequiresCurrentMembershipAndCanonicalStore(t *testing.T) {
	t.Parallel()

	registry := newTestRegistry(t)
	store := &fakeEventStore{}
	if err := registry.AttachEventStore(store); err != nil {
		t.Fatalf("AttachEventStore() error = %v", err)
	}
	summary := createDefaultRoom(t, registry, "creator")
	logs, err := registry.GameLogs(context.Background(), "creator", summary.RoomID)
	if err != nil || len(logs) != 0 {
		t.Fatalf("member GameLogs() = %#v, %v", logs, err)
	}
	if _, err := registry.GameLogs(context.Background(), "outsider", summary.RoomID); !errors.Is(err, ErrNotMember) {
		t.Fatalf("outsider GameLogs() error = %v, want ErrNotMember", err)
	}
	if _, err := registry.GameLogs(context.Background(), "creator", "missing"); !errors.Is(err, ErrRoomNotFound) {
		t.Fatalf("missing GameLogs() error = %v, want ErrRoomNotFound", err)
	}
	store.setFailure(errors.New("disk unavailable"))
	if _, err := registry.GameLogs(context.Background(), "creator", summary.RoomID); !errors.Is(err, ErrEventStoreUnavailable) {
		t.Fatalf("failed-store GameLogs() error = %v, want ErrEventStoreUnavailable", err)
	}
}

func TestDeriveGameLogsBuildsCompletedMatchesInSequenceOrder(t *testing.T) {
	t.Parallel()

	rows := []storage.EventRow{
		gameLogRow(t, 1, protocol.EventGameStarted, protocol.GameStartedEvent{Type: protocol.EventGameStarted, Sequence: 1, RoomID: "room-1", MatchID: "match-a", Payload: protocol.GameStartedPayload{FirstPlayerID: "player-a"}}),
		gameLogRow(t, 2, protocol.EventYutResult, protocol.YutResultEvent{Type: protocol.EventYutResult, Sequence: 2, RoomID: "room-1", MatchID: "match-a", Payload: protocol.YutResultPayload{PlayerID: "player-a", Token: protocol.ResultTokenView{TokenID: "token-1", Result: domain.YutDo, Origin: domain.ResultOriginInitialThrow}}}),
		gameLogRow(t, 3, protocol.EventPieceMoved, protocol.PieceMovedEvent{Type: protocol.EventPieceMoved, Sequence: 3, RoomID: "room-1", MatchID: "match-a", Payload: protocol.PieceMovedPayload{PieceIDs: []domain.PieceID{"A-1", "A-2"}, ToSpaceID: gameLogSpace("space-1"), MovementKind: domain.MovementForward}}),
		gameLogRow(t, 4, protocol.EventGameEnded, protocol.GameEndedEvent{Type: protocol.EventGameEnded, Sequence: 4, RoomID: "room-1", MatchID: "match-a", Payload: protocol.GameEndedPayload{Status: "finished", WinnerTeamID: gameLogTeam(domain.TeamA), Reason: protocol.GameEndedReasonAllFinished}}),
		gameLogRow(t, 5, protocol.EventGameStarted, protocol.GameStartedEvent{Type: protocol.EventGameStarted, Sequence: 5, RoomID: "room-1", MatchID: "match-b", Payload: protocol.GameStartedPayload{FirstPlayerID: "player-b"}}),
		gameLogRow(t, 6, protocol.EventGameEnded, protocol.GameEndedEvent{Type: protocol.EventGameEnded, Sequence: 6, RoomID: "room-1", MatchID: "match-b", Payload: protocol.GameEndedPayload{Status: "invalid", Reason: "server_restart"}}),
		gameLogRow(t, 7, protocol.EventGameStarted, protocol.GameStartedEvent{Type: protocol.EventGameStarted, Sequence: 7, RoomID: "room-1", MatchID: "match-live", Payload: protocol.GameStartedPayload{FirstPlayerID: "player-a"}}),
	}

	logs, err := deriveGameLogs(rows)
	if err != nil {
		t.Fatalf("deriveGameLogs() error = %v", err)
	}
	if len(logs) != 2 {
		t.Fatalf("len(logs) = %d, want 2", len(logs))
	}
	first := logs[0]
	if first.MatchID != "match-a" || first.StartedSequence != 1 || first.EndedSequence != 4 || first.Status != "finished" || first.WinnerTeamID == nil || *first.WinnerTeamID != domain.TeamA || first.Reason != protocol.GameEndedReasonAllFinished {
		t.Fatalf("first log = %+v", first)
	}
	if got := first.Entries; len(got) != 3 || got[0] != (GameLogEntry{Sequence: 2, Notation: "{도}"}) || got[1] != (GameLogEntry{Sequence: 3, Notation: "{[A-1][-][space-1]}"}) || got[2] != (GameLogEntry{Sequence: 3, Notation: "{[A-2][-][space-1]}"}) {
		t.Fatalf("first entries = %+v", got)
	}
	second := logs[1]
	if second.MatchID != "match-b" || second.StartedSequence != 5 || second.EndedSequence != 6 || second.Status != "invalid" || second.WinnerTeamID != nil || second.Reason != "server_restart" || len(second.Entries) != 0 {
		t.Fatalf("second log = %+v", second)
	}
	encoded, err := json.Marshal(second)
	if err != nil || string(encoded) == "" || !bytes.Contains(encoded, []byte(`"entries":[]`)) {
		t.Fatalf("invalid log JSON = %q, %v; entries must be an array", encoded, err)
	}
}

func TestDeriveGameLogsRejectsMalformedStoredMatchEvent(t *testing.T) {
	t.Parallel()

	_, err := deriveGameLogs([]storage.EventRow{{RoomID: "room-1", Sequence: 1, EventType: protocol.EventGameStarted, PayloadJSON: []byte(`{"version":1,"direction":"server_event","type":"GAME_STARTED","sequence":1,"room_id":"room-1","match_id":"match-a","payload":{"first_player_id":""}}`)}})
	if err == nil {
		t.Fatal("deriveGameLogs() error = nil, want malformed payload rejection")
	}
}

func TestDeriveGameLogsRejectsMalformedUnrenderedMatchPayload(t *testing.T) {
	t.Parallel()

	rows := []storage.EventRow{
		gameLogRow(t, 1, protocol.EventGameStarted, protocol.GameStartedEvent{Type: protocol.EventGameStarted, Sequence: 1, RoomID: "room-1", MatchID: "match-a", Payload: protocol.GameStartedPayload{FirstPlayerID: "player-a"}}),
		{RoomID: "room-1", Sequence: 2, EventType: protocol.EventGamePaused, PayloadJSON: []byte(`{"version":1,"direction":"server_event","type":"GAME_PAUSED","sequence":2,"room_id":"room-1","match_id":"match-a","payload":{"reason":"unknown","paused_by_player_id":null,"ends_at":null,"preserved_timer_remaining_ms":0}}`)},
	}
	if _, err := deriveGameLogs(rows); err == nil {
		t.Fatal("deriveGameLogs() error = nil, want malformed GAME_PAUSED rejection")
	}
}

func gameLogRow(t *testing.T, sequence uint64, eventType string, value any) storage.EventRow {
	t.Helper()
	value = gameLogWireEvent(value)
	payload, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}
	return storage.EventRow{RoomID: "room-1", Sequence: sequence, EventType: eventType, PayloadJSON: payload}
}

func gameLogWireEvent(value any) any {
	switch event := value.(type) {
	case protocol.GameStartedEvent:
		event.Version, event.Direction = protocol.Version1, protocol.DirectionServerEvent
		return event
	case protocol.YutResultEvent:
		event.Version, event.Direction = protocol.Version1, protocol.DirectionServerEvent
		return event
	case protocol.PieceMovedEvent:
		event.Version, event.Direction = protocol.Version1, protocol.DirectionServerEvent
		return event
	case protocol.GameEndedEvent:
		event.Version, event.Direction = protocol.Version1, protocol.DirectionServerEvent
		return event
	default:
		return value
	}
}

func gameLogSpace(value domain.SpaceID) *domain.SpaceID { return &value }

func gameLogTeam(value domain.TeamID) *domain.TeamID { return &value }
