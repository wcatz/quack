package scraper

import (
	"bufio"
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type Engine struct {
	galleryDLPath  string
	downloadDir    string
	nitterInstance string
	httpClient     *http.Client
	logger         *slog.Logger
}

func NewEngine(galleryDLPath, downloadDir, nitterInstance string, logger *slog.Logger) *Engine {
	return &Engine{
		galleryDLPath:  galleryDLPath,
		downloadDir:    downloadDir,
		nitterInstance: nitterInstance,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		logger: logger,
	}
}

// Scrape dispatches to the appropriate scraper based on source type.
func (e *Engine) Scrape(ctx context.Context, src Source) ([]Result, error) {
	switch src.Type {
	case TypeGalleryDL:
		return e.scrapeGalleryDL(ctx, src)
	case TypeHTTPAPI:
		return e.scrapeHTTPAPI(ctx, src)
	case TypeReddit:
		return e.scrapeReddit(ctx, src)
	case TypeNitter:
		return e.scrapeNitter(ctx, src)
	case TypeTenor:
		return e.scrapeTenor(ctx, src)
	default:
		return nil, fmt.Errorf("unknown source type: %s", src.Type)
	}
}

// Download fetches the media bytes from a URL.
func (e *Engine) Download(ctx context.Context, url string) ([]byte, error) {
	// Convert imgur .gifv to actual .gif
	if strings.Contains(url, "imgur.com") && strings.HasSuffix(url, ".gifv") {
		url = strings.TrimSuffix(url, "v")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("User-Agent", "quack-scraper/1.0 (duck image collector)")

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download %s: status %d", url, resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body %s: %w", url, err)
	}
	return data, nil
}

// scrapeGalleryDL runs gallery-dl with --dump-json and parses output.
func (e *Engine) scrapeGalleryDL(ctx context.Context, src Source) ([]Result, error) {
	args := []string{"--dump-json"}
	args = append(args, src.Args...)
	args = append(args, src.URL)

	e.logger.Info("running gallery-dl", "source", src.Name, "url", src.URL)
	cmd := exec.CommandContext(ctx, e.galleryDLPath, args...)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("gallery-dl stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("gallery-dl start: %w", err)
	}

	var results []Result
	scanner := bufio.NewScanner(stdout)
	// gallery-dl --dump-json outputs one JSON array per item: [directory_fmt, filename_fmt, url, metadata]
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		var entry []json.RawMessage
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			// Try parsing as object (some gallery-dl versions output objects)
			var obj galleryDLObject
			if err2 := json.Unmarshal([]byte(line), &obj); err2 != nil {
				e.logger.Warn("skipping unparseable gallery-dl line", "error", err)
				continue
			}
			results = append(results, Result{
				URL:       obj.URL,
				Source:    src.Name,
				SourceID:  fmt.Sprintf("%v", obj.TweetID),
				SourceURL: obj.URL,
				Filename:  obj.Filename,
				Extension: "." + obj.Extension,
			})
			continue
		}

		// Array format: [directory, filename, url, metadata_object]
		if len(entry) >= 3 {
			var url string
			json.Unmarshal(entry[2], &url)

			var meta map[string]interface{}
			if len(entry) >= 4 {
				json.Unmarshal(entry[3], &meta)
			}

			ext := filepath.Ext(url)
			if ext == "" {
				ext = ".jpg"
			}

			sourceID := ""
			if meta != nil {
				if tid, ok := meta["tweet_id"]; ok {
					sourceID = fmt.Sprintf("%v", tid)
				} else if pid, ok := meta["id"]; ok {
					sourceID = fmt.Sprintf("%v", pid)
				}
			}

			results = append(results, Result{
				URL:       url,
				Source:    src.Name,
				SourceID:  sourceID,
				SourceURL: url,
				Extension: ext,
			})
		}
	}

	if err := cmd.Wait(); err != nil {
		e.logger.Warn("gallery-dl exited with error", "source", src.Name, "error", err)
	}

	e.logger.Info("gallery-dl completed", "source", src.Name, "results", len(results))
	return results, nil
}

type galleryDLObject struct {
	URL       string `json:"url"`
	Filename  string `json:"filename"`
	Extension string `json:"extension"`
	TweetID   int64  `json:"tweet_id"`
	Category  string `json:"category"`
}

