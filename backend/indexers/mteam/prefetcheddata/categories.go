package prefetcheddata

import (
	"sort"
	"strconv"

	"github.com/autoget-project/autoget/backend/indexers"
	"github.com/rs/zerolog/log"
)

const (
	categoryAdult   = "adult"
	categoryNormal  = "normal"
	categoryGayPorn = "440"

	baseURL = "https://api.m-team.cc"
)

var (
	rootCategories = map[string]string{
		"100": categoryNormal, // Movie
		"105": categoryNormal, // TV Series
		"444": categoryNormal, // Documentary
		"110": categoryNormal, // Music
		"443": categoryNormal, // edu
		"447": categoryNormal, // Game
		"449": categoryNormal, // Anime
		"450": categoryNormal, // Others
		"115": categoryAdult,  // AV Censored
		"120": categoryAdult,  // AV Uncensored
		"445": categoryAdult,  // IV
		"446": categoryAdult,  // HCG
	}
)

type listCategories struct {
	Data struct {
		List []struct {
			CreatedDate      string `json:"createdDate"`
			LastModifiedDate string `json:"lastModifiedDate"`
			ID               string `json:"id"`
			Order            string `json:"order"`
			NameChs          string `json:"nameChs"`
			NameCht          string `json:"nameCht"`
			NameEng          string `json:"nameEng"`
			Image            string `json:"image"`
			Parent           string `json:"parent"`
		} `json:"list"`

		// We don't use following fields because they don't contains
		// all subcategories. For example the parent of tvshow(105).
		Adult  []string `json:"adult"`
		Movie  []string `json:"movie"`
		Music  []string `json:"music"`
		Tvshow []string `json:"tvshow"`

		// We don't use following fields
		Waterfall []string `json:"waterfall"`
	} `json:"data"`

	// We don't use following fields
	Code    interface{} `json:"code"`
	Message string      `json:"message"`
}

// categoryWithOrder has same json definition with indexers.Category.
type categoryWithOrder struct {
	ID            string               `json:"id"`
	Name          string               `json:"name"`
	SubCategories []*categoryWithOrder `json:"subCategories,omitempty"`
	Order         int
	NumericID     int
}

type CategoryInfo struct {
	Name              string                       `json:"name"`
	Mode              string                       `json:"mode"`
	Categories        []string                     `json:"categories"` // You can not search resources on "115" but need to includes all sub.
	OrganizerCategory []indexers.OrganizerCategory `json:"organizer_category"`
}

type categoryJSON struct {
	CategoryTree  []*categoryWithOrder     `json:"tree"`
	CategoryInfos map[string]*CategoryInfo `json:"flat"`
}

func (l *listCategories) toCategoryJSON(excludeGayContent bool) *categoryJSON {
	adultRoot := &categoryWithOrder{
		ID:   categoryAdult,
		Name: categoryAdult,
	}
	normalRoot := &categoryWithOrder{
		ID:   categoryNormal,
		Name: categoryNormal,
	}
	roots := []*categoryWithOrder{
		normalRoot,
		adultRoot,
	}

	categories := map[string]*categoryWithOrder{
		categoryAdult:  adultRoot,
		categoryNormal: normalRoot,
	}

	for _, cat := range l.Data.List {
		if excludeGayContent && cat.ID == categoryGayPorn {
			continue
		}
		id, err := strconv.Atoi(cat.ID)
		if err != nil {
			log.Fatal().Msgf("Category ID is not a number: %s", cat.ID)
		}
		order, err := strconv.Atoi(cat.Order)
		if err != nil {
			log.Fatal().Msgf("Category Order is not a number: id = %s, order = %s", cat.ID, cat.Order)
		}

		categories[cat.ID] = &categoryWithOrder{
			ID:        cat.ID,
			Name:      cat.NameChs,
			Order:     order,
			NumericID: id,
		}
	}

	for _, cat := range l.Data.List {
		if excludeGayContent && cat.ID == categoryGayPorn {
			continue
		}
		parent := cat.Parent
		if parent == "" {
			var ok bool
			parent, ok = rootCategories[cat.ID]
			if !ok {
				log.Fatal().Msgf("Got unknown root category: %s %s", cat.ID, cat.NameChs)
			}
		}

		p, ok := categories[parent]
		if !ok {
			log.Fatal().Msgf("Category %s has unknown parent %s", cat.ID, parent)
		}

		p.SubCategories = append(p.SubCategories, categories[cat.ID])
	}

	sortSubCategories(adultRoot)
	sortSubCategories(normalRoot)

	categoryInfos := map[string]*CategoryInfo{}
	categoryInfo(adultRoot, categoryInfos, categoryAdult)
	categoryInfo(normalRoot, categoryInfos, categoryNormal)

	addOrganizerCategory(categoryInfos)

	return &categoryJSON{
		CategoryTree:  roots,
		CategoryInfos: categoryInfos,
	}
}

func sortSubCategories(category *categoryWithOrder) {
	sort.SliceStable(category.SubCategories, func(i, j int) bool {
		if category.SubCategories[i].Order != category.SubCategories[j].Order {
			return category.SubCategories[i].Order < category.SubCategories[j].Order
		}
		return category.SubCategories[i].NumericID < category.SubCategories[j].NumericID
	})

	for _, sub := range category.SubCategories {
		sortSubCategories(sub)
	}
}

