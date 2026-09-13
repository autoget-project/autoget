package grok

import "testing"

// TestMaxToolRoundsExternalMirror guards against drift between the unexported
// package constant maxToolRounds and the mirrored constant used by
// TestGrokProvider_GenerateStructuredWithTools_RoundLimit in the external
// grok_test package (which cannot reference unexported identifiers).
func TestMaxToolRoundsExternalMirror(t *testing.T) {
	t.Parallel()

	const externalMirror = 10
	if maxToolRounds != externalMirror {
		t.Fatalf("maxToolRounds drifted from the external test mirror: got %d, want %d", maxToolRounds, externalMirror)
	}
}
