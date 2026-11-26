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

func TestPlanActionTargetExists(t *testing.T) {
	target := "movie/a.mkv"

	data, err := json.Marshal(PlanAction{File: "a.mkv", Action: "move", Target: &target})
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	if want := `{"file":"a.mkv","action":"move","target":"movie/a.mkv"}`; string(data) != want {
		t.Fatalf("got %s, want %s", data, want)
	}

	data, err = json.Marshal(PlanAction{File: "a.mkv", Action: "move", Target: &target, TargetExists: true})
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	var got PlanAction
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if !got.TargetExists {
		t.Fatalf("target_exists lost in round trip: %s", data)
	}
}
