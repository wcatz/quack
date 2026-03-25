package scraper

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type Engine struct {
	galleryDLPath string
	downloadDir   string
	httpClient    *http.Client
	logger        *slog.Logger
}

func NewEngine(galleryDLPath, downloadDir string, logger *slog.Logger) *Engine {
	return &Engine{
		galleryDLPath: galleryDLPath,
		downloadDir:   downloadDir,
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
	default:
		return nil, fmt.Errorf("unknown source type: %s", src.Type)
	}
}

// Download fetches the media bytes from a URL.
func (e *Engine) Download(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

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
