package db

import (
	"database/sql"
	"os"
	"time"

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
		PrepareStmt:    true,
		TranslateError: true,
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
	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	configurePool(sqlDB)
	if err := Migrate(db); err != nil {
		return nil, err
	}
	return db, nil
}

// configurePool sets connection pool limits suited to a single-user app.
func configurePool(sqlDB *sql.DB) {
	sqlDB.SetMaxOpenConns(25)
	sqlDB.SetMaxIdleConns(5)
	sqlDB.SetConnMaxLifetime(30 * time.Minute)
	sqlDB.SetConnMaxIdleTime(5 * time.Minute)
}

// Config returns the shared gorm configuration, for callers that open their
// own dialector (e.g. the dbtest package).
func Config() *gorm.Config {
	return gormConfig
}

// Migrate creates or updates all tables.
func Migrate(db *gorm.DB) error {
	return db.AutoMigrate(
		&DownloadStatus{},
		&RSSSearch{},
	)
}
