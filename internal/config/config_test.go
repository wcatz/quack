package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDefaults(t *testing.T) {
	// Load with no config file — should use defaults
	cfg, err := Load("/nonexistent/path/config.yaml")
	if err == nil {
		t.Fatal("expected error for nonexistent config file")
	}
	_ = cfg
}

func TestLoadFromFile(t *testing.T) {
	dir := t.TempDir()
	configFile := filepath.Join(dir, "config.yaml")
	content := `
server:
  port: 9090
  adminToken: "test-token"
minio:
  endpoint: "http://localhost:9000"
  region: "us-west-2"
  accessKey: "testkey"
  secretKey: "testsecret"
  bucket: "testbucket"
  publicURL: "http://cdn.example.com"
storage:
  dbPath: "/tmp/test.db"
scraper:
  galleryDlPath: "/usr/bin/gallery-dl"
  downloadDir: "/tmp/dl"
  concurrency: 4
sources:
  - name: "test-source"
    type: "http-api"
    url: "https://example.com/api"
    schedule: "*/5 * * * *"
log:
  level: "debug"
`
	if err := os.WriteFile(configFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(configFile)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.Server.Port != 9090 {
		t.Errorf("Server.Port = %d, want 9090", cfg.Server.Port)
	}
	if cfg.Server.AdminToken != "test-token" {
		t.Errorf("Server.AdminToken = %q, want %q", cfg.Server.AdminToken, "test-token")
	}
	if cfg.MinIO.Endpoint != "http://localhost:9000" {
		t.Errorf("MinIO.Endpoint = %q, want %q", cfg.MinIO.Endpoint, "http://localhost:9000")
	}
	if cfg.MinIO.Region != "us-west-2" {
		t.Errorf("MinIO.Region = %q, want %q", cfg.MinIO.Region, "us-west-2")
	}
	if cfg.MinIO.Bucket != "testbucket" {
		t.Errorf("MinIO.Bucket = %q, want %q", cfg.MinIO.Bucket, "testbucket")
	}
	if cfg.Storage.DBPath != "/tmp/test.db" {
		t.Errorf("Storage.DBPath = %q, want %q", cfg.Storage.DBPath, "/tmp/test.db")
	}
	if cfg.Scraper.Concurrency != 4 {
		t.Errorf("Scraper.Concurrency = %d, want 4", cfg.Scraper.Concurrency)
	}
	if len(cfg.Sources) != 1 {
		t.Fatalf("len(Sources) = %d, want 1", len(cfg.Sources))
	}
	if cfg.Sources[0].Name != "test-source" {
		t.Errorf("Sources[0].Name = %q, want %q", cfg.Sources[0].Name, "test-source")
	}
	if cfg.Log.Level != "debug" {
		t.Errorf("Log.Level = %q, want %q", cfg.Log.Level, "debug")
	}
}

func TestLoadEnvOverride(t *testing.T) {
	t.Setenv("MINIO_ACCESS_KEY", "env-access-key")
	t.Setenv("MINIO_SECRET_KEY", "env-secret-key")
	t.Setenv("ADMIN_TOKEN", "env-admin-token")

	dir := t.TempDir()
	configFile := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(configFile, []byte("server:\n  port: 8080\n"), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(configFile)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.MinIO.AccessKey != "env-access-key" {
		t.Errorf("MinIO.AccessKey = %q, want %q", cfg.MinIO.AccessKey, "env-access-key")
	}
	if cfg.MinIO.SecretKey != "env-secret-key" {
		t.Errorf("MinIO.SecretKey = %q, want %q", cfg.MinIO.SecretKey, "env-secret-key")
	}
	if cfg.Server.AdminToken != "env-admin-token" {
		t.Errorf("Server.AdminToken = %q, want %q", cfg.Server.AdminToken, "env-admin-token")
	}
}
