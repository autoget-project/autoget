package stage2enricher

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/unix"

	"github.com/autoget-project/organizer/internal/ai"
)

const (
	lockTimeout = 10 * time.Second
)

var (
	titleAttrRegex = regexp.MustCompile(`<div class="[^"]*actor-box[^"]*">[\s\S]*?<a [^>]*title="([^"]+)"`)
)

// ActorStore manages JAV actress directories and aliases with flock concurrency control.
type ActorStore struct {
	mu              sync.Mutex
	filePath        string
	flareSolverrURL string
	aiProvider      ai.Provider
	httpClient      *http.Client
}

// NewActorStore creates a new ActorStore instance.
func NewActorStore(filePath, flareSolverrURL string, aiProv ai.Provider) *ActorStore {
	return &ActorStore{
		filePath:        filePath,
		flareSolverrURL: flareSolverrURL,
		aiProvider:      aiProv,
		httpClient:      &http.Client{Timeout: 70 * time.Second},
	}
}

// FindDir looks up if any of the provided actor names matches an existing actor directory in actor.json.
// Returns (directoryName, true, err) if found, ("", false, nil) if none match.
// Note: the error return intentionally extends the spec's (string, bool) shape.
func (s *ActorStore) FindDir(actorNames []string) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var dir string
	var found bool

	err := s.withLock(false, func() error {
		storeMap, err := s.readStoreUnlocked()
		if err != nil {
			return err
		}

		nameToDir := make(map[string]string)
		for d, aliases := range storeMap {
			for _, a := range aliases {
				nameToDir[a] = d
			}
		}

		for _, name := range actorNames {
			trimmed := strings.TrimSpace(name)
			if d, ok := nameToDir[trimmed]; ok {
				dir = d
				found = true
				return nil
			}
		}
		return nil
	})

	return dir, found, err
}

// AddActorAlias adds or merges aliases for an actor directory.
// Returns the directory used (either existing directory if alias merged, or the provided name).
func (s *ActorStore) AddActorAlias(name string, aliases []string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var chosenDir string

	err := s.withLock(true, func() error {
		storeMap, err := s.readStoreUnlocked()
		if err != nil {
			return err
		}

		nameToDir := make(map[string]string)
		for d, existingAliases := range storeMap {
			for _, a := range existingAliases {
				nameToDir[a] = d
			}
		}

		// Check if any alias already exists
		var existingDir string
		for _, a := range aliases {
			trimmed := strings.TrimSpace(a)
			if d, ok := nameToDir[trimmed]; ok {
				existingDir = d
				break
			}
		}

		if existingDir != "" {
			chosenDir = existingDir
			existingSet := make(map[string]struct{})
			for _, a := range storeMap[existingDir] {
				existingSet[a] = struct{}{}
			}
			for _, a := range aliases {
				trimmed := strings.TrimSpace(a)
				if trimmed != "" {
					if _, exists := existingSet[trimmed]; !exists {
						storeMap[existingDir] = append(storeMap[existingDir], trimmed)
						existingSet[trimmed] = struct{}{}
					}
				}
			}
		} else {
			chosenDir = name
			cleaned := make([]string, 0, len(aliases))
			seen := make(map[string]struct{})
			for _, a := range aliases {
				trimmed := strings.TrimSpace(a)
				if trimmed != "" {
					if _, ok := seen[trimmed]; !ok {
						seen[trimmed] = struct{}{}
						cleaned = append(cleaned, trimmed)
					}
				}
			}
			storeMap[name] = cleaned
		}

		return s.writeStoreUnlocked(storeMap)
	})

	return chosenDir, err
}

// SearchAndEnrichActor fetches aliases via FlareSolverr (or WebSearch) and merges them into the store.
func (s *ActorStore) SearchAndEnrichActor(ctx context.Context, actorNames []string) (string, error) {
	if len(actorNames) == 0 {
		return "素人", nil
	}

	dir, found, err := s.FindDir(actorNames)
	if err == nil && found {
		return dir, nil
	}

	primaryName := strings.TrimSpace(actorNames[0])
	aliases := s.SearchAliasFromJavDB(ctx, primaryName)
	if len(aliases) == 0 {
		aliases = []string{primaryName}
	}

	// Expand aliases using LLM if provider available
	if s.aiProvider != nil {
		expanded, err := s.expandAliasesWithLLM(ctx, aliases)
		if err == nil && len(expanded) > 0 {
			aliases = expanded
		}
	}

	return s.AddActorAlias(primaryName, aliases)
}

// SearchAliasFromJavDB queries FlareSolverr for actor aliases.
func (s *ActorStore) SearchAliasFromJavDB(ctx context.Context, name string) []string {
	if s.flareSolverrURL == "" {
		return []string{name}
	}

	searchURL := fmt.Sprintf("https://javdb.com/search?f=actor&q=%s", url.QueryEscape(name))
	payload := map[string]interface{}{
		"cmd":        "request.get",
		"url":        searchURL,
		"maxTimeout": 60000,
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return []string{name}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.flareSolverrURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return []string{name}
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return []string{name}
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return []string{name}
	}

	return ParseJavDBResponse(respBody, name)
}

