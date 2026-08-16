// Package protocol owns typed WebSocket message boundaries.
package protocol

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"unicode/utf8"

	"buk-yutnori/internal/domain"
)

const (
	// Version1 is the only currently supported WebSocket protocol version.
	Version1 = 1

	DirectionClientCommand Direction = "client_command"

	CommandSelectTeam       CommandType = "SELECT_TEAM"
	CommandSetReady         CommandType = "SET_READY"
	CommandStartGame        CommandType = "START_GAME"
	CommandThrowYut         CommandType = "THROW_YUT"
	CommandSelectResult     CommandType = "SELECT_RESULT"
	CommandSelectPiece      CommandType = "SELECT_PIECE"
	CommandSelectRoute      CommandType = "SELECT_ROUTE"
	CommandSendChat         CommandType = "SEND_CHAT"
	CommandReconnect        CommandType = "RECONNECT"
	CommandConfirmGameStart CommandType = "CONFIRM_GAME_START"

	// MaxChatCodePoints is the v1 SEND_CHAT and CHAT_MESSAGE text limit.
	MaxChatCodePoints = 200
)

var ErrInvalidClientCommand = errors.New("invalid client command")

// Direction identifies which side emitted a WebSocket envelope.
type Direction string

// CommandType identifies a client command defined by the v1 schema.
type CommandType string

// ClientCommand is a validated, typed client command. Payload contains one of
// the payload value types declared below.
type ClientCommand struct {
	Version   int
	Direction Direction
	Type      CommandType
	RequestID *string
	CommandID string
	RoomID    domain.RoomID
	MatchID   *domain.MatchID
	Payload   any
}

type EmptyPayload struct{}

type SelectTeamPayload struct {
	TeamID domain.TeamID
}

type SetReadyPayload struct {
	Ready bool
}

type SelectResultPayload struct {
	TokenID domain.ResultTokenID
}

type SelectPiecePayload struct {
	TokenID domain.ResultTokenID
	PieceID domain.PieceID
}

type SelectRoutePayload struct {
	TokenID domain.ResultTokenID
	PieceID domain.PieceID
	Route   domain.Route
}

type SendChatPayload struct {
	Text string
}

type ReconnectPayload struct {
	LastSequence uint64
}

type wireClientCommand struct {
	Version   *int            `json:"version"`
	Direction *Direction      `json:"direction"`
	Type      *CommandType    `json:"type"`
	RequestID *string         `json:"request_id"`
	CommandID *string         `json:"command_id"`
	Sequence  json.RawMessage `json:"sequence"`
	RoomID    *domain.RoomID  `json:"room_id"`
	MatchID   *domain.MatchID `json:"match_id"`
	Payload   json.RawMessage `json:"payload"`
}

// DecodeClientCommand validates the complete v1 client command envelope and
// converts its payload to a command-specific value.
func DecodeClientCommand(message []byte) (ClientCommand, error) {
	if !utf8.Valid(message) {
		return ClientCommand{}, invalidCommand("message is not valid UTF-8")
	}
	var wire wireClientCommand
	if err := decodeStrict(message, &wire); err != nil {
		return ClientCommand{}, invalidCommand("decode envelope: %v", err)
	}
	if wire.Version == nil || *wire.Version != Version1 {
		return ClientCommand{}, invalidCommand("unsupported or missing version")
	}
	if wire.Direction == nil || *wire.Direction != DirectionClientCommand {
		return ClientCommand{}, invalidCommand("invalid or missing direction")
	}
	if wire.Type == nil {
		return ClientCommand{}, invalidCommand("missing type")
	}
	if wire.CommandID == nil || *wire.CommandID == "" {
		return ClientCommand{}, invalidCommand("missing command_id")
	}
	if wire.RoomID == nil {
		return ClientCommand{}, invalidCommand("missing room_id")
	}
	if err := wire.RoomID.Validate(); err != nil {
		return ClientCommand{}, invalidCommand("invalid room_id")
	}
	if wire.RequestID != nil && *wire.RequestID == "" {
		return ClientCommand{}, invalidCommand("empty request_id")
	}
	if wire.MatchID != nil {
		if err := wire.MatchID.Validate(); err != nil {
			return ClientCommand{}, invalidCommand("invalid match_id")
		}
	}
	if len(wire.Sequence) > 0 && !bytes.Equal(bytes.TrimSpace(wire.Sequence), []byte("null")) {
		return ClientCommand{}, invalidCommand("client sequence must be null")
	}
	if len(wire.Payload) == 0 || bytes.Equal(bytes.TrimSpace(wire.Payload), []byte("null")) {
		return ClientCommand{}, invalidCommand("missing payload")
	}
	if requiresMatch(*wire.Type) && wire.MatchID == nil {
		return ClientCommand{}, invalidCommand("match_id is required for %s", *wire.Type)
	}

	payload, err := decodePayload(*wire.Type, wire.Payload)
	if err != nil {
		return ClientCommand{}, err
	}
	return ClientCommand{
		Version:   *wire.Version,
		Direction: *wire.Direction,
		Type:      *wire.Type,
		RequestID: wire.RequestID,
		CommandID: *wire.CommandID,
		RoomID:    *wire.RoomID,
		MatchID:   wire.MatchID,
		Payload:   payload,
	}, nil
}

func requiresMatch(commandType CommandType) bool {
	switch commandType {
	case CommandThrowYut,
		CommandSelectResult,
		CommandSelectPiece,
		CommandSelectRoute,
		CommandReconnect,
		CommandConfirmGameStart:
		return true
	default:
		return false
	}
}

