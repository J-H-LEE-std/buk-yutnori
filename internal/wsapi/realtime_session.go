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
// RoomEventSource subscribes authenticated users to registry room events.
type RoomEventSource interface {
	SubscribeEvents(user auth.UserID) (*application.RoomEventSubscription, error)
}

// Presence receives authenticated WebSocket lifecycle transitions. The
// implementation owns multi-connection reference counting.
type Presence interface {
	ConnectionOpened(user auth.UserID) error
	ConnectionClosed(user auth.UserID) error
}

type RealtimeSession struct {
	processor CommandProcessor
	events    application.ChatEventSource
	lobbies   RoomEventSource
	presence  Presence
}

// SetPresence attaches the authoritative authenticated connection tracker.
// Call before Serve.
func (session *RealtimeSession) SetPresence(presence Presence) error {
	if isNilSessionDependency(presence) {
		return fmt.Errorf("%w: presence tracker is required", ErrInvalidConfiguration)
	}
	session.presence = presence
	return nil
}

// SetLobbyEvents attaches the registry event hub. Call before Serve.
func (session *RealtimeSession) SetLobbyEvents(source RoomEventSource) error {
	if isNilSessionDependency(source) {
		return fmt.Errorf("%w: lobby event source is required", ErrInvalidConfiguration)
	}
	session.lobbies = source
	return nil
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

func (session *RealtimeSession) serve(ctx context.Context, user auth.User, connection realtimeConnection) (serveErr error) {
	if session == nil || session.processor == nil || session.events == nil || isNilSessionDependency(connection) {
		return ErrInvalidConfiguration
	}
	if session.presence != nil {
		if err := session.presence.ConnectionOpened(user.ID); err != nil {
			// A started match deliberately enters its durable storage-pause
			// recovery path when a presence event cannot be persisted. The
			// connection is already registered and must remain available for
			// recovery instead of being torn down and immediately marked absent.
			if !errors.Is(err, application.ErrEventStoreUnavailable) {
				closeErr := session.presence.ConnectionClosed(user.ID)
				return errors.Join(fmt.Errorf("register connection presence: %w", err), closeErr)
			}
		}
		defer func() {
			if err := session.presence.ConnectionClosed(user.ID); err != nil {
				presenceErr := fmt.Errorf("release connection presence: %w", err)
				if serveErr == nil {
					serveErr = presenceErr
				} else {
					serveErr = errors.Join(serveErr, presenceErr)
				}
			}
		}()
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
	defer subscription.Close()

	var lobbyEvents <-chan application.RoomEvent
	var lobbySubscription *application.RoomEventSubscription
	if session.lobbies != nil {
		lobbySubscription, err = session.lobbies.SubscribeEvents(user.ID)
		if err != nil {
			return fmt.Errorf("subscribe room events: %w", err)
		}
		lobbyEvents = lobbySubscription.Events()
	}

	sessionContext, cancelSession := context.WithCancel(ctx)
	commandContext, cancelCommands := context.WithCancel(ctx)
	var lobbyDone <-chan struct{}
	if lobbySubscription != nil {
		lobbyDone = lobbySubscription.Done()
	}
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
		case <-lobbyDone:
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
		if lobbySubscription != nil {
			lobbySubscription.Close()
		}
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
		case lobbyEvent, ok := <-lobbyEvents:
			if !ok {
				lobbyEvents = nil
				continue
			}
			if err := connection.WriteJSON(sessionContext, lobbyEvent.Message); err != nil {
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
