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

	// Overview scanner thresholds
	ScannerNoiseFloor    int // Min unique paths to be a scanner candidate (default 10)
	ScannerCandidateCap  int // Max scanner candidates from Stage 1 SQL (default 200)
	ScannerEnrichBatch   int // Max candidates per enrichment query batch (default 100)
	ScannerPathThreshold int // Unique paths to earn SCAN_SINGLE_HOST flag (default 30)

	// Overview sprayer thresholds
	SprayerHostThreshold int // Unique hosts to earn SPRAY_HOSTS flag (default 5)

	// Overview flooder thresholds
	FlooderMinTotal    int // Min total requests to a single path to be a flooder candidate (default 50)
	FlooderCandidateCap int // Max flooder candidates from Stage 1 SQL (default 200)
	FlooderMaxPaths    int // Max unique paths for an IP to qualify as a flooder (default 3)

	// Shared reason-flag thresholds
	BurstinessThreshold float64 // Burstiness ratio to earn BURSTY flag (default 5.0)
	PeakRPSThreshold    float64 // Peak RPS to earn HIGH_PEAK flag (default 10.0)
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

		// Overview scanner thresholds
		ScannerNoiseFloor:    getEnvInt("ARB_TELEMETRY_SCANNER_NOISE_FLOOR", 10),
		ScannerCandidateCap:  getEnvInt("ARB_TELEMETRY_SCANNER_CANDIDATE_CAP", 200),
		ScannerEnrichBatch:   getEnvInt("ARB_TELEMETRY_SCANNER_ENRICH_BATCH", 100),
		ScannerPathThreshold: getEnvInt("ARB_TELEMETRY_SCANNER_PATH_THRESHOLD", 30),

		// Overview sprayer thresholds
		SprayerHostThreshold: getEnvInt("ARB_TELEMETRY_SPRAYER_HOST_THRESHOLD", 5),

		// Overview flooder thresholds
		FlooderMinTotal:    getEnvInt("ARB_TELEMETRY_FLOODER_MIN_TOTAL", 50),
		FlooderCandidateCap: getEnvInt("ARB_TELEMETRY_FLOODER_CANDIDATE_CAP", 200),
		FlooderMaxPaths:    getEnvInt("ARB_TELEMETRY_FLOODER_MAX_PATHS", 3),

		// Shared reason-flag thresholds
		BurstinessThreshold: getEnvFloat("ARB_TELEMETRY_BURSTINESS_THRESHOLD", 5.0),
		PeakRPSThreshold:    getEnvFloat("ARB_TELEMETRY_PEAK_RPS_THRESHOLD", 10.0),
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

// getEnvFloat reads a float64 environment variable or returns a default value
func getEnvFloat(key string, defaultValue float64) float64 {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	f, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return defaultValue
	}
	return f
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
