package application

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"buk-yutnori/internal/domain"
	"buk-yutnori/internal/protocol"
	"buk-yutnori/internal/storage"
)

// GameLog is a completed match's human-readable, derived event record. It is
// deliberately a read model: room_events remains the sole canonical record
// (ADR-0014).
type GameLog struct {
	MatchID         domain.MatchID `json:"match_id"`
	StartedSequence uint64         `json:"started_sequence"`
	EndedSequence   uint64         `json:"ended_sequence"`
	Status          string         `json:"status"`
	WinnerTeamID    *domain.TeamID `json:"winner_team_id,omitempty"`
	Reason          string         `json:"reason"`
	Entries         []GameLogEntry `json:"entries"`
}

// GameLogEntry is one server-derived notation from a canonical event.
type GameLogEntry struct {
	Sequence uint64 `json:"sequence"`
	Notation string `json:"notation"`
}

type storedMatchEvent struct {
	Version   int                `json:"version"`
	Direction protocol.Direction `json:"direction"`
	Type      string             `json:"type"`
	Sequence  uint64             `json:"sequence"`
	RoomID    domain.RoomID      `json:"room_id"`
	MatchID   domain.MatchID     `json:"match_id"`
	Payload   json.RawMessage    `json:"payload"`
}

type gameLogBuild struct {
	GameLog
	ended bool
}

// deriveGameLogs turns durable v1 match events into completed game logs. A
// malformed matching event rejects the whole result: silently guessing a
// human-readable record would make a display model appear authoritative.
func deriveGameLogs(rows []storage.EventRow) ([]GameLog, error) {
	logs := make([]GameLog, 0)
	builds := make(map[domain.MatchID]*gameLogBuild)

	for _, row := range rows {
		if !isGameLogEventType(row.EventType) {
			continue
		}
		event, err := decodeStoredMatchEvent(row)
		if err != nil {
			return nil, err
		}

		switch event.Type {
		case protocol.EventGameStarted:
			if _, exists := builds[event.MatchID]; exists {
				return nil, fmt.Errorf("derive game log: duplicate GAME_STARTED for %s", event.MatchID)
			}
			if err := validateGameStartedEvent(event); err != nil {
				return nil, err
			}
			builds[event.MatchID] = &gameLogBuild{GameLog: GameLog{MatchID: event.MatchID, StartedSequence: event.Sequence, Entries: make([]GameLogEntry, 0)}}
		case protocol.EventYutResult:
			build, exists := builds[event.MatchID]
			if !exists || build.ended {
				return nil, fmt.Errorf("derive game log: YUT_RESULT outside active match %s", event.MatchID)
			}
			notation, err := yutNotation(event)
			if err != nil {
				return nil, err
			}
			build.Entries = append(build.Entries, GameLogEntry{Sequence: event.Sequence, Notation: notation})
		case protocol.EventPieceMoved:
			build, exists := builds[event.MatchID]
			if !exists || build.ended {
				return nil, fmt.Errorf("derive game log: PIECE_MOVED outside active match %s", event.MatchID)
			}
			entries, err := movementNotations(event)
			if err != nil {
				return nil, err
			}
			build.Entries = append(build.Entries, entries...)
		case protocol.EventGameEnded:
			build, exists := builds[event.MatchID]
			if !exists || build.ended {
				return nil, fmt.Errorf("derive game log: GAME_ENDED outside active match %s", event.MatchID)
			}
			status, winner, reason, err := gameEndDetails(event)
			if err != nil {
				return nil, err
			}
			build.EndedSequence = event.Sequence
			build.Status = status
			build.WinnerTeamID = winner
			build.Reason = reason
			build.ended = true
			logs = append(logs, build.GameLog)
		default:
			build, exists := builds[event.MatchID]
			if !exists || build.ended {
				return nil, fmt.Errorf("derive game log: %s outside active match %s", event.Type, event.MatchID)
			}
			if err := validateUnrenderedMatchEvent(event); err != nil {
				return nil, err
			}
		}
	}
	return logs, nil
}

func isGameLogEventType(eventType string) bool {
	switch eventType {
	case protocol.EventGameStarted, protocol.EventTurnStarted, protocol.EventYutResult,
		protocol.EventResultQueueUpdated, protocol.EventMoveRequired, protocol.EventPieceMoved,
		protocol.EventPiecesStacked, protocol.EventPiecesCaptured, protocol.EventBukResolved,
		protocol.EventCPUControlStarted, protocol.EventGamePaused,
		protocol.EventGameResumed, protocol.EventGameEnded:
		return true
	default:
		return false
	}
}

