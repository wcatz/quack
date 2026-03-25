package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

func (s *Server) handleRandom(w http.ResponseWriter, r *http.Request) {
	s.serveRandom(w, r, "")
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
		http.Error(w, `{"error":"no ducks available"}`, http.StatusNotFound)
		return
	}

	mediaType := "image"
	if strings.HasPrefix(key, "gifs/") {
		mediaType = "gif"
	}

	// JSON response mode
	if r.URL.Query().Get("json") == "true" {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()

		url, err := s.s3.GetPublicURL(ctx, key, s.publicURL, 5*time.Minute)
		if err != nil {
			s.logger.Error("failed to get URL", "key", key, "error", err)
			http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"url":  url,
			"type": mediaType,
			"key":  key,
		})
		return
	}

	// Redirect mode (default)
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	url, err := s.s3.GetPublicURL(ctx, key, s.publicURL, 5*time.Minute)
	if err != nil {
		s.logger.Error("failed to get presigned URL", "key", key, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, url, http.StatusFound)
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
