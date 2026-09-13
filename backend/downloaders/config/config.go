package config

import (
	"fmt"

	"github.com/autoget-project/autoget/backend/internal/db"
)

type TransmissionConfig struct {
	URL         string `yaml:"url"`
	TorrentsDir string `yaml:"torrents_dir"`
	DownloadDir string `yaml:"download_dir"`
	FinishedDir string `yaml:"finished_dir"`
	Username    string `yaml:"username"`
	Password    string `yaml:"password"`
}

func (c *TransmissionConfig) Validate() error {
	if c.URL == "" {
		return fmt.Errorf("transmission RPC URL is required")
	}
	if c.TorrentsDir == "" {
		return fmt.Errorf("torrents directory is required")
	}
	if c.DownloadDir == "" {
		return fmt.Errorf("download directory is required")
	}
	if c.FinishedDir == "" {
		return fmt.Errorf("finished directory is required")
	}
	return nil
}

// SeedingPolicy we use at least X MB uploaded in last Y days as
// a condition to continue seeding.
type SeedingPolicy struct {
	IntervalInDays    int   `yaml:"interval_in_days"`
	UploadAtLeastInMB int64 `yaml:"upload_at_least_in_mb"`
}

func (p *SeedingPolicy) Validate() error {
	if p.IntervalInDays == 0 {
		return fmt.Errorf("interval in days is required")
	}
	if p.IntervalInDays > db.StoreMaxDays {
		return fmt.Errorf("interval in days should be less than 30")
	}
	if p.UploadAtLeastInMB == 0 {
		return fmt.Errorf("upload at least in MB is required")
	}
	return nil
}

type TransferConfig struct {
	Mode        string `yaml:"mode"`          // "local" or "http"
	ChunkSizeMB int64  `yaml:"chunk_size_mb"` // in MB, default 32
	MaxRetries  int    `yaml:"max_retries"`   // default 5
	Concurrency int    `yaml:"concurrency"`   // concurrent files per torrent, default 2
}

func (t *TransferConfig) Validate() error {
	if t.Mode == "" {
		t.Mode = "local"
	}
	if t.Mode != "local" && t.Mode != "http" {
		return fmt.Errorf("transfer mode must be 'local' or 'http', got %q", t.Mode)
	}
	if t.ChunkSizeMB <= 0 {
		t.ChunkSizeMB = 32
	}
	if t.MaxRetries < 0 {
		t.MaxRetries = 5
	}
	if t.Concurrency <= 0 {
		t.Concurrency = 2
	}
	return nil
}

type DownloaderConfig struct {
	Transmission  *TransmissionConfig `yaml:"transmission"`
	SeedingPolicy *SeedingPolicy      `yaml:"seeding_policy"`
	Transfer      *TransferConfig     `yaml:"transfer"`
}

func (c *DownloaderConfig) Validate() error {
	if c.Transmission == nil {
		return fmt.Errorf("transmission config is required")
	}
	if err := c.Transmission.Validate(); err != nil {
		return err
	}
	if c.SeedingPolicy != nil {
		if err := c.SeedingPolicy.Validate(); err != nil {
			return err
		}
	}
	if c.Transfer != nil {
		if err := c.Transfer.Validate(); err != nil {
			return err
		}
	}
	return nil
}
