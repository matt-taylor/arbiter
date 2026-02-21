package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/mysql"
	_ "github.com/golang-migrate/migrate/v4/database/sqlite3"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/domostack/arbiter/internal/arbiter"
	"github.com/domostack/arbiter/internal/config"
	"github.com/domostack/arbiter/internal/downstream"
	"github.com/domostack/arbiter/internal/httpserver"
	"github.com/domostack/arbiter/internal/pack"
	"github.com/domostack/arbiter/internal/policycache"
	"github.com/domostack/arbiter/internal/store"
	"github.com/domostack/arbiter/internal/telemetry"
	"github.com/domostack/arbiter/internal/telemetry/query"
	"github.com/rs/zerolog"
)

func main() {
	// Check for CLI subcommands
	if len(os.Args) > 1 && os.Args[1] == "apply-pack" {
		applyPackCommand()
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "nuke-db" {
		nukeDBCommand()
		return
	}

	// Initialize logger
	logger := zerolog.New(os.Stdout).With().
		Timestamp().
		Logger()

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to load configuration")
	}

	// Run database migrations
	if err := runMigrations(cfg.DatabaseURL, logger); err != nil {
		logger.Fatal().Err(err).Msg("failed to run migrations")
	}

	// Initialize store
	dbStore, err := store.NewSQLiteStore(cfg.DatabaseURL)
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to initialize store")
	}
	defer dbStore.Close()

	// Initialize cache
	cache := policycache.NewCache(dbStore, cfg.CacheTTL)

	// Initialize downstream client
	downstreamClient := downstream.NewClient(
		cfg.KillswitchBaseURL,
		cfg.GatekeeperBaseURL,
		cfg.KillswitchTimeout,
		cfg.GatekeeperTimeout,
	)

	// Initialize decision engine
	engine := arbiter.NewEngine(
		cache,
		downstreamClient,
		cfg.KillswitchPublicHost,
		cfg.GatekeeperPublicHost,
	)

	// Determine static directory (frontend/dist if it exists)
	staticDir := ""
	if exePath, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exePath)
		// Try relative to executable
		distPath := filepath.Join(exeDir, "..", "frontend", "dist")
		if _, err := os.Stat(distPath); err == nil {
			staticDir = distPath
		} else {
			// Try relative to current working directory
			cwd, _ := os.Getwd()
			distPath = filepath.Join(cwd, "frontend", "dist")
			if _, err := os.Stat(distPath); err == nil {
				staticDir = distPath
			}
		}
	}

	// Initialize telemetry publisher
	publisher := telemetry.NewPublisher(cfg.Telemetry, logger)

	// Initialize telemetry query API (optional, MariaDB connection)
	var telemetryRepo *query.Repository
	if cfg.TelemetryAPI.Enabled {
		// Run MariaDB rollup table migrations (idempotent — same migrations the consumer runs)
		if err := runTelemetryMigrations(cfg.TelemetryAPI.DBDSN, logger); err != nil {
			logger.Fatal().Err(err).Msg("telemetry MariaDB migrations failed")
		}

		telemetryDB, err := sql.Open("mysql", cfg.TelemetryAPI.DBDSN)
		if err != nil {
			logger.Fatal().Err(err).Msg("failed to open telemetry API MariaDB connection")
		}
		telemetryDB.SetMaxOpenConns(5)
		telemetryDB.SetMaxIdleConns(2)
		telemetryDB.SetConnMaxIdleTime(30 * time.Second)
		telemetryDB.SetConnMaxLifetime(5 * time.Minute)

		ctx := context.Background()
		if err := telemetryDB.PingContext(ctx); err != nil {
			logger.Fatal().Err(err).Msg("cannot reach telemetry API MariaDB")
		}
		defer telemetryDB.Close() // main owns lifecycle
		telemetryRepo = query.NewRepositoryWithConfig(telemetryDB, query.RepositoryConfig{
			ScannerNoiseFloor:   cfg.TelemetryAPI.ScannerNoiseFloor,
			ScannerCandidateCap: cfg.TelemetryAPI.ScannerCandidateCap,
			ScannerEnrichBatch:  cfg.TelemetryAPI.ScannerEnrichBatch,
			FlooderMinTotal:     cfg.TelemetryAPI.FlooderMinTotal,
			FlooderCandidateCap: cfg.TelemetryAPI.FlooderCandidateCap,
			FlooderMaxPaths:     cfg.TelemetryAPI.FlooderMaxPaths,
		})
		logger.Info().Msg("telemetry query API enabled (MariaDB connected)")
	}

	// Initialize HTTP server
	server := httpserver.NewServer(
		cfg.BindAddr,
		engine,
		cache,
		dbStore,
		logger,
		cfg.KillswitchPublicHost,
		cfg.GatekeeperPublicHost,
		cfg.KillswitchBaseURL,
		staticDir,
		publisher,
		telemetryRepo,
		cfg.TelemetryAPI,
	)

	// Setup graceful shutdown
	_, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start server in goroutine
	serverErr := make(chan error, 1)
	go func() {
		if err := server.Start(); err != nil {
			serverErr <- err
		}
	}()

	// Wait for interrupt signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	select {
	case <-sigChan:
		logger.Info().Msg("received shutdown signal")
	case err := <-serverErr:
		logger.Error().Err(err).Msg("server error")
	}

	// Graceful shutdown
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error().Err(err).Msg("error during shutdown")
	} else {
		logger.Info().Msg("server shutdown complete")
	}

	// Close telemetry publisher after server is shut down.
	// Close() signals the worker to stop immediately (no drain) and closes the Redis client.
	publisher.Close()
}