// ParseJavDBResponse parses FlareSolverr JSON response (e.g. archived/1.json) and extracts aliases.
func ParseJavDBResponse(data []byte, defaultName string) []string {
	var fsResp struct {
		Status   string `json:"status"`
		Solution struct {
			Response string `json:"response"`
		} `json:"solution"`
	}
	if err := json.Unmarshal(data, &fsResp); err != nil || fsResp.Status != "ok" {
		return []string{defaultName}
	}

	html := fsResp.Solution.Response
	aliases := ExtractAliasesFromHTML(html)
	if len(aliases) == 0 && defaultName != "" {
		return []string{defaultName}
	}

	hasDefault := false
	for _, a := range aliases {
		if a == defaultName {
			hasDefault = true
			break
		}
	}
	if !hasDefault && defaultName != "" {
		aliases = append(aliases, defaultName)
	}

	return aliases
}

// ExtractAliasesFromHTML extracts actor aliases from JAVDB HTML (.actor-box title attribute).
func ExtractAliasesFromHTML(html string) []string {
	match := titleAttrRegex.FindStringSubmatch(html)
	if len(match) > 1 {
		rawTitle := match[1]
		parts := strings.Split(rawTitle, ",")
		var cleaned []string
		seen := make(map[string]struct{})
		for _, p := range parts {
			t := strings.TrimSpace(p)
			if t != "" {
				if _, ok := seen[t]; !ok {
					seen[t] = struct{}{}
					cleaned = append(cleaned, t)
				}
			}
		}
		return cleaned
	}
	return nil
}

type aliasExpansionOutput struct {
	Aliases []string `json:"aliases"`
}

func (s *ActorStore) expandAliasesWithLLM(ctx context.Context, currentAliases []string) ([]string, error) {
	prompt := fmt.Sprintf(`Task: You are an AI assistant specialized in expanding alias information for JAV (Japanese Adult Video) actors.
Input aliases: %v
Return a JSON object with:
{"aliases": [<all unique names, including Chinese translations (Simplified/Traditional) and original names>]}`, currentAliases)

	var out aliasExpansionOutput
	if err := s.aiProvider.GenerateStructured(ctx, prompt, aliasExpansionOutput{}, &out); err != nil {
		return nil, err
	}
	if len(out.Aliases) > 0 {
		log.Printf("stage2 actor alias expansion: %v -> %v", currentAliases, out.Aliases)
		return out.Aliases, nil
	}
	log.Printf("stage2 actor alias expansion: llm returned no aliases, keeping %v", currentAliases)
	return currentAliases, nil
}

func (s *ActorStore) withLock(exclusive bool, fn func() error) error {
	lockPath := s.filePath + ".lock"
	if err := os.MkdirAll(filepath.Dir(lockPath), 0755); err != nil {
		return fmt.Errorf("failed to create lock dir: %w", err)
	}

	lockFile, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return fmt.Errorf("failed to open lock file %s: %w", lockPath, err)
	}
	defer func() { _ = lockFile.Close() }()

	how := unix.LOCK_SH
	if exclusive {
		how = unix.LOCK_EX
	}

	// Try acquiring lock with timeout
	deadline := time.Now().Add(lockTimeout)
	for {
		err := unix.Flock(int(lockFile.Fd()), how|unix.LOCK_NB)
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for flock on %s after %v", lockPath, lockTimeout)
		}
		time.Sleep(10 * time.Millisecond)
	}
	defer func() { _ = unix.Flock(int(lockFile.Fd()), unix.LOCK_UN) }()

	return fn()
}

func (s *ActorStore) readStoreUnlocked() (map[string][]string, error) {
	if _, err := os.Stat(s.filePath); os.IsNotExist(err) {
		return make(map[string][]string), nil
	}

	data, err := os.ReadFile(s.filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read actor file %s: %w", s.filePath, err)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return make(map[string][]string), nil
	}

	var res map[string][]string
	if err := json.Unmarshal(data, &res); err != nil {
		return nil, fmt.Errorf("failed to unmarshal actor file JSON: %w", err)
	}
	return res, nil
}

func (s *ActorStore) writeStoreUnlocked(storeMap map[string][]string) error {
	dir := filepath.Dir(s.filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create actor dir %s: %w", dir, err)
	}

	// Format with 2-space indentation and SetEscapeHTML(false)
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(storeMap); err != nil {
		return fmt.Errorf("failed to encode actor store: %w", err)
	}

	// Atomic write using temp file + Rename
	tmpFile, err := os.CreateTemp(dir, "actor_*.tmp")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpName := tmpFile.Name()
	defer func() { _ = os.Remove(tmpName) }()

	if _, err := tmpFile.Write(buf.Bytes()); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("failed to write to temp file: %w", err)
	}
	if err := tmpFile.Sync(); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("failed to sync temp file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("failed to close temp file: %w", err)
	}

	if err := os.Rename(tmpName, s.filePath); err != nil {
		return fmt.Errorf("failed to atomic rename %s to %s: %w", tmpName, s.filePath, err)
	}

	return nil
}
