package store

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// HostPolicy represents a host policy in the database
type HostPolicy struct {
	ID                  int64   `json:"id"`
	Host                string  `json:"host"`
	KillswitchRequired  bool    `json:"killswitch_required"`
	GatekeeperRequired  bool    `json:"gatekeeper_required"`
	Notes               *string `json:"notes,omitempty"`
	CreatedAt           string  `json:"created_at"`
	UpdatedAt           string  `json:"updated_at"`
}

// Store defines the interface for policy storage operations
type Store interface {
	GetByHost(host string) (*HostPolicy, error)
	GetByID(id int64) (*HostPolicy, error)
	List() ([]*HostPolicy, error)
	Create(policy *HostPolicy) (*HostPolicy, error)
	Update(id int64, policy *HostPolicy) (*HostPolicy, error)
	Delete(id int64) error
	Close() error
}

// SQLiteStore implements Store using SQLite
type SQLiteStore struct {
	db *sql.DB
}

// NewSQLiteStore creates a new SQLite store instance
func NewSQLiteStore(databaseURL string) (*SQLiteStore, error) {
	// Parse database URL - handle sqlite:/// prefix
	dbPath := databaseURL
	if strings.HasPrefix(databaseURL, "sqlite:///") {
		dbPath = strings.TrimPrefix(databaseURL, "sqlite:///")
	} else if strings.HasPrefix(databaseURL, "file:") {
		dbPath = strings.TrimPrefix(databaseURL, "file:")
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Enable WAL mode
	if _, err := db.Exec("PRAGMA journal_mode=WAL;"); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to enable WAL mode: %w", err)
	}

	// Set busy timeout
	if _, err := db.Exec("PRAGMA busy_timeout=2000;"); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to set busy timeout: %w", err)
	}

	// Test connection
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return &SQLiteStore{db: db}, nil
}

// GetByHost retrieves a policy by host (exact match, case-insensitive)
func (s *SQLiteStore) GetByHost(host string) (*HostPolicy, error) {
	host = strings.ToLower(host)
	var policy HostPolicy
	var notes sql.NullString

	err := s.db.QueryRow(
		"SELECT id, host, killswitch_required, gatekeeper_required, notes, created_at, updated_at FROM host_policies WHERE LOWER(host) = ?",
		host,
	).Scan(
		&policy.ID,
		&policy.Host,
		&policy.KillswitchRequired,
		&policy.GatekeeperRequired,
		&notes,
		&policy.CreatedAt,
		&policy.UpdatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get policy by host: %w", err)
	}

	if notes.Valid {
		policy.Notes = &notes.String
	}

	return &policy, nil
}

// GetByID retrieves a policy by ID
func (s *SQLiteStore) GetByID(id int64) (*HostPolicy, error) {
	var policy HostPolicy
	var notes sql.NullString

	err := s.db.QueryRow(
		"SELECT id, host, killswitch_required, gatekeeper_required, notes, created_at, updated_at FROM host_policies WHERE id = ?",
		id,
	).Scan(
		&policy.ID,
		&policy.Host,
		&policy.KillswitchRequired,
		&policy.GatekeeperRequired,
		&notes,
		&policy.CreatedAt,
		&policy.UpdatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get policy by id: %w", err)
	}

	if notes.Valid {
		policy.Notes = &notes.String
	}

	return &policy, nil
}

// List retrieves all policies
func (s *SQLiteStore) List() ([]*HostPolicy, error) {
	rows, err := s.db.Query(
		"SELECT id, host, killswitch_required, gatekeeper_required, notes, created_at, updated_at FROM host_policies ORDER BY host",
	)
	if err != nil {
		return nil, fmt.Errorf("failed to list policies: %w", err)
	}
	defer rows.Close()

	var policies []*HostPolicy
	for rows.Next() {
		var policy HostPolicy
		var notes sql.NullString

		if err := rows.Scan(
			&policy.ID,
			&policy.Host,
			&policy.KillswitchRequired,
			&policy.GatekeeperRequired,
			&notes,
			&policy.CreatedAt,
			&policy.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan policy: %w", err)
		}

		if notes.Valid {
			policy.Notes = &notes.String
		}

		policies = append(policies, &policy)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating policies: %w", err)
	}

	return policies, nil
}

// Create creates a new policy
func (s *SQLiteStore) Create(policy *HostPolicy) (*HostPolicy, error) {
	// Normalize host to lowercase
	host := strings.ToLower(strings.TrimSpace(policy.Host))
	if host == "" {
		return nil, fmt.Errorf("host cannot be empty")
	}

	now := time.Now().UTC().Format(time.RFC3339)

	var notes sql.NullString
	if policy.Notes != nil {
		notes = sql.NullString{String: *policy.Notes, Valid: true}
	}

	result, err := s.db.Exec(
		"INSERT INTO host_policies (host, killswitch_required, gatekeeper_required, notes, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)",
		host,
		policy.KillswitchRequired,
		policy.GatekeeperRequired,
		notes,
		now,
		now,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create policy: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("failed to get last insert id: %w", err)
	}

	return s.GetByID(id)
}

// Update updates an existing policy
func (s *SQLiteStore) Update(id int64, policy *HostPolicy) (*HostPolicy, error) {
	// Normalize host to lowercase if provided
	host := strings.ToLower(strings.TrimSpace(policy.Host))
	if host == "" {
		return nil, fmt.Errorf("host cannot be empty")
	}

	now := time.Now().UTC().Format(time.RFC3339)

	var notes sql.NullString
	if policy.Notes != nil {
		notes = sql.NullString{String: *policy.Notes, Valid: true}
	}

	_, err := s.db.Exec(
		"UPDATE host_policies SET host = ?, killswitch_required = ?, gatekeeper_required = ?, notes = ?, updated_at = ? WHERE id = ?",
		host,
		policy.KillswitchRequired,
		policy.GatekeeperRequired,
		notes,
		now,
		id,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to update policy: %w", err)
	}

	return s.GetByID(id)
}

// Delete deletes a policy by ID
func (s *SQLiteStore) Delete(id int64) error {
	result, err := s.db.Exec("DELETE FROM host_policies WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("failed to delete policy: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("policy not found")
	}

	return nil
}

// Close closes the database connection
func (s *SQLiteStore) Close() error {
	return s.db.Close()
}
