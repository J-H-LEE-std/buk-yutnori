package turn

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"buk-yutnori/internal/domain"
	"buk-yutnori/internal/domain/room"

	"gopkg.in/yaml.v3"
)

func TestCanonicalQueueSpecMatchesDomain(t *testing.T) {
	path := filepath.Join("..", "..", "..", "spec", "turn_state_machine.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", path, err)
	}

	var document turnSpecDocument
	if err := yaml.Unmarshal(data, &document); err != nil {
		t.Fatalf("Unmarshal(%q) error = %v", path, err)
	}
	if document.Version != 1 {
		t.Fatalf("spec version = %d, want 1", document.Version)
	}
	if !document.Queue.Token.StableIDRequired {
		t.Fatal("spec does not require stable token IDs")
	}
	wantResults := []domain.YutResult{
		domain.YutDo,
		domain.YutGae,
		domain.YutGeol,
		domain.YutYut,
		domain.YutMo,
		domain.YutBackdo,
		domain.YutBuk,
	}
	wantOrigins := []domain.ResultOrigin{
		domain.ResultOriginInitialThrow,
		domain.ResultOriginYutExtra,
		domain.ResultOriginMoExtra,
		domain.ResultOriginCaptureExtra,
	}
	if !reflect.DeepEqual(document.Queue.Token.ResultValues, wantResults) {
		t.Fatalf("spec results = %v, want %v", document.Queue.Token.ResultValues, wantResults)
	}
	if !reflect.DeepEqual(document.Queue.Token.Origins, wantOrigins) {
		t.Fatalf("spec origins = %v, want %v", document.Queue.Token.Origins, wantOrigins)
	}
	if document.Queue.BaseOrder != room.MovementFIFO {
		t.Fatalf("base order = %q, want %q", document.Queue.BaseOrder, room.MovementFIFO)
	}
	freeRule := strings.ToLower(document.Queue.FreeMode.Rule)
	if !strings.Contains(freeRule, "first buk token") || !strings.Contains(freeRule, "ordering barrier") {
		t.Fatalf("unexpected free-mode rule: %q", document.Queue.FreeMode.Rule)
	}
	if document.Queue.UnusableOrdinaryToken != "discard_only_that_token" {
		t.Fatalf("unusable ordinary token policy = %q", document.Queue.UnusableOrdinaryToken)
	}
	if document.ExtraThrow.AppendPosition != "queue_tail" {
		t.Fatalf("extra throw append position = %q", document.ExtraThrow.AppendPosition)
	}
}

func TestResultTokenSchemasMatchDomain(t *testing.T) {
	wantResults := []string{"do", "gae", "geol", "yut", "mo", "backdo", "buk"}
	wantOrigins := []string{"initial_throw", "yut_extra", "mo_extra", "capture_extra"}

	for _, name := range []string{"game_snapshot.schema.json", "ws_server_event.schema.json"} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join("..", "..", "..", "schemas", name)
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("ReadFile(%q) error = %v", path, err)
			}
			var schema resultTokenSchemaDocument
			if err := json.Unmarshal(data, &schema); err != nil {
				t.Fatalf("Unmarshal(%q) error = %v", path, err)
			}
			definition, ok := schema.Defs["result_token"]
			if !ok {
				t.Fatal("$defs.result_token is missing")
			}
			if !reflect.DeepEqual(definition.Properties.Result.Enum, wantResults) {
				t.Fatalf("result enum = %v, want %v", definition.Properties.Result.Enum, wantResults)
			}
			if !reflect.DeepEqual(definition.Properties.Origin.Enum, wantOrigins) {
				t.Fatalf("origin enum = %v, want %v", definition.Properties.Origin.Enum, wantOrigins)
			}
			for _, required := range []string{"token_id", "result", "origin"} {
				if !containsString(definition.Required, required) {
					t.Fatalf("required fields %v do not contain %q", definition.Required, required)
				}
			}
			if name == "game_snapshot.schema.json" &&
				!containsString(definition.Required, "generated_by_player_id") {
				t.Fatalf("snapshot token required fields = %v", definition.Required)
			}
		})
	}
}

func containsString(values []string, candidate string) bool {
	for _, value := range values {
		if value == candidate {
			return true
		}
	}
	return false
}

type turnSpecDocument struct {
	Version int `yaml:"version"`
	Queue   struct {
		Token struct {
			StableIDRequired bool                  `yaml:"stable_id_required"`
			ResultValues     []domain.YutResult    `yaml:"result_values"`
			Origins          []domain.ResultOrigin `yaml:"origins"`
		} `yaml:"token"`
		BaseOrder room.MovementOrder `yaml:"base_order"`
		FreeMode  struct {
			Rule string `yaml:"rule"`
		} `yaml:"free_mode"`
		UnusableOrdinaryToken string `yaml:"unusable_ordinary_token"`
	} `yaml:"queue"`
	ExtraThrow struct {
		AppendPosition string `yaml:"append_position"`
	} `yaml:"extra_throw"`
}

type resultTokenSchemaDocument struct {
	Defs map[string]struct {
		Required   []string `json:"required"`
		Properties struct {
			Result struct {
				Enum []string `json:"enum"`
			} `json:"result"`
			Origin struct {
				Enum []string `json:"enum"`
			} `json:"origin"`
		} `json:"properties"`
	} `json:"$defs"`
}
