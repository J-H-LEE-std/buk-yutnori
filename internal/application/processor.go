// Package application coordinates authenticated client commands without
// owning transport, persistence, or game-rule state.
package application

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sync"

	"buk-yutnori/internal/auth"
	"buk-yutnori/internal/domain"
	"buk-yutnori/internal/protocol"
)

var (
	ErrInvalidConfiguration = errors.New("invalid application configuration")
	ErrInvalidCommand       = errors.New("invalid application command")
	ErrCommandIDConflict    = errors.New("command_id reused with different command content")
)

// Executor applies one command to the authoritative application and returns
// the outcome produced by that first execution.
type Executor interface {
	Execute(context.Context, auth.User, protocol.ClientCommand) (protocol.CommandOutcome, error)
}

// Processor provides room-lifetime idempotency and single execution for
// authenticated (user_id, command_id) keys.
type Processor struct {
	mu       sync.Mutex
	executor Executor
	entries  map[commandKey]*commandEntry
}

type commandKey struct {
	userID    auth.UserID
	commandID string
}

type commandEntry struct {
	roomID      domain.RoomID
	fingerprint []byte
	ready       chan struct{}
	result      protocol.CommandResult
	err         error
	completed   bool
	evict       bool
}

// NewProcessor constructs an empty idempotency registry around executor.
func NewProcessor(executor Executor) (*Processor, error) {
	if isNilInterface(executor) {
		return nil, fmt.Errorf("%w: executor is required", ErrInvalidConfiguration)
	}
	return &Processor{executor: executor, entries: make(map[commandKey]*commandEntry)}, nil
}

// Process executes a new authenticated command or replays the first retained
// result. Concurrent duplicates wait for the same in-flight execution.
func (processor *Processor) Process(ctx context.Context, user auth.User, command protocol.ClientCommand) (protocol.CommandResult, error) {
	if processor == nil || processor.executor == nil {
		return protocol.CommandResult{}, fmt.Errorf("%w: processor is required", ErrInvalidConfiguration)
	}
	if err := ctx.Err(); err != nil {
		return protocol.CommandResult{}, err
	}
	if err := user.ID.Validate(); err != nil {
		return protocol.CommandResult{}, fmt.Errorf("%w: authenticated user_id", ErrInvalidCommand)
	}
	fingerprint, err := commandFingerprint(command)
	if err != nil {
		return protocol.CommandResult{}, err
	}
	key := commandKey{userID: user.ID, commandID: command.CommandID}

	processor.mu.Lock()
	if existing, ok := processor.entries[key]; ok {
		if !bytes.Equal(existing.fingerprint, fingerprint) {
			processor.mu.Unlock()
			return protocol.CommandResult{}, ErrCommandIDConflict
		}
		ready := existing.ready
		processor.mu.Unlock()
		select {
		case <-ready:
			return existing.result.Clone(), existing.err
		case <-ctx.Done():
			return protocol.CommandResult{}, ctx.Err()
		}
	}
	entry := &commandEntry{
		roomID:      command.RoomID,
		fingerprint: append([]byte(nil), fingerprint...),
		ready:       make(chan struct{}),
	}
	processor.entries[key] = entry
	processor.mu.Unlock()

	outcome, executionErr := processor.executor.Execute(ctx, user, cloneCommand(command))
	var result protocol.CommandResult
	if executionErr == nil {
		result, executionErr = protocol.NewCommandResult(command, outcome)
	}
	retain := executionErr == nil && shouldRetain(result)

	processor.mu.Lock()
	entry.result = result.Clone()
	entry.err = executionErr
	entry.completed = true
	if !retain || entry.evict {
		if processor.entries[key] == entry {
			delete(processor.entries, key)
		}
	}
	close(entry.ready)
	processor.mu.Unlock()
	return result.Clone(), executionErr
}

// ForgetClosedRoom removes idempotency records after the room lifecycle owner
// has atomically stopped all new command intake for roomID. An already
// in-flight record remains visible to duplicates until its one execution
// finishes, then is evicted. Calling Process for that room after this method
// violates the lifecycle-owner contract.
func (processor *Processor) ForgetClosedRoom(roomID domain.RoomID) {
	if processor == nil {
		return
	}
	processor.mu.Lock()
	defer processor.mu.Unlock()
	for key, entry := range processor.entries {
		if entry.roomID != roomID {
			continue
		}
		if entry.completed {
			delete(processor.entries, key)
		} else {
			entry.evict = true
		}
	}
}

func shouldRetain(result protocol.CommandResult) bool {
	if result.Payload.Status == protocol.CommandAccepted {
		return true
	}
	return result.Payload.Status == protocol.CommandRejected &&
		result.Payload.Error != nil && !result.Payload.Error.Retriable
}

func commandFingerprint(command protocol.ClientCommand) ([]byte, error) {
	if command.Version != protocol.Version1 || command.Direction != protocol.DirectionClientCommand || command.Type == "" || command.CommandID == "" || command.Payload == nil {
		return nil, fmt.Errorf("%w: incomplete command envelope", ErrInvalidCommand)
	}
	if err := command.RoomID.Validate(); err != nil {
		return nil, fmt.Errorf("%w: room_id: %v", ErrInvalidCommand, err)
	}
	if command.RequestID != nil && *command.RequestID == "" {
		return nil, fmt.Errorf("%w: empty request_id", ErrInvalidCommand)
	}
	if command.MatchID != nil {
		if err := command.MatchID.Validate(); err != nil {
			return nil, fmt.Errorf("%w: match_id: %v", ErrInvalidCommand, err)
		}
	}
	normalized := struct {
		Version   int                  `json:"version"`
		Direction protocol.Direction   `json:"direction"`
		Type      protocol.CommandType `json:"type"`
		RequestID *string              `json:"request_id"`
		CommandID string               `json:"command_id"`
		RoomID    domain.RoomID        `json:"room_id"`
		MatchID   *domain.MatchID      `json:"match_id"`
		Payload   any                  `json:"payload"`
	}{
		Version: command.Version, Direction: command.Direction, Type: command.Type,
		RequestID: command.RequestID, CommandID: command.CommandID, RoomID: command.RoomID,
		MatchID: command.MatchID, Payload: command.Payload,
	}
	encoded, err := json.Marshal(normalized)
	if err != nil {
		return nil, fmt.Errorf("%w: encode normalized command: %v", ErrInvalidCommand, err)
	}
	return encoded, nil
}

func cloneCommand(command protocol.ClientCommand) protocol.ClientCommand {
	if command.RequestID != nil {
		requestID := *command.RequestID
		command.RequestID = &requestID
	}
	if command.MatchID != nil {
		matchID := *command.MatchID
		command.MatchID = &matchID
	}
	return command
}

func isNilInterface(value any) bool {
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
