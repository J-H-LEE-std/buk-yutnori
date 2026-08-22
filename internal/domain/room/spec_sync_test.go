package room

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestCanonicalRoomSettingsSpecMatchesDomain(t *testing.T) {
	path := filepath.Join("..", "..", "..", "spec", "room_settings.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", path, err)
	}

	var document roomSettingsDocument
	if err := yaml.Unmarshal(data, &document); err != nil {
		t.Fatalf("Unmarshal(%q) error = %v", path, err)
	}

	if document.Version != 1 {
		t.Fatalf("spec version = %d, want 1", document.Version)
	}
	if document.Defaults != DefaultSettings() {
		t.Fatalf("spec defaults = %+v, domain defaults = %+v", document.Defaults, DefaultSettings())
	}

	assertEqualSlice(t, "max_players", document.Allowed.MaxPlayers, allowedMaxPlayers)
	assertEqualSlice(t, "piece_count", document.Allowed.PieceCounts, allowedPieceCounts)
	assertEqualSlice(t, "capture_extra_throw", document.Allowed.CaptureExtraThrows, allowedCaptureExtraThrows)
	assertEqualSlice(t, "shortcut_policy", document.Allowed.ShortcutPolicies, allowedShortcutPolicies)
	assertEqualSlice(t, "movement_order", document.Allowed.MovementOrders, allowedMovementOrders)
	assertEqualSlice(t, "throw_timeout_seconds", document.Allowed.ThrowTimeoutSeconds, allowedThrowTimeoutSeconds)
	assertEqualSlice(t, "move_timeout_seconds", document.Allowed.MovementTimeoutSeconds, allowedMovementTimeoutSeconds)

	const bukDependency = "random_buk_destination may be true only when buk_mode_enabled is true"
	if !contains(document.Constraints, bukDependency) {
		t.Fatalf("spec constraints do not contain %q: %v", bukDependency, document.Constraints)
	}

	if !document.Creation.Title.Required {
		t.Fatal("spec creation title must be required")
	}
	if document.Creation.Title.MinGraphemes != MinTitleGraphemes {
		t.Fatalf(
			"spec creation title min_graphemes = %d, domain = %d",
			document.Creation.Title.MinGraphemes,
			MinTitleGraphemes,
		)
	}
	if document.Creation.Title.MaxGraphemes != MaxTitleGraphemes {
		t.Fatalf(
			"spec creation title max_graphemes = %d, domain = %d",
			document.Creation.Title.MaxGraphemes,
			MaxTitleGraphemes,
		)
	}
	if document.Creation.Password.Required {
		t.Fatal("spec creation password must be optional")
	}
	if document.Creation.Password.Pattern != RoomPasswordPattern {
		t.Fatalf(
			"spec creation password pattern = %q, domain = %q",
			document.Creation.Password.Pattern,
			RoomPasswordPattern,
		)
	}
}

type roomSettingsDocument struct {
	Version  int      `yaml:"version"`
	Defaults Settings `yaml:"defaults"`
	Allowed  struct {
		MaxPlayers             []int                     `yaml:"max_players"`
		PieceCounts            []int                     `yaml:"piece_count"`
		CaptureExtraThrows     []CaptureExtraThrowPolicy `yaml:"capture_extra_throw"`
		ShortcutPolicies       []ShortcutPolicy          `yaml:"shortcut_policy"`
		MovementOrders         []MovementOrder           `yaml:"movement_order"`
		ThrowTimeoutSeconds    []int                     `yaml:"throw_timeout_seconds"`
		MovementTimeoutSeconds []int                     `yaml:"move_timeout_seconds"`
	} `yaml:"allowed"`
	Constraints []string `yaml:"constraints"`
	Creation    struct {
		Title struct {
			Required     bool `yaml:"required"`
			MinGraphemes int  `yaml:"min_graphemes"`
			MaxGraphemes int  `yaml:"max_graphemes"`
		} `yaml:"title"`
		Password struct {
			Required bool   `yaml:"required"`
			Pattern  string `yaml:"pattern"`
		} `yaml:"password"`
	} `yaml:"creation"`
}

func assertEqualSlice[T any](t *testing.T, field string, got, want []T) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("spec allowed %s = %v, domain = %v", field, got, want)
	}
}
