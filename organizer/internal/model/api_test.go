package model

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPlanActionJSON(t *testing.T) {
	t.Parallel()

	// Move action serializes its target.
	targetPath := "movie/Chinese/Test (2024)/Test (2024).mkv"
	dataMove, err := json.Marshal(PlanAction{File: "test.mkv", Action: "move", Target: &targetPath})
	require.NoError(t, err)
	assert.JSONEq(t,
		`{"file":"test.mkv","action":"move","target":"movie/Chinese/Test (2024)/Test (2024).mkv"}`,
		string(dataMove))

	// Skip action must explicitly serialize as "target": null.
	dataSkip, err := json.Marshal(PlanAction{File: "sample.mkv", Action: "skip", Target: nil})
	require.NoError(t, err)
	assert.JSONEq(t, `{"file":"sample.mkv","action":"skip","target":null}`, string(dataSkip))

	// Round trip.
	var unmarshaledSkip PlanAction
	require.NoError(t, json.Unmarshal(dataSkip, &unmarshaledSkip))
	assert.Nil(t, unmarshaledSkip.Target)
	assert.Equal(t, PlanAction{File: "sample.mkv", Action: "skip"}, unmarshaledSkip)
}

func TestPlanResponseJSON(t *testing.T) {
	t.Parallel()

	// A nil Error must marshal as "error": null.
	data, err := json.Marshal(PlanResponse{Plan: []PlanAction{}, Error: nil})
	require.NoError(t, err)
	assert.Contains(t, string(data), `"error":null`)

	// A set Error carries the message.
	errMsg := "something failed"
	dataWithErr, err := json.Marshal(PlanResponse{Plan: []PlanAction{}, Error: &errMsg})
	require.NoError(t, err)
	assert.Contains(t, string(dataWithErr), `"error":"something failed"`)
}

func TestAPIRequestsAndResponses(t *testing.T) {
	t.Parallel()

	// APIPlanRequest round trip.
	var planReq APIPlanRequest
	require.NoError(t, json.Unmarshal(
		[]byte(`{"dir":"/incoming/movie","files":["a.mkv","b.nfo"],"metadata":{"dmm_id":"123"}}`),
		&planReq))
	assert.Equal(t, "/incoming/movie", planReq.Dir)
	assert.Equal(t, []string{"a.mkv", "b.nfo"}, planReq.Files)
	assert.Equal(t, map[string]interface{}{"dmm_id": "123"}, planReq.Metadata)

	// APIExecuteRequest round trip.
	var execReq APIExecuteRequest
	require.NoError(t, json.Unmarshal(
		[]byte(`{"dir":"/incoming/movie","plan":[{"file":"a.mkv","action":"move","target":"movie/a.mkv"}]}`),
		&execReq))
	require.Len(t, execReq.Plan, 1)
	require.NotNil(t, execReq.Plan[0].Target)
	assert.Equal(t, "movie/a.mkv", *execReq.Plan[0].Target)

	// ExecuteResponse carries the failure reason.
	target := "movie/b.mkv"
	dataResp, err := json.Marshal(ExecuteResponse{FailedMove: []PlanFailed{{
		File:   "b.mkv",
		Action: "move",
		Target: &target,
		Reason: "file not found",
	}}})
	require.NoError(t, err)
	assert.Contains(t, string(dataResp), `"reason":"file not found"`)

	// APIReplanRequest round trip.
	var replanReq APIReplanRequest
	require.NoError(t, json.Unmarshal(
		[]byte(`{"files":["a.mkv"],"metadata":null,"previous_response":{"plan":[],"error":null},"user_hint":"this is tv"}`),
		&replanReq))
	assert.Equal(t, []string{"a.mkv"}, replanReq.Files)
	assert.Equal(t, "this is tv", replanReq.UserHint)
}
