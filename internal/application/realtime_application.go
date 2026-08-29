package application

import (
	"context"
	"fmt"
	"time"

	"buk-yutnori/internal/auth"
	"buk-yutnori/internal/domain"
	"buk-yutnori/internal/protocol"
	"buk-yutnori/internal/storage"
)

// RealtimeApplication composes the authenticated lobby chat scope, the room
// actor, idempotency registry, and room-lifetime cleanup. SEND_CHAT is routed
// only when its explicit scope is `lobby`; room and match commands stay with
// their authoritative registry owners.
type RealtimeApplication struct {
	chat      *LobbyChatRoom
	sequences *RoomEventSequences
	actor     *RoomActor
	processor *Processor
	lobby     *LobbyCommandExecutor
	match     *MatchCommandExecutor
}

type realtimeRouter struct {
	actor *RoomActor
	lobby *LobbyCommandExecutor
	match *MatchCommandExecutor
}

// NewRealtimeApplication constructs the lobby chat runtime plus authoritative
// room and match routing. chatLogStore may be nil for memory-only tests.
func NewRealtimeApplication(now func() time.Time, lobbies *RoomRegistry, chatLogStore storage.EventStore) (*RealtimeApplication, error) {
	if now == nil {
		return nil, fmt.Errorf("%w: realtime clock is required", ErrInvalidConfiguration)
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
	chat, err := NewLobbyChatRoom(sequences, now, chatLogStore)
	if err != nil {
		return nil, err
	}

	application := &RealtimeApplication{
		chat: chat, sequences: sequences, lobby: lobbyExecutor, match: matchExecutor,
	}
	actor, err := NewRoomActor(LobbyChatRoomID, chat, func(roomID domain.RoomID) {
		if application.processor != nil {
			application.processor.ForgetClosedRoom(roomID)
		}
		sequences.ForgetClosedRoom(roomID)
	})
	if err != nil {
		sequences.ForgetClosedRoom(LobbyChatRoomID)
		_ = chat.Close(context.Background())
		return nil, err
	}
	application.actor = actor
	processor, err := NewProcessor(&realtimeRouter{actor: actor, lobby: lobbyExecutor, match: matchExecutor})
	if err != nil {
		_ = actor.Close(context.Background())
		_ = chat.Close(context.Background())
		return nil, err
	}
	application.processor = processor
	return application, nil
}

// Processor returns the room-lifetime idempotent command processor.
func (application *RealtimeApplication) Processor() *Processor {
	if application == nil {
		return nil
	}
	return application.processor
}

// Lobbies returns the authoritative room registry for event subscription.
func (application *RealtimeApplication) Lobbies() *RoomRegistry {
	if application == nil {
		return nil
	}
	return application.lobby.lobbies
}

// LobbyChatEvents returns the authenticated lobby chat event source.
func (application *RealtimeApplication) LobbyChatEvents() ChatEventSource {
	if application == nil {
		return nil
	}
	return application.chat
}

// Close stops lobby chat admission and drains its bounded best-effort log
// queue after any accepted command completes.
func (application *RealtimeApplication) Close(ctx context.Context) error {
	if application == nil || application.actor == nil {
		return fmt.Errorf("%w: realtime application is required", ErrInvalidConfiguration)
	}
	if err := application.actor.Close(ctx); err != nil {
		return err
	}
	return application.chat.Close(ctx)
}

func (router *realtimeRouter) Execute(ctx context.Context, user auth.User, command protocol.ClientCommand) (protocol.CommandOutcome, error) {
	if router == nil || router.actor == nil {
		return protocol.CommandOutcome{}, fmt.Errorf("%w: lobby chat actor is required", ErrInvalidConfiguration)
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
		if command.RoomID != LobbyChatRoomID {
			return rejectedLobbyChatCommand("ROOM_NOT_FOUND", "lobby chat scope not found", true), nil
		}
		return router.actor.Execute(ctx, user, command)
	default:
		return (UnavailableExecutor{}).Execute(ctx, user, command)
	}
}

func rejectedLobbyChatCommand(code, message string, retriable bool) protocol.CommandOutcome {
	return protocol.CommandOutcome{
		Status: protocol.CommandRejected,
		Error:  &protocol.CommandError{Code: code, Message: message, Retriable: retriable},
	}
}