// runMigrations runs database migrations
func runMigrations(databaseURL string, logger zerolog.Logger) error {
	// Parse database URL for migrate
	dbPath := databaseURL
	if len(dbPath) > 10 && dbPath[:10] == "sqlite:///" {
		dbPath = dbPath[10:]
	} else if len(dbPath) > 5 && dbPath[:5] == "file:" {
		dbPath = dbPath[5:]
	}

	// Convert to absolute path
	dbPath, err := filepath.Abs(dbPath)
	if err != nil {
		return fmt.Errorf("failed to get absolute database path: %w", err)
	}

	// Ensure the directory exists
	dbDir := filepath.Dir(dbPath)
	if dbDir != "." && dbDir != "" {
		if err := os.MkdirAll(dbDir, 0755); err != nil {
			return fmt.Errorf("failed to create database directory: %w", err)
		}
	}

	// Get migrations directory
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}
	exeDir := filepath.Dir(exePath)

	// Try relative to executable
	migrationsPath := filepath.Join(exeDir, "..", "migrations")
	if _, err := os.Stat(migrationsPath); os.IsNotExist(err) {
		// Try relative to current working directory
		cwd, _ := os.Getwd()
		migrationsPath = filepath.Join(cwd, "migrations")
		if _, err := os.Stat(migrationsPath); os.IsNotExist(err) {
			return fmt.Errorf("migrations directory not found")
		}
	}

	// Convert to absolute path
	migrationsPath, err = filepath.Abs(migrationsPath)
	if err != nil {
		return fmt.Errorf("failed to get absolute path: %w", err)
	}

	// Use file:// protocol for source
	sourceURL := fmt.Sprintf("file://%s", migrationsPath)

	// Use sqlite3:// protocol for database
	dbURL := fmt.Sprintf("sqlite3://%s", dbPath)

	logger.Info().
		Str("source", sourceURL).
		Str("database", dbURL).
		Msg("running migrations")

	m, err := migrate.New(sourceURL, dbURL)
	if err != nil {
		return fmt.Errorf("failed to create migrate instance: %w", err)
	}
	defer m.Close()

	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	if err == migrate.ErrNoChange {
		logger.Info().Msg("database is up to date")
	} else {
		logger.Info().Msg("migrations completed successfully")
	}

	return nil
}

