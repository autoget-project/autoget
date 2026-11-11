package db

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigurePool(t *testing.T) {
	gdb, err := SqliteForTest()
	require.NoError(t, err)
	sqlDB, err := gdb.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	configurePool(sqlDB)
	assert.Equal(t, 25, sqlDB.Stats().MaxOpenConnections)
}
