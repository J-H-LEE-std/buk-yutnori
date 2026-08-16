package protocol

import (
	"errors"
	"fmt"

	"buk-yutnori/internal/domain"
)

const (
	DirectionServerResponse Direction = "server_response"
	ResponseCommandResult             = "COMMAND_RESULT"

	CommandAccepted CommandStatus = "accepted"
	CommandRejected CommandStatus = "rejected"
)

var ErrInvalidCommandOutcome = errors.New("invalid command outcome")

// CommandStatus reports whether the authoritative application accepted a
// client command.
type CommandStatus string

// CommandError is the public, non-sensitive rejection attached to a command
// result.
type CommandError struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Retriable bool   `json:"retriable"`
}

// CommandOutcome is the application-owned decision used to construct a
// protocol response. A sequence range identifies events committed by the
// first execution of the command.
type CommandOutcome struct {
	Status             CommandStatus
	EventSequenceStart *uint64
	EventSequenceEnd   *uint64
	Error              *CommandError
}

// CommandResultPayload is the v1 COMMAND_RESULT payload.
type CommandResultPayload struct {
	Status             CommandStatus `json:"status"`
	EventSequenceStart *uint64       `json:"event_sequence_start"`
	EventSequenceEnd   *uint64       `json:"event_sequence_end"`
	Error              *CommandError `json:"error"`
}

// CommandResult is the typed v1 server response for one client command.
// Sequence is always null because only server events consume sequence values.
type CommandResult struct {
	Version   int                  `json:"version"`
	Direction Direction            `json:"direction"`
	Type      string               `json:"type"`
	RequestID *string              `json:"request_id"`
	CommandID string               `json:"command_id"`
	Sequence  *uint64              `json:"sequence"`
	RoomID    domain.RoomID        `json:"room_id"`
	MatchID   *domain.MatchID      `json:"match_id"`
	Payload   CommandResultPayload `json:"payload"`
}

// NewCommandResult binds a validated application outcome to the immutable
// routing identifiers from the original client command.
func NewCommandResult(command ClientCommand, outcome CommandOutcome) (CommandResult, error) {
	if err := validateResultCommand(command); err != nil {
		return CommandResult{}, err
	}
	if err := validateCommandOutcome(outcome); err != nil {
		return CommandResult{}, err
	}
	return CommandResult{
		Version:   Version1,
		Direction: DirectionServerResponse,
		Type:      ResponseCommandResult,
		RequestID: clonePointer(command.RequestID),
		CommandID: command.CommandID,
		Sequence:  nil,
		RoomID:    command.RoomID,
		MatchID:   clonePointer(command.MatchID),
		Payload: CommandResultPayload{
			Status:             outcome.Status,
			EventSequenceStart: clonePointer(outcome.EventSequenceStart),
			EventSequenceEnd:   clonePointer(outcome.EventSequenceEnd),
			Error:              clonePointer(outcome.Error),
		},
	}, nil
}

// Clone returns a result whose optional fields can be safely owned by another
// caller without mutating a retained idempotency record.
func (result CommandResult) Clone() CommandResult {
	result.RequestID = clonePointer(result.RequestID)
	result.Sequence = clonePointer(result.Sequence)
	result.MatchID = clonePointer(result.MatchID)
	result.Payload.EventSequenceStart = clonePointer(result.Payload.EventSequenceStart)
	result.Payload.EventSequenceEnd = clonePointer(result.Payload.EventSequenceEnd)
	result.Payload.Error = clonePointer(result.Payload.Error)
	return result
}

func validateResultCommand(command ClientCommand) error {
	if command.Version != Version1 || command.Direction != DirectionClientCommand || command.Type == "" || command.CommandID == "" {
		return fmt.Errorf("%w: invalid command envelope", ErrInvalidClientCommand)
	}
	if err := command.RoomID.Validate(); err != nil {
		return fmt.Errorf("%w: room_id: %v", ErrInvalidClientCommand, err)
	}
	if command.RequestID != nil && *command.RequestID == "" {
		return fmt.Errorf("%w: empty request_id", ErrInvalidClientCommand)
	}
	if command.MatchID != nil {
		if err := command.MatchID.Validate(); err != nil {
			return fmt.Errorf("%w: match_id: %v", ErrInvalidClientCommand, err)
		}
	}
	return nil
}

func validateCommandOutcome(outcome CommandOutcome) error {
	if (outcome.EventSequenceStart == nil) != (outcome.EventSequenceEnd == nil) {
		return fmt.Errorf("%w: sequence range must contain both bounds", ErrInvalidCommandOutcome)
	}
	if outcome.EventSequenceStart != nil && *outcome.EventSequenceStart > *outcome.EventSequenceEnd {
		return fmt.Errorf("%w: reversed sequence range", ErrInvalidCommandOutcome)
	}
	switch outcome.Status {
	case CommandAccepted:
		if outcome.Error != nil {
			return fmt.Errorf("%w: accepted result must not contain an error", ErrInvalidCommandOutcome)
		}
	case CommandRejected:
		if outcome.Error == nil || outcome.Error.Code == "" || outcome.Error.Message == "" {
			return fmt.Errorf("%w: rejected result requires a public error", ErrInvalidCommandOutcome)
		}
	default:
		return fmt.Errorf("%w: unknown status %q", ErrInvalidCommandOutcome, outcome.Status)
	}
	return nil
}

func clonePointer[T any](value *T) *T {
	if value == nil {
		return nil
	}
	clone := *value
	return &clone
}
