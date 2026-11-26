package db

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestAddSearch(t *testing.T) {
	db, err := SqliteForTest()
	require.NoError(t, err)

	search := &RSSSearch{
		Indexer: "testIndexer",
		Text:    "testText",
		Action:  "testAction",
		URL:     "http://test.com",
	}

	err = AddSearch(db, search)
	assert.NoError(t, err)

	var retrievedSearch RSSSearch
	err = db.First(&retrievedSearch, search.ID).Error
	assert.NoError(t, err)
	assert.Equal(t, search.Indexer, retrievedSearch.Indexer)
	assert.Equal(t, search.Text, retrievedSearch.Text)
	assert.Equal(t, search.Action, retrievedSearch.Action)
	assert.Equal(t, search.URL, retrievedSearch.URL)
}

func TestGetSearchsByIndexer(t *testing.T) {
	db, err := SqliteForTest()
	require.NoError(t, err)

	// Add some test data
	db.Create(&RSSSearch{Indexer: "indexer1", Text: "text1", Action: "action1", URL: "url1"})
	db.Create(&RSSSearch{Indexer: "indexer1", Text: "text2", Action: "action2", URL: "url2"})
	db.Create(&RSSSearch{Indexer: "indexer2", Text: "text3", Action: "action3", URL: "url3"})

	// Test with existing indexer
	searchs, err := GetSearchsByIndexer(db, "indexer1")
	assert.NoError(t, err)
	assert.Len(t, searchs, 2)
	assert.Equal(t, "text1", searchs[0].Text)
	assert.Equal(t, "text2", searchs[1].Text)

	// Test with non-existing indexer
	searchs, err = GetSearchsByIndexer(db, "nonExistentIndexer")
	assert.NoError(t, err)
	assert.Len(t, searchs, 0)
}

func TestDeleteSearch(t *testing.T) {
	db, err := SqliteForTest()
	require.NoError(t, err)

	// Add a search to delete
	search := RSSSearch{
		Indexer: "deleteIndexer",
		Text:    "deleteText",
		Action:  "deleteAction",
		URL:     "http://delete.com",
	}
	db.Create(&search)

	// Test successful deletion
	err = DeleteSearch(db, search.ID)
	assert.NoError(t, err)

	var deletedSearch RSSSearch
	err = db.First(&deletedSearch, search.ID).Error
	assert.Error(t, err) // Should return an error as record is not found
	assert.Equal(t, gorm.ErrRecordNotFound, err)

	// Test deleting a non-existent search (should not return an error)
	err = DeleteSearch(db, 999) // Assuming 999 is a non-existent ID
	assert.NoError(t, err)
}

func TestRSSSearch_IndexerIsIndexed(t *testing.T) {
	d, err := SqliteForTest()
	require.NoError(t, err)

	assert.True(t, d.Migrator().HasIndex(&RSSSearch{}, "Indexer"))
}
