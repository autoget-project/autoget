package prefetcheddata

import (
	"maps"
	"slices"
	"strings"

	"github.com/autoget-project/autoget/backend/indexers"
	nPrefetcheddata "github.com/autoget-project/autoget/backend/indexers/nyaa/prefetcheddata"
)

var (
	CategoryTree = map[string]indexers.Category{
		"0_0": {ID: "0_0", Name: "All categories", SubCategories: []indexers.Category{
			{ID: "1_0", Name: "Art", SubCategories: []indexers.Category{
				{ID: "1_1", Name: "Art - Anime"},
				{ID: "1_2", Name: "Art - Doujinshi"},
				{ID: "1_3", Name: "Art - Games"},
				{ID: "1_4", Name: "Art - Manga"},
				{ID: "1_5", Name: "Art - Pictures"},
			}},
			{ID: "2_0", Name: "Real Life", SubCategories: []indexers.Category{
				{ID: "2_1", Name: "Real Life - Pictures"},
				{ID: "2_2", Name: "Real Life - Videos"},
			}},
		}},
	}

	Categories = nPrefetcheddata.FlattenCategory(CategoryTree)

	CategoriesList = slices.SortedFunc(maps.Values(Categories), func(a, b indexers.Category) int {
		return strings.Compare(a.ID, b.ID)
	})
)
