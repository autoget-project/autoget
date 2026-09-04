package sukebei

import (
	"gorm.io/gorm"

	"github.com/autoget-project/autoget/backend/indexers/nyaa"
	"github.com/autoget-project/autoget/backend/indexers/sukebei/prefetcheddata"
	"github.com/autoget-project/autoget/backend/internal/notify"
)

const (
	defaultBaseURL = "https://sukebei.nyaa.si/"
)

type Client struct {
	nyaa.Client
}

func NewClient(config *nyaa.Config, torrentsDir string, db *gorm.DB, notify notify.INotifier) *Client {
	c := &Client{}
	c.Client = *nyaa.NewClient(config, torrentsDir, db, notify)
	c.Name_ = "sukebei"
	c.DefaultBaseURL = defaultBaseURL
	c.CategoriesMap = prefetcheddata.Categories
	c.CategoriesList = prefetcheddata.CategoriesList
	c.ToOrganizerCategoryMap = prefetcheddata.ToOrganizerCategory

	return c
}