func decodeStoredMatchEvent(row storage.EventRow) (storedMatchEvent, error) {
	var event storedMatchEvent
	decoder := json.NewDecoder(bytes.NewReader(row.PayloadJSON))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&event); err != nil {
		return storedMatchEvent{}, fmt.Errorf("derive game log: decode sequence %d: %w", row.Sequence, err)
	}
	if err := requireSingleJSONValue(decoder); err != nil {
		return storedMatchEvent{}, fmt.Errorf("derive game log: decode sequence %d: %w", row.Sequence, err)
	}
	if event.Version != protocol.Version1 || event.Direction != protocol.DirectionServerEvent {
		return storedMatchEvent{}, fmt.Errorf("derive game log: invalid envelope at sequence %d", row.Sequence)
	}
	if event.Type != row.EventType || event.Sequence != row.Sequence || event.RoomID != row.RoomID {
		return storedMatchEvent{}, fmt.Errorf("derive game log: row and payload mismatch at sequence %d", row.Sequence)
	}
	if err := event.RoomID.Validate(); err != nil {
		return storedMatchEvent{}, fmt.Errorf("derive game log: room_id at sequence %d: %w", row.Sequence, err)
	}
	if err := event.MatchID.Validate(); err != nil {
		return storedMatchEvent{}, fmt.Errorf("derive game log: match_id at sequence %d: %w", row.Sequence, err)
	}
	if len(event.Payload) == 0 {
		return storedMatchEvent{}, fmt.Errorf("derive game log: payload missing at sequence %d", row.Sequence)
	}
	return event, nil
}

func requireSingleJSONValue(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}

func validateGameStartedEvent(event storedMatchEvent) error {
	var payload protocol.GameStartedPayload
	if err := decodePayload(event, &payload); err != nil {
		return err
	}
	_, err := protocol.NewGameStartedEvent(event.RoomID, event.MatchID, event.Sequence, payload)
	if err != nil {
		return fmt.Errorf("derive game log: GAME_STARTED at sequence %d: %w", event.Sequence, err)
	}
	return nil
}

func yutNotation(event storedMatchEvent) (string, error) {
	var payload protocol.YutResultPayload
	if err := decodePayload(event, &payload); err != nil {
		return "", err
	}
	if _, err := protocol.NewYutResultEvent(event.RoomID, event.MatchID, event.Sequence, payload); err != nil {
		return "", fmt.Errorf("derive game log: YUT_RESULT at sequence %d: %w", event.Sequence, err)
	}
	name, exists := map[domain.YutResult]string{
		domain.YutDo: "도", domain.YutGae: "개", domain.YutGeol: "걸", domain.YutYut: "윷",
		domain.YutMo: "모", domain.YutBackdo: "빽도", domain.YutBuk: "북",
	}[payload.Token.Result]
	if !exists {
		return "", fmt.Errorf("derive game log: unknown yut result at sequence %d", event.Sequence)
	}
	return "{" + name + "}", nil
}

func movementNotations(event storedMatchEvent) ([]GameLogEntry, error) {
	var payload protocol.PieceMovedPayload
	if err := decodePayload(event, &payload); err != nil {
		return nil, err
	}
	if _, err := protocol.NewPieceMovedEvent(event.RoomID, event.MatchID, event.Sequence, payload); err != nil {
		return nil, fmt.Errorf("derive game log: PIECE_MOVED at sequence %d: %w", event.Sequence, err)
	}
	from := gameLogSpaceName(payload.FromSpaceID)
	to := gameLogSpaceName(payload.ToSpaceID)
	entries := make([]GameLogEntry, 0, len(payload.PieceIDs))
	for _, pieceID := range payload.PieceIDs {
		entries = append(entries, GameLogEntry{Sequence: event.Sequence, Notation: fmt.Sprintf("{[%s][%s][%s]}", pieceID, from, to)})
	}
	return entries, nil
}

func gameLogSpaceName(spaceID *domain.SpaceID) string {
	if spaceID == nil {
		return "-"
	}
	return string(*spaceID)
}

