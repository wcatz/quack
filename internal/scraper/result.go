package scraper

// Result represents a single scraped media item.
type Result struct {
	URL       string // Direct URL to the media file
	Source    string // Source name, e.g. "twitter-duckposter"
	SourceID  string // Source-specific ID, e.g. tweet ID
	SourceURL string // Original page URL
	Filename  string // Suggested filename
	Extension string // File extension including dot, e.g. ".jpg", ".gif"
}
