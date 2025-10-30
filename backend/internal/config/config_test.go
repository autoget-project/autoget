package config

import (
	"os"
	"testing"

	dlconfig "github.com/autoget-project/autoget/backend/downloaders/config"
	"github.com/autoget-project/autoget/backend/indexers/mteam"
	"github.com/autoget-project/autoget/backend/indexers/nyaa"
	"github.com/autoget-project/autoget/backend/internal/notify/telegram"
	"github.com/stretchr/testify/assert"
)

func TestReadConfig(t *testing.T) {
	// Test case 1: Config with Sukebei
	t.Run("Config with Sukebei", func(t *testing.T) {
		configContent := `
port: "8080"
proxy_url: "http://localhost:8888"
pg_dsn: dsn
organizer_service: "http://organizer:8080"
telegram:
  token: "telegram_token"
  chat_id: "telegram_chat_id"
mteam:
  base_url: "http://mteam.example.com"
  api_key: "mteam_key"
  downloader: "transmission"
nyaa:
  base_url: "http://nyaa.example.com"
  use_proxy: true
  downloader: "transmission"
sukebei:
  base_url: "http://sukebei.example.com"
  downloader: "transmission"
downloaders:
  transmission:
    transmission:
      url: "http://localhost:9091"
      torrents_dir: "/tmp/torrents"
      download_dir: "/tmp/downloads"
      finished_dir: "/tmp/finished"
`
		tmpFile, err := os.CreateTemp("", "config_with_sukebei_*.yaml")
		assert.NoError(t, err)
		defer os.Remove(tmpFile.Name())
		_, err = tmpFile.WriteString(configContent)
		assert.NoError(t, err)
		tmpFile.Close()

		cfg, err := ReadConfig(tmpFile.Name())
		assert.NoError(t, err)
		assert.NotNil(t, cfg)

		assert.Equal(t, "8080", cfg.Port)
		assert.Equal(t, "http://localhost:8888", cfg.ProxyURL)
		assert.Equal(t, "http://organizer:8080", cfg.OrganizerService)
		assert.NotNil(t, cfg.Telegram)
		assert.Equal(t, "telegram_token", cfg.Telegram.Token)
		assert.Equal(t, "telegram_chat_id", cfg.Telegram.ChatID)
		assert.NotNil(t, cfg.MTeam)
		assert.Equal(t, "http://mteam.example.com", cfg.MTeam.BaseURL)
		assert.Equal(t, "mteam_key", cfg.MTeam.APIKey)
		assert.Equal(t, "transmission", cfg.MTeam.Downloader)
		assert.NotNil(t, cfg.Nyaa)
		assert.Equal(t, "http://nyaa.example.com", cfg.Nyaa.BaseURL)
		assert.True(t, cfg.Nyaa.UseProxy)
		assert.Equal(t, "transmission", cfg.Nyaa.Downloader)
		assert.NotNil(t, cfg.Sukebei)
		assert.Equal(t, "http://sukebei.example.com", cfg.Sukebei.BaseURL)
		assert.Equal(t, "transmission", cfg.Sukebei.Downloader)
		assert.NotNil(t, cfg.Downloaders["transmission"])
		assert.Equal(t, "http://localhost:9091", cfg.Downloaders["transmission"].Transmission.URL)
		assert.Equal(t, "/tmp/torrents", cfg.Downloaders["transmission"].Transmission.TorrentsDir)
		assert.Equal(t, "/tmp/downloads", cfg.Downloaders["transmission"].Transmission.DownloadDir)
	})

	// Test case 2: Config without Sukebei
	t.Run("Config without Sukebei", func(t *testing.T) {
		configContent := `
port: "8081"
proxy_url: "http://localhost:9999"
pg_dsn: dsn
organizer_service: "http://organizer:8081"
telegram:
  token: "telegram_token_2"
  chat_id: "telegram_chat_id_2"
mteam:
  base_url: "http://mteam.example.org"
  api_key: "mteam_key_2"
  downloader: "transmission"
nyaa:
  base_url: "http://nyaa.example.org"
  downloader: "transmission"
downloaders:
  transmission:
    transmission:
      url: "http://localhost:9091"
      torrents_dir: "/tmp/torrents"
      download_dir: "/tmp/downloads"
      finished_dir: "/tmp/finished"
`
		tmpFile, err := os.CreateTemp("", "config_without_sukebei_*.yaml")
		assert.NoError(t, err)
		defer os.Remove(tmpFile.Name())
		_, err = tmpFile.WriteString(configContent)
		assert.NoError(t, err)
		tmpFile.Close()

		cfg, err := ReadConfig(tmpFile.Name())
		assert.NoError(t, err)
		assert.NotNil(t, cfg)

		assert.Equal(t, "8081", cfg.Port)
		assert.Equal(t, "http://localhost:9999", cfg.ProxyURL)
		assert.Equal(t, "http://organizer:8081", cfg.OrganizerService)
		assert.NotNil(t, cfg.Telegram)
		assert.Equal(t, "telegram_token_2", cfg.Telegram.Token)
		assert.Equal(t, "telegram_chat_id_2", cfg.Telegram.ChatID)
		assert.NotNil(t, cfg.MTeam)
		assert.Equal(t, "http://mteam.example.org", cfg.MTeam.BaseURL)
		assert.Equal(t, "mteam_key_2", cfg.MTeam.APIKey)
		assert.Equal(t, "transmission", cfg.MTeam.Downloader)
		assert.NotNil(t, cfg.Nyaa)
		assert.Equal(t, "http://nyaa.example.org", cfg.Nyaa.BaseURL)
		assert.Equal(t, "transmission", cfg.Nyaa.Downloader)
		assert.Nil(t, cfg.Sukebei) // Sukebei should be nil
		assert.NotNil(t, cfg.Downloaders["transmission"])
		assert.Equal(t, "http://localhost:9091", cfg.Downloaders["transmission"].Transmission.URL)
		assert.Equal(t, "/tmp/torrents", cfg.Downloaders["transmission"].Transmission.TorrentsDir)
		assert.Equal(t, "/tmp/downloads", cfg.Downloaders["transmission"].Transmission.DownloadDir)
	})
}

