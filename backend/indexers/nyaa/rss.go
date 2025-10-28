package nyaa

import (
	"net/url"
	"strings"

	"github.com/autoget-project/autoget/backend/indexers"
	"github.com/autoget-project/autoget/backend/indexers/rsshelper"
	"github.com/mmcdole/gofeed"
	"github.com/robfig/cron/v3"
)

func (c *Client) RegisterRSSCronjob(cron *cron.Cron) {
	cron.AddFunc("@every 5m", func() {
		items, err := c.pullRSS()
		if err != nil {
			logger.Error().Err(err).Msg("Failed to pull RSS feed")
			return
		}

		c.SearchRSS(items)
	})
}

func (c *Client) pullRSS() ([]*indexers.RSSItem, error) {
	u, _ := url.Parse(c.getBaseURL())
	query := u.Query()
	query.Set("page", "rss")
	u.RawQuery = query.Encode()

	fp := gofeed.NewParser()
	fp.Client = c.httpClient
	f, err := fp.ParseURL(u.String())
	if err != nil {
		return nil, err
	}

	items := []*indexers.RSSItem{}
	for _, item := range f.Items {
		items = append(items, c.ParseRSSItem(item))
	}

	return items, nil
}

func (c *Client) ParseRSSItem(item *gofeed.Item) *indexers.RSSItem {
	catergory := ""
	if item.Extensions != nil &&
		item.Extensions["nyaa"] != nil &&
		len(item.Extensions["nyaa"]["categoryId"]) > 0 &&
		len(item.Extensions["nyaa"]["category"]) > 0 {
		catergory = c.getCategoryFromRSSCategory(
			item.Extensions["nyaa"]["categoryId"][0].Value,
			item.Extensions["nyaa"]["category"][0].Value)
	}

	return &indexers.RSSItem{
		ResID:     getResourceIDFromRSSGUID(item.GUID),
		Title:     item.Title,
		URL:       item.Link,
		Catergory: catergory,
	}
}

func (c *Client) getCategoryFromRSSCategory(categoryID, category string) string {
	if cat, ok := c.CategoriesMap[categoryID]; ok {
		return cat.Name
	} else {
		return category
	}
}

func getResourceIDFromRSSGUID(guid string) string {
	parts := strings.Split(guid, "/")
	return parts[len(parts)-1]
}

func (c *Client) SearchRSS(items []*indexers.RSSItem) {
	rsshelper.SearchRSS(c, c.db, c.notify, items)
}
