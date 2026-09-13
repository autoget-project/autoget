package testutil

import (
	"os"
	"strings"
	"testing"
)

// LoadEnvFile parses a KEY=VALUE env file (e.g. the gitignored .env.e2e) and
// exports every entry into the process environment unless a variable with the
// same name is already set, so explicitly exported values keep precedence.
// Blank lines and # comments are ignored; a missing file is silently ignored
// so offline CI environments keep working.
func LoadEnvFile(t *testing.T, path string) {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			t.Fatalf("read env file %s: %v", path, err)
		}
		return
	}

	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" {
			continue
		}
		if _, exists := os.LookupEnv(key); !exists {
			t.Setenv(key, value)
		}
	}
}
