package wsapi

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	"buk-yutnori/internal/application"
	"buk-yutnori/internal/auth"
	"buk-yutnori/internal/protocol"
)

// CommandProcessor owns application execution and idempotency across
// authenticated WebSocket connections.
type CommandProcessor interface {
	Process(context.Context, auth.User, protocol.ClientCommand) (protocol.CommandResult, error)
}

// CommandSession reads validated commands and writes their authoritative
// COMMAND_RESULT responses until the connection closes.
type CommandSession struct {
	processor CommandProcessor
}

// NewCommandSession constructs the WebSocket-to-application session loop.
func NewCommandSession(processor CommandProcessor) (*CommandSession, error) {
	if isNilSessionDependency(processor) {
		return nil, fmt.Errorf("%w: command processor is required", ErrInvalidConfiguration)
	}
	return &CommandSession{processor: processor}, nil
}

// Serve implements Session.
func (session *CommandSession) Serve(ctx context.Context, user auth.User, connection *Connection) error {
	if session == nil || session.processor == nil || connection == nil {
		return ErrInvalidConfiguration
	}
	for {
		command, err := connection.ReadCommand(ctx)
		if err != nil {
			return err
		}
		result, err := session.processor.Process(ctx, user, command)
		if errors.Is(err, application.ErrCommandIDConflict) {
			return connection.CloseCommandIDConflict()
		}
		if err != nil {
			return fmt.Errorf("process application command: %w", err)
		}
		if err := connection.WriteJSON(ctx, result); err != nil {
			return err
		}
	}
}

func isNilSessionDependency(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}