// runTelemetryMigrations runs golang-migrate against the telemetry MariaDB database.
// Migration files are expected in db/migrations/ relative to the executable or cwd.
// This is the same migration set that the telemetry consumer runs on boot.
func runTelemetryMigrations(dsn string, logger zerolog.Logger) error {
	// Locate db/migrations/ directory
	candidates := []string{}

	if exePath, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exePath)
		candidates = append(candidates, filepath.Join(exeDir, "..", "db", "migrations"))
	}
	if cwd, err := os.Getwd(); err == nil {
		candidates = append(candidates, filepath.Join(cwd, "db", "migrations"))
	}

	var migrationsPath string
	for _, p := range candidates {
		abs, err := filepath.Abs(p)
		if err != nil {
			continue
		}
		if info, err := os.Stat(abs); err == nil && info.IsDir() {
			migrationsPath = abs
			break
		}
	}
	if migrationsPath == "" {
		return fmt.Errorf("telemetry migrations directory not found (tried: %s)", strings.Join(candidates, ", "))
	}

	sourceURL := fmt.Sprintf("file://%s", migrationsPath)

	// Convert go-sql-driver DSN to mysql:// URL for golang-migrate
	dbURL := dsn
	if !strings.HasPrefix(dbURL, "mysql://") {
		dbURL = "mysql://" + dbURL
	}

	logger.Info().
		Str("source", sourceURL).
		Msg("running telemetry MariaDB migrations")

	m, err := migrate.New(sourceURL, dbURL)
	if err != nil {
		return fmt.Errorf("failed to create migrate instance: %w", err)
	}
	defer m.Close()

	err = m.Up()
	if err != nil && err != migrate.ErrNoChange {
		return fmt.Errorf("failed to run telemetry migrations: %w", err)
	}

	if err == migrate.ErrNoChange {
		logger.Info().Msg("telemetry MariaDB schema is up to date")
	} else {
		logger.Info().Msg("telemetry MariaDB migrations completed successfully")
	}

	return nil
}

// applyPackCommand handles the apply-pack CLI subcommand
func applyPackCommand() {
	// Create a new flag set for the apply-pack subcommand
	// Skip os.Args[0] (program name) and os.Args[1] ("apply-pack")
	fs := flag.NewFlagSet("apply-pack", flag.ExitOnError)
	var packFile string
	fs.StringVar(&packFile, "file", "", "Path to policy pack YAML file (required)")

	// Parse only the remaining arguments (skip program name and "apply-pack")
	if len(os.Args) < 3 {
		fmt.Fprintf(os.Stderr, "Error: --file flag is required\n")
		fmt.Fprintf(os.Stderr, "Usage: arbiter apply-pack --file /path/to/pack.yml\n")
		os.Exit(1)
	}

	fs.Parse(os.Args[2:])

	if packFile == "" {
		fmt.Fprintf(os.Stderr, "Error: --file flag is required\n")
		fmt.Fprintf(os.Stderr, "Usage: arbiter apply-pack --file /path/to/pack.yml\n")
		os.Exit(1)
	}

	// Initialize logger
	logger := zerolog.New(os.Stdout).With().
		Timestamp().
		Logger()

	// Load minimal configuration (only DATABASE_URL required for pack operations)
	cfg, err := config.LoadMinimal()
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to load configuration")
	}

	// Run database migrations
	if err := runMigrations(cfg.DatabaseURL, logger); err != nil {
		logger.Fatal().Err(err).Msg("failed to run migrations")
	}

	// Initialize store
	dbStore, err := store.NewSQLiteStore(cfg.DatabaseURL)
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to initialize store")
	}
	defer dbStore.Close()

	// Initialize cache
	cache := policycache.NewCache(dbStore, cfg.CacheTTL)

	// Parse pack file
	logger.Info().Str("file", packFile).Msg("parsing policy pack")
	policyPack, err := pack.ParsePackFile(packFile)
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to parse policy pack")
	}

	logger.Info().
		Str("pack", policyPack.Pack).
		Int("version", policyPack.Version).
		Int("policies", len(policyPack.Policies)).
		Msg("pack parsed successfully")

	// Apply pack
	logger.Info().Msg("applying policy pack")
	if err := pack.ApplyPack(
		dbStore,
		cache,
		policyPack,
		cfg.KillswitchPublicHost,
		cfg.GatekeeperPublicHost,
	); err != nil {
		logger.Fatal().Err(err).Msg("failed to apply policy pack")
	}

	logger.Info().Msg("policy pack applied successfully")
}

