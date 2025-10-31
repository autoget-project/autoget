package db

import (
	"os"
	"time"

	"github.com/glebarez/sqlite"
	zlog "github.com/rs/zerolog/log"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	glog "gorm.io/gorm/logger"
)

var (
	logger = zlog.With().Str("component", "db").Logger()

	gormConfig = &gorm.Config{
		Logger: glog.New(
			&logger,
			glog.Config{
				SlowThreshold:             100 * time.Millisecond,
				LogLevel:                  logLevel(),
				IgnoreRecordNotFoundError: false,
				ParameterizedQueries:      false,
				Colorful:                  false,
			},
		),
		PrepareStmt: true,
	}
)

func logLevel() glog.LogLevel {
	if os.Getenv("DB_DEBUG") != "" {
		return glog.Info
	}
	return glog.Warn
}

func Pg(dsn string) (*gorm.DB, error) {
	db, err := gorm.Open(postgres.Open(dsn), gormConfig)
	if err != nil {
		return nil, err
	}
	if err := migrate(db); err != nil {
		return nil, err
	}
	return db, nil
}

func SqliteForTest() (*gorm.DB, error) {
	db, err := gorm.Open(sqlite.Open(":memory:"), gormConfig)
	if err != nil {
		return nil, err
	}
	if err := migrate(db); err != nil {
		return nil, err
	}
	return db, nil
}

func migrate(db *gorm.DB) error {
	return db.AutoMigrate(
		&DownloadStatus{},
		&RSSSearch{},
	)
}
