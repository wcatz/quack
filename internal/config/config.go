package config

import (
	"fmt"
	"strings"

	"github.com/spf13/viper"
)

type Config struct {
	Server  ServerConfig  `mapstructure:"server"`
	MinIO   MinIOConfig   `mapstructure:"minio"`
	Storage StorageConfig `mapstructure:"storage"`
	Scraper ScraperConfig `mapstructure:"scraper"`
	Sources []Source      `mapstructure:"sources"`
	Log     LogConfig     `mapstructure:"log"`
}

type ServerConfig struct {
	Port       int    `mapstructure:"port"`
	AdminToken string `mapstructure:"adminToken"`
}

type MinIOConfig struct {
	Endpoint  string `mapstructure:"endpoint"`
	Region    string `mapstructure:"region"`
	AccessKey string `mapstructure:"accessKey"`
	SecretKey string `mapstructure:"secretKey"`
	Bucket    string `mapstructure:"bucket"`
	PublicURL string `mapstructure:"publicURL"`
}

type StorageConfig struct {
	DBPath string `mapstructure:"dbPath"`
}

type ScraperConfig struct {
	GalleryDLPath  string `mapstructure:"galleryDlPath"`
	DownloadDir    string `mapstructure:"downloadDir"`
	Concurrency    int    `mapstructure:"concurrency"`
	NitterInstance string `mapstructure:"nitterInstance"`
}

type Source struct {
	Name     string   `mapstructure:"name"`
	Type     string   `mapstructure:"type"` // "gallery-dl" or "http-api"
	URL      string   `mapstructure:"url"`
	Schedule string   `mapstructure:"schedule"`
	Args     []string `mapstructure:"args"`
}

type LogConfig struct {
	Level string `mapstructure:"level"`
}

func Load(path string) (*Config, error) {
	v := viper.New()

	// Defaults
	v.SetDefault("server.port", 8080)
	v.SetDefault("minio.region", "us-east-1")
	v.SetDefault("minio.bucket", "ducks")
	v.SetDefault("storage.dbPath", "/data/quack.db")
	v.SetDefault("scraper.galleryDlPath", "/usr/local/bin/gallery-dl")
	v.SetDefault("scraper.downloadDir", "/tmp/downloads")
	v.SetDefault("scraper.concurrency", 2)
	v.SetDefault("log.level", "info")

	// Env var overrides
	v.SetEnvPrefix("QUACK")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	// Explicit env bindings for secrets
	v.BindEnv("minio.accessKey", "MINIO_ACCESS_KEY")
	v.BindEnv("minio.secretKey", "MINIO_SECRET_KEY")
	v.BindEnv("server.adminToken", "ADMIN_TOKEN")

	// Config file
	if path != "" {
		v.SetConfigFile(path)
	} else {
		v.SetConfigName("config")
		v.SetConfigType("yaml")
		v.AddConfigPath(".")
		v.AddConfigPath("/app")
	}

	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("read config: %w", err)
		}
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

	return &cfg, nil
}
