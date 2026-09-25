package telemetry_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autoget-project/autoget/organizer/internal/telemetry"
)

func TestReadTraceSpans_Normal(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "traces.jsonl")

	now := time.Now()
	span1 := fmt.Sprintf(`{"trace_id":"trace-1","span_id":"s1","name":"%s","start_time":"%s","end_time":"%s","duration_ms":10.5,"attributes":{"%s":"/test/dir"}}`+"\n",
		telemetry.SpanPipelineCreatePlan,
		now.Format(time.RFC3339Nano),
		now.Add(10*time.Millisecond).Format(time.RFC3339Nano),
		telemetry.AttrOrganizerDir,
	)
	span2 := fmt.Sprintf(`{"trace_id":"trace-1","span_id":"s2","parent_span_id":"s1","name":"%s","start_time":"%s","end_time":"%s","duration_ms":5.0,"attributes":{"%s":"movie"}}`+"\n",
		telemetry.SpanStage1Classify,
		now.Add(1*time.Millisecond).Format(time.RFC3339Nano),
		now.Add(6*time.Millisecond).Format(time.RFC3339Nano),
		telemetry.AttrStage1Category,
	)
	spanOther := fmt.Sprintf(`{"trace_id":"trace-2","span_id":"s3","name":"%s","start_time":"%s","end_time":"%s","duration_ms":2.0}`+"\n",
		telemetry.SpanPipelineCreatePlan,
		now.Add(20*time.Millisecond).Format(time.RFC3339Nano),
		now.Add(22*time.Millisecond).Format(time.RFC3339Nano),
	)

	err := os.WriteFile(filePath, []byte(span1+span2+spanOther), 0o644)
	require.NoError(t, err)

	spans, err := telemetry.ReadTraceSpans(filePath, "trace-1")
	require.NoError(t, err)
	require.Len(t, spans, 2)

	assert.Equal(t, "trace-1", spans[0].TraceID)
	assert.Equal(t, telemetry.SpanPipelineCreatePlan, spans[0].Name)
	assert.Equal(t, "/test/dir", spans[0].StringAttr(telemetry.AttrOrganizerDir))

	assert.Equal(t, "trace-1", spans[1].TraceID)
	assert.Equal(t, telemetry.SpanStage1Classify, spans[1].Name)
	assert.Equal(t, "movie", spans[1].StringAttr(telemetry.AttrStage1Category))

	// Test non-existent traceID
	_, err = telemetry.ReadTraceSpans(filePath, "non-existent")
	assert.ErrorIs(t, err, telemetry.ErrTraceNotFound)
}

func TestReadTraceSpans_TornLineRecovery(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "traces_torn.jsonl")

	now := time.Now()
	span1 := fmt.Sprintf(`{"trace_id":"trace-torn","span_id":"s1","name":"%s","start_time":"%s","end_time":"%s","duration_ms":10.5}`+"\n",
		telemetry.SpanPipelineCreatePlan,
		now.Format(time.RFC3339Nano),
		now.Add(10*time.Millisecond).Format(time.RFC3339Nano),
	)
	tornLine := `{"trace_id":"trace-torn","span_id":"s2","name":"organizer.stage1` // cut off before ending

	err := os.WriteFile(filePath, []byte(span1+tornLine), 0o644)
	require.NoError(t, err)

	spans, err := telemetry.ReadTraceSpans(filePath, "trace-torn")
	require.NoError(t, err, "should not error on torn line at EOF")
	require.Len(t, spans, 1)
	assert.Equal(t, "s1", spans[0].SpanID)
}

func TestGetLatestTraceID(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "traces_latest.jsonl")

	now := time.Now()
	span1 := fmt.Sprintf(`{"trace_id":"trace-111","span_id":"s1","name":"test","start_time":"%s","end_time":"%s"}`+"\n",
		now.Format(time.RFC3339Nano),
		now.Add(time.Millisecond).Format(time.RFC3339Nano),
	)
	span2 := fmt.Sprintf(`{"trace_id":"trace-222","span_id":"s2","name":"test","start_time":"%s","end_time":"%s"}`+"\n",
		now.Add(2*time.Millisecond).Format(time.RFC3339Nano),
		now.Add(3*time.Millisecond).Format(time.RFC3339Nano),
	)

	err := os.WriteFile(filePath, []byte(span1+span2), 0o644)
	require.NoError(t, err)

	latestID, err := telemetry.GetLatestTraceID(filePath)
	require.NoError(t, err)
	assert.Equal(t, "trace-222", latestID)

	// Now append a torn line
	f, err := os.OpenFile(filePath, os.O_APPEND|os.O_WRONLY, 0o644)
	require.NoError(t, err)
	_, err = f.WriteString(`{"trace_id":"trace-333","span_id":`)
	require.NoError(t, err)
	_ = f.Close()

	// Should still return trace-222 because trace-333 line was torn
	latestID2, err := telemetry.GetLatestTraceID(filePath)
	require.NoError(t, err)
	assert.Equal(t, "trace-222", latestID2)
}

func TestGetLatestTraceID_Empty(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "traces_empty.jsonl")
	err := os.WriteFile(filePath, []byte(""), 0o644)
	require.NoError(t, err)

	_, err = telemetry.GetLatestTraceID(filePath)
	assert.Error(t, err)
}
