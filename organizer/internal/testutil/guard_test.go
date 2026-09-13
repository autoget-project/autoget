package testutil_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/autoget-project/organizer/internal/testutil"
)

func TestSkipIfNoAPIKey_WhenSet(t *testing.T) {
	// t.Setenv is incompatible with t.Parallel, so this case stays serial.

	t.Setenv("XAI_API_KEY", "dummy-key")
	t.Setenv("GEMINI_API_KEY", "dummy-key")

	// Should not skip.
	testutil.SkipIfNoAPIKey(t, "grok")
	testutil.SkipIfNoAPIKey(t, "gemini")
	testutil.SkipIfNoAPIKey(t, "all")
	assert.False(t, t.Skipped())
}

func TestSkipIfNoAPIKey_WhenMissing(t *testing.T) {
	// t.Setenv is incompatible with t.Parallel, so this case stays serial.

	t.Setenv("XAI_API_KEY", "")
	t.Setenv("GEMINI_API_KEY", "")

	tests := []struct {
		provider string
		keyEnv   string
	}{
		{"grok", "XAI_API_KEY"},
		{"gemini", "GEMINI_API_KEY"},
		{"all", "both keys"},
	}

	for _, tt := range tests {
		t.Run(tt.provider, func(t *testing.T) {
			testutil.SkipIfNoAPIKey(t, tt.provider)
			assert.True(t, t.Skipped(), "expected skip for provider %s when %s unset", tt.provider, tt.keyEnv)
		})
	}
}
