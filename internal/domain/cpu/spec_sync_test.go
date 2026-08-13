package cpu

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestCanonicalCPUPolicySpecMatchesDomain(t *testing.T) {
	path := filepath.Join("..", "..", "..", "spec", "cpu_policy.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", path, err)
	}
	var document cpuPolicyDocument
	if err := yaml.Unmarshal(data, &document); err != nil {
		t.Fatalf("Unmarshal(%q) error = %v", path, err)
	}

	if document.Version != 1 {
		t.Fatalf("version = %d, want 1", document.Version)
	}
	if document.QueueOrder != "fifo" {
		t.Fatalf("queue_order = %q, want fifo", document.QueueOrder)
	}
	if document.ShortcutChoice != "always_shortcut_when_available" {
		t.Fatalf("shortcut_choice = %q", document.ShortcutChoice)
	}
	wantPriority := []string{
		"immediate_finish",
		"capture_opponent",
		"resolve_buk",
		"stack_with_ally",
		"enter_shortcut",
		"move_piece_closest_to_finish",
		"deploy_waiting_piece",
		"weighted_or_uniform_random_tiebreak",
	}
	if !reflect.DeepEqual(document.Priority, wantPriority) {
		t.Fatalf("priority = %v, want %v", document.Priority, wantPriority)
	}
	if document.FinishDistance.Source != "board_graph.finish_distance" ||
		document.FinishDistance.SharedWith != "buk_target_selection" {
		t.Fatalf("finish distance contract = %#v", document.FinishDistance)
	}
	if document.Tiebreak.OrdinaryEqualCandidates != "server_random" ||
		document.Tiebreak.TestMode != "deterministic_seed" {
		t.Fatalf("tiebreak contract = %#v", document.Tiebreak)
	}
	if document.Tiebreak.BukEqualDistance.Unit != "position_group" ||
		document.Tiebreak.BukEqualDistance.Weight != "current_piece_count" {
		t.Fatalf("Buk tiebreak contract = %#v", document.Tiebreak.BukEqualDistance)
	}
}

type cpuPolicyDocument struct {
	Version        int      `yaml:"version"`
	QueueOrder     string   `yaml:"queue_order"`
	ShortcutChoice string   `yaml:"shortcut_choice"`
	Priority       []string `yaml:"priority"`
	FinishDistance struct {
		Source     string `yaml:"source"`
		SharedWith string `yaml:"shared_with"`
	} `yaml:"finish_distance"`
	Tiebreak struct {
		OrdinaryEqualCandidates string `yaml:"ordinary_equal_candidates"`
		BukEqualDistance        struct {
			Unit   string `yaml:"unit"`
			Weight string `yaml:"weight"`
		} `yaml:"buk_equal_distance"`
		TestMode string `yaml:"test_mode"`
	} `yaml:"tiebreak"`
}
