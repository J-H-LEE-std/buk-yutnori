package cpu

import "errors"

var (
	// ErrInvalidPolicyConfig identifies a missing or invalid immutable dependency.
	ErrInvalidPolicyConfig = errors.New("invalid CPU policy configuration")

	// ErrInvalidDecisionInput identifies malformed match, turn, or team input.
	ErrInvalidDecisionInput = errors.New("invalid CPU decision input")

	// ErrNoResultToken identifies a decision request without a queued result.
	ErrNoResultToken = errors.New("no result token for CPU decision")

	// ErrInvalidMovePlans identifies inconsistent read-only plans from the match engine.
	ErrInvalidMovePlans = errors.New("invalid CPU move plans")

	// ErrRandomSourceOutOfRange identifies a source that violated BoundedSource.
	ErrRandomSourceOutOfRange = errors.New("CPU random source returned an out-of-range value")
)
