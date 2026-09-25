package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCleanRequestJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "plain json",
			input:    `{"dir":"d1","files":["f.mkv"]}`,
			expected: `{"dir":"d1","files":["f.mkv"]}`,
		},
		{
			name:     "json with spaces and newlines",
			input:    "  \n{\"dir\":\"d1\"}\n\t",
			expected: `{"dir":"d1"}`,
		},
		{
			name:     "log line with PLAN_REQ prefix",
			input:    `2026/09/25 14:00:00 [PLAN_REQ] {"dir":"d1","files":["f.mkv"]}`,
			expected: `{"dir":"d1","files":["f.mkv"]}`,
		},
		{
			name:     "only prefix and json",
			input:    `[PLAN_REQ] {"dir":"d1"}`,
			expected: `{"dir":"d1"}`,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := cleanRequestJSON([]byte(tc.input))
			assert.Equal(t, tc.expected, string(got))
		})
	}
}

func TestReadInput_FileAndArgs(t *testing.T) {
	tempDir := t.TempDir()
	reqFile := filepath.Join(tempDir, "req.json")
	require.NoError(t, os.WriteFile(reqFile, []byte(`{"dir":"file_dir"}`), 0o644))

	// Via fileFlag
	data, err := readInput(reqFile, nil)
	require.NoError(t, err)
	assert.Equal(t, `{"dir":"file_dir"}`, string(data))

	// Via args JSON string
	data, err = readInput("", []string{`{"dir":"inline_dir"}`})
	require.NoError(t, err)
	assert.Equal(t, `{"dir":"inline_dir"}`, string(data))

	// Via args log line
	data, err = readInput("", []string{`2026/09/25 12:00:00 [PLAN_REQ] {"dir":"log_dir"}`})
	require.NoError(t, err)
	assert.Equal(t, `{"dir":"log_dir"}`, string(data))
}
