package protocol

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"buk-yutnori/internal/domain"
)

func TestDecodeClientCommandAcceptsCanonicalCommands(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		message     string
		wantType    CommandType
		wantPayload string
		wantMatchID string
	}{
		{name: "select team", message: `{"version":1,"direction":"client_command","type":"SELECT_TEAM","command_id":"cmd-1","room_id":"room-1","payload":{"team_id":"A"}}`, wantType: CommandSelectTeam, wantPayload: "protocol.SelectTeamPayload"},
		{name: "set ready", message: `{"version":1,"direction":"client_command","type":"SET_READY","command_id":"cmd-2","room_id":"room-1","payload":{"ready":false}}`, wantType: CommandSetReady, wantPayload: "protocol.SetReadyPayload"},
		{name: "start game", message: `{"version":1,"direction":"client_command","type":"START_GAME","request_id":"req-1","command_id":"cmd-3","sequence":null,"room_id":"room-1","payload":{}}`, wantType: CommandStartGame, wantPayload: "protocol.EmptyPayload"},
		{name: "throw yut", message: `{"version":1,"direction":"client_command","type":"THROW_YUT","command_id":"cmd-4","room_id":"room-1","match_id":"match-1","payload":{}}`, wantType: CommandThrowYut, wantPayload: "protocol.EmptyPayload", wantMatchID: "match-1"},
		{name: "select result", message: `{"version":1,"direction":"client_command","type":"SELECT_RESULT","command_id":"cmd-5","room_id":"room-1","match_id":"match-1","payload":{"token_id":"token-1"}}`, wantType: CommandSelectResult, wantPayload: "protocol.SelectResultPayload", wantMatchID: "match-1"},
		{name: "select piece", message: `{"version":1,"direction":"client_command","type":"SELECT_PIECE","command_id":"cmd-6","room_id":"room-1","match_id":"match-1","payload":{"token_id":"token-1","piece_id":"piece-1"}}`, wantType: CommandSelectPiece, wantPayload: "protocol.SelectPiecePayload", wantMatchID: "match-1"},
		{name: "select route", message: `{"version":1,"direction":"client_command","type":"SELECT_ROUTE","command_id":"cmd-7","room_id":"room-1","match_id":"match-1","payload":{"token_id":"token-1","piece_id":"piece-1","route":"shortcut"}}`, wantType: CommandSelectRoute, wantPayload: "protocol.SelectRoutePayload", wantMatchID: "match-1"},
		{name: "send Korean chat", message: `{"version":1,"direction":"client_command","type":"SEND_CHAT","command_id":"cmd-8","room_id":"room-1","payload":{"text":"안녕하세요"}}`, wantType: CommandSendChat, wantPayload: "protocol.SendChatPayload"},
		{name: "reconnect", message: `{"version":1,"direction":"client_command","type":"RECONNECT","request_id":null,"command_id":"cmd-9","room_id":"room-1","match_id":"match-1","payload":{"last_sequence":41}}`, wantType: CommandReconnect, wantPayload: "protocol.ReconnectPayload", wantMatchID: "match-1"},
		{name: "confirm start", message: `{"version":1,"direction":"client_command","type":"CONFIRM_GAME_START","command_id":"cmd-10","room_id":"room-1","match_id":"match-1","payload":{}}`, wantType: CommandConfirmGameStart, wantPayload: "protocol.EmptyPayload", wantMatchID: "match-1"},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			command, err := DecodeClientCommand([]byte(test.message))
			if err != nil {
				t.Fatalf("DecodeClientCommand() error = %v", err)
			}
			if command.Version != Version1 || command.Direction != DirectionClientCommand || command.Type != test.wantType {
				t.Fatalf("command envelope = %+v", command)
			}
			if command.CommandID == "" {
				t.Fatal("command ID is empty")
			}
			if command.RoomID != domain.RoomID("room-1") {
				t.Fatalf("room ID = %q", command.RoomID)
			}
			if got := fmt.Sprintf("%T", command.Payload); got != test.wantPayload {
				t.Fatalf("payload type = %q, want %q", got, test.wantPayload)
			}
			if test.wantMatchID == "" {
				if command.MatchID != nil {
					t.Fatalf("match ID = %q, want nil", *command.MatchID)
				}
			} else if command.MatchID == nil || command.MatchID.String() != test.wantMatchID {
				t.Fatalf("match ID = %v, want %q", command.MatchID, test.wantMatchID)
			}
		})
	}
}

