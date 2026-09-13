package upload

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// GC periodically scans the upload root directory and purges expired sessions and orphaned files.
type GC struct {
	store       *Store
	expireHours time.Duration
}

// NewGC creates a new GC instance.
func NewGC(store *Store, expireHours time.Duration) *GC {
	if expireHours <= 0 {
		expireHours = 72 * time.Hour
	}
	return &GC{
		store:       store,
		expireHours: expireHours,
	}
}

// Run starts the periodic GC loop until ctx is cancelled.
func (g *GC) Run(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 1 * time.Hour
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			g.CleanOnce(time.Now())
		}
	}
}

// CleanOnce performs a single sweep over the uploads root directory.
func (g *GC) CleanOnce(now time.Time) {
	entries, err := os.ReadDir(g.store.root)
	if err != nil {
		log.Printf("upload gc: read dir %s error: %v", g.store.root, err)
		return
	}

	cutoff := now.Add(-g.expireHours).Unix()

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		fullPath := filepath.Join(g.store.root, name)

		if strings.HasSuffix(name, ".json") {
			uploadID := strings.TrimSuffix(name, ".json")
			g.checkAndCleanMeta(uploadID, fullPath, cutoff)
		} else if strings.HasSuffix(name, ".part") {
			uploadID := strings.TrimSuffix(name, ".part")
			metaFile := filepath.Join(g.store.root, uploadID+".json")
			if _, err := os.Stat(metaFile); os.IsNotExist(err) {
				// Orphaned .part file without metadata
				info, err := entry.Info()
				if err == nil && info.ModTime().Unix() < cutoff {
					_ = os.Remove(fullPath)
					g.store.removeLock(uploadID)
					log.Printf("upload gc: removed orphaned part file %s", name)
				}
			}
		}
	}
}

func (g *GC) checkAndCleanMeta(uploadID, metaPath string, cutoff int64) {
	data, err := os.ReadFile(metaPath)
	if err != nil {
		return
	}

	var meta UploadMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		// Corrupted metadata: remove if mod time is older than cutoff
		if fi, statErr := os.Stat(metaPath); statErr == nil && fi.ModTime().Unix() < cutoff {
			_ = os.Remove(metaPath)
			_ = os.Remove(filepath.Join(g.store.root, uploadID+".part"))
			g.store.removeLock(uploadID)
			log.Printf("upload gc: removed corrupted upload %s", uploadID)
		}
		return
	}

	if meta.UpdatedAt < cutoff {
		_ = g.store.Cancel(uploadID)
		log.Printf("upload gc: purged expired upload %s (last updated %s)", uploadID, time.Unix(meta.UpdatedAt, 0).Format(time.RFC3339))
	}
}
