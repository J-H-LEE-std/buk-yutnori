package wsapi

import (
	"context"
	"errors"
	"fmt"

	"buk-yutnori/internal/application"
	"buk-yutnori/internal/auth"
	"buk-yutnori/internal/protocol"
)

// RealtimeSession multiplexes authoritative command results and prototype
// room chat events through one serialized WebSocket writer.
type RealtimeSession struct {
	processor CommandProcessor
	events    application.ChatEventSource
}

type processedCommand struct {
	result protocol.CommandResult
	err    error
}

type realtimeConnection interface {
	ReadCommand(context.Context) (protocol.ClientCommand, error)
	WriteJSON(context.Context, any) error
	CloseCommandIDConflict() error
	CloseEventBackpressure() error
}

// NewRealtimeSession constructs the command and chat event session loop.
func NewRealtimeSession(processor CommandProcessor, events application.ChatEventSource) (*RealtimeSession, error) {
	if isNilSessionDependency(processor) || isNilSessionDependency(events) {
		return nil, fmt.Errorf("%w: command processor and chat event source are required", ErrInvalidConfiguration)
	}
	return &RealtimeSession{processor: processor, events: events}, nil
}

// Serve implements Session with one reader goroutine and one writer loop.
func (session *RealtimeSession) Serve(ctx context.Context, user auth.User, connection *Connection) error {
	return session.serve(ctx, user, connection)
}

func (session *RealtimeSession) serve(ctx context.Context, user auth.User, connection realtimeConnection) error {
	if session == nil || session.processor == nil || session.events == nil || isNilSessionDependency(connection) {
		return ErrInvalidConfiguration
	}
	subscription, err := session.events.Subscribe(user)
	if err != nil {
		return fmt.Errorf("subscribe chat events: %w", err)
	}
	if isNilSessionDependency(subscription) {
		return fmt.Errorf("%w: chat subscription is required", ErrInvalidConfiguration)
	}
	events, subscriptionDone := subscription.Events(), subscription.Done()
	if events == nil || subscriptionDone == nil {
		subscription.Close()
		return fmt.Errorf("%w: chat subscription channels are required", ErrInvalidConfiguration)
	}

	sessionContext, cancelSession := context.WithCancel(ctx)
	commandContext, cancelCommands := context.WithCancel(ctx)
	backpressureResult := make(chan error, 1)
	watcherDone := make(chan struct{})
	go func() {
		defer close(watcherDone)
		select {
		case <-subscriptionDone:
			if sessionContext.Err() != nil {
				return
			}
			cancelCommands()
			backpressureResult <- connection.CloseEventBackpressure()
		case <-sessionContext.Done():
		}
	}()
	defer func() {
		cancelCommands()
		cancelSession()
		<-watcherDone
		subscription.Close()
	}()
	processed := make(chan processedCommand)
	go session.readCommands(sessionContext, commandContext, user, connection, processed)

	for {
		select {
		case <-sessionContext.Done():
			return sessionContext.Err()
		case closeErr := <-backpressureResult:
			return closeErr
		case event, ok := <-events:
			if !ok {
				if subscriptionDropped(subscriptionDone) {
					return waitForBackpressureClose(sessionContext, backpressureResult)
				}
				return fmt.Errorf("%w: chat event stream closed without subscription signal", ErrInvalidConfiguration)
			}
			if err := connection.WriteJSON(sessionContext, event); err != nil {
				if subscriptionDropped(subscriptionDone) {
					return waitForBackpressureClose(sessionContext, backpressureResult)
				}
				return err
			}
		case command := <-processed:
			if errors.Is(command.err, application.ErrCommandIDConflict) {
				return connection.CloseCommandIDConflict()
			}
			if command.err != nil {
				return command.err
			}
			if err := connection.WriteJSON(sessionContext, command.result); err != nil {
				if subscriptionDropped(subscriptionDone) {
					return waitForBackpressureClose(sessionContext, backpressureResult)
				}
				return err
			}
		}
	}
}

func (session *RealtimeSession) readCommands(readContext, commandContext context.Context, user auth.User, connection realtimeConnection, processed chan<- processedCommand) {
	for {
		command, err := connection.ReadCommand(readContext)
		if err != nil {
			sendProcessedCommand(readContext, processed, processedCommand{err: err})
			return
		}
		if commandContext.Err() != nil {
			return
		}
		result, err := session.processor.Process(commandContext, user, command)
		if commandContext.Err() != nil {
			return
		}
		if !sendProcessedCommand(readContext, processed, processedCommand{result: result, err: err}) || err != nil {
			return
		}
	}
}

func subscriptionDropped(done <-chan struct{}) bool {
	select {
	case <-done:
		return true
	default:
		return false
	}
}

func waitForBackpressureClose(ctx context.Context, result <-chan error) error {
	select {
	case err := <-result:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func sendProcessedCommand(ctx context.Context, destination chan<- processedCommand, command processedCommand) bool {
	select {
	case destination <- command:
		return true
	case <-ctx.Done():
		return false
	}
}