func TestDecodeClientCommandRejectsInvalidWireData(t *testing.T) {
	t.Parallel()

	valid := `{"version":1,"direction":"client_command","type":"SEND_CHAT","command_id":"cmd-1","room_id":"room-1","payload":{"text":"hello"}}`
	tooLong := strings.Repeat("가", 201)
	tests := []struct {
		name    string
		message []byte
	}{
		{name: "invalid UTF-8", message: append([]byte(valid[:len(valid)-2]), 0xff, '"', '}', '}')},
		{name: "not object", message: []byte(`[]`)},
		{name: "missing version", message: []byte(`{"direction":"client_command","type":"SEND_CHAT","command_id":"cmd-1","room_id":"room-1","payload":{"text":"hello"}}`)},
		{name: "wrong version", message: []byte(`{"version":2,"direction":"client_command","type":"SEND_CHAT","command_id":"cmd-1","room_id":"room-1","payload":{"text":"hello"}}`)},
		{name: "wrong direction", message: []byte(`{"version":1,"direction":"server_event","type":"SEND_CHAT","command_id":"cmd-1","room_id":"room-1","payload":{"text":"hello"}}`)},
		{name: "missing command ID", message: []byte(`{"version":1,"direction":"client_command","type":"SEND_CHAT","room_id":"room-1","payload":{"text":"hello"}}`)},
		{name: "empty command ID", message: []byte(`{"version":1,"direction":"client_command","type":"SEND_CHAT","command_id":"","room_id":"room-1","payload":{"text":"hello"}}`)},
		{name: "missing room ID", message: []byte(`{"version":1,"direction":"client_command","type":"SEND_CHAT","command_id":"cmd-1","payload":{"text":"hello"}}`)},
		{name: "non-null sequence", message: []byte(`{"version":1,"direction":"client_command","type":"SEND_CHAT","command_id":"cmd-1","sequence":7,"room_id":"room-1","payload":{"text":"hello"}}`)},
		{name: "unknown envelope field", message: []byte(`{"version":1,"direction":"client_command","type":"SEND_CHAT","command_id":"cmd-1","room_id":"room-1","payload":{"text":"hello"},"user_id":"attacker"}`)},
		{name: "case variant envelope field", message: []byte(`{"Version":1,"direction":"client_command","type":"SEND_CHAT","command_id":"cmd-1","room_id":"room-1","payload":{"text":"hello"}}`)},
		{name: "duplicate envelope key", message: []byte(`{"version":1,"direction":"client_command","type":"SEND_CHAT","command_id":"cmd-1","command_id":"cmd-2","room_id":"room-1","payload":{"text":"hello"}}`)},
		{name: "trailing JSON", message: []byte(valid + `{}`)},
		{name: "unknown command", message: []byte(`{"version":1,"direction":"client_command","type":"DELETE_ROOM","command_id":"cmd-1","room_id":"room-1","payload":{}}`)},
		{name: "match command without match", message: []byte(`{"version":1,"direction":"client_command","type":"THROW_YUT","command_id":"cmd-1","room_id":"room-1","payload":{}}`)},
		{name: "invalid team", message: []byte(`{"version":1,"direction":"client_command","type":"SELECT_TEAM","command_id":"cmd-1","room_id":"room-1","payload":{"team_id":"C"}}`)},
		{name: "unknown payload field", message: []byte(`{"version":1,"direction":"client_command","type":"SET_READY","command_id":"cmd-1","room_id":"room-1","payload":{"ready":true,"force":true}}`)},
		{name: "case variant payload field", message: []byte(`{"version":1,"direction":"client_command","type":"SET_READY","command_id":"cmd-1","room_id":"room-1","payload":{"Ready":true}}`)},
		{name: "duplicate payload key", message: []byte(`{"version":1,"direction":"client_command","type":"SEND_CHAT","command_id":"cmd-1","room_id":"room-1","payload":{"text":"one","text":"two"}}`)},
		{name: "null payload", message: []byte(`{"version":1,"direction":"client_command","type":"START_GAME","command_id":"cmd-1","room_id":"room-1","payload":null}`)},
		{name: "empty chat", message: []byte(`{"version":1,"direction":"client_command","type":"SEND_CHAT","command_id":"cmd-1","room_id":"room-1","payload":{"text":""}}`)},
		{name: "chat over 200 code points", message: []byte(`{"version":1,"direction":"client_command","type":"SEND_CHAT","command_id":"cmd-1","room_id":"room-1","payload":{"text":"` + tooLong + `"}}`)},
		{name: "fractional sequence", message: []byte(`{"version":1,"direction":"client_command","type":"RECONNECT","command_id":"cmd-1","room_id":"room-1","match_id":"match-1","payload":{"last_sequence":1.5}}`)},
		{name: "negative sequence", message: []byte(`{"version":1,"direction":"client_command","type":"RECONNECT","command_id":"cmd-1","room_id":"room-1","match_id":"match-1","payload":{"last_sequence":-1}}`)},
		{name: "invalid route", message: []byte(`{"version":1,"direction":"client_command","type":"SELECT_ROUTE","command_id":"cmd-1","room_id":"room-1","match_id":"match-1","payload":{"token_id":"token-1","piece_id":"piece-1","route":"center"}}`)},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := DecodeClientCommand(test.message); !errors.Is(err, ErrInvalidClientCommand) {
				t.Fatalf("DecodeClientCommand() error = %v, want ErrInvalidClientCommand", err)
			}
		})
	}
}
