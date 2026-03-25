package scraper

import "github.com/wcatz/quack/internal/config"

// SourceType constants.
const (
	TypeGalleryDL = "gallery-dl"
	TypeHTTPAPI   = "http-api"
	TypeReddit    = "reddit"
	TypeNitter    = "nitter"
	TypeTenor     = "tenor"
)

// SourceFromConfig converts a config.Source to the scraper's internal representation.
func SourceFromConfig(src config.Source) Source {
	return Source{
		Name:     src.Name,
		Type:     src.Type,
		URL:      src.URL,
		Schedule: src.Schedule,
		Args:     src.Args,
	}
}

// Source defines a scrape target.
type Source struct {
	Name     string
	Type     string // TypeGalleryDL, TypeHTTPAPI, TypeReddit, or TypeNitter
	URL      string
	Schedule string
	Args     []string
}
