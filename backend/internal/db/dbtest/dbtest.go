// Package dbtest provides an in-memory sqlite database for tests. It lives in
// its own package so the sqlite driver is not linked into the production binary.
package dbtest

import (
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/autoget-project/autoget/backend/internal/db"
)

func SqliteForTest() (*gorm.DB, error) {
	gdb, err := gorm.Open(sqlite.Open(":memory:"), db.Config())
	if err != nil {
		return nil, err
	}
	// ":memory:" is per-connection; pin one connection so background
	// goroutines see the same database as the test.
	sqlDB, err := gdb.DB()
	if err != nil {
		return nil, err
	}
	sqlDB.SetMaxOpenConns(1)
	if err := db.Migrate(gdb); err != nil {
		return nil, err
	}
	return gdb, nil
}
