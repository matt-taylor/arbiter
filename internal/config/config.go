package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config holds all configuration for the Arbiter service
type Config struct {
	BindAddr              string
	DatabaseURL           string
	KillswitchBaseURL    string
	GatekeeperBaseURL     string
	KillswitchTimeout    time.Duration
	GatekeeperTimeout    time.Duration
	CacheTTL             time.Duration
	KillswitchPublicHost string
	GatekeeperPublicHost string
}

// Load reads configuration from environment variables
func Load() (*Config, error) {
	cfg := &Config{}

	// Required configuration
	bindAddr := os.Getenv("ARBITER_BIND_ADDR")
	if bindAddr == "" {
		return nil, fmt.Errorf("ARBITER_BIND_ADDR is required")
	}
	cfg.BindAddr = bindAddr

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}
	cfg.DatabaseURL = databaseURL

	killswitchBaseURL := os.Getenv("KILLSWITCH_BASE_URL")
	if killswitchBaseURL == "" {
		return nil, fmt.Errorf("KILLSWITCH_BASE_URL is required")
	}
	cfg.KillswitchBaseURL = killswitchBaseURL

	gatekeeperBaseURL := os.Getenv("GATEKEEPER_BASE_URL")
	if gatekeeperBaseURL == "" {
		return nil, fmt.Errorf("GATEKEEPER_BASE_URL is required")
	}
	cfg.GatekeeperBaseURL = gatekeeperBaseURL

	// Optional timeouts (default 1500ms)
	killswitchTimeoutMs := getEnvInt("KILLSWITCH_TIMEOUT_MS", 1500)
	cfg.KillswitchTimeout = time.Duration(killswitchTimeoutMs) * time.Millisecond

	gatekeeperTimeoutMs := getEnvInt("GATEKEEPER_TIMEOUT_MS", 1500)
	cfg.GatekeeperTimeout = time.Duration(gatekeeperTimeoutMs) * time.Millisecond

	// Optional cache TTL (default 600 seconds)
	cacheTTLSeconds := getEnvInt("CACHE_TTL_SECONDS", 600)
	cfg.CacheTTL = time.Duration(cacheTTLSeconds) * time.Second

	// Optional forced host configs
	cfg.KillswitchPublicHost = os.Getenv("KILLSWITCH_PUBLIC_HOST")
	cfg.GatekeeperPublicHost = os.Getenv("GATEKEEPER_PUBLIC_HOST")

	return cfg, nil
}

// getEnvInt reads an integer environment variable or returns a default value
func getEnvInt(key string, defaultValue int) int {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	intValue, err := strconv.Atoi(value)
	if err != nil {
		return defaultValue
	}
	return intValue
}
