package main

import (
	"context"
	"database/sql"
	"os"
	"os/signal"
	"syscall"

	_ "github.com/go-sql-driver/mysql"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"

	"github.com/domostack/arbiter/internal/telemetry/consumer"
)

func main() {
	logger := zerolog.New(os.Stdout).With().
		Timestamp().
		Logger()

	// Load consumer config from env vars
	cfg, err := consumer.LoadConsumerConfig()
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to load telemetry consumer config")
	}
	if !cfg.Enabled {
		logger.Info().Msg("telemetry consumer is disabled (ARB_TELEMETRY_CONSUMER_ENABLED != true); exiting")
		return
	}

	// Connect to Redis
	opts, err := redis.ParseURL(cfg.RedisURL)
	if err != nil {
		logger.Fatal().Err(err).Str("url", cfg.RedisURL).Msg("invalid Redis URL")
	}
	rdb := redis.NewClient(opts)
	defer rdb.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := rdb.Ping(ctx).Err(); err != nil {
		logger.Fatal().Err(err).Msg("cannot reach Redis")
	}
	logger.Info().Str("url", cfg.RedisURL).Msg("connected to Redis")

	// Open MariaDB connection pool
	db, err := sql.Open("mysql", cfg.DBDSN)
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to open MariaDB connection")
	}
	defer db.Close()

	// Conservative pool settings — only the consumer writes
	db.SetMaxOpenConns(2)
	db.SetMaxIdleConns(2)

	if err := db.PingContext(ctx); err != nil {
		logger.Fatal().Err(err).Msg("cannot reach MariaDB")
	}
	logger.Info().Msg("connected to MariaDB")

	// Run schema migrations on boot (idempotent)
	if err := runMigrations(cfg.DBDSN, logger); err != nil {
		logger.Fatal().Err(err).Msg("database migrations failed")
	}

	rollupDB := consumer.NewRollupDB(db)

	// Create consumer
	c := consumer.NewConsumer(*cfg, rdb, rollupDB, logger)

	// Create consumer group (idempotent)
	if err := c.EnsureGroup(ctx); err != nil {
		logger.Fatal().Err(err).Msg("failed to create/verify consumer group")
	}

	// Optional: claim stale PEL entries
	if err := c.ClaimPending(ctx); err != nil {
		logger.Warn().Err(err).Msg("failed to claim PEL entries (continuing)")
	}

	// Run consumer in goroutine
	go c.Run(ctx)

	// Wait for shutdown signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	<-sigCh

	logger.Info().Msg("received shutdown signal")
	c.Shutdown()
	cancel()

	logger.Info().Msg("telemetry consumer exited")
}
