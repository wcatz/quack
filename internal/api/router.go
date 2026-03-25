package api

import (
	"embed"
	"io/fs"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/wcatz/quack/internal/scheduler"
	"github.com/wcatz/quack/internal/storage"
)

//go:embed all:static
var webFS embed.FS

type Server struct {
	router    chi.Router
	scheduler *scheduler.Scheduler
	s3        *storage.S3Client
	publicURL string
	logger    *slog.Logger
}

func NewServer(sched *scheduler.Scheduler, s3Client *storage.S3Client, publicURL string, logger *slog.Logger) *Server {
	s := &Server{
		scheduler: sched,
		s3:        s3Client,
		publicURL: publicURL,
		logger:    logger,
	}

	r := chi.NewRouter()
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Compress(5))

	// API routes
	r.Route("/api/v1", func(r chi.Router) {
		r.Get("/random", s.handleRandom)
		r.Get("/random/gif", s.handleRandomGIF)
		r.Get("/random/image", s.handleRandomImage)
		r.Get("/count", s.handleCount)
		r.Get("/health", s.handleHealth)
		r.Post("/scrape", s.handleScrape)
	})

	// Serve embedded frontend
	staticFS, err := fs.Sub(webFS, "static")
	if err != nil {
		logger.Error("failed to create sub FS", "error", err)
	} else {
		r.Handle("/*", http.FileServer(http.FS(staticFS)))
	}

	s.router = r
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.router.ServeHTTP(w, r)
}
