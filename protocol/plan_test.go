package protocol

import (
	"encoding/json"
	"testing"
)

func TestPlanActionJSON(t *testing.T) {
	target := "target/path"
	action := PlanAction{
		File:   "file.mkv",
		Action: "move",
		Target: &target,
	}

	data, err := json.Marshal(action)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var action2 PlanAction
	if err := json.Unmarshal(data, &action2); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if action2.File != action.File || action2.Action != action.Action || *action2.Target != *action.Target {
		t.Fatalf("action mismatch: %+v vs %+v", action, action2)
	}
}

func TestPlanActionSkipNullTarget(t *testing.T) {
	action := PlanAction{
		File:   "file.mkv",
		Action: "skip",
		Target: nil,
	}

	data, err := json.Marshal(action)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	expected := `{"file":"file.mkv","action":"skip","target":null}`
	if string(data) != expected {
		t.Fatalf("expected %s, got %s", expected, string(data))
	}
}
