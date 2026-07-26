package domain

import "fmt"

// PlayerID is the stable identifier of a player in game state.
type PlayerID string

// PieceID is the stable identifier of a team-owned piece.
type PieceID string

// SpaceID is the stable identifier of a logical board space.
type SpaceID string

// RoomID is the stable identifier of a room.
type RoomID string

// MatchID is the stable identifier of a match.
type MatchID string

func validateID(kind, value string) error {
	if value == "" {
		return fmt.Errorf("%w: %s must not be empty", ErrInvalidID, kind)
	}
	return nil
}

// Validate reports whether id satisfies the shared schema constraint.
func (id PlayerID) Validate() error {
	return validateID("PlayerID", string(id))
}

// String returns the identifier unchanged.
func (id PlayerID) String() string {
	return string(id)
}

// MarshalJSON encodes id as a validated JSON string.
func (id PlayerID) MarshalJSON() ([]byte, error) {
	return marshalValidatedString(id, PlayerID.Validate)
}

// UnmarshalJSON decodes and validates a JSON string.
func (id *PlayerID) UnmarshalJSON(data []byte) error {
	value, err := unmarshalValidatedString(data, PlayerID.Validate)
	if err != nil {
		return err
	}
	*id = value
	return nil
}

// Validate reports whether id satisfies the shared schema constraint.
func (id PieceID) Validate() error {
	return validateID("PieceID", string(id))
}

// String returns the identifier unchanged.
func (id PieceID) String() string {
	return string(id)
}

// MarshalJSON encodes id as a validated JSON string.
func (id PieceID) MarshalJSON() ([]byte, error) {
	return marshalValidatedString(id, PieceID.Validate)
}

// UnmarshalJSON decodes and validates a JSON string.
func (id *PieceID) UnmarshalJSON(data []byte) error {
	value, err := unmarshalValidatedString(data, PieceID.Validate)
	if err != nil {
		return err
	}
	*id = value
	return nil
}

// Validate reports whether id satisfies the shared schema constraint.
func (id SpaceID) Validate() error {
	return validateID("SpaceID", string(id))
}

// String returns the identifier unchanged.
func (id SpaceID) String() string {
	return string(id)
}

// MarshalJSON encodes id as a validated JSON string.
func (id SpaceID) MarshalJSON() ([]byte, error) {
	return marshalValidatedString(id, SpaceID.Validate)
}

// UnmarshalJSON decodes and validates a JSON string.
func (id *SpaceID) UnmarshalJSON(data []byte) error {
	value, err := unmarshalValidatedString(data, SpaceID.Validate)
	if err != nil {
		return err
	}
	*id = value
	return nil
}

// Validate reports whether id satisfies the shared schema constraint.
func (id RoomID) Validate() error {
	return validateID("RoomID", string(id))
}

// String returns the identifier unchanged.
func (id RoomID) String() string {
	return string(id)
}

// MarshalJSON encodes id as a validated JSON string.
func (id RoomID) MarshalJSON() ([]byte, error) {
	return marshalValidatedString(id, RoomID.Validate)
}

// UnmarshalJSON decodes and validates a JSON string.
func (id *RoomID) UnmarshalJSON(data []byte) error {
	value, err := unmarshalValidatedString(data, RoomID.Validate)
	if err != nil {
		return err
	}
	*id = value
	return nil
}

// Validate reports whether id satisfies the shared schema constraint.
func (id MatchID) Validate() error {
	return validateID("MatchID", string(id))
}

// String returns the identifier unchanged.
func (id MatchID) String() string {
	return string(id)
}

// MarshalJSON encodes id as a validated JSON string.
func (id MatchID) MarshalJSON() ([]byte, error) {
	return marshalValidatedString(id, MatchID.Validate)
}

// UnmarshalJSON decodes and validates a JSON string.
func (id *MatchID) UnmarshalJSON(data []byte) error {
	value, err := unmarshalValidatedString(data, MatchID.Validate)
	if err != nil {
		return err
	}
	*id = value
	return nil
}
