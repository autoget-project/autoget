package sukebei

import (
	"github.com/autoget-project/autoget/backend/indexers/nyaa"
	"github.com/autoget-project/autoget/backend/indexers/sukebei/prefetcheddata"
	"github.com/autoget-project/autoget/backend/internal/notify"
	"gorm.io/gorm"
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
	c.Client.DefaultBaseURL = defaultBaseURL
	c.Client.CategoriesMap = prefetcheddata.Categories
	c.Client.CategoriesList = prefetcheddata.CategoriesList

	return c
}