// nukeDBCommand handles the nuke-db CLI subcommand
func nukeDBCommand() {
	// Initialize logger
	logger := zerolog.New(os.Stdout).With().
		Timestamp().
		Logger()

	// Load minimal configuration (only DATABASE_URL required)
	cfg, err := config.LoadMinimal()
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to load configuration")
	}

	// Parse database path
	dbPath := cfg.DatabaseURL
	if strings.HasPrefix(dbPath, "sqlite:///") {
		dbPath = strings.TrimPrefix(dbPath, "sqlite:///")
	} else if strings.HasPrefix(dbPath, "file:") {
		dbPath = strings.TrimPrefix(dbPath, "file:")
	}

	// Convert to absolute path
	dbPath, err = filepath.Abs(dbPath)
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to get absolute database path")
	}

	// Check if database exists
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		logger.Info().Str("path", dbPath).Msg("database does not exist, nothing to delete")
		return
	}

	// Confirmation prompt
	fmt.Fprintf(os.Stderr, "\n⚠️  WARNING: This will DELETE the entire database at:\n   %s\n\n", dbPath)
	fmt.Fprintf(os.Stderr, "This action cannot be undone. All policies will be permanently deleted.\n\n")
	fmt.Fprintf(os.Stderr, "Type 'yes' to confirm: ")

	var confirmation string
	fmt.Scanln(&confirmation)

	if confirmation != "yes" {
		fmt.Fprintf(os.Stderr, "\nAborted. Database was not deleted.\n")
		os.Exit(0)
	}

	// Close any existing connections by attempting to open and close
	// This ensures the file is not locked
	dbStore, err := store.NewSQLiteStore(cfg.DatabaseURL)
	if err == nil {
		dbStore.Close()
	}

	// Delete the database file
	logger.Info().Str("path", dbPath).Msg("deleting database file")
	if err := os.Remove(dbPath); err != nil {
		logger.Fatal().Err(err).Msg("failed to delete database file")
	}

	// Also delete WAL and SHM files if they exist
	walPath := dbPath + "-wal"
	shmPath := dbPath + "-shm"
	if _, err := os.Stat(walPath); err == nil {
		os.Remove(walPath)
	}
	if _, err := os.Stat(shmPath); err == nil {
		os.Remove(shmPath)
	}

	logger.Info().Msg("database deleted successfully")

	// Re-run migrations to create a fresh database
	logger.Info().Msg("creating fresh database with migrations")
	if err := runMigrations(cfg.DatabaseURL, logger); err != nil {
		logger.Fatal().Err(err).Msg("failed to run migrations")
	}

	// Invalidate cache to ensure any running server instances reload data
	// Create a temporary store and cache instance to invalidate
	tempStore, err := store.NewSQLiteStore(cfg.DatabaseURL)
	if err == nil {
		cache := policycache.NewCache(tempStore, cfg.CacheTTL)
		cache.Invalidate()
		logger.Info().Msg("cache invalidated")
		tempStore.Close()
	}

	logger.Info().Msg("database reset complete")
}
