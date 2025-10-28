package mteam

import (
	"time"

	_ "embed"

	"github.com/autoget-project/autoget/backend/indexers"
	"github.com/autoget-project/autoget/backend/indexers/mteam/prefetcheddata"
	"github.com/autoget-project/autoget/backend/internal/errors"
	"github.com/autoget-project/autoget/backend/internal/notify"
	"github.com/rs/zerolog/log"
	"gorm.io/gorm"
)

var (
	_ indexers.IIndexer = (*MTeam)(nil)

	logger = log.With().Str("indexer", name).Logger()
)

const (
	name           = "m-team"
	defaultBaseURL = "https://api.m-team.cc"

	categoryAdult   = "adult"
	categoryNormal  = "normal"
	categoryGayPorn = "440"

	httpTimeout = 5 * time.Second
)

var (
	//go:embed prefetcheddata/data.json
	categoriesJSON []byte
)

type Config struct {
	BaseURL           string `yaml:"base_url"`
	APIKey            string `yaml:"api_key"`
	ExcludeGayContent bool   `yaml:"exclude_gay_content"`
	RSS               string `yaml:"rss"`

	Downloader string `yaml:"downloader"`
}

func (c *Config) getBaseURL() string {
	if c.BaseURL == "" {
		return defaultBaseURL
	}
	return c.BaseURL
}

type MTeamType int

const (
	MTeamTypeNormal MTeamType = iota
	MTeamTypeAdult
)

type MTeam struct {
	indexers.IndexerBasicInfo

	mType MTeamType

	config *Config
	db     *gorm.DB
	notify notify.INotifier

	prefetched *prefetcheddata.Data
	standards  map[string]string

	torrentsDir string
}

func NewMTeam(config *Config, mType MTeamType, torrentsDir string, db *gorm.DB, notify notify.INotifier) *MTeam {
	if config.APIKey == "" {
		return nil
	}

	n := name
	if mType == MTeamTypeAdult {
		n += ":adult"
	}

	m := &MTeam{
		IndexerBasicInfo: *indexers.NewIndexerBasicInfo(n, config.Downloader, true),
		mType:            mType,
		config:           config,
		db:               db,
		standards:        map[string]string{},
		torrentsDir:      torrentsDir,
		notify:           notify,
	}

	var err error
	m.prefetched, err = prefetcheddata.Read()
	if err != nil {
		logger.Fatal().Err(err).Msgf("Failed to read prefetched data: %v", err)
	}

	for k, v := range m.prefetched.Standards {
		m.standards[v] = k
	}

	return m
}

func (m *MTeam) Categories() ([]indexers.Category, *errors.HTTPStatusError) {
	if m.mType == MTeamTypeAdult {
		return []indexers.Category{m.prefetched.Categories.Tree[1]}, nil
	} else {
		return []indexers.Category{m.prefetched.Categories.Tree[0]}, nil
	}
}
