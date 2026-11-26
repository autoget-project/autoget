package db

import (
	"strings"

	"gorm.io/gorm"
)

type RSSSearch struct {
	gorm.Model
	Indexer string `gorm:"index"`
	Text    string `gorm:"text"`
	Action  string `gorm:"action"`

	// founded
	ResID     string `gorm:"res_id"`
	Title     string `gorm:"title"`
	Catergory string `gorm:"category"`
	URL       string `gorm:"url"`
}

func (s *RSSSearch) TableName() string {
	return "rss_search"
}

func GetSearchsByIndexer(db *gorm.DB, indexer string) ([]*RSSSearch, error) {
	var searchs []*RSSSearch
	err := db.Where("indexer = ?", indexer).Find(&searchs).Error
	if err != nil {
		return nil, err
	}
	return searchs, nil
}

func AddSearch(db *gorm.DB, search *RSSSearch) error {
	search.Text = strings.ToLower(search.Text)
	return db.Create(search).Error
}

func UpdateSearch(db *gorm.DB, search *RSSSearch) error {
	return db.Save(search).Error
}

func DeleteSearch(db *gorm.DB, id uint) error {
	return db.Delete(&RSSSearch{}, id).Error
}
