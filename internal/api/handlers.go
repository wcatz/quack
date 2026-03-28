package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

func (s *Server) handleRandom(w http.ResponseWriter, r *http.Request) {
	// Support ?type=gif|jpg for goduckbot compatibility
	filter := ""
	switch r.URL.Query().Get("type") {
	case "gif":
		filter = "gif"
	case "jpg", "image":
		filter = "image"
	}
	s.serveRandom(w, r, filter)
}

func (s *Server) handleRandomGIF(w http.ResponseWriter, r *http.Request) {
	s.serveRandom(w, r, "gif")
}

func (s *Server) handleRandomImage(w http.ResponseWriter, r *http.Request) {
	s.serveRandom(w, r, "image")
}

func (s *Server) serveRandom(w http.ResponseWriter, r *http.Request, filter string) {
	key, ok := s.scheduler.RandomKey(filter)
	if !ok {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error":"no ducks available"}`))
		return
	}

	mediaType := "image"
	if strings.HasPrefix(key, "gifs/") {
		mediaType = "gif"
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	url, err := s.s3.GetPublicURL(ctx, key, s.publicURL, 5*time.Minute)
	if err != nil {
		s.logger.Error("failed to get URL", "key", key, "error", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":"internal error"}`))
		return
	}

	// JSON is the default response mode (goduckbot compatibility)
	// Use ?redirect=true for redirect mode
	if r.URL.Query().Get("redirect") == "true" {
		http.Redirect(w, r, url, http.StatusFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"url":  url,
		"type": mediaType,
		"key":  key,
	})
}

func (s *Server) handleScrape(w http.ResponseWriter, r *http.Request) {
	newCount, skipped := s.scheduler.RunAll(r.Context())
	images, gifs := s.scheduler.Counts()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]int{
		"new":     newCount,
		"skipped": skipped,
		"total":   images + gifs,
		"images":  images,
		"gifs":    gifs,
	})
}

func (s *Server) handleCount(w http.ResponseWriter, r *http.Request) {
	images, gifs := s.scheduler.Counts()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]int{
		"total":  images + gifs,
		"images": images,
		"gifs":   gifs,
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"status":"ok"}`))
}

func (s *Server) handleGallery(w http.ResponseWriter, r *http.Request) {
	keys := s.scheduler.AllKeys()

	// Pagination: ?offset=0&limit=20
	limit := 20
	offset := 0
	if v, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && v > 0 && v <= 100 {
		limit = v
	}
	if v, err := strconv.Atoi(r.URL.Query().Get("offset")); err == nil && v >= 0 {
		offset = v
	}

	total := len(keys)
	if offset > total {
		offset = total
	}
	end := offset + limit
	if end > total {
		end = total
	}
	page := keys[offset:end]

	type item struct {
		Key  string `json:"key"`
		URL  string `json:"url"`
		Type string `json:"type"`
	}

	items := make([]item, 0, len(page))
	for _, k := range page {
		url, _ := s.s3.GetPublicURL(r.Context(), k, s.publicURL, 5*time.Minute)
		t := "image"
		if strings.HasPrefix(k, "gifs/") {
			t = "gif"
		}
		items = append(items, item{Key: k, URL: url, Type: t})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"items":  items,
		"total":  total,
		"offset": offset,
		"limit":  limit,
	})
}

func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) {
	key := chi.URLParam(r, "*")
	if key == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"key required"}`))
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	if err := s.s3.Delete(ctx, key); err != nil {
		s.logger.Error("delete failed", "key", key, "error", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	s.scheduler.DeleteKey(key)
	s.logger.Info("image deleted", "key", key)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status": "deleted",
		"key":    key,
	})
}

func (s *Server) handleCleanup(w http.ResponseWriter, r *http.Request) {
	maxSize := int64(8 * 1024 * 1024) // default 8MB
	if q := r.URL.Query().Get("max_size"); q != "" {
		if v, err := strconv.ParseInt(q, 10, 64); err == nil && v > 0 {
			maxSize = v
		}
	}
	dryRun := r.URL.Query().Get("dry_run") != "false"

	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	type oversizedItem struct {
		Key  string `json:"key"`
		Size string `json:"size"`
	}

	var found []oversizedItem
	var deleted []oversizedItem

	for _, prefix := range []string{"images/", "gifs/"} {
		objects, err := s.s3.ListObjectsWithSize(ctx, prefix)
		if err != nil {
			s.logger.Error("cleanup list failed", "prefix", prefix, "error", err)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}

		for _, obj := range objects {
			if obj.Size > maxSize {
				item := oversizedItem{
					Key:  obj.Key,
					Size: fmt.Sprintf("%.2f MB", float64(obj.Size)/(1024*1024)),
				}
				found = append(found, item)

				if !dryRun {
					if err := s.s3.Delete(ctx, obj.Key); err != nil {
						s.logger.Error("cleanup delete failed", "key", obj.Key, "error", err)
						continue
					}
					s.scheduler.DeleteKey(obj.Key)
					s.logger.Info("cleanup deleted oversized file", "key", obj.Key, "size", obj.Size)
					deleted = append(deleted, item)
				}
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"max_size_bytes": maxSize,
		"dry_run":        dryRun,
		"found":          len(found),
		"deleted":        len(deleted),
		"oversized":      found,
	})
}