func gameEndDetails(event storedMatchEvent) (string, *domain.TeamID, string, error) {
	var payload protocol.GameEndedPayload
	if err := decodePayload(event, &payload); err != nil {
		return "", nil, "", err
	}
	switch payload.Status {
	case "finished":
		if payload.WinnerTeamID == nil {
			return "", nil, "", fmt.Errorf("derive game log: finished GAME_ENDED missing winner at sequence %d", event.Sequence)
		}
		if _, err := protocol.NewFinishedGameEndedEvent(event.RoomID, event.MatchID, event.Sequence, *payload.WinnerTeamID, payload.Reason); err != nil {
			return "", nil, "", fmt.Errorf("derive game log: GAME_ENDED at sequence %d: %w", event.Sequence, err)
		}
		winner := *payload.WinnerTeamID
		return payload.Status, &winner, payload.Reason, nil
	case "invalid":
		if payload.WinnerTeamID != nil {
			return "", nil, "", fmt.Errorf("derive game log: invalid GAME_ENDED has winner at sequence %d", event.Sequence)
		}
		if _, err := protocol.NewInvalidGameEndedEvent(event.RoomID, event.MatchID, event.Sequence, payload.Reason); err != nil {
			return "", nil, "", fmt.Errorf("derive game log: GAME_ENDED at sequence %d: %w", event.Sequence, err)
		}
		return payload.Status, nil, payload.Reason, nil
	default:
		return "", nil, "", fmt.Errorf("derive game log: unknown GAME_ENDED status at sequence %d", event.Sequence)
	}
}

func validateUnrenderedMatchEvent(event storedMatchEvent) error {
	var err error
	switch event.Type {
	case protocol.EventTurnStarted:
		var payload protocol.TurnStartedPayload
		err = decodePayload(event, &payload)
		if err == nil {
			_, err = protocol.NewTurnStartedEvent(event.RoomID, event.MatchID, event.Sequence, payload)
		}
	case protocol.EventResultQueueUpdated:
		var payload protocol.ResultQueueUpdatedPayload
		err = decodePayload(event, &payload)
		if err == nil {
			_, err = protocol.NewResultQueueUpdatedEvent(event.RoomID, event.MatchID, event.Sequence, payload.ResultQueue)
		}
	case protocol.EventMoveRequired:
		var payload protocol.MoveRequiredPayload
		err = decodePayload(event, &payload)
		if err == nil {
			_, err = protocol.NewMoveRequiredEvent(event.RoomID, event.MatchID, event.Sequence, payload)
		}
	case protocol.EventPiecesStacked:
		var payload protocol.PiecesStackedPayload
		err = decodePayload(event, &payload)
		if err == nil {
			_, err = protocol.NewPiecesStackedEvent(event.RoomID, event.MatchID, event.Sequence, payload)
		}
	case protocol.EventPiecesCaptured:
		var payload protocol.PiecesCapturedPayload
		err = decodePayload(event, &payload)
		if err == nil {
			_, err = protocol.NewPiecesCapturedEvent(event.RoomID, event.MatchID, event.Sequence, payload)
		}
	case protocol.EventBukResolved:
		var payload protocol.BukResolvedPayload
		err = decodePayload(event, &payload)
		if err == nil {
			_, err = protocol.NewBukResolvedEvent(event.RoomID, event.MatchID, event.Sequence, payload)
		}
	case protocol.EventCPUControlStarted:
		var payload protocol.CPUControlStartedPayload
		err = decodePayload(event, &payload)
		if err == nil {
			_, err = protocol.NewCPUControlStartedEvent(event.RoomID, event.MatchID, event.Sequence, payload)
		}
	case protocol.EventGamePaused:
		var payload protocol.GamePausedPayload
		err = decodePayload(event, &payload)
		if err == nil {
			_, err = protocol.NewGamePausedEvent(event.RoomID, event.MatchID, event.Sequence, payload)
		}
	case protocol.EventGameResumed:
		var payload protocol.GameResumedPayload
		err = decodePayload(event, &payload)
		if err == nil {
			_, err = protocol.NewGameResumedEvent(event.RoomID, event.MatchID, event.Sequence, payload)
		}
	default:
		return fmt.Errorf("derive game log: unsupported match event %s", event.Type)
	}
	if err != nil {
		return fmt.Errorf("derive game log: %s at sequence %d: %w", event.Type, event.Sequence, err)
	}
	return nil
}

func decodePayload(event storedMatchEvent, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(event.Payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("derive game log: %s payload at sequence %d: %w", event.Type, event.Sequence, err)
	}
	if err := requireSingleJSONValue(decoder); err != nil {
		return fmt.Errorf("derive game log: %s payload at sequence %d: %w", event.Type, event.Sequence, err)
	}
	return nil
}
