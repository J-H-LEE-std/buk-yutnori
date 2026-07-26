package domain

import "errors"

var (
	// ErrInvalidID identifies an empty opaque domain identifier.
	ErrInvalidID = errors.New("invalid domain ID")

	// ErrInvalidEnumValue identifies a value outside a canonical enum.
	ErrInvalidEnumValue = errors.New("invalid domain enum value")
)
