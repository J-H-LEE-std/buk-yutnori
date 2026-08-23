package application

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"buk-yutnori/internal/auth"
	"buk-yutnori/internal/domain"
	"buk-yutnori/internal/protocol"
)

const (
	// PrototypeMatchID is the fixed match scope used only by the Milestone 2
	// reconnect vertical prototype. A room registry replaces it in Milestone 3.
	PrototypeMatchID domain.MatchID = "prototype-match"

	prototypeRoomInitializationSequence uint64 = 1
)

// PrototypeRealtimeApplication composes the fixed chat room, reconnect
// snapshot source, room actor, idempotency registry, and room-lifetime cleanup,
// and routes Milestone 3 lobby commands to the authoritative room registry.
// The fixed prototype scope itself is still not the room registry.
type PrototypeRealtimeApplication struct {
	room      *PrototypeChatRoom
	sequences *RoomEventSequences
	actor     *RoomActor
	processor *Processor
	lobby     *LobbyCommandExecutor
}

type prototypeRoomExecutor struct {
	room      *PrototypeChatRoom
	sequences *RoomEventSequences
}

type prototypeRoomRouter struct {
	actor *RoomActor
	lobby *LobbyCommandExecutor
}

type prototypeGameSnapshot struct {
	RoomID         domain.RoomID           `json:"room_id"`
	MatchID        domain.MatchID          `json:"match_id"`
	Sequence       uint64                  `json:"sequence"`
	Status         string                  `json:"status"`
	Teams          []prototypeSnapshotTeam `json:"teams"`
	Participants   []json.RawMessage       `json:"participants"`
	CurrentTurn    prototypeSnapshotTurn   `json:"current_turn"`
	ResultQueue    []json.RawMessage       `json:"result_queue"`
	Pieces         []json.RawMessage       `json:"pieces"`
	Stacks         []json.RawMessage       `json:"stacks"`
	PositionGroups []json.RawMessage       `json:"position_groups"`
	Buk            prototypeSnapshotBuk    `json:"buk"`
	Pause          prototypeSnapshotPause  `json:"pause"`
}

type prototypeSnapshotTeam struct {
	TeamID    domain.TeamID `json:"team_id"`
	PlayerIDs []string      `json:"player_ids"`
	TurnOrder []string      `json:"turn_order"`
}

type prototypeSnapshotTurn struct {
	PlayerID      *string                `json:"player_id"`
	Phase         string                 `json:"phase"`
	RequiredInput string                 `json:"required_input"`
	Timer         prototypeSnapshotTimer `json:"timer"`
}

type prototypeSnapshotTimer struct {
	Phase       string  `json:"phase"`
	RemainingMS uint64  `json:"remaining_ms"`
	DeadlineAt  *string `json:"deadline_at"`
}

type prototypeSnapshotBuk struct {
	Enabled            bool    `json:"enabled"`
	DestinationSpaceID *string `json:"destination_space_id"`
}

type prototypeSnapshotPause struct {
	Used   bool    `json:"used"`
	Paused bool    `json:"paused"`
	EndsAt *string `json:"ends_at"`
}

