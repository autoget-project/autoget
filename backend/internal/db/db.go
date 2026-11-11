package db

import (
	"database/sql"
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
	if err := migrate(db); err != nil {
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

func SqliteForTest() (*gorm.DB, error) {
	db, err := gorm.Open(sqlite.Open(":memory:"), gormConfig)
	if err != nil {
		return nil, err
	}
	// ":memory:" is per-connection; pin one connection so background
	// goroutines see the same database as the test.
	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	sqlDB.SetMaxOpenConns(1)
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
