package room

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestDefaultSettingsAreValid(t *testing.T) {
	settings := DefaultSettings()

	if err := settings.Validate(); err != nil {
		t.Fatalf("DefaultSettings().Validate() error = %v", err)
	}

	want := Settings{
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
	if settings != want {
		t.Fatalf("DefaultSettings() = %+v, want %+v", settings, want)
	}
}

func TestSettingsUseCanonicalJSONFieldNames(t *testing.T) {
	data, err := json.Marshal(DefaultSettings())
	if err != nil {
		t.Fatalf("Marshal(DefaultSettings()) error = %v", err)
	}

	want := `{"max_players":8,"piece_count":4,"stacking_enabled":true,"capture_extra_throw":"always","yut_mo_extra_throw":true,"shortcut_policy":"selectable","backdo_enabled":true,"buk_mode_enabled":false,"random_buk_destination":false,"movement_order":"free","throw_timeout_seconds":20,"move_timeout_seconds":90}`
	if string(data) != want {
		t.Fatalf("Marshal(DefaultSettings()) = %s, want %s", data, want)
	}
}

func TestAllCanonicalSettingsCombinations(t *testing.T) {
	booleans := []bool{false, true}
	checked := 0

	for _, maxPlayers := range allowedMaxPlayers {
		for _, pieceCount := range allowedPieceCounts {
			for _, captureExtraThrow := range allowedCaptureExtraThrows {
				for _, shortcutPolicy := range allowedShortcutPolicies {
					for _, movementOrder := range allowedMovementOrders {
						for _, throwTimeout := range allowedThrowTimeoutSeconds {
							for _, movementTimeout := range allowedMovementTimeoutSeconds {
								for _, stackingEnabled := range booleans {
									for _, yutMoExtraThrow := range booleans {
										for _, backdoEnabled := range booleans {
											for _, bukModeEnabled := range booleans {
												for _, randomBukDestination := range booleans {
													settings := Settings{
														MaxPlayers:           maxPlayers,
														PieceCount:           pieceCount,
														StackingEnabled:      stackingEnabled,
														CaptureExtraThrow:    captureExtraThrow,
														YutMoExtraThrow:      yutMoExtraThrow,
														ShortcutPolicy:       shortcutPolicy,
														BackdoEnabled:        backdoEnabled,
														BukModeEnabled:       bukModeEnabled,
														RandomBukDestination: randomBukDestination,
														MovementOrder:        movementOrder,
														ThrowTimeoutSeconds:  throwTimeout,
														MoveTimeoutSeconds:   movementTimeout,
													}

													err := settings.Validate()
													wantValid := bukModeEnabled || !randomBukDestination
													if wantValid && err != nil {
														t.Fatalf("valid settings rejected: %+v: %v", settings, err)
													}
													if !wantValid && !errors.Is(err, ErrInvalidSettings) {
														t.Fatalf("invalid Buk dependency error = %v, want ErrInvalidSettings", err)
													}
													checked++
												}
											}
										}
									}
								}
							}
						}
					}
				}
			}
		}
	}

	if checked != 69120 {
		t.Fatalf("checked %d combinations, want 69120", checked)
	}
}

func TestSettingsRejectInvalidValues(t *testing.T) {
	tests := []struct {
		name   string
		field  string
		mutate func(*Settings)
	}{
		{name: "max players", field: "max_players", mutate: func(value *Settings) { value.MaxPlayers = 3 }},
		{name: "piece count", field: "piece_count", mutate: func(value *Settings) { value.PieceCount = 5 }},
		{name: "capture extra throw", field: "capture_extra_throw", mutate: func(value *Settings) { value.CaptureExtraThrow = "sometimes" }},
		{name: "shortcut policy", field: "shortcut_policy", mutate: func(value *Settings) { value.ShortcutPolicy = "none" }},
		{name: "movement order", field: "movement_order", mutate: func(value *Settings) { value.MovementOrder = "random" }},
		{name: "throw timeout", field: "throw_timeout_seconds", mutate: func(value *Settings) { value.ThrowTimeoutSeconds = 15 }},
		{name: "movement timeout", field: "move_timeout_seconds", mutate: func(value *Settings) { value.MoveTimeoutSeconds = 45 }},
		{
			name:  "random Buk without Buk mode",
			field: "random_buk_destination",
			mutate: func(value *Settings) {
				value.BukModeEnabled = false
				value.RandomBukDestination = true
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			settings := DefaultSettings()
			test.mutate(&settings)

			err := settings.Validate()
			if !errors.Is(err, ErrInvalidSettings) {
				t.Fatalf("Validate() error = %v, want ErrInvalidSettings", err)
			}
			if !strings.Contains(err.Error(), test.field) {
				t.Fatalf("Validate() error = %q, want field %q", err, test.field)
			}
		})
	}
}

func TestSettingsValidationReportsAllProblems(t *testing.T) {
	settings := DefaultSettings()
	settings.MaxPlayers = 5
	settings.PieceCount = 1
	settings.CaptureExtraThrow = "invalid"
	settings.ShortcutPolicy = "invalid"
	settings.MovementOrder = "invalid"
	settings.ThrowTimeoutSeconds = 25
	settings.MoveTimeoutSeconds = 15
	settings.RandomBukDestination = true

	err := settings.Validate()
	var validationError *ValidationError
	if !errors.As(err, &validationError) {
		t.Fatalf("Validate() error = %v, want *ValidationError", err)
	}
	if got, want := len(validationError.Problems), 8; got != want {
		t.Fatalf("len(Problems) = %d, want %d: %v", got, want, validationError.Problems)
	}
}