// NewPrototypeRealtimeApplication constructs the fixed Milestone 2 runtime
// plus the Milestone 3 lobby command routing. Sequence one records that the
// fixed room entered its prototype match scope before any WebSocket can
// subscribe; its state is represented by snapshots.
func NewPrototypeRealtimeApplication(now func() time.Time, lobbies *RoomRegistry) (*PrototypeRealtimeApplication, error) {
	if now == nil {
		return nil, fmt.Errorf("%w: prototype clock is required", ErrInvalidConfiguration)
	}
	lobbyExecutor, err := NewLobbyCommandExecutor(lobbies)
	if err != nil {
		return nil, err
	}
	sequences := NewRoomEventSequences()
	room, err := NewPrototypeChatRoom(sequences, now)
	if err != nil {
		return nil, err
	}
	sequence, err := sequences.CommitNext(PrototypeRoomID)
	if err != nil {
		return nil, fmt.Errorf("commit prototype room initialization: %w", err)
	}
	if sequence != prototypeRoomInitializationSequence {
		return nil, fmt.Errorf("%w: prototype room initialization sequence is %d", ErrInvalidConfiguration, sequence)
	}

	application := &PrototypeRealtimeApplication{room: room, sequences: sequences, lobby: lobbyExecutor}
	executor := &prototypeRoomExecutor{room: room, sequences: sequences}
	actor, err := NewRoomActor(PrototypeRoomID, executor, func(roomID domain.RoomID) {
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
	processor, err := NewProcessor(&prototypeRoomRouter{actor: actor, lobby: lobbyExecutor})
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
	case protocol.CommandSendChat, protocol.CommandReconnect:
		if command.RoomID != PrototypeRoomID {
			return rejectedPrototypeCommand("ROOM_NOT_FOUND", "prototype room not found", true), nil
		}
		return router.actor.Execute(ctx, user, command)
	default:
		return (UnavailableExecutor{}).Execute(ctx, user, command)
	}
}

func (executor *prototypeRoomExecutor) Execute(ctx context.Context, user auth.User, command protocol.ClientCommand) (protocol.CommandOutcome, error) {
	if executor == nil || executor.room == nil || executor.sequences == nil {
		return protocol.CommandOutcome{}, fmt.Errorf("%w: prototype room executor is required", ErrInvalidConfiguration)
	}
	if command.Type != protocol.CommandReconnect {
		return executor.room.Execute(ctx, user, command)
	}
	if err := ctx.Err(); err != nil {
		return protocol.CommandOutcome{}, err
	}
	if err := user.ID.Validate(); err != nil {
		return protocol.CommandOutcome{}, fmt.Errorf("%w: authenticated user_id", ErrInvalidCommand)
	}
	if command.RoomID != PrototypeRoomID {
		return rejectedPrototypeCommand("ROOM_NOT_FOUND", "prototype room not found", true), nil
	}
	if command.MatchID == nil || *command.MatchID != PrototypeMatchID {
		return rejectedPrototypeCommand(protocol.ErrorCodeResyncRequired, "prototype match scope is unavailable", true), nil
	}
	payload, ok := command.Payload.(protocol.ReconnectPayload)
	if !ok {
		return protocol.CommandOutcome{}, fmt.Errorf("%w: invalid RECONNECT payload", ErrInvalidCommand)
	}
	boundary, err := executor.sequences.Boundary(PrototypeRoomID)
	if err != nil {
		return protocol.CommandOutcome{}, fmt.Errorf("read prototype snapshot boundary: %w", err)
	}
	if boundary < prototypeRoomInitializationSequence {
		return protocol.CommandOutcome{}, fmt.Errorf("%w: prototype snapshot boundary is unavailable", ErrInvalidConfiguration)
	}
	if payload.LastSequence > boundary {
		return rejectedPrototypeCommand(protocol.ErrorCodeResyncRequired, "client sequence is ahead of the prototype room", true), nil
	}
	snapshot, err := json.Marshal(newPrototypeGameSnapshot(boundary))
	if err != nil {
		return protocol.CommandOutcome{}, fmt.Errorf("encode prototype game snapshot: %w", err)
	}
	synchronization, err := protocol.NewReconnectSynchronization(command, snapshot, nil)
	if err != nil {
		return protocol.CommandOutcome{}, fmt.Errorf("build prototype reconnect synchronization: %w", err)
	}
	return protocol.CommandOutcome{
		Status:          protocol.CommandAccepted,
		Synchronization: &synchronization,
	}, nil
}

func newPrototypeGameSnapshot(sequence uint64) prototypeGameSnapshot {
	emptyRawValues := func() []json.RawMessage { return make([]json.RawMessage, 0) }
	return prototypeGameSnapshot{
		RoomID: PrototypeRoomID, MatchID: PrototypeMatchID, Sequence: sequence, Status: "starting",
		Teams: []prototypeSnapshotTeam{
			{TeamID: domain.TeamA, PlayerIDs: []string{}, TurnOrder: []string{}},
			{TeamID: domain.TeamB, PlayerIDs: []string{}, TurnOrder: []string{}},
		},
		Participants: emptyRawValues(),
		CurrentTurn: prototypeSnapshotTurn{
			Phase: "turn_start", RequiredInput: "none",
			Timer: prototypeSnapshotTimer{Phase: "none", RemainingMS: 0},
		},
		ResultQueue: emptyRawValues(), Pieces: emptyRawValues(), Stacks: emptyRawValues(),
		PositionGroups: emptyRawValues(),
		Buk:            prototypeSnapshotBuk{Enabled: false},
		Pause:          prototypeSnapshotPause{Used: false, Paused: false},
	}
}

func rejectedPrototypeCommand(code, message string, retriable bool) protocol.CommandOutcome {
	return protocol.CommandOutcome{
		Status: protocol.CommandRejected,
		Error:  &protocol.CommandError{Code: code, Message: message, Retriable: retriable},
	}
}
