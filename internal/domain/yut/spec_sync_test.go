package yut

import (
	"bytes"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"buk-yutnori/internal/domain"

	"gopkg.in/yaml.v3"
)

func TestCanonicalProbabilitySpecMatchesDomainWeights(t *testing.T) {
	path := filepath.Join("..", "..", "..", "spec", "yut_probabilities.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", path, err)
	}

	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	var document probabilityDocument
	if err := decoder.Decode(&document); err != nil {
		t.Fatalf("Decode(%q) error = %v", path, err)
	}
	var trailing yaml.Node
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		t.Fatalf("Decode trailing %q error = %v, want EOF", path, err)
	}
	if document.Version != 1 {
		t.Fatalf("spec version = %d, want 1", document.Version)
	}
	if len(document.Modes) != 4 {
		t.Fatalf("spec mode count = %d, want 4", len(document.Modes))
	}
	if document.Sampling.Authority != "server" ||
		!document.Sampling.TestSeedSupported ||
		document.Sampling.LogLevel != "final_result" ||
		document.Sampling.Method != "normalized_weighted_categorical" {
		t.Fatalf("unexpected sampling policy: %+v", document.Sampling)
	}

	tests := []struct {
		name     string
		modeName string
		mode     Mode
	}{
		{name: "no backdo or Buk", modeName: "no_backdo_no_buk", mode: Mode{}},
		{name: "backdo only", modeName: "backdo_only", mode: Mode{BackdoEnabled: true}},
		{name: "Buk only", modeName: "buk_only", mode: Mode{BukModeEnabled: true}},
		{name: "backdo and Buk", modeName: "backdo_and_buk", mode: Mode{BackdoEnabled: true, BukModeEnabled: true}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			modeWeights, ok := document.Modes[test.modeName]
			if !ok {
				t.Fatalf("spec mode %q is missing", test.modeName)
			}
			want := map[domain.YutResult]uint64{
				domain.YutDo:     hundredths(t, modeWeights.Do),
				domain.YutGae:    hundredths(t, document.Common.Gae),
				domain.YutGeol:   hundredths(t, document.Common.Geol),
				domain.YutYut:    hundredths(t, document.Common.Yut),
				domain.YutMo:     hundredths(t, document.Common.Mo),
				domain.YutBackdo: hundredths(t, modeWeights.Backdo),
				domain.YutBuk:    hundredths(t, modeWeights.Buk),
			}
			got := canonicalWeights(test.mode)
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("domain weights = %v, spec = %v", got, want)
			}

			var total uint64
			for _, weight := range got {
				total += weight
			}
			if total != canonicalTotalWeight {
				t.Fatalf("total weight = %d, want %d", total, canonicalTotalWeight)
			}
		})
	}
}

func hundredths(t *testing.T, value float64) uint64 {
	t.Helper()
	scaled := value * 100
	rounded := math.Round(scaled)
	if value < 0 || math.Abs(scaled-rounded) > 1e-9 {
		t.Fatalf("probability %v is not a non-negative hundredth", value)
	}
	return uint64(rounded)
}

type probabilityDocument struct {
	Version     int    `yaml:"version"`
	Description string `yaml:"description"`
	Common      struct {
		Gae  float64 `yaml:"gae"`
		Geol float64 `yaml:"geol"`
		Yut  float64 `yaml:"yut"`
		Mo   float64 `yaml:"mo"`
	} `yaml:"common"`
	Modes map[string]struct {
		Do     float64 `yaml:"do"`
		Backdo float64 `yaml:"backdo"`
		Buk    float64 `yaml:"buk"`
	} `yaml:"modes"`
	Sampling struct {
		Authority         string `yaml:"authority"`
		TestSeedSupported bool   `yaml:"test_seed_supported"`
		LogLevel          string `yaml:"log_level"`
		Method            string `yaml:"method"`
	} `yaml:"sampling"`
}