func categoryInfo(categories *categoryWithOrder, m map[string]*CategoryInfo, mode string) {
	subs := []string{}
	if categories.Name != categoryAdult && categories.Name != categoryNormal {
		for _, sub := range categories.SubCategories {
			subs = append(subs, sub.ID)
		}
		if len(subs) == 0 {
			subs = append(subs, categories.ID)
		}
	}

	m[categories.ID] = &CategoryInfo{
		Name:       categories.Name,
		Mode:       mode,
		Categories: subs,
	}

	for _, sub := range categories.SubCategories {
		categoryInfo(sub, m, mode)
	}
}

var toOrganizerCategory = map[string][]indexers.OrganizerCategory{
	// Movies
	"100": {indexers.OrganizerCategoryMovie}, // 电影
	"401": {indexers.OrganizerCategoryMovie}, // 电影/SD
	"419": {indexers.OrganizerCategoryMovie}, // 电影/HD
	"420": {indexers.OrganizerCategoryMovie}, // 电影/DVDiSo
	"421": {indexers.OrganizerCategoryMovie}, // 电影/Blu-Ray
	"439": {indexers.OrganizerCategoryMovie}, // 电影/Remux

	// TV Series & Shows
	"105": {indexers.OrganizerCategoryTVSeries}, // 影剧/综艺
	"403": {indexers.OrganizerCategoryTVSeries}, // 影剧/综艺/SD
	"402": {indexers.OrganizerCategoryTVSeries}, // 影剧/综艺/HD
	"438": {indexers.OrganizerCategoryTVSeries}, // 影剧/综艺/BD
	"435": {indexers.OrganizerCategoryTVSeries}, // 影剧/综艺/DVDiSo

	// Documentary
	"444": {indexers.OrganizerCategoryTVSeries}, // 紀錄
	"404": {indexers.OrganizerCategoryTVSeries}, // 纪录

	// Music
	"110": {indexers.OrganizerCategoryMusic, indexers.OrganizerCategoryMusicVideo}, // Music
	"434": {indexers.OrganizerCategoryMusic},                                       // Music(无损)
	"406": {indexers.OrganizerCategoryMusicVideo},                                  // 演唱

	// Anime
	"449": {indexers.OrganizerCategoryTVSeries, indexers.OrganizerCategoryMovie}, // 動漫
	"405": {indexers.OrganizerCategoryTVSeries, indexers.OrganizerCategoryMovie}, // 动画

	// Others
	"427": {indexers.OrganizerCategoryBook},      // 電子書
	"442": {indexers.OrganizerCategoryAudioBook}, // 有聲書

	// Adult Content
	"115": {indexers.OrganizerCategoryBangoPorn, indexers.OrganizerCategoryPorn}, // AV(有码)
	"410": {indexers.OrganizerCategoryBangoPorn, indexers.OrganizerCategoryPorn}, // AV(有码)/HD Censored
	"424": {indexers.OrganizerCategoryBangoPorn, indexers.OrganizerCategoryPorn}, // AV(有码)/SD Censored
	"437": {indexers.OrganizerCategoryBangoPorn, indexers.OrganizerCategoryPorn}, // AV(有码)/DVDiSo Censored
	"431": {indexers.OrganizerCategoryBangoPorn, indexers.OrganizerCategoryPorn}, // AV(有码)/Blu-Ray Censored
	"120": {indexers.OrganizerCategoryBangoPorn, indexers.OrganizerCategoryPorn}, // AV(无码)
	"429": {indexers.OrganizerCategoryBangoPorn, indexers.OrganizerCategoryPorn}, // AV(无码)/HD Uncensored
	"430": {indexers.OrganizerCategoryBangoPorn, indexers.OrganizerCategoryPorn}, // AV(无码)/SD Uncensored
	"426": {indexers.OrganizerCategoryBangoPorn, indexers.OrganizerCategoryPorn}, // AV(无码)/DVDiSo Uncensored
	"432": {indexers.OrganizerCategoryBangoPorn, indexers.OrganizerCategoryPorn}, // AV(无码)/Blu-Ray Uncensored
	"436": {indexers.OrganizerCategoryBangoPorn, indexers.OrganizerCategoryPorn}, // AV(网站)/0Day
	"440": {indexers.OrganizerCategoryBangoPorn, indexers.OrganizerCategoryPorn}, // AV(Gay)/HD
	"445": {indexers.OrganizerCategoryPhotobook, indexers.OrganizerCategoryPorn}, // IV
	"425": {indexers.OrganizerCategoryPorn},                                      // IV(写真影集)
	"433": {indexers.OrganizerCategoryPhotobook},                                 // IV(写真图集)
	"412": {indexers.OrganizerCategoryTVSeries, indexers.OrganizerCategoryMovie}, // H-动漫
	"413": {indexers.OrganizerCategoryBook},                                      // H-漫画
}

func addOrganizerCategory(m map[string]*CategoryInfo) {
	for categoryID, categoryInfo := range m {
		if organizerCategories, exists := toOrganizerCategory[categoryID]; exists {
			categoryInfo.OrganizerCategory = organizerCategories
		} else {
			categoryInfo.OrganizerCategory = []indexers.OrganizerCategory{}
		}
	}
}

func fetchCategories(apiKey string, excludeGayContent bool) (*categoryJSON, error) {
	categories := &listCategories{}
	if err := fetchMTeamAPI(baseURL+"/api/torrent/categoryList", apiKey, categories); err != nil {
		return nil, err
	}

	return categories.toCategoryJSON(excludeGayContent), nil
}
