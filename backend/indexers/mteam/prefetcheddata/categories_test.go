package prefetcheddata

import (
	"encoding/json"
	"os"
	"testing"

	_ "embed"

	"github.com/autoget-project/autoget/backend/indexers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	//go:embed test_res/list_categories.json
	testToCategoriesInput []byte

	apiKey = os.Getenv("MTEAM_API_KEY")
)

func TestToCategoryJSON(t *testing.T) {
	categories := &listCategories{}
	err := json.Unmarshal(testToCategoriesInput, categories)
	require.NoError(t, err)

	got := categories.toCategoryJSON(false)
	want := &categoryJSON{
		CategoryTree: []*categoryWithOrder{
			{
				ID:   "normal",
				Name: "normal",
				SubCategories: []*categoryWithOrder{
					{
						ID:        "110",
						Name:      "Music",
						Order:     4,
						NumericID: 110,
						SubCategories: []*categoryWithOrder{
							{
								ID:        "434",
								Name:      "Music(无损)",
								Order:     1,
								NumericID: 434,
							},
						},
					},
				},
			},
			{
				ID:   "adult",
				Name: "adult",
				SubCategories: []*categoryWithOrder{
					{
						ID:        "115",
						Name:      "AV(有码)",
						Order:     20,
						NumericID: 115,
						SubCategories: []*categoryWithOrder{
							{
								ID:        "410",
								Name:      "AV(有码)/HD Censored",
								Order:     31,
								NumericID: 410,
							},
							{
								ID:        "440",
								Name:      "AV(Gay)/HD",
								Order:     440,
								NumericID: 440,
							},
						},
					},
				},
			},
		},
		CategoryInfos: map[string]*CategoryInfo{
			"110":    {"Music", "normal", []string{"434"}, []indexers.OrganizerCategory{indexers.OrganizerCategoryMusic, indexers.OrganizerCategoryMusicVideo}},
			"115":    {"AV(有码)", "adult", []string{"410", "440"}, []indexers.OrganizerCategory{indexers.OrganizerCategoryBangoPorn, indexers.OrganizerCategoryPorn}},
			"410":    {"AV(有码)/HD Censored", "adult", []string{"410"}, []indexers.OrganizerCategory{indexers.OrganizerCategoryBangoPorn, indexers.OrganizerCategoryPorn}},
			"434":    {"Music(无损)", "normal", []string{"434"}, []indexers.OrganizerCategory{indexers.OrganizerCategoryMusic}},
			"440":    {"AV(Gay)/HD", "adult", []string{"440"}, []indexers.OrganizerCategory{indexers.OrganizerCategoryBangoPorn, indexers.OrganizerCategoryPorn}},
			"adult":  {"adult", "adult", []string{}, []indexers.OrganizerCategory{}},
			"normal": {"normal", "normal", []string{}, []indexers.OrganizerCategory{}},
		},
	}
	assert.Equal(t, want, got)
}