func decodePayload(commandType CommandType, raw json.RawMessage) (any, error) {
	switch commandType {
	case CommandSelectTeam:
		var payload struct {
			TeamID domain.TeamID `json:"team_id"`
		}
		if err := decodeStrict(raw, &payload); err != nil || payload.TeamID.Validate() != nil {
			return nil, invalidCommand("invalid SELECT_TEAM payload")
		}
		return SelectTeamPayload{TeamID: payload.TeamID}, nil

	case CommandSetReady:
		var payload struct {
			Ready *bool `json:"ready"`
		}
		if err := decodeStrict(raw, &payload); err != nil || payload.Ready == nil {
			return nil, invalidCommand("invalid SET_READY payload")
		}
		return SetReadyPayload{Ready: *payload.Ready}, nil

	case CommandStartGame, CommandThrowYut, CommandConfirmGameStart:
		var payload EmptyPayload
		if err := decodeStrict(raw, &payload); err != nil {
			return nil, invalidCommand("invalid %s payload", commandType)
		}
		return payload, nil

	case CommandSelectResult:
		var payload struct {
			TokenID domain.ResultTokenID `json:"token_id"`
		}
		if err := decodeStrict(raw, &payload); err != nil || payload.TokenID.Validate() != nil {
			return nil, invalidCommand("invalid SELECT_RESULT payload")
		}
		return SelectResultPayload{TokenID: payload.TokenID}, nil

	case CommandSelectPiece:
		var payload struct {
			TokenID domain.ResultTokenID `json:"token_id"`
			PieceID domain.PieceID       `json:"piece_id"`
		}
		if err := decodeStrict(raw, &payload); err != nil || payload.TokenID.Validate() != nil || payload.PieceID.Validate() != nil {
			return nil, invalidCommand("invalid SELECT_PIECE payload")
		}
		return SelectPiecePayload{TokenID: payload.TokenID, PieceID: payload.PieceID}, nil

	case CommandSelectRoute:
		var payload struct {
			TokenID domain.ResultTokenID `json:"token_id"`
			PieceID domain.PieceID       `json:"piece_id"`
			Route   domain.Route         `json:"route"`
		}
		if err := decodeStrict(raw, &payload); err != nil || payload.TokenID.Validate() != nil || payload.PieceID.Validate() != nil || payload.Route.Validate() != nil {
			return nil, invalidCommand("invalid SELECT_ROUTE payload")
		}
		return SelectRoutePayload{TokenID: payload.TokenID, PieceID: payload.PieceID, Route: payload.Route}, nil

	case CommandSendChat:
		var payload struct {
			Text *string `json:"text"`
		}
		if err := decodeStrict(raw, &payload); err != nil || payload.Text == nil || *payload.Text == "" || utf8.RuneCountInString(*payload.Text) > MaxChatCodePoints {
			return nil, invalidCommand("invalid SEND_CHAT payload")
		}
		return SendChatPayload{Text: *payload.Text}, nil

	case CommandReconnect:
		var payload struct {
			LastSequence *uint64 `json:"last_sequence"`
		}
		if err := decodeStrict(raw, &payload); err != nil || payload.LastSequence == nil {
			return nil, invalidCommand("invalid RECONNECT payload")
		}
		return ReconnectPayload{LastSequence: *payload.LastSequence}, nil

	default:
		return nil, invalidCommand("unknown command type")
	}
}

func decodeStrict(data []byte, destination any) error {
	if err := rejectDuplicateObjectKeys(data); err != nil {
		return err
	}
	if err := requireExactObjectFields(data, destination); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}

func requireExactObjectFields(data []byte, destination any) error {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		return err
	}
	if object == nil {
		return errors.New("JSON value must be an object")
	}

	destinationType := reflect.TypeOf(destination)
	for destinationType.Kind() == reflect.Pointer {
		destinationType = destinationType.Elem()
	}
	if destinationType.Kind() != reflect.Struct {
		return errors.New("JSON destination must be a struct")
	}
	allowed := make(map[string]struct{}, destinationType.NumField())
	for index := 0; index < destinationType.NumField(); index++ {
		name := strings.Split(destinationType.Field(index).Tag.Get("json"), ",")[0]
		if name != "" && name != "-" {
			allowed[name] = struct{}{}
		}
	}
	for name := range object {
		if _, ok := allowed[name]; !ok {
			return fmt.Errorf("unknown object field %q", name)
		}
	}
	return nil
}

func rejectDuplicateObjectKeys(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := walkJSONValue(decoder); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}

func walkJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return errors.New("object key is not a string")
			}
			if _, duplicate := seen[key]; duplicate {
				return fmt.Errorf("duplicate object key %q", key)
			}
			seen[key] = struct{}{}
			if err := walkJSONValue(decoder); err != nil {
				return err
			}
		}
		return requireClosingDelimiter(decoder, '}')
	case '[':
		for decoder.More() {
			if err := walkJSONValue(decoder); err != nil {
				return err
			}
		}
		return requireClosingDelimiter(decoder, ']')
	default:
		return fmt.Errorf("unexpected closing delimiter %q", delimiter)
	}
}

func requireClosingDelimiter(decoder *json.Decoder, expected json.Delim) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	if token != expected {
		return fmt.Errorf("unexpected JSON delimiter %q", token)
	}
	return nil
}

func invalidCommand(format string, arguments ...any) error {
	detail := fmt.Sprintf(format, arguments...)
	detail = strings.TrimSpace(detail)
	return fmt.Errorf("%w: %s", ErrInvalidClientCommand, detail)
}
