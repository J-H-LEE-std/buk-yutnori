package turn

import (
	"encoding/json"
	"fmt"
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
	wantSpaces := map[domain.YutResult]int{
		domain.YutDo:   1,
		domain.YutGae:  2,
		domain.YutGeol: 3,
		domain.YutYut:  4,
		domain.YutMo:   5,
	}
	if !reflect.DeepEqual(document.Queue.Token.OrdinaryMovementSpaces, wantSpaces) {
		t.Fatalf(
			"spec ordinary movement spaces = %v, want %v",
			document.Queue.Token.OrdinaryMovementSpaces,
			wantSpaces,
		)
	}
	for result, want := range wantSpaces {
		got, err := result.OrdinaryMovementSpaces()
		if err != nil {
			t.Fatalf("OrdinaryMovementSpaces(%q) error = %v", result, err)
		}
		if got != want {
			t.Fatalf("OrdinaryMovementSpaces(%q) = %d, want %d", result, got, want)
		}
	}
	if !reflect.DeepEqual(document.Queue.Token.Origins, wantOrigins) {
		t.Fatalf("spec origins = %v, want %v", document.Queue.Token.Origins, wantOrigins)
	}
	if document.Queue.BaseOrder != room.MovementFIFO {
		t.Fatalf("base order = %q, want %q", document.Queue.BaseOrder, room.MovementFIFO)
	}
	freeRule := strings.ToLower(document.Queue.FreeMode.Rule)
	if !strings.Contains(freeRule, "buk token") || !strings.Contains(freeRule, "generated") || !strings.Contains(freeRule, "immediately") {
		t.Fatalf("unexpected free-mode rule: %q", document.Queue.FreeMode.Rule)
	}
	if document.Queue.UnusableOrdinaryToken != "discard_only_that_token" {
		t.Fatalf("unusable ordinary token policy = %q", document.Queue.UnusableOrdinaryToken)
	}
	if document.ExtraThrow.AppendPosition != "queue_tail" {
		t.Fatalf("extra throw append position = %q", document.ExtraThrow.AppendPosition)
	}
}

