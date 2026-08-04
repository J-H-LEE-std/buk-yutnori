// Package room contains pure domain rules for rooms and their settings.
package room

import (
	"errors"
	"fmt"
	"strings"
)

// CaptureExtraThrowPolicy determines which captures grant an extra throw.
type CaptureExtraThrowPolicy string

const (
	CaptureExtraThrowAlways              CaptureExtraThrowPolicy = "always"
	CaptureExtraThrowDoToGeolPlusSpecial CaptureExtraThrowPolicy = "do_to_geol_plus_special"
	CaptureExtraThrowNone                CaptureExtraThrowPolicy = "none"
)

// ShortcutPolicy determines whether eligible shortcuts are selected or forced.
type ShortcutPolicy string

const (
	ShortcutForced     ShortcutPolicy = "forced"
	ShortcutSelectable ShortcutPolicy = "selectable"
)

// MovementOrder determines how ordinary result tokens may be selected.
type MovementOrder string

const (
	MovementFIFO MovementOrder = "fifo"
	MovementFree MovementOrder = "free"
)

var (
	allowedMaxPlayers         = []int{2, 4, 6, 8}
	allowedPieceCounts        = []int{2, 3, 4}
	allowedCaptureExtraThrows = []CaptureExtraThrowPolicy{
		CaptureExtraThrowAlways,
		CaptureExtraThrowDoToGeolPlusSpecial,
		CaptureExtraThrowNone,
	}
	allowedShortcutPolicies = []ShortcutPolicy{
		ShortcutForced,
		ShortcutSelectable,
	}
	allowedMovementOrders = []MovementOrder{
		MovementFIFO,
		MovementFree,
	}
	allowedThrowTimeoutSeconds    = []int{10, 20, 30}
	allowedMovementTimeoutSeconds = []int{30, 60, 90, 120, 150}
)

// Settings contains the complete canonical rule configuration for one room.
type Settings struct {
	MaxPlayers           int                     `json:"max_players" yaml:"max_players"`
	PieceCount           int                     `json:"piece_count" yaml:"piece_count"`
	StackingEnabled      bool                    `json:"stacking_enabled" yaml:"stacking_enabled"`
	CaptureExtraThrow    CaptureExtraThrowPolicy `json:"capture_extra_throw" yaml:"capture_extra_throw"`
	YutMoExtraThrow      bool                    `json:"yut_mo_extra_throw" yaml:"yut_mo_extra_throw"`
	ShortcutPolicy       ShortcutPolicy          `json:"shortcut_policy" yaml:"shortcut_policy"`
	BackdoEnabled        bool                    `json:"backdo_enabled" yaml:"backdo_enabled"`
	BukModeEnabled       bool                    `json:"buk_mode_enabled" yaml:"buk_mode_enabled"`
	RandomBukDestination bool                    `json:"random_buk_destination" yaml:"random_buk_destination"`
	MovementOrder        MovementOrder           `json:"movement_order" yaml:"movement_order"`
	ThrowTimeoutSeconds  int                     `json:"throw_timeout_seconds" yaml:"throw_timeout_seconds"`
	MoveTimeoutSeconds   int                     `json:"move_timeout_seconds" yaml:"move_timeout_seconds"`
}

// DefaultSettings returns the canonical room defaults.
func DefaultSettings() Settings {
	return Settings{
		MaxPlayers:           8,
		PieceCount:           4,
		StackingEnabled:      true,
		CaptureExtraThrow:    CaptureExtraThrowAlways,
		YutMoExtraThrow:      true,
		ShortcutPolicy:       ShortcutSelectable,
		BackdoEnabled:        true,
		BukModeEnabled:       false,
		RandomBukDestination: false,
		MovementOrder:        MovementFree,
		ThrowTimeoutSeconds:  20,
		MoveTimeoutSeconds:   90,
	}
}

// ErrInvalidSettings identifies a room setting outside the canonical contract.
var ErrInvalidSettings = errors.New("invalid room settings")

// ValidationError reports every invalid room setting found in one pass.
type ValidationError struct {
	Problems []string
}

// Error implements error.
func (e *ValidationError) Error() string {
	return fmt.Sprintf("%v: %s", ErrInvalidSettings, strings.Join(e.Problems, "; "))
}

// Unwrap allows callers to identify room setting validation failures.
func (e *ValidationError) Unwrap() error {
	return ErrInvalidSettings
}

// Validate reports whether settings satisfy the canonical room settings contract.
func (settings Settings) Validate() error {
	var problems []string

	if !contains(allowedMaxPlayers, settings.MaxPlayers) {
		problems = append(problems, fmt.Sprintf("max_players %d is not allowed", settings.MaxPlayers))
	}
	if !contains(allowedPieceCounts, settings.PieceCount) {
		problems = append(problems, fmt.Sprintf("piece_count %d is not allowed", settings.PieceCount))
	}
	if !contains(allowedCaptureExtraThrows, settings.CaptureExtraThrow) {
		problems = append(
			problems,
			fmt.Sprintf("capture_extra_throw %q is not allowed", settings.CaptureExtraThrow),
		)
	}
	if !contains(allowedShortcutPolicies, settings.ShortcutPolicy) {
		problems = append(problems, fmt.Sprintf("shortcut_policy %q is not allowed", settings.ShortcutPolicy))
	}
	if !contains(allowedMovementOrders, settings.MovementOrder) {
		problems = append(problems, fmt.Sprintf("movement_order %q is not allowed", settings.MovementOrder))
	}
	if !contains(allowedThrowTimeoutSeconds, settings.ThrowTimeoutSeconds) {
		problems = append(
			problems,
			fmt.Sprintf("throw_timeout_seconds %d is not allowed", settings.ThrowTimeoutSeconds),
		)
	}
	if !contains(allowedMovementTimeoutSeconds, settings.MoveTimeoutSeconds) {
		problems = append(
			problems,
			fmt.Sprintf("move_timeout_seconds %d is not allowed", settings.MoveTimeoutSeconds),
		)
	}
	if settings.RandomBukDestination && !settings.BukModeEnabled {
		problems = append(
			problems,
			"random_buk_destination requires buk_mode_enabled",
		)
	}

	if len(problems) > 0 {
		return &ValidationError{Problems: problems}
	}
	return nil
}

func contains[T comparable](values []T, candidate T) bool {
	for _, value := range values {
		if value == candidate {
			return true
		}
	}
	return false
}
