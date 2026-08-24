package application

import (
	"context"
	"fmt"
	"time"

	"buk-yutnori/internal/auth"
	"buk-yutnori/internal/domain"
	"buk-yutnori/internal/protocol"
)

// RealtimeApplication composes the fixed prototype chat room, the room
// actor, idempotency registry, and room-lifetime cleanup, and routes every
// command type to its authoritative executor: lobby commands and the
// canonical match runtime go to the room registry; SEND_CHAT stays on the
// Milestone 2 prototype chat scope (ADR-0008). The former fixed
// prototype-match reconnect scope from ADR-0013 is retired: RECONNECT now
// returns real assembled snapshots for started registry rooms.
type PrototypeRealtimeApplication struct {
	room      *PrototypeChatRoom
	sequences *RoomEventSequences
	actor     *RoomActor
	processor *Processor
	lobby     *LobbyCommandExecutor
	match     *MatchCommandExecutor
}

type prototypeRoomRouter struct {
	actor *RoomActor
	lobby *LobbyCommandExecutor
	match *MatchCommandExecutor
}

// NewPrototypeRealtimeApplication constructs the chat runtime plus the
// authoritative lobby and match command routing. The prototype room no
// longer bootstraps a sequence-1 initialization event: its sequence space
// starts empty and only real chat events consume it.
func NewPrototypeRealtimeApplication(now func() time.Time, lobbies *RoomRegistry) (*PrototypeRealtimeApplication, error) {
	if now == nil {
		return nil, fmt.Errorf("%w: prototype clock is required", ErrInvalidConfiguration)
	}
	lobbyExecutor, err := NewLobbyCommandExecutor(lobbies)
	if err != nil {
		return nil, err
	}
	matchExecutor, err := NewMatchCommandExecutor(lobbies)
	if err != nil {
		return nil, err
	}
	sequences := NewRoomEventSequences()
	room, err := NewPrototypeChatRoom(sequences, now)
	if err != nil {
		return nil, err
	}

	application := &PrototypeRealtimeApplication{
		room: room, sequences: sequences, lobby: lobbyExecutor, match: matchExecutor,
	}
	actor, err := NewRoomActor(PrototypeRoomID, room, func(roomID domain.RoomID) {
		if application.processor != nil {
			application.processor.ForgetClosedRoom(roomID)
		}
		sequences.ForgetClosedRoom(roomID)
	})
	if err != nil {
		sequences.ForgetClosedRoom(PrototypeRoomID)
		return nil, err
	}
	application.actor = actor
	processor, err := NewProcessor(&prototypeRoomRouter{actor: actor, lobby: lobbyExecutor, match: matchExecutor})
	if err != nil {
		_ = actor.Close(context.Background())
		return nil, err
	}
	application.processor = processor
	return application, nil
}

// Processor returns the room-lifetime idempotent command processor.
func (application *PrototypeRealtimeApplication) Processor() *Processor {
	if application == nil {
		return nil
	}
	return application.processor
}

// Lobbies returns the authoritative room registry for event subscription.
func (application *PrototypeRealtimeApplication) Lobbies() *RoomRegistry {
	if application == nil {
		return nil
	}
	return application.lobby.lobbies
}

// ChatEvents returns the fixed room's authenticated chat event source.
func (application *PrototypeRealtimeApplication) ChatEvents() ChatEventSource {
	if application == nil {
		return nil
	}
	return application.room
}

// Close stops actor admission and releases room-lifetime sequence and
// idempotency state after any accepted command completes.
func (application *PrototypeRealtimeApplication) Close(ctx context.Context) error {
	if application == nil || application.actor == nil {
		return fmt.Errorf("%w: prototype realtime application is required", ErrInvalidConfiguration)
	}
	return application.actor.Close(ctx)
}

func (router *prototypeRoomRouter) Execute(ctx context.Context, user auth.User, command protocol.ClientCommand) (protocol.CommandOutcome, error) {
	if router == nil || router.actor == nil {
		return protocol.CommandOutcome{}, fmt.Errorf("%w: prototype room actor is required", ErrInvalidConfiguration)
	}
	switch command.Type {
	case protocol.CommandSelectTeam, protocol.CommandSetReady,
		protocol.CommandStartGame, protocol.CommandConfirmGameStart:
		return router.lobby.Execute(ctx, user, command)
	case protocol.CommandThrowYut, protocol.CommandSelectResult,
		protocol.CommandSelectPiece, protocol.CommandSelectRoute,
		protocol.CommandPauseGame, protocol.CommandResumeGame,
		protocol.CommandReconnect:
		return router.match.Execute(ctx, user, command)
	case protocol.CommandSendChat:
		if command.RoomID != PrototypeRoomID {
			return rejectedPrototypeCommand("ROOM_NOT_FOUND", "prototype room not found", true), nil
		}
		return router.actor.Execute(ctx, user, command)
	default:
		return (UnavailableExecutor{}).Execute(ctx, user, command)
	}
}

func rejectedPrototypeCommand(code, message string, retriable bool) protocol.CommandOutcome {
	return protocol.CommandOutcome{
		Status: protocol.CommandRejected,
		Error:  &protocol.CommandError{Code: code, Message: message, Retriable: retriable},
	}
}