// scrapeHTTPAPI handles simple HTTP API sources like random-d.uk.
func (e *Engine) scrapeHTTPAPI(ctx context.Context, src Source) ([]Result, error) {
	e.logger.Info("fetching from HTTP API", "source", src.Name, "url", src.URL)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, src.URL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", src.URL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch %s: status %d", src.URL, resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	// random-d.uk returns JSON: {"url": "https://random-d.uk/api/123.jpg", "message": "Quack!"}
	var apiResp struct {
		URL     string `json:"url"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	ext := filepath.Ext(apiResp.URL)
	if ext == "" {
		ext = ".jpg"
	}

	// Use the filename from the URL as a stable ID
	parts := strings.Split(apiResp.URL, "/")
	sourceID := parts[len(parts)-1]

	return []Result{
		{
			URL:       apiResp.URL,
			Source:    src.Name,
			SourceID:  sourceID,
			SourceURL: apiResp.URL,
			Extension: ext,
		},
	}, nil
}

// scrapeReddit fetches posts from Reddit's JSON API and extracts image URLs.
func (e *Engine) scrapeReddit(ctx context.Context, src Source) ([]Result, error) {
	// Build JSON URL: subreddit pages get /hot.json, search URLs get .json suffix
	var jsonURL string
	if strings.Contains(src.URL, "/search") {
		if strings.Contains(src.URL, "?") {
			jsonURL = strings.Replace(src.URL, "/search?", "/search.json?", 1) + "&raw_json=1&limit=100"
		} else {
			jsonURL = src.URL + ".json?raw_json=1&limit=100"
		}
	} else {
		jsonURL = strings.TrimSuffix(src.URL, "/") + "/hot.json?limit=100&raw_json=1"
	}

	e.logger.Info("fetching reddit JSON", "source", src.Name, "url", jsonURL)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, jsonURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("User-Agent", "quack-scraper/1.0 (duck image collector)")

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", jsonURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch %s: status %d", jsonURL, resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	var listing redditListing
	if err := json.Unmarshal(body, &listing); err != nil {
		return nil, fmt.Errorf("parse reddit JSON: %w", err)
	}

	var results []Result
	for _, child := range listing.Data.Children {
		post := child.Data
		if post.IsSelf || post.URL == "" {
			continue
		}

		// Direct image links (i.redd.it, i.imgur.com, etc.)
		imageURL := ""
		if isImageURL(post.URL) {
			imageURL = post.URL
		} else if post.Preview != nil && len(post.Preview.Images) > 0 {
			// Use preview source as fallback
			imageURL = post.Preview.Images[0].Source.URL
		}

		if imageURL == "" {
			continue
		}

		ext := filepath.Ext(imageURL)
		if idx := strings.Index(ext, "?"); idx > 0 {
			ext = ext[:idx]
		}
		if ext == "" {
			ext = ".jpg"
		}

		results = append(results, Result{
			URL:       imageURL,
			Source:    src.Name,
			SourceID:  post.ID,
			SourceURL: "https://www.reddit.com" + post.Permalink,
			Extension: ext,
		})
	}

	e.logger.Info("reddit scrape completed", "source", src.Name, "results", len(results))
	return results, nil
}

type redditListing struct {
	Data struct {
		Children []struct {
			Data redditPost `json:"data"`
		} `json:"children"`
	} `json:"data"`
}

type redditPost struct {
	ID        string         `json:"id"`
	Title     string         `json:"title"`
	URL       string         `json:"url"`
	Permalink string         `json:"permalink"`
	IsSelf    bool           `json:"is_self"`
	PostHint  string         `json:"post_hint"`
	Preview   *redditPreview `json:"preview"`
}

type redditPreview struct {
	Images []struct {
		Source struct {
			URL string `json:"url"`
		} `json:"source"`
	} `json:"images"`
}

func isImageURL(u string) bool {
	lower := strings.ToLower(u)
	for _, ext := range []string{".jpg", ".jpeg", ".png", ".gif", ".webp"} {
		if strings.HasSuffix(lower, ext) || strings.Contains(lower, ext+"?") {
			return true
		}
	}
	// Known image hosts
	return strings.Contains(lower, "i.redd.it") ||
		strings.Contains(lower, "i.imgur.com")
}

// scrapeNitter fetches media from a Nitter instance RSS feed.
func (e *Engine) scrapeNitter(ctx context.Context, src Source) ([]Result, error) {
	if e.nitterInstance == "" {
		return nil, fmt.Errorf("nitter instance not configured")
	}

	// src.URL is the username or path, e.g. "bestducksdaily"
	username := strings.TrimPrefix(src.URL, "@")
	rssURL := strings.TrimSuffix(e.nitterInstance, "/") + "/" + username + "/rss"

	e.logger.Info("fetching nitter RSS", "source", src.Name, "url", rssURL)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rssURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("User-Agent", "quack-scraper/1.0 (duck image collector)")

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", rssURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch %s: status %d", rssURL, resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	var feed nitterRSS
	if err := xml.Unmarshal(body, &feed); err != nil {
		return nil, fmt.Errorf("parse nitter RSS: %w", err)
	}

	imgRe := regexp.MustCompile(`<img\s+src="([^"]+)"`)

	var results []Result
	for _, item := range feed.Channel.Items {
		// Extract tweet ID from GUID or link URL
		tweetID := item.GUID
		if tweetID == "" {
			link := strings.Split(strings.TrimSuffix(item.Link, "/"), "#")[0]
			parts := strings.Split(link, "/")
			tweetID = parts[len(parts)-1]
		}

		// Find all image URLs in the description HTML
		matches := imgRe.FindAllStringSubmatch(item.Description, -1)
		for i, m := range matches {
			imgURL := m[1]

			// Nitter proxies images — convert to direct Twitter CDN URL
			// Nitter URLs look like: /pic/media%2F... or /pic/orig/media%2F...
			if strings.Contains(imgURL, "/pic/") {
				imgURL = nitterToDirectURL(e.nitterInstance, imgURL)
			}

			if imgURL == "" {
				continue
			}

			ext := filepath.Ext(imgURL)
			if idx := strings.Index(ext, "?"); idx > 0 {
				ext = ext[:idx]
			}
			if ext == "" {
				ext = ".jpg"
			}

			sourceID := tweetID
			if i > 0 {
				sourceID = fmt.Sprintf("%s_%d", tweetID, i)
			}

			results = append(results, Result{
				URL:       imgURL,
				Source:    src.Name,
				SourceID:  sourceID,
				SourceURL: item.Link,
				Extension: ext,
			})
		}
	}

	e.logger.Info("nitter scrape completed", "source", src.Name, "results", len(results))
	return results, nil
}

