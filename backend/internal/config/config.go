package config

import (
	"fmt"
	"os"

	dlconfig "github.com/autoget-project/autoget/backend/downloaders/config"
	"github.com/autoget-project/autoget/backend/indexers/mteam"
	"github.com/autoget-project/autoget/backend/indexers/nyaa"
	"github.com/autoget-project/autoget/backend/internal/notify/telegram"
	"gopkg.in/yaml.v3"
)

type Config struct {
	Port     string `yaml:"port"`
	ProxyURL string `yaml:"proxy_url"`
	PgDSN    string `yaml:"pg_dsn"`

	Telegram *telegram.Config `yaml:"telegram"`

	MTeam   *mteam.Config `yaml:"mteam"`
	Nyaa    *nyaa.Config  `yaml:"nyaa"`
	Sukebei *nyaa.Config  `yaml:"sukebei"`

	Downloaders map[string]*dlconfig.DownloaderConfig `yaml:"downloaders"`
}

func ReadConfig(path string) (*Config, error) {
	config := &Config{}

	b, err := os.ReadFile(path)
	yaml.Unmarshal(b, config)
	if err != nil {
		return nil, err
	}

	if config.Nyaa != nil {
		config.Nyaa.SetProxyURL(config.ProxyURL)
	}
	if config.Sukebei != nil {
		config.Sukebei.SetProxyURL(config.ProxyURL)
	}

	if err := config.validate(); err != nil {
		return nil, err
	}

	return config, nil
}

func (c *Config) validate() error {
	if c.PgDSN == "" {
		return fmt.Errorf("postgres DSN is required")
	}

	if c.Telegram == nil {
		return fmt.Errorf("telegram config is required")
	}

	if c.Telegram.Token == "" {
		return fmt.Errorf("telegram token is required")
	}
	if c.Telegram.ChatID == "" {
		return fmt.Errorf("telegram chat ID is required")
	}

	if c.MTeam != nil {
		if c.MTeam.APIKey == "" {
			return fmt.Errorf("m-team API key is required")
		}
		if c.MTeam.Downloader == "" {
			return fmt.Errorf("m-team downloader is required")
		}

		if _, ok := c.Downloaders[c.MTeam.Downloader]; !ok {
			return fmt.Errorf("unknown m-team downloader: %s", c.MTeam.Downloader)
		}
	}

	if c.Nyaa != nil {
		if c.Nyaa.Downloader == "" {
			return fmt.Errorf("nyaa downloader is required")
		}

		if _, ok := c.Downloaders[c.Nyaa.Downloader]; !ok {
			return fmt.Errorf("unknown nyaa downloader: %s", c.Nyaa.Downloader)
		}
	}

	if c.Sukebei != nil {
		if c.Sukebei.Downloader == "" {
			return fmt.Errorf("sukebei downloader is required")
		}

		if _, ok := c.Downloaders[c.Sukebei.Downloader]; !ok {
			return fmt.Errorf("unknown sukebei downloader: %s", c.Sukebei.Downloader)
		}
	}

	for name, downloader := range c.Downloaders {
		if err := downloader.Validate(); err != nil {
			return fmt.Errorf("invalid downloader config for %s: %v", name, err)
		}
	}
	return nil
}
