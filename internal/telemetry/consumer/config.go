package consumer

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// TelemetryConsumerConfig holds configuration for the telemetry consumer binary.
// Loaded entirely from environment variables.
type TelemetryConsumerConfig struct {
	Enabled       bool
	RedisURL      string
	StreamKey     string
	ConsumerGroup string
	ConsumerName  string
	GroupStartID  string // "$" = only new; "0" = replay from beginning
	ReadCount     int
	BlockMs       int
	FlushMs       int
	PathCap       int
	DBDSN         string
	RetentionDays int // informational, used by maint script only

	// PEL claim (XAUTOCLAIM) settings
	PELClaimEnabled bool
	PELIdleMs       int
	PELClaimCount   int
}

// LoadConsumerConfig reads telemetry consumer configuration from environment variables.
// Returns an error if the consumer is enabled but required fields (e.g., DB DSN) are missing.
func LoadConsumerConfig() (*TelemetryConsumerConfig, error) {
	cfg := &TelemetryConsumerConfig{
		Enabled:         getEnvBool("ARB_TELEMETRY_CONSUMER_ENABLED", false),
		RedisURL:        getEnvString("ARB_TELEMETRY_REDIS_URL", "redis://localhost:6379/0"),
		StreamKey:       getEnvString("ARB_TELEMETRY_STREAM_KEY", "arb:v1:events"),
		ConsumerGroup:   getEnvString("ARB_TELEMETRY_CONSUMER_GROUP", "arbiter-telemetry-v1"),
		ConsumerName:    getEnvString("ARB_TELEMETRY_CONSUMER_NAME", defaultConsumerName()),
		GroupStartID:    getEnvString("ARB_TELEMETRY_GROUP_START_ID", "$"),
		ReadCount:       getEnvInt("ARB_TELEMETRY_READ_COUNT", 1000),
		BlockMs:         getEnvInt("ARB_TELEMETRY_BLOCK_MS", 200),
		FlushMs:         getEnvInt("ARB_TELEMETRY_FLUSH_MS", 1000),
		PathCap:         getEnvInt("ARB_TELEMETRY_PATH_CAP", 50),
		DBDSN:           getEnvString("ARB_TELEMETRY_DB_DSN", ""),
		RetentionDays:   getEnvInt("ARB_TELEMETRY_RETENTION_DAYS", 7),
		PELClaimEnabled: getEnvBool("ARB_TELEMETRY_PEL_CLAIM_ENABLED", false),
		PELIdleMs:       getEnvInt("ARB_TELEMETRY_PEL_IDLE_MS", 300000),
		PELClaimCount:   getEnvInt("ARB_TELEMETRY_PEL_CLAIM_COUNT", 1000),
	}

	if !cfg.Enabled {
		return cfg, nil
	}

	// Validate required fields when enabled
	if cfg.DBDSN == "" {
		return nil, fmt.Errorf("ARB_TELEMETRY_DB_DSN is required when consumer is enabled")
	}

	// Ensure DSN includes parseTime=true and loc=UTC
	cfg.DBDSN = ensureDSNParams(cfg.DBDSN)

	return cfg, nil
}

// ensureDSNParams appends parseTime=true and loc=UTC to the DSN if not already present.
func ensureDSNParams(dsn string) string {
	// Find the params portion (after '?')
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

// defaultConsumerName generates a consumer name from hostname and PID.
func defaultConsumerName() string {
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown"
	}
	return fmt.Sprintf("%s-%d", hostname, os.Getpid())
}

// ---------------------------------------------------------------------------
// env helpers (same pattern as internal/config/config.go)
// ---------------------------------------------------------------------------

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

func getEnvString(key string, defaultValue string) string {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	return value
}