func TestConfig_validate(t *testing.T) {
	tests := []struct {
		name    string
		config  *Config
		wantErr string
	}{
		{
			name: "Valid config",
			config: &Config{
				PgDSN:            "dsn",
				OrganizerService: "http://organizer.svc",
				Telegram: &telegram.Config{
					Token:  "test_token",
					ChatID: "test_chat_id",
				},
				MTeam: &mteam.Config{
					APIKey:     "test_key",
					Downloader: "test_downloader",
				},
				Nyaa: &nyaa.Config{
					Downloader: "test_downloader",
				},
				Sukebei: &nyaa.Config{
					Downloader: "test_downloader",
				},
				Downloaders: map[string]*dlconfig.DownloaderConfig{
					"test_downloader": {
						Transmission: &dlconfig.TransmissionConfig{
							URL:         "http://localhost:9091",
							TorrentsDir: "/tmp/torrents",
							DownloadDir: "/tmp/downloads",
							FinishedDir: "/tmp/finished",
						},
					},
				},
			},
			wantErr: "",
		},
		{
			name: "Missing organizer_service",
			config: &Config{
				PgDSN: "dsn",
				Telegram: &telegram.Config{
					Token:  "test_token",
					ChatID: "test_chat_id",
				},
			},
			wantErr: "organizer_service is required",
		},
		{
			name: "Missing Telegram config",
			config: &Config{
				PgDSN:            "dsn",
				OrganizerService: "http://organizer.svc",
				MTeam: &mteam.Config{
					APIKey:     "test_key",
					Downloader: "test_downloader",
				},
				Downloaders: map[string]*dlconfig.DownloaderConfig{
					"test_downloader": {
						Transmission: &dlconfig.TransmissionConfig{
							URL:         "http://localhost:9091",
							TorrentsDir: "/tmp/torrents",
							DownloadDir: "/tmp/downloads",
						},
					},
				},
			},
			wantErr: "telegram config is required",
		},
		{
			name: "Telegram missing token",
			config: &Config{
				PgDSN:            "dsn",
				OrganizerService: "http://organizer.svc",
				Telegram: &telegram.Config{
					ChatID: "test_chat_id",
				},
				MTeam: &mteam.Config{
					APIKey:     "test_key",
					Downloader: "test_downloader",
				},
				Downloaders: map[string]*dlconfig.DownloaderConfig{
					"test_downloader": {
						Transmission: &dlconfig.TransmissionConfig{
							URL:         "http://localhost:9091",
							TorrentsDir: "/tmp/torrents",
							DownloadDir: "/tmp/downloads",
						},
					},
				},
			},
			wantErr: "telegram token is required",
		},
		{
			name: "Telegram missing chat ID",
			config: &Config{
				PgDSN:            "dsn",
				OrganizerService: "http://organizer.svc",
				Telegram: &telegram.Config{
					Token: "test_token",
				},
				MTeam: &mteam.Config{
					APIKey:     "test_key",
					Downloader: "test_downloader",
				},
				Downloaders: map[string]*dlconfig.DownloaderConfig{
					"test_downloader": {
						Transmission: &dlconfig.TransmissionConfig{
							URL:         "http://localhost:9091",
							TorrentsDir: "/tmp/torrents",
							DownloadDir: "/tmp/downloads",
						},
					},
				},
			},
			wantErr: "telegram chat ID is required",
		},
		{
			name: "MTeam missing API key",
			config: &Config{
				PgDSN:            "dsn",
				OrganizerService: "http://organizer.svc",
				Telegram: &telegram.Config{
					Token:  "test_token",
					ChatID: "test_chat_id",
				},
				MTeam: &mteam.Config{
					Downloader: "test_downloader",
				},
				Downloaders: map[string]*dlconfig.DownloaderConfig{
					"test_downloader": {
						Transmission: &dlconfig.TransmissionConfig{
							URL:         "http://localhost:9091",
							TorrentsDir: "/tmp/torrents",
							DownloadDir: "/tmp/downloads",
						},
					},
				},
			},
			wantErr: "m-team API key is required",
		},
		{
			name: "MTeam missing downloader",
			config: &Config{
				PgDSN:            "dsn",
				OrganizerService: "http://organizer.svc",
				Telegram: &telegram.Config{
					Token:  "test_token",
					ChatID: "test_chat_id",
				},
				MTeam: &mteam.Config{
					APIKey: "test_key",
				},
				Downloaders: map[string]*dlconfig.DownloaderConfig{
					"test_downloader": {
						Transmission: &dlconfig.TransmissionConfig{
							URL:         "http://localhost:9091",
							TorrentsDir: "/tmp/torrents",
							DownloadDir: "/tmp/downloads",
						},
					},
				},
			},
			wantErr: "m-team downloader is required",
		},
		{
			name: "MTeam unknown downloader",
			config: &Config{
				PgDSN:            "dsn",
				OrganizerService: "http://organizer.svc",
				Telegram: &telegram.Config{
					Token:  "test_token",
					ChatID: "test_chat_id",
				},
				MTeam: &mteam.Config{
					APIKey:     "test_key",
					Downloader: "unknown_downloader",
				},
				Downloaders: map[string]*dlconfig.DownloaderConfig{
					"test_downloader": {
						Transmission: &dlconfig.TransmissionConfig{
							URL:         "http://localhost:9091",
							TorrentsDir: "/tmp/torrents",
							DownloadDir: "/tmp/downloads",
						},
					},
				},
			},
			wantErr: "unknown m-team downloader: unknown_downloader",
		},
		{
			name: "Nyaa missing downloader",
			config: &Config{
				PgDSN:            "dsn",
				OrganizerService: "http://organizer.svc",
				Telegram: &telegram.Config{
					Token:  "test_token",
					ChatID: "test_chat_id",
				},
				Nyaa: &nyaa.Config{},
				Downloaders: map[string]*dlconfig.DownloaderConfig{
					"test_downloader": {
						Transmission: &dlconfig.TransmissionConfig{
							URL:         "http://localhost:9091",
							TorrentsDir: "/tmp/torrents",
							DownloadDir: "/tmp/downloads",
						},
					},
				},
			},
			wantErr: "nyaa downloader is required",
		},
		{
			name: "Nyaa unknown downloader",
			config: &Config{
				PgDSN:            "dsn",
				OrganizerService: "http://organizer.svc",
				Telegram: &telegram.Config{
					Token:  "test_token",
					ChatID: "test_chat_id",
				},
				Nyaa: &nyaa.Config{
					Downloader: "unknown_downloader",
				},
				Downloaders: map[string]*dlconfig.DownloaderConfig{
					"test_downloader": {
						Transmission: &dlconfig.TransmissionConfig{
							URL:         "http://localhost:9091",
							TorrentsDir: "/tmp/torrents",
							DownloadDir: "/tmp/downloads",
						},
					},
				},
			},
			wantErr: "unknown nyaa downloader: unknown_downloader",
		},
		{
			name: "Sukebei missing downloader",
			config: &Config{
				PgDSN:            "dsn",
				OrganizerService: "http://organizer.svc",
				Telegram: &telegram.Config{
					Token:  "test_token",
					ChatID: "test_chat_id",
				},
				Sukebei: &nyaa.Config{},
				Downloaders: map[string]*dlconfig.DownloaderConfig{
					"test_downloader": {
						Transmission: &dlconfig.TransmissionConfig{
							URL:         "http://localhost:9091",
							TorrentsDir: "/tmp/torrents",
							DownloadDir: "/tmp/downloads",
						},
					},
				},
			},
			wantErr: "sukebei downloader is required",
		},
		{
			name: "Sukebei unknown downloader",
			config: &Config{
				PgDSN:            "dsn",
				OrganizerService: "http://organizer.svc",
				Telegram: &telegram.Config{
					Token:  "test_token",
					ChatID: "test_chat_id",
				},
				Sukebei: &nyaa.Config{
					Downloader: "unknown_downloader",
				},
				Downloaders: map[string]*dlconfig.DownloaderConfig{
					"test_downloader": {
						Transmission: &dlconfig.TransmissionConfig{
							URL:         "http://localhost:9091",
							TorrentsDir: "/tmp/torrents",
							DownloadDir: "/tmp/downloads",
						},
					},
				},
			},
			wantErr: "unknown sukebei downloader: unknown_downloader",
		},
		{
			name: "Invalid downloader config (missing transmission config)",
			config: &Config{
				PgDSN:            "dsn",
				OrganizerService: "http://organizer.svc",
				Telegram: &telegram.Config{
					Token:  "test_token",
					ChatID: "test_chat_id",
				},
				Downloaders: map[string]*dlconfig.DownloaderConfig{
					"invalid_downloader": {}, // Missing Transmission config
				},
			},
			wantErr: "invalid downloader config for invalid_downloader: transmission config is required",
		},
		{
			name: "Invalid downloader config (invalid transmission URL)",
			config: &Config{
				PgDSN:            "dsn",
				OrganizerService: "http://organizer.svc",
				Telegram: &telegram.Config{
					Token:  "test_token",
					ChatID: "test_chat_id",
				},
				Downloaders: map[string]*dlconfig.DownloaderConfig{
					"invalid_downloader": {
						Transmission: &dlconfig.TransmissionConfig{
							URL:         "", // Invalid URL
							TorrentsDir: "/tmp/torrents",
							DownloadDir: "/tmp/downloads",
						},
					},
				},
			},
			wantErr: "invalid downloader config for invalid_downloader: transmission RPC URL is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.validate()
			if tt.wantErr != "" {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