func TestCanonicalTurnStatesMatchDomain(t *testing.T) {
	path := filepath.Join("..", "..", "..", "spec", "turn_state_machine.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", path, err)
	}

	var document turnSpecDocument
	if err := yaml.Unmarshal(data, &document); err != nil {
		t.Fatalf("Unmarshal(%q) error = %v", path, err)
	}
	want := []domain.TurnPhase{
		domain.TurnStart,
		domain.TurnWaitThrow,
		domain.TurnThrowingChain,
		domain.TurnResolveQueue,
		domain.TurnWaitMoveSelection,
		domain.TurnWaitRouteSelection,
		domain.TurnApplyMove,
		domain.TurnResolveStackCaptureFinish,
		domain.TurnResolveBuk,
		domain.TurnCPUControl,
		domain.TurnPaused,
		domain.TurnEnd,
		domain.TurnMatchEnd,
	}
	if !reflect.DeepEqual(document.States, want) {
		t.Fatalf("spec states = %v, want %v", document.States, want)
	}
	if document.Queue.MaxPendingTokens != MaxResultQueueTokens || !document.Queue.SealOnReachingMax ||
		document.Queue.AfterSeal != "suppress_all_extra_throws_for_remainder_of_turn" {
		t.Fatalf("queue cap/seal contract = %#v", document.Queue)
	}
	if document.ExtraThrow.OnYutOrMo != "immediate_when_enabled_and_turn_not_sealed" {
		t.Fatalf("yut/mo extra throw = %q", document.ExtraThrow.OnYutOrMo)
	}
	if document.ExtraThrow.OnCapture != "immediate_when_policy_allows_and_turn_not_sealed" {
		t.Fatalf("capture extra throw = %q", document.ExtraThrow.OnCapture)
	}
	if document.Queue.BukNoCandidate != "discard_buk_and_end_turn" {
		t.Fatalf("Buk no-candidate policy = %q", document.Queue.BukNoCandidate)
	}
	if !reflect.DeepEqual(document.Queue.BukPriority.AppliesTo,
		[]room.MovementOrder{room.MovementFIFO, room.MovementFree}) ||
		document.Queue.BukPriority.Invariant != "buk_is_resolved_immediately_when_generated" ||
		!document.Queue.BukPriority.NoStackedBukTokens {
		t.Fatalf("Buk priority contract = %#v", document.Queue.BukPriority)
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

func TestSnapshotSchemaResourceLimitsMatchDomain(t *testing.T) {
	path := filepath.Join("..", "..", "..", "schemas", "game_snapshot.schema.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", path, err)
	}
	var schema struct {
		Properties struct {
			ResultQueue struct {
				MaxItems int `json:"maxItems"`
			} `json:"result_queue"`
		} `json:"properties"`
		Defs map[string]struct {
			Properties map[string]struct {
				MaxItems int `json:"maxItems"`
			} `json:"properties"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatalf("Unmarshal(%q) error = %v", path, err)
	}
	if got := schema.Properties.ResultQueue.MaxItems; got != MaxResultQueueTokens {
		t.Fatalf("schema result_queue maxItems = %d, want %d", got, MaxResultQueueTokens)
	}
	moveRequest, ok := schema.Defs["move_request"]
	if !ok {
		t.Fatal("schema missing move_request definition")
	}
	wantCandidates := MaxResultQueueTokens * room.MaxPiecesPerTeam
	if got := moveRequest.Properties["candidates"].MaxItems; got != wantCandidates {
		t.Fatalf("schema candidate maxItems = %d, want %d", got, wantCandidates)
	}
	eventPath := filepath.Join("..", "..", "..", "schemas", "ws_server_event.schema.json")
	eventData, err := os.ReadFile(eventPath)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", eventPath, err)
	}
	var eventSchema struct {
		Defs map[string]struct {
			AllOf []struct {
				Properties struct {
					Payload struct {
						Properties map[string]struct {
							MaxItems int `json:"maxItems"`
						} `json:"properties"`
					} `json:"payload"`
				} `json:"properties"`
			} `json:"allOf"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(eventData, &eventSchema); err != nil {
		t.Fatalf("Unmarshal(%q) error = %v", eventPath, err)
	}
	queueUpdated, ok := eventSchema.Defs["result_queue_updated"]
	if !ok {
		t.Fatal("event schema missing result_queue_updated definition")
	}
	var eventQueueLimit int
	for _, alternative := range queueUpdated.AllOf {
		if resultQueue, exists := alternative.Properties.Payload.Properties["result_queue"]; exists {
			eventQueueLimit = resultQueue.MaxItems
		}
	}
	if eventQueueLimit != MaxResultQueueTokens {
		t.Fatalf("event result_queue maxItems = %d, want %d", eventQueueLimit, MaxResultQueueTokens)
	}
	moveRequired, ok := eventSchema.Defs["move_required"]
	if !ok {
		t.Fatal("event schema missing move_required definition")
	}
	var eventCandidateLimit int
	for _, alternative := range moveRequired.AllOf {
		if candidates, exists := alternative.Properties.Payload.Properties["candidates"]; exists {
			eventCandidateLimit = candidates.MaxItems
		}
	}
	if eventCandidateLimit != wantCandidates {
		t.Fatalf("event candidates maxItems = %d, want %d", eventCandidateLimit, wantCandidates)
	}
}

func TestBrowserMoveCandidateBoundMatchesDomain(t *testing.T) {
	path := filepath.Join("..", "..", "..", "client", "web", "shell.html")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", path, err)
	}
	source := string(data)
	teamPieceBound := fmt.Sprintf("const MAX_TEAM_PIECES = %d;", room.MaxPiecesPerTeam)
	if !strings.Contains(source, teamPieceBound) {
		t.Fatalf("browser shell must derive MAX_TEAM_PIECES from domain bound %d", room.MaxPiecesPerTeam)
	}
	resultQueueBound := fmt.Sprintf("const MAX_PENDING_RESULT_TOKENS = %d;", MaxResultQueueTokens)
	if !strings.Contains(source, resultQueueBound) {
		t.Fatalf("browser shell must match result queue bound %d", MaxResultQueueTokens)
	}
	if !strings.Contains(source, "const MAX_MOVE_CANDIDATES = MAX_PENDING_RESULT_TOKENS * MAX_TEAM_PIECES;") {
		t.Fatal("browser candidate limit must multiply result and team-piece bounds")
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
	Version int                `yaml:"version"`
	States  []domain.TurnPhase `yaml:"states"`
	Queue   struct {
		MaxPendingTokens  int    `yaml:"max_pending_tokens"`
		SealOnReachingMax bool   `yaml:"seal_on_reaching_max"`
		AfterSeal         string `yaml:"after_seal"`
		Token             struct {
			StableIDRequired       bool                     `yaml:"stable_id_required"`
			ResultValues           []domain.YutResult       `yaml:"result_values"`
			OrdinaryMovementSpaces map[domain.YutResult]int `yaml:"ordinary_movement_spaces"`
			Origins                []domain.ResultOrigin    `yaml:"origins"`
		} `yaml:"token"`
		BaseOrder room.MovementOrder `yaml:"base_order"`
		FreeMode  struct {
			Rule string `yaml:"rule"`
		} `yaml:"free_mode"`
		UnusableOrdinaryToken string `yaml:"unusable_ordinary_token"`
		BukNoCandidate        string `yaml:"buk_no_candidate"`
		BukPriority           struct {
			AppliesTo          []room.MovementOrder `yaml:"applies_to"`
			Invariant          string               `yaml:"invariant"`
			NoStackedBukTokens bool                 `yaml:"no_stacked_buk_tokens"`
		} `yaml:"buk_priority"`
	} `yaml:"queue"`
	ExtraThrow struct {
		AppendPosition string `yaml:"append_position"`
		OnYutOrMo      string `yaml:"on_yut_or_mo"`
		OnCapture      string `yaml:"on_capture"`
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
