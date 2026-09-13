package testutil

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadEnvFile_MissingFileIgnored(t *testing.T) {
	t.Parallel()

	LoadEnvFile(t, filepath.Join(t.TempDir(), "absent.env"))
}

func TestLoadEnvFile_LoadsKeysAndKeepsExisting(t *testing.T) {
	// t.Setenv is incompatible with t.Parallel, so this case stays serial.

	path := filepath.Join(t.TempDir(), ".env.e2e")
	content := "# provider keys\n" +
		"XAI_API_KEY=xai-fake\n" +
		"\n" +
		"MODEL=gemini:gemini-3.5-flash-lite\n" +
		"BAD_LINE_WITHOUT_EQUALS\n" +
		"  SPACED_KEY  =  spaced value  \n"
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	t.Setenv("MODEL", "preset-from-shell")

	LoadEnvFile(t, path)

	assert.Equal(t, "xai-fake", os.Getenv("XAI_API_KEY"), "new key must be loaded")
	assert.Equal(t, "preset-from-shell", os.Getenv("MODEL"), "already-set variable must keep precedence")
	assert.Equal(t, "spaced value", os.Getenv("SPACED_KEY"), "keys/values must be trimmed")
}
