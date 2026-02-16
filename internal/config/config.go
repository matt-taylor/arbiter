package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// TelemetryConfig holds telemetry-specific configuration
type TelemetryConfig struct {
	Enabled    bool
	RedisURL   string
	StreamKey  string
	TimeoutMs  int
	BufferSize int
}

// TelemetryAPIConfig holds configuration for the read-only telemetry query API.
type TelemetryAPIConfig struct {
	Enabled           bool
	DBDSN             string
	MaxWindowMinutes  int
	MaxLimit          int
	TrustProxyHeaders bool
}

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
	Telemetry            TelemetryConfig
	TelemetryAPI         TelemetryAPIConfig
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

	// Telemetry configuration (all optional; misconfiguration never prevents startup)
	cfg.Telemetry = TelemetryConfig{
		Enabled:    getEnvBool("ARB_TELEMETRY_ENABLED", false),
		RedisURL:   getEnvString("ARB_TELEMETRY_REDIS_URL", "redis://localhost:6379/0"),
		StreamKey:  getEnvString("ARB_TELEMETRY_STREAM_KEY", "arb:v1:events"),
		TimeoutMs:  getEnvInt("ARB_TELEMETRY_TIMEOUT_MS", 25),
		BufferSize: getEnvInt("ARB_TELEMETRY_BUFFER_SIZE", 8192),
	}

	// Telemetry Query API configuration (all optional; API stays off unless explicitly enabled)
	cfg.TelemetryAPI = TelemetryAPIConfig{
		Enabled:           getEnvBool("ARB_TELEMETRY_API_ENABLED", false),
		DBDSN:             getEnvString("ARB_TELEMETRY_API_DB_DSN", ""),
		MaxWindowMinutes:  getEnvInt("ARB_TELEMETRY_API_MAX_WINDOW_MINUTES", 60),
		MaxLimit:          getEnvInt("ARB_TELEMETRY_API_MAX_LIMIT", 100),
		TrustProxyHeaders: getEnvBool("ARB_TELEMETRY_API_TRUST_PROXY_HEADERS", true),
	}
	if cfg.TelemetryAPI.Enabled {
		if cfg.TelemetryAPI.DBDSN == "" {
			return nil, fmt.Errorf("ARB_TELEMETRY_API_DB_DSN is required when telemetry API is enabled")
		}
		cfg.TelemetryAPI.DBDSN = ensureDSNParams(cfg.TelemetryAPI.DBDSN)
	}

	return cfg, nil
}

// ensureDSNParams appends parseTime=true and loc=UTC to a MySQL DSN if not already present.
func ensureDSNParams(dsn string) string {
	hasParams := strings.Contains(dsn, "?")

	needParseTime := !strings.Contains(dsn, "parseTime=true")
	needLoc := !strings.Contains(dsn, "loc=UTC") && !strings.Contains(dsn, "loc=utc")

	if !needParseTime && !needLoc {
		return dsn
	}

	var additions []string
	if needParseTime {
		additions = append(additions, "parseTime=true")
	}
	if needLoc {
		additions = append(additions, "loc=UTC")
	}

	suffix := strings.Join(additions, "&")
	if hasParams {
		return dsn + "&" + suffix
	}
	return dsn + "?" + suffix
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

// getEnvBool reads a boolean environment variable or returns a default value.
// Truthy values: "true", "1", "yes" (case-insensitive).
func getEnvBool(key string, defaultValue bool) bool {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	switch strings.ToLower(value) {
	case "true", "1", "yes":
		return true
	case "false", "0", "no":
		return false
	default:
		return defaultValue
	}
}

// getEnvString reads a string environment variable or returns a default value
func getEnvString(key string, defaultValue string) string {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	return value
}

// LoadMinimal loads minimal configuration for CLI commands (like apply-pack)
// Only requires DATABASE_URL; other fields are optional
func LoadMinimal() (*Config, error) {
	cfg := &Config{}

	// Only database URL is required for pack operations
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}
	cfg.DatabaseURL = databaseURL

	// Optional forced host configs (for anti-recursion constraints)
	cfg.KillswitchPublicHost = os.Getenv("KILLSWITCH_PUBLIC_HOST")
	cfg.GatekeeperPublicHost = os.Getenv("GATEKEEPER_PUBLIC_HOST")

	// Set defaults for other fields (not used in pack operations)
	cfg.CacheTTL = 600 * time.Second

	return cfg, nil
}