type nitterRSS struct {
	XMLName xml.Name `xml:"rss"`
	Channel struct {
		Items []nitterItem `xml:"item"`
	} `xml:"channel"`
}

type nitterItem struct {
	Title       string `xml:"title"`
	Link        string `xml:"link"`
	Description string `xml:"description"`
	PubDate     string `xml:"pubDate"`
	GUID        string `xml:"guid"`
}

// nitterToDirectURL converts a Nitter proxied image URL to a direct pbs.twimg.com URL.
func nitterToDirectURL(instance, imgPath string) string {
	// Strip any Nitter instance prefix from full URLs
	path := imgPath
	if strings.HasPrefix(path, "http") {
		if idx := strings.Index(path, "/pic/"); idx >= 0 {
			path = path[idx:]
		} else {
			return path
		}
	}

	// /pic/orig/media%2F... -> https://pbs.twimg.com/media/...
	// /pic/media%2F... -> https://pbs.twimg.com/media/...
	path = strings.TrimPrefix(path, "/pic/orig/")
	path = strings.TrimPrefix(path, "/pic/")

	decoded := strings.ReplaceAll(path, "%2F", "/")

	return "https://pbs.twimg.com/" + decoded
}

// scrapeTenor searches Tenor for GIFs matching the source URL (used as search query).
func (e *Engine) scrapeTenor(ctx context.Context, src Source) ([]Result, error) {
	// src.URL is the search query, e.g. "duck" or "duckling"
	// src.Args[0] is the API key if provided
	apiKey := "AIzaSyAyimkuYQYF_FXVALexPuGQctUWRURdCYQ" // public Tenor key
	if len(src.Args) > 0 {
		apiKey = src.Args[0]
	}

	limit := 50
	tenorURL := fmt.Sprintf("https://tenor.googleapis.com/v2/search?q=%s&key=%s&media_filter=gif&limit=%d",
		src.URL, apiKey, limit)

	e.logger.Info("fetching tenor GIFs", "source", src.Name, "query", src.URL)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, tenorURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch tenor: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("tenor API status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read tenor response: %w", err)
	}

	var tenorResp tenorSearchResponse
	if err := json.Unmarshal(body, &tenorResp); err != nil {
		return nil, fmt.Errorf("parse tenor response: %w", err)
	}

	var results []Result
	for _, item := range tenorResp.Results {
		gifFormat, ok := item.MediaFormats["gif"]
		if !ok {
			continue
		}

		results = append(results, Result{
			URL:       gifFormat.URL,
			Source:    src.Name,
			SourceID:  item.ID,
			SourceURL: item.ItemURL,
			Extension: ".gif",
		})
	}

	e.logger.Info("tenor scrape completed", "source", src.Name, "results", len(results))
	return results, nil
}

type tenorSearchResponse struct {
	Results []tenorResult `json:"results"`
	Next    string        `json:"next"`
}

type tenorResult struct {
	ID           string                     `json:"id"`
	Title        string                     `json:"title"`
	ItemURL      string                     `json:"itemurl"`
	MediaFormats map[string]tenorMediaFormat `json:"media_formats"`
}

type tenorMediaFormat struct {
	URL  string `json:"url"`
	Dims []int  `json:"dims"`
	Size int    `json:"size"`
}
