package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
	"github.com/wcatz/quack/internal/dedup"
	"github.com/wcatz/quack/internal/scraper"
	"github.com/wcatz/quack/internal/storage"
)

type Scheduler struct {
	cron    *cron.Cron
	engine  *scraper.Engine
	dedup   *dedup.Store
	s3      *storage.S3Client
	logger  *slog.Logger

	mu    sync.RWMutex
	index []string // in-memory list of all object keys
}

func New(engine *scraper.Engine, dedupStore *dedup.Store, s3Client *storage.S3Client, logger *slog.Logger) *Scheduler {
	return &Scheduler{
		cron:   cron.New(),
		engine: engine,
		dedup:  dedupStore,
		s3:     s3Client,
		logger: logger,
	}
}

// Start registers cron jobs for each source and starts the scheduler.
func (s *Scheduler) Start(sources []scraper.Source) error {
	// Load existing keys into the in-memory index
	s.index = s.dedup.AllKeys()
	s.logger.Info("loaded index from dedup store", "keys", len(s.index))

	for _, src := range sources {
		src := src
		if _, err := s.cron.AddFunc(src.Schedule, func() {
			s.runScrape(context.Background(), src)
		}); err != nil {
			return fmt.Errorf("add cron for %s: %w", src.Name, err)
		}
		s.logger.Info("scheduled source", "name", src.Name, "schedule", src.Schedule)
	}

	s.cron.Start()

	// Run initial scrape for all sources
	go func() {
		for _, src := range sources {
			s.runScrape(context.Background(), src)
		}
	}()

	return nil
}

func (s *Scheduler) Stop() {
	ctx := s.cron.Stop()
	<-ctx.Done()
}

func (s *Scheduler) runScrape(ctx context.Context, src scraper.Source) {
	s.logger.Info("starting scrape", "source", src.Name)

	results, err := s.engine.Scrape(ctx, src)
	if err != nil {
		s.logger.Error("scrape failed", "source", src.Name, "error", err)
		return
	}

	var newCount, skipCount int
	for _, r := range results {
		if s.dedup.Seen(r.Source, r.SourceID) {
			skipCount++
			continue
		}

		data, err := s.engine.Download(ctx, r.URL)
		if err != nil {
			s.logger.Warn("download failed", "url", r.URL, "error", err)
			continue
		}

		hash := storage.ContentHash(data)
		ext := r.Extension
		if ext == "" {
			ext = filepath.Ext(r.URL)
		}
		if ext == "" {
			ext = ".jpg"
		}
		objectKey := storage.ObjectKey(hash, ext)

		// Content-hash dedup: check if the same image already exists in S3
		exists, err := s.s3.Exists(ctx, objectKey)
		if err != nil {
			s.logger.Warn("s3 exists check failed", "key", objectKey, "error", err)
		}

		if exists {
			// Image exists but source ID is new; just mark it
			s.dedup.Mark(r.Source, r.SourceID, objectKey)
			skipCount++
			continue
		}

		contentType := "image/jpeg"
		mediaType := "images"
		if strings.ToLower(ext) == ".gif" {
			contentType = "image/gif"
			mediaType = "gifs"
		} else if strings.ToLower(ext) == ".png" {
			contentType = "image/png"
		} else if strings.ToLower(ext) == ".webp" {
			contentType = "image/webp"
		}

		metadata := map[string]string{
			"source":     r.Source,
			"source-id":  r.SourceID,
			"source-url": r.SourceURL,
			"scraped-at": time.Now().UTC().Format(time.RFC3339),
		}

		if err := s.s3.Put(ctx, objectKey, data, contentType, metadata); err != nil {
			s.logger.Error("s3 upload failed", "key", objectKey, "error", err)
			continue
		}

		s.dedup.Mark(r.Source, r.SourceID, objectKey)
		s.dedup.IncrementCount(mediaType)

		s.mu.Lock()
		s.index = append(s.index, objectKey)
		s.mu.Unlock()

		newCount++
	}

	s.logger.Info("scrape completed",
		"source", src.Name,
		"new", newCount,
		"skipped", skipCount,
		"total_index", s.IndexSize(),
	)
}

// RandomKey returns a random object key from the index.
func (s *Scheduler) RandomKey(filter string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if len(s.index) == 0 {
		return "", false
	}

	var filtered []string
	switch filter {
	case "gif":
		for _, k := range s.index {
			if strings.HasPrefix(k, "gifs/") {
				filtered = append(filtered, k)
			}
		}
	case "image":
		for _, k := range s.index {
			if strings.HasPrefix(k, "images/") {
				filtered = append(filtered, k)
			}
		}
	default:
		filtered = s.index
	}

	if len(filtered) == 0 {
		return "", false
	}

	return filtered[rand.IntN(len(filtered))], true
}

// IndexSize returns the total number of indexed objects.
func (s *Scheduler) IndexSize() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.index)
}

// Counts returns the number of images and gifs from the dedup store.
func (s *Scheduler) Counts() (images, gifs int) {
	return s.dedup.Counts()
}
