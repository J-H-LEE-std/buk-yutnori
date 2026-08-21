package application

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"buk-yutnori/internal/auth"
	"buk-yutnori/internal/domain"
	"buk-yutnori/internal/protocol"
)

var (
	// ErrRoomActorClosed reports a command submitted after room closure began.
	ErrRoomActorClosed = errors.New("room actor is closed")
	// ErrRoomActorMismatch reports a command routed to another room's actor.
	ErrRoomActorMismatch = errors.New("command room does not match room actor")
	// ErrRoomActorPanicked reports a room-owned callback panic contained at the
	// actor goroutine boundary. The failed room is closed before this is exposed.
	ErrRoomActorPanicked = errors.New("room actor callback panicked")
)

// RoomActorCleanup releases room-lifetime resources after the actor has
// stopped command admission and completed its last accepted execution.
type RoomActorCleanup func(domain.RoomID)

// RoomActor is the single authoritative execution boundary for one live
// room. Its unbuffered mailbox admits at most the command currently being
// executed; callers provide backpressure instead of building an actor queue.
type RoomActor struct {
	roomID   domain.RoomID
	executor Executor
	cleanup  RoomActorCleanup

	closeOnce sync.Once
	closed    atomic.Bool
	requests  chan roomActorRequest
	stopping  chan struct{}
	done      chan struct{}
	terminal  error
}

type roomActorRequest struct {
	ctx      context.Context
	user     auth.User
	command  protocol.ClientCommand
	response chan roomActorResponse
}

type roomActorResponse struct {
	outcome protocol.CommandOutcome
	err     error
}

// NewRoomActor starts the execution owner for roomID. cleanup is called once
// after closure and after any command already accepted by the actor finishes.
func NewRoomActor(roomID domain.RoomID, executor Executor, cleanup RoomActorCleanup) (*RoomActor, error) {
	if err := roomID.Validate(); err != nil {
		return nil, fmt.Errorf("%w: room_id: %v", ErrInvalidConfiguration, err)
	}
	if isNilInterface(executor) {
		return nil, fmt.Errorf("%w: room executor is required", ErrInvalidConfiguration)
	}
	if cleanup == nil {
		return nil, fmt.Errorf("%w: room cleanup is required", ErrInvalidConfiguration)
	}

	actor := &RoomActor{
		roomID:   roomID,
		executor: executor,
		cleanup:  cleanup,
		requests: make(chan roomActorRequest),
		stopping: make(chan struct{}),
		done:     make(chan struct{}),
	}
	go actor.run()
	return actor, nil
}

// Execute admits one command to the room actor. The caller context controls
// waiting only until admission. Once admitted, execution is room-owned and
// completes even if the transport caller disconnects or cancels its context.
func (actor *RoomActor) Execute(ctx context.Context, user auth.User, command protocol.ClientCommand) (protocol.CommandOutcome, error) {
	if actor == nil || actor.executor == nil || actor.requests == nil || actor.stopping == nil || actor.done == nil || ctx == nil {
		return protocol.CommandOutcome{}, fmt.Errorf("%w: room actor is required", ErrInvalidConfiguration)
	}
	if err := ctx.Err(); err != nil {
		return protocol.CommandOutcome{}, err
	}
	if command.RoomID != actor.roomID {
		return protocol.CommandOutcome{}, fmt.Errorf(
			"%w: command room %q, actor room %q",
			ErrRoomActorMismatch,
			command.RoomID,
			actor.roomID,
		)
	}

	request := roomActorRequest{
		ctx:      context.WithoutCancel(ctx),
		user:     user,
		command:  command,
		response: make(chan roomActorResponse, 1),
	}

	if actor.closed.Load() {
		return protocol.CommandOutcome{}, ErrRoomActorClosed
	}
	select {
	case actor.requests <- request:
	case <-actor.stopping:
		return protocol.CommandOutcome{}, ErrRoomActorClosed
	case <-ctx.Done():
		return protocol.CommandOutcome{}, ctx.Err()
	}

	response := <-request.response
	return response.outcome, response.err
}

// Close atomically stops new command admission and waits for the current
// accepted command and room cleanup. A canceled wait does not interrupt the
// actor's command or cleanup; a later Close call may continue waiting.
func (actor *RoomActor) Close(ctx context.Context) error {
	if actor == nil || actor.stopping == nil || actor.done == nil || ctx == nil {
		return fmt.Errorf("%w: room actor is required", ErrInvalidConfiguration)
	}

	actor.stop()

	select {
	case <-actor.done:
		return actor.terminal
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (actor *RoomActor) run() {
	defer close(actor.done)
	var terminal error
	defer func() {
		actor.terminal = errors.Join(terminal, containRoomActorCleanupPanic(actor.cleanup, actor.roomID))
	}()

	for {
		select {
		case <-actor.stopping:
			return
		case request := <-actor.requests:
			if actor.closed.Load() {
				request.response <- roomActorResponse{err: ErrRoomActorClosed}
				return
			}
			outcome, err, panicked := containRoomActorExecutorPanic(actor.executor, request.ctx, request.user, request.command)
			if panicked {
				actor.stop()
				terminal = err
			}
			request.response <- roomActorResponse{outcome: outcome, err: err}
			if panicked {
				return
			}
		}
	}
}

func (actor *RoomActor) stop() {
	actor.closeOnce.Do(func() {
		actor.closed.Store(true)
		close(actor.stopping)
	})
}

func containRoomActorExecutorPanic(
	executor Executor,
	ctx context.Context,
	user auth.User,
	command protocol.ClientCommand,
) (outcome protocol.CommandOutcome, err error, panicked bool) {
	defer func() {
		if recovered := recover(); recovered != nil {
			outcome = protocol.CommandOutcome{}
			err = fmt.Errorf("%w: executor (%T)", ErrRoomActorPanicked, recovered)
			panicked = true
		}
	}()
	outcome, err = executor.Execute(ctx, user, command)
	return outcome, err, false
}

func containRoomActorCleanupPanic(cleanup RoomActorCleanup, roomID domain.RoomID) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("%w: cleanup (%T)", ErrRoomActorPanicked, recovered)
		}
	}()
	cleanup(roomID)
	return nil
}
