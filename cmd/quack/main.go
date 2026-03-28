package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/wcatz/quack/internal/api"
	"github.com/wcatz/quack/internal/config"
	"github.com/wcatz/quack/internal/dedup"
	"github.com/wcatz/quack/internal/scheduler"
	"github.com/wcatz/quack/internal/scraper"
	"github.com/wcatz/quack/internal/storage"
)

var (
	version   = "dev"
	commitSHA = "unknown"
	buildDate = "unknown"
)

func main() {
	configPath := flag.String("config", "", "path to config file")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Printf("quack %s (%s) built %s\n", version, commitSHA, buildDate)
		os.Exit(0)
	}

	// Logger
	logLevel := slog.LevelInfo
	cfg, err := config.Load(*configPath)
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	switch cfg.Log.Level {
	case "debug":
		logLevel = slog.LevelDebug
	case "warn":
		logLevel = slog.LevelWarn
	case "error":
		logLevel = slog.LevelError
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: logLevel}))
	slog.SetDefault(logger)

	logger.Info("starting quack",
		"version", version,
		"commit", commitSHA,
		"built", buildDate,
	)

	// S3 client
	s3Client, err := storage.NewS3Client(
		cfg.MinIO.Endpoint,
		cfg.MinIO.Region,
		cfg.MinIO.AccessKey,
		cfg.MinIO.SecretKey,
		cfg.MinIO.Bucket,
		logger,
	)
	if err != nil {
		logger.Error("failed to create S3 client", "error", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	if err := s3Client.EnsureBucket(ctx); err != nil {
		logger.Error("failed to ensure bucket", "error", err)
		os.Exit(1)
	}
	cancel()

	// Dedup store
	dedupStore, err := dedup.Open(cfg.Storage.DBPath, logger)
	if err != nil {
		logger.Error("failed to open dedup store", "error", err)
		os.Exit(1)
	}
	defer dedupStore.Close()

	// Scraper engine
	engine := scraper.NewEngine(cfg.Scraper.GalleryDLPath, cfg.Scraper.DownloadDir, cfg.Scraper.NitterInstance, logger)

	// Convert config sources to scraper sources
	var sources []scraper.Source
	for _, s := range cfg.Sources {
		sources = append(sources, scraper.SourceFromConfig(s))
	}

	// Scheduler
	sched := scheduler.New(engine, dedupStore, s3Client, cfg.Scraper.MaxFileSize, logger)
	if err := sched.Start(sources); err != nil {
		logger.Error("failed to start scheduler", "error", err)
		os.Exit(1)
	}
	defer sched.Stop()

	// HTTP server
	srv := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Server.Port),
		Handler:      api.NewServer(sched, s3Client, cfg.MinIO.PublicURL, cfg.Server.AdminToken, logger),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 0, // disabled for streaming endpoints (moderate)
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		logger.Info("listening", "port", cfg.Server.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	sig := <-quit
	logger.Info("shutting down", "signal", sig)

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("server shutdown error", "error", err)
	}

	logger.Info("quack stopped")
}
