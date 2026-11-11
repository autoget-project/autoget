package db

import (
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// SqliteForTest lives here (not in dbtest) because dbtest imports db.
func SqliteForTest() (*gorm.DB, error) {
	gdb, err := gorm.Open(sqlite.Open(":memory:"), gormConfig)
	if err != nil {
		return nil, err
	}
	sqlDB, err := gdb.DB()
	if err != nil {
		return nil, err
	}
	sqlDB.SetMaxOpenConns(1)
	if err := Migrate(gdb); err != nil {
		return nil, err
	}
	return gdb, nil
}
