package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/sqlite3"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/domostack/arbiter/internal/arbiter"
	"github.com/domostack/arbiter/internal/config"
	"github.com/domostack/arbiter/internal/downstream"
	"github.com/domostack/arbiter/internal/httpserver"
	"github.com/domostack/arbiter/internal/policycache"
	"github.com/domostack/arbiter/internal/store"
	"github.com/rs/zerolog"
)

func main() {
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

	// Initialize HTTP server
	server := httpserver.NewServer(
		cfg.BindAddr,
		engine,
		cache,
		dbStore,
		logger,
		cfg.KillswitchPublicHost,
		cfg.GatekeeperPublicHost,
		staticDir,
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
