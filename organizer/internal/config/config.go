package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"

	"github.com/autoget-project/autoget/organizer/internal/model"
)

// Config holds the application configuration loaded from environment variables.
type Config struct {
	DownloadCompletedDir string
	TargetDir            string
	JavActorFile         string
	FlareSolverrURL      string
	TMDBAPIKey           string
	TMDBLanguage         string
	MetaTubeAPIURL       string
	MetaTubeAPIKey       string
	Model                string
	XaiAPIKey            string
	GeminiAPIKey         string
	Provider             string // "grok" or "gemini"

	// TPDBAPIToken is optional: enables ThePornDB scene search for the porn
	// planner. When empty, the porn planner silently falls back to the local
	// naming chain.
	TPDBAPIToken string
}

// LoadConfig reads configuration from environment variables.
func LoadConfig() *Config {
	tmdbLanguage := os.Getenv("TMDB_RESPONSE_LANGUAGE")
	if tmdbLanguage == "" {
		tmdbLanguage = "zh-CN"
	}
	return &Config{
		DownloadCompletedDir: os.Getenv("DOWNLOAD_COMPLETED_DIR"),
		TargetDir:            os.Getenv("TARGET_DIR"),
		JavActorFile:         os.Getenv("JAV_ACTOR_FILE"),
		FlareSolverrURL:      os.Getenv("FLARESOLVERR_URL"),
		TMDBAPIKey:           os.Getenv("TMDB_API_KEY"),
		TMDBLanguage:         tmdbLanguage,
		MetaTubeAPIURL:       os.Getenv("METATUBE_API_URL"),
		MetaTubeAPIKey:       os.Getenv("METATUBE_API_KEY"),
		Model:                os.Getenv("MODEL"),
		XaiAPIKey:            os.Getenv("XAI_API_KEY"),
		GeminiAPIKey:         os.Getenv("GEMINI_API_KEY"),
		TPDBAPIToken:         os.Getenv("TPDB_API_TOKEN"),
	}
}

// StartupCheck performs comprehensive environment, provider, and directory checks.
func StartupCheck(cfg *Config) error {
	if cfg == nil {
		return fmt.Errorf("config is nil")
	}

	// 1. Required environment variables
	if cfg.DownloadCompletedDir == "" {
		return fmt.Errorf("DOWNLOAD_COMPLETED_DIR environment variable is not set or is empty")
	}
	if cfg.TargetDir == "" {
		return fmt.Errorf("TARGET_DIR environment variable is not set or is empty")
	}
	if cfg.JavActorFile == "" {
		return fmt.Errorf("JAV_ACTOR_FILE environment variable is not set or is empty")
	}
	if cfg.FlareSolverrURL == "" {
		return fmt.Errorf("FLARESOLVERR_URL environment variable is not set or is empty")
	}
	if cfg.TMDBAPIKey == "" {
		return fmt.Errorf("TMDB_API_KEY environment variable is not set or is empty")
	}
	if cfg.MetaTubeAPIURL == "" {
		return fmt.Errorf("METATUBE_API_URL environment variable is not set or is empty")
	}
	if cfg.Model == "" {
		return fmt.Errorf("MODEL environment variable is not set or is empty")
	}

	// 2. Provider resolution & API key validation
	provider, err := ResolveProvider(cfg.Model, cfg.XaiAPIKey, cfg.GeminiAPIKey)
	if err != nil {
		return fmt.Errorf("provider validation failed: %w", err)
	}
	cfg.Provider = provider

	// 3. TARGET_DIR existence and subdirectories validation
	if err := checkDirWritable(cfg.TargetDir); err != nil {
		return fmt.Errorf("target dir %s check failed: %w", cfg.TargetDir, err)
	}

	for _, dirName := range model.AllTargetDirs {
		subDirPath := filepath.Join(cfg.TargetDir, string(dirName))
		if err := checkDirWritable(subDirPath); err != nil {
			return fmt.Errorf("required target subdirectory %s error: %w", subDirPath, err)
		}
	}

	// 4. Check DOWNLOAD_COMPLETED_DIR writable
	if err := checkDirWritable(cfg.DownloadCompletedDir); err != nil {
		return fmt.Errorf("download completed dir %s check failed: %w", cfg.DownloadCompletedDir, err)
	}

	return nil
}

// ResolveProvider determines the AI provider from Model and API Keys.
// Rules:
// - Prefix xai: or model name contains grok -> requires XAI_API_KEY -> provider "grok"
// - Prefix gemini: or model name contains gemini -> requires GEMINI_API_KEY -> provider "gemini"
// - If no prefix / ambiguous:
//   - If only XAI_API_KEY is present -> "grok"
//   - If only GEMINI_API_KEY is present -> "gemini"
//   - If both or neither -> error
func ResolveProvider(modelName, xaiKey, geminiKey string) (string, error) {
	if xaiKey == "" && geminiKey == "" {
		return "", fmt.Errorf("neither XAI_API_KEY nor GEMINI_API_KEY is set")
	}

	lowerModel := strings.ToLower(modelName)

	// Explicit provider prefixes take precedence over substring keyword matches
	// (e.g. "gemini:grok-4" must resolve to "gemini", not "grok").
	if strings.HasPrefix(lowerModel, "xai:") {
		if xaiKey == "" {
			return "", fmt.Errorf("model %q requires XAI_API_KEY, but it is not set", modelName)
		}
		return "grok", nil
	}
	if strings.HasPrefix(lowerModel, "gemini:") {
		if geminiKey == "" {
			return "", fmt.Errorf("model %q requires GEMINI_API_KEY, but it is not set", modelName)
		}
		return "gemini", nil
	}

	if strings.Contains(lowerModel, "grok") {
		if xaiKey == "" {
			return "", fmt.Errorf("model %q requires XAI_API_KEY, but it is not set", modelName)
		}
		return "grok", nil
	}

	if strings.Contains(lowerModel, "gemini") {
		if geminiKey == "" {
			return "", fmt.Errorf("model %q requires GEMINI_API_KEY, but it is not set", modelName)
		}
		return "gemini", nil
	}

	// No specific model prefix / keyword
	if xaiKey != "" && geminiKey == "" {
		return "grok", nil
	}
	if geminiKey != "" && xaiKey == "" {
		return "gemini", nil
	}

	return "", fmt.Errorf("ambiguous model %q: both or neither provider keys match without prefix (use xai: or gemini: prefix)", modelName)
}

// checkDirWritable checks if path exists, is a directory, and has write permission.
// Note: uses unix.Access, so this package targets POSIX platforms only (fine for
// the Linux deployment target; revisit if Windows CI is ever added).
func checkDirWritable(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("path %s is not a directory", path)
	}
	// Check unix write permission
	if unix.Access(path, unix.W_OK) != nil {
		return fmt.Errorf("directory %s is not writable", path)
	}
	return nil
}
