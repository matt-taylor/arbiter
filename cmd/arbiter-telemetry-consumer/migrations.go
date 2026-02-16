package main

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/mysql"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/rs/zerolog"
)

// runMigrations runs golang-migrate against the telemetry MariaDB database.
// Migration files are expected in db/migrations/ relative to the executable
// or the current working directory.
func runMigrations(dsn string, logger zerolog.Logger) error {
	// Locate migrations directory
	migrationsPath, err := findMigrationsDir()
	if err != nil {
		return err
	}

	sourceURL := fmt.Sprintf("file://%s", migrationsPath)

	// Convert go-sql-driver DSN to a mysql:// URL that golang-migrate expects
	dbURL, err := dsnToMigrateURL(dsn)
	if err != nil {
		return fmt.Errorf("failed to convert DSN for migrate: %w", err)
	}

	logger.Info().
		Str("source", sourceURL).
		Str("database", maskDSN(dsn)).
		Msg("running database migrations")

	m, err := migrate.New(sourceURL, dbURL)
	if err != nil {
		return fmt.Errorf("failed to create migrate instance: %w", err)
	}
	defer m.Close()

	err = m.Up()
	if err != nil && err != migrate.ErrNoChange {
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	if err == migrate.ErrNoChange {
		logger.Info().Msg("database schema is up to date")
	} else {
		logger.Info().Msg("database migrations completed successfully")
	}

	return nil
}

// findMigrationsDir looks for db/migrations/ relative to the executable
// and then relative to the current working directory.
func findMigrationsDir() (string, error) {
	candidates := []string{}

	// Try relative to executable: ../db/migrations
	if exePath, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exePath)
		candidates = append(candidates, filepath.Join(exeDir, "..", "db", "migrations"))
	}

	// Try relative to cwd: ./db/migrations
	if cwd, err := os.Getwd(); err == nil {
		candidates = append(candidates, filepath.Join(cwd, "db", "migrations"))
	}

	for _, p := range candidates {
		abs, err := filepath.Abs(p)
		if err != nil {
			continue
		}
		if info, err := os.Stat(abs); err == nil && info.IsDir() {
			return abs, nil
		}
	}

	return "", fmt.Errorf("migrations directory not found (tried: %s)", strings.Join(candidates, ", "))
}

// dsnToMigrateURL converts a go-sql-driver/mysql DSN like
//
//	user:pass@tcp(host:port)/dbname?params
//
// to golang-migrate's expected URL format:
//
//	mysql://user:pass@tcp(host:port)/dbname?params
func dsnToMigrateURL(dsn string) (string, error) {
	// If it already starts with mysql://, return as-is
	if strings.HasPrefix(dsn, "mysql://") {
		return dsn, nil
	}

	// The go-sql-driver DSN format is: [user[:password]@][net[(addr)]]/dbname[?param1=value1&...]
	// golang-migrate expects: mysql://user:password@tcp(host:port)/dbname?params
	// We can simply prepend mysql:// to the DSN.
	return "mysql://" + dsn, nil
}

// maskDSN masks the password in a DSN for safe logging.
func maskDSN(dsn string) string {
	// Try to parse as a URL first (in case it has mysql:// prefix)
	if strings.HasPrefix(dsn, "mysql://") {
		if u, err := url.Parse(dsn); err == nil && u.User != nil {
			if _, hasPass := u.User.Password(); hasPass {
				u.User = url.UserPassword(u.User.Username(), "***")
				return u.String()
			}
		}
		return dsn
	}

	// go-sql-driver format: user:pass@tcp(...)
	atIdx := strings.Index(dsn, "@")
	if atIdx < 0 {
		return dsn
	}
	userPass := dsn[:atIdx]
	rest := dsn[atIdx:]
	colonIdx := strings.Index(userPass, ":")
	if colonIdx < 0 {
		return dsn
	}
	return userPass[:colonIdx+1] + "***" + rest
}
