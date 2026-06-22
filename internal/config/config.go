package config

import (
	"os"
	"strconv"
	"time"
)

type Config struct {
	Addr                  string
	DBPath                string
	DefaultMaxBodySize    int64
	LogRetentionDays      int
	ResponseHeaderTimeout time.Duration
	ShutdownTimeout       time.Duration
}

func Load() Config {
	return Config{
		Addr:                  getenv("REQUESTLENS_ADDR", ":8080"),
		DBPath:                getenv("REQUESTLENS_DB_PATH", "data/requestlens.db"),
		DefaultMaxBodySize:    getenvInt64("REQUESTLENS_DEFAULT_MAX_BODY_SIZE", 0),
		LogRetentionDays:      getenvInt("REQUESTLENS_LOG_RETENTION_DAYS", 14),
		ResponseHeaderTimeout: time.Duration(getenvInt("REQUESTLENS_RESPONSE_HEADER_TIMEOUT_SECONDS", 60)) * time.Second,
		ShutdownTimeout:       10 * time.Second,
	}
}

func getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func getenvInt(key string, fallback int) int {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func getenvInt64(key string, fallback int64) int64 {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return fallback
	}
	return parsed
}
