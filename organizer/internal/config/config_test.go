package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autoget-project/autoget/organizer/internal/model"
)

func TestStartupCheckMissingEnv(t *testing.T) {
	t.Parallel()

	// TPDB_API_TOKEN is optional: it is never checked by StartupCheck, so the
	// missing-env cases below run with it unset by default.
	cfg := &Config{}
	require.Error(t, StartupCheck(cfg), "empty config must fail startup check")

	cfg.DownloadCompletedDir = t.TempDir()
	require.Error(t, StartupCheck(cfg), "missing TARGET_DIR must fail startup check")
}

func TestLoadConfigDefaults(t *testing.T) {
	// Not parallel: mutates the process environment via t.Setenv.
	t.Setenv("DOWNLOAD_COMPLETED_DIR", "/downloads")
	t.Setenv("TARGET_DIR", "/target")
	t.Setenv("TMDB_API_KEY", "tmdb-key")
	t.Setenv("METATUBE_API_URL", "http://metatube:8080")
	t.Setenv("METATUBE_API_KEY", "metatube-key")
	t.Setenv("TPDB_API_TOKEN", "tpdb-key")

	cfg := LoadConfig()

	assert.Equal(t, "/downloads", cfg.DownloadCompletedDir)
	assert.Equal(t, "tmdb-key", cfg.TMDBAPIKey)
	assert.Equal(t, "http://metatube:8080", cfg.MetaTubeAPIURL)
	assert.Equal(t, "metatube-key", cfg.MetaTubeAPIKey)
	assert.Equal(t, "tpdb-key", cfg.TPDBAPIToken)

	// TMDB_RESPONSE_LANGUAGE unset must default to Simplified Chinese.
	assert.Equal(t, "zh-CN", cfg.TMDBLanguage)
}

func TestResolveProvider(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		model     string
		xaiKey    string
		geminiKey string
		expected  string
		wantErr   bool
	}{
		{
			name:    "neither key provided",
			model:   "grok-beta",
			wantErr: true,
		},
		{
			name:     "xai prefix with key",
			model:    "xai:grok-2",
			xaiKey:   "xai-123",
			expected: "grok",
		},
		{
			name:      "xai prefix without key",
			model:     "xai:grok-2",
			geminiKey: "gemini-123",
			wantErr:   true,
		},
		{
			name:      "gemini prefix with key",
			model:     "gemini:gemini-1.5-flash",
			geminiKey: "gemini-123",
			expected:  "gemini",
		},
		{
			name:    "gemini prefix without key",
			model:   "gemini:gemini-1.5-flash",
			xaiKey:  "xai-123",
			wantErr: true,
		},
		{
			name:      "both keys present and xai prefix",
			model:     "xai:grok-beta",
			xaiKey:    "xai-123",
			geminiKey: "gemini-123",
			expected:  "grok",
		},
		{
			name:      "both keys present and gemini prefix",
			model:     "gemini:gemini-1.5-pro",
			xaiKey:    "xai-123",
			geminiKey: "gemini-123",
			expected:  "gemini",
		},
		{
			name:      "explicit prefix beats substring keyword",
			model:     "gemini:grok-4",
			xaiKey:    "xai-123",
			geminiKey: "gemini-123",
			expected:  "gemini",
		},
		{
			name:      "both keys present without prefix or keyword",
			model:     "custom-model",
			xaiKey:    "xai-123",
			geminiKey: "gemini-123",
			wantErr:   true,
		},
		{
			name:     "single xai key fallback",
			model:    "custom-model",
			xaiKey:   "xai-123",
			expected: "grok",
		},
		{
			name:      "single gemini key fallback",
			model:     "custom-model",
			geminiKey: "gemini-123",
			expected:  "gemini",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := ResolveProvider(tt.model, tt.xaiKey, tt.geminiKey)
			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestStartupCheckDirectories(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	targetDir := filepath.Join(tempDir, "target")
	downloadDir := filepath.Join(tempDir, "download")
	require.NoError(t, os.MkdirAll(downloadDir, 0o755))

	cfg := &Config{
		DownloadCompletedDir: downloadDir,
		TargetDir:            targetDir,
		JavActorFile:         filepath.Join(tempDir, "actor.json"),
		FlareSolverrURL:      "http://localhost:8191",
		TMDBAPIKey:           "tmdb-key",
		MetaTubeAPIURL:       "http://localhost:9000",
		Model:                "xai:grok-2",
		XaiAPIKey:            "valid-key",
	}

	// targetDir does not exist yet.
	require.Error(t, StartupCheck(cfg))

	// targetDir exists but the required subdirectories are missing.
	require.NoError(t, os.MkdirAll(targetDir, 0o755))
	require.Error(t, StartupCheck(cfg))

	// All required subdirectories present (including anim_movie and
	// anim_tv_series): startup check must now succeed.
	for _, sub := range model.AllTargetDirs {
		require.NoError(t, os.MkdirAll(filepath.Join(targetDir, string(sub)), 0o755))
	}
	require.NoError(t, StartupCheck(cfg))
	assert.Equal(t, "grok", cfg.Provider)
}
