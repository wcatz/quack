package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"
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
