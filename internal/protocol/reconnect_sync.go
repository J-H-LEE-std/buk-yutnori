package protocol

import (
	"encoding/json"
	"errors"
	"fmt"

	"buk-yutnori/internal/domain"
)

const ErrorCodeResyncRequired = "RESYNC_REQUIRED"

var ErrInvalidReconnectSynchronization = errors.New("invalid reconnect synchronization")

// ReconnectSynchronization contains one atomic game snapshot and the
// contiguous room events committed after its sequence boundary.
type ReconnectSynchronization struct {
	Snapshot json.RawMessage   `json:"snapshot"`
	Events   []json.RawMessage `json:"events"`
}

type reconnectSnapshotScope struct {
	RoomID   domain.RoomID  `json:"room_id"`
	MatchID  domain.MatchID `json:"match_id"`
	Sequence uint64         `json:"sequence"`
}

type reconnectEventScope struct {
	Version   int             `json:"version"`
	Direction Direction       `json:"direction"`
	Type      string          `json:"type"`
	Sequence  uint64          `json:"sequence"`
	RoomID    domain.RoomID   `json:"room_id"`
	MatchID   *domain.MatchID `json:"match_id"`
}

// NewReconnectSynchronization validates routing and sequence continuity while
// preserving the complete schema-validated snapshot and event JSON values.
func NewReconnectSynchronization(command ClientCommand, snapshot json.RawMessage, events []json.RawMessage) (ReconnectSynchronization, error) {
	synchronization := ReconnectSynchronization{
		Snapshot: cloneRawMessage(snapshot),
		Events:   cloneRawMessages(events),
	}
	if err := validateReconnectSynchronization(command, synchronization); err != nil {
		return ReconnectSynchronization{}, err
	}
	return synchronization, nil
}

// Clone returns synchronization bytes owned independently by the caller.
func (synchronization ReconnectSynchronization) Clone() ReconnectSynchronization {
	return ReconnectSynchronization{
		Snapshot: cloneRawMessage(synchronization.Snapshot),
		Events:   cloneRawMessages(synchronization.Events),
	}
}

func validateReconnectSynchronization(command ClientCommand, synchronization ReconnectSynchronization) error {
	if err := validateResultCommand(command); err != nil {
		return invalidReconnectSynchronization("invalid command envelope: %v", err)
	}
	if command.Type != CommandReconnect {
		return invalidReconnectSynchronization("command type must be RECONNECT")
	}
	if command.MatchID == nil || command.MatchID.Validate() != nil {
		return invalidReconnectSynchronization("match_id is required")
	}
	payload, ok := command.Payload.(ReconnectPayload)
	if !ok {
		return invalidReconnectSynchronization("invalid RECONNECT payload")
	}

	snapshot, err := decodeReconnectSnapshotScope(synchronization.Snapshot)
	if err != nil {
		return err
	}
	if snapshot.RoomID != command.RoomID {
		return invalidReconnectSynchronization("snapshot room_id differs from command")
	}
	if snapshot.MatchID != *command.MatchID {
		return invalidReconnectSynchronization("snapshot match_id differs from command")
	}
	if snapshot.Sequence < payload.LastSequence {
		return invalidReconnectSynchronization("snapshot sequence would roll back client state")
	}

	expectedSequence := snapshot.Sequence
	for index, rawEvent := range synchronization.Events {
		if expectedSequence == ^uint64(0) {
			return invalidReconnectSynchronization("event sequence overflow")
		}
		expectedSequence++
		event, eventErr := decodeReconnectEventScope(rawEvent)
		if eventErr != nil {
			return eventErr
		}
		if event.Sequence != expectedSequence {
			return invalidReconnectSynchronization("event %d sequence is not contiguous", index)
		}
		if event.RoomID != snapshot.RoomID {
			return invalidReconnectSynchronization("event %d room_id differs from snapshot", index)
		}
		if event.MatchID != nil && *event.MatchID != snapshot.MatchID {
			return invalidReconnectSynchronization("event %d match_id differs from snapshot", index)
		}
	}
	return nil
}

func decodeReconnectSnapshotScope(raw json.RawMessage) (reconnectSnapshotScope, error) {
	if len(raw) == 0 {
		return reconnectSnapshotScope{}, invalidReconnectSynchronization("snapshot is required")
	}
	if err := rejectDuplicateObjectKeys(raw); err != nil {
		return reconnectSnapshotScope{}, invalidReconnectSynchronization("invalid snapshot: %v", err)
	}
	var snapshot reconnectSnapshotScope
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		return reconnectSnapshotScope{}, invalidReconnectSynchronization("decode snapshot: %v", err)
	}
	if snapshot.RoomID.Validate() != nil || snapshot.MatchID.Validate() != nil || snapshot.Sequence == 0 {
		return reconnectSnapshotScope{}, invalidReconnectSynchronization("snapshot routing and sequence are required")
	}
	return snapshot, nil
}

func decodeReconnectEventScope(raw json.RawMessage) (reconnectEventScope, error) {
	if len(raw) == 0 {
		return reconnectEventScope{}, invalidReconnectSynchronization("event is required")
	}
	if err := rejectDuplicateObjectKeys(raw); err != nil {
		return reconnectEventScope{}, invalidReconnectSynchronization("invalid event: %v", err)
	}
	var event reconnectEventScope
	if err := json.Unmarshal(raw, &event); err != nil {
		return reconnectEventScope{}, invalidReconnectSynchronization("decode event: %v", err)
	}
	if event.Version != Version1 || event.Direction != DirectionServerEvent || event.Type == "" || event.Sequence == 0 || event.RoomID.Validate() != nil {
		return reconnectEventScope{}, invalidReconnectSynchronization("invalid event envelope")
	}
	if event.MatchID != nil && event.MatchID.Validate() != nil {
		return reconnectEventScope{}, invalidReconnectSynchronization("invalid event match_id")
	}
	return event, nil
}

func cloneRawMessage(raw json.RawMessage) json.RawMessage {
	return append(json.RawMessage(nil), raw...)
}

func cloneRawMessages(values []json.RawMessage) []json.RawMessage {
	if values == nil {
		return []json.RawMessage{}
	}
	cloned := make([]json.RawMessage, len(values))
	for index := range values {
		cloned[index] = cloneRawMessage(values[index])
	}
	return cloned
}

func invalidReconnectSynchronization(format string, arguments ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidReconnectSynchronization, fmt.Sprintf(format, arguments...))
}
