package prefetcheddata

import (
	"net/http"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/autoget-project/autoget/backend/indexers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	baseURL = "https://nyaa.si/"
)

func TestCategories(t *testing.T) {
	res, err := http.Get(baseURL)
	require.NoError(t, err)

	defer res.Body.Close()
	require.Equal(t, 200, res.StatusCode)

	// Load the HTML document
	doc, err := goquery.NewDocumentFromReader(res.Body)
	require.NoError(t, err)

	got := map[string]indexers.Category{}

	doc.Find("select[name='c'] option").Each(func(i int, s *goquery.Selection) {
		id, exists := s.Attr("value")
		if !exists {
			return // Skip if value attribute doesn't exist
		}
		name, exists := s.Attr("title")
		if !exists {
			return // Skip if title attribute doesn't exist
		}

		category := indexers.Category{
			ID:   id,
			Name: name,
		}
		got[id] = category
	})

	assert.Equal(t, Categories, got)
}
