package api

import (
	"context"
	"encoding/json"
	"net/http"
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

	type item struct {
		Key  string `json:"key"`
		URL  string `json:"url"`
		Type string `json:"type"`
	}

	items := make([]item, 0, len(keys))
	for _, k := range keys {
		url, _ := s.s3.GetPublicURL(r.Context(), k, s.publicURL, 5*time.Minute)
		t := "image"
		if strings.HasPrefix(k, "gifs/") {
			t = "gif"
		}
		items = append(items, item{Key: k, URL: url, Type: t})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(items)
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
