package testutil

import (
	"os"
	"strings"
	"testing"
)

// SkipIfNoAPIKey checks environment variables and calls t.Skip if no valid API key is set.
// provider can be "grok", "xai", "gemini", or "all".
func SkipIfNoAPIKey(t *testing.T, provider string) {
	t.Helper()

	lower := strings.ToLower(provider)
	switch lower {
	case "grok", "xai":
		if os.Getenv("XAI_API_KEY") == "" {
			t.Skip("Skipping test: XAI_API_KEY is not set")
		}
	case "gemini":
		if os.Getenv("GEMINI_API_KEY") == "" {
			t.Skip("Skipping test: GEMINI_API_KEY is not set")
		}
	case "all":
		if os.Getenv("XAI_API_KEY") == "" && os.Getenv("GEMINI_API_KEY") == "" {
			t.Skip("Skipping test: Neither XAI_API_KEY nor GEMINI_API_KEY is set")
		}
	default:
		// Unknown provider name, check both
		if os.Getenv("XAI_API_KEY") == "" && os.Getenv("GEMINI_API_KEY") == "" {
			t.Skipf("Skipping test: No API key found for provider %s", provider)
		}
	}
}
