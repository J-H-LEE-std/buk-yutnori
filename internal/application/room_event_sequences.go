package application

import (
	"errors"
	"fmt"
	"math"
	"sync"

	"buk-yutnori/internal/domain"
)

var (
	// ErrInvalidRoomEventSequence reports a sequence operation without a valid room.
	ErrInvalidRoomEventSequence = errors.New("invalid room event sequence")
	// ErrRoomEventSequenceExhausted reports that a room cannot issue another sequence.
	ErrRoomEventSequenceExhausted = errors.New("room event sequence exhausted")
)

// RoomEventSequences owns the last committed server-event sequence for each
// live room. Sequence zero is the boundary before a room has committed an
// event; the first committed event receives sequence one.
//
// This boundary does not persist events. A caller that requires durable event
// storage must call CommitNext only after that event has been stored
// successfully and while the room's state transition remains serialized.
type RoomEventSequences struct {
	mutex  sync.Mutex
	values map[domain.RoomID]uint64
}

// NewRoomEventSequences constructs an empty room-scoped sequence registry.
func NewRoomEventSequences() *RoomEventSequences {
	return &RoomEventSequences{values: make(map[domain.RoomID]uint64)}
}

// CommitNext advances and returns a room's event sequence. Calls for the same
// or different rooms may run concurrently.
func (sequences *RoomEventSequences) CommitNext(roomID domain.RoomID) (uint64, error) {
	if err := roomID.Validate(); err != nil {
		return 0, fmt.Errorf("%w: room_id: %v", ErrInvalidRoomEventSequence, err)
	}

	sequences.mutex.Lock()
	defer sequences.mutex.Unlock()
	current := sequences.values[roomID]
	if current == math.MaxUint64 {
		return 0, ErrRoomEventSequenceExhausted
	}
	next := current + 1
	sequences.values[roomID] = next
	return next, nil
}

// Boundary returns the latest committed sequence for a room, or zero when the
// room has not committed an event.
func (sequences *RoomEventSequences) Boundary(roomID domain.RoomID) (uint64, error) {
	if err := roomID.Validate(); err != nil {
		return 0, fmt.Errorf("%w: room_id: %v", ErrInvalidRoomEventSequence, err)
	}

	sequences.mutex.Lock()
	defer sequences.mutex.Unlock()
	return sequences.values[roomID], nil
}

// ForgetClosedRoom releases a closed room's in-memory sequence boundary. The
// room lifecycle owner must stop new event commits and finish any in-flight
// commit before calling this method. Room identifiers are not reused.
func (sequences *RoomEventSequences) ForgetClosedRoom(roomID domain.RoomID) {
	if roomID.Validate() != nil {
		return
	}

	sequences.mutex.Lock()
	defer sequences.mutex.Unlock()
	delete(sequences.values, roomID)
}
