package consumer

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// maxRowsPerInsert caps the number of value tuples in a single INSERT statement
// to stay within MariaDB's max_allowed_packet.
const maxRowsPerInsert = 500

// RollupDB writes aggregated rollup data to MariaDB.
type RollupDB struct {
	db *sql.DB
}

// NewRollupDB creates a new RollupDB from an existing *sql.DB connection.
func NewRollupDB(db *sql.DB) *RollupDB {
	return &RollupDB{db: db}
}

// Flush writes the snapshot data to MariaDB in a single transaction.
// Returns an error if any INSERT fails (the transaction is rolled back).
func (r *RollupDB) Flush(ctx context.Context, snap Snapshot) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("rollupdb: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	// Flush host+IP rows
	if err := r.flushHostIP(ctx, tx, snap.HostIP); err != nil {
		return err
	}

	// Flush host+IP+path rows
	if err := r.flushHostIPPath(ctx, tx, snap.HostIPPath); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("rollupdb: commit: %w", err)
	}
	return nil
}

// Close closes the underlying database connection.
func (r *RollupDB) Close() error {
	return r.db.Close()
}

// ---------------------------------------------------------------------------
// Host+IP upserts
// ---------------------------------------------------------------------------

func (r *RollupDB) flushHostIP(ctx context.Context, tx *sql.Tx, rows map[HostIPKey]*HostIPCounters) error {
	if len(rows) == 0 {
		return nil
	}

	batches := buildHostIPBatches(rows)
	for _, b := range batches {
		if _, err := tx.ExecContext(ctx, b.SQL, b.Args...); err != nil {
			return fmt.Errorf("rollupdb: insert host_ip: %w", err)
		}
	}
	return nil
}

// hostIPBatch is a single INSERT statement with its parameters.
type hostIPBatch struct {
	SQL  string
	Args []interface{}
}

// hostIPEntry pairs a key with its counters for batch building.
type hostIPEntry struct {
	key HostIPKey
	val *HostIPCounters
}

// buildHostIPBatches generates INSERT ... ON DUPLICATE KEY UPDATE batches.
func buildHostIPBatches(rows map[HostIPKey]*HostIPCounters) []hostIPBatch {
	entries := make([]hostIPEntry, 0, len(rows))
	for k, v := range rows {
		entries = append(entries, hostIPEntry{k, v})
	}

	var batches []hostIPBatch
	for i := 0; i < len(entries); i += maxRowsPerInsert {
		end := i + maxRowsPerInsert
		if end > len(entries) {
			end = len(entries)
		}
		chunk := entries[i:end]

		sql, args := buildHostIPSQL(chunk)
		batches = append(batches, hostIPBatch{SQL: sql, Args: args})
	}
	return batches
}

func buildHostIPSQL(entries []hostIPEntry) (string, []interface{}) {
	// 14 columns: bucket_start, host, ip, total, c_401..c_5xx (5), m_get..m_delete (5)
	const cols = 14
	const colNames = `(bucket_start, host, ip, total, c_401, c_403, c_404, c_429, c_5xx, m_get, m_post, m_put, m_patch, m_delete)`

	placeholders := make([]string, len(entries))
	args := make([]interface{}, 0, len(entries)*cols)

	for i, e := range entries {
		placeholders[i] = "(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)"
		args = append(args,
			e.key.BucketStart, e.key.Host, e.key.IP,
			e.val.Total,
			e.val.C401, e.val.C403, e.val.C404, e.val.C429, e.val.C5xx,
			e.val.MGet, e.val.MPost, e.val.MPut, e.val.MPatch, e.val.MDelete,
		)
	}

	var sb strings.Builder
	sb.WriteString("INSERT INTO arb_host_ip_10s ")
	sb.WriteString(colNames)
	sb.WriteString(" VALUES ")
	sb.WriteString(strings.Join(placeholders, ", "))
	sb.WriteString(` ON DUPLICATE KEY UPDATE ` +
		`total = total + VALUES(total), ` +
		`c_401 = c_401 + VALUES(c_401), ` +
		`c_403 = c_403 + VALUES(c_403), ` +
		`c_404 = c_404 + VALUES(c_404), ` +
		`c_429 = c_429 + VALUES(c_429), ` +
		`c_5xx = c_5xx + VALUES(c_5xx), ` +
		`m_get = m_get + VALUES(m_get), ` +
		`m_post = m_post + VALUES(m_post), ` +
		`m_put = m_put + VALUES(m_put), ` +
		`m_patch = m_patch + VALUES(m_patch), ` +
		`m_delete = m_delete + VALUES(m_delete)`)

	return sb.String(), args
}

// ---------------------------------------------------------------------------
// Host+IP+Path upserts
// ---------------------------------------------------------------------------

func (r *RollupDB) flushHostIPPath(ctx context.Context, tx *sql.Tx, rows map[HostIPPathKey]*PathCounters) error {
	if len(rows) == 0 {
		return nil
	}

	batches := buildHostIPPathBatches(rows)
	for _, b := range batches {
		if _, err := tx.ExecContext(ctx, b.SQL, b.Args...); err != nil {
			return fmt.Errorf("rollupdb: insert host_ip_path: %w", err)
		}
	}
	return nil
}

type hostIPPathBatch struct {
	SQL  string
	Args []interface{}
}

// hostIPPathEntry pairs a key with its counters for batch building.
type hostIPPathEntry struct {
	key HostIPPathKey
	val *PathCounters
}

func buildHostIPPathBatches(rows map[HostIPPathKey]*PathCounters) []hostIPPathBatch {
	entries := make([]hostIPPathEntry, 0, len(rows))
	for k, v := range rows {
		entries = append(entries, hostIPPathEntry{k, v})
	}

	var batches []hostIPPathBatch
	for i := 0; i < len(entries); i += maxRowsPerInsert {
		end := i + maxRowsPerInsert
		if end > len(entries) {
			end = len(entries)
		}
		chunk := entries[i:end]

		sql, args := buildHostIPPathSQL(chunk)
		batches = append(batches, hostIPPathBatch{SQL: sql, Args: args})
	}
	return batches
}

func buildHostIPPathSQL(entries []hostIPPathEntry) (string, []interface{}) {
	// 11 columns: bucket_start, host, ip, path_hash, path, total, c_401..c_5xx (5)
	const cols = 11
	const colNames = `(bucket_start, host, ip, path_hash, path, total, c_401, c_403, c_404, c_429, c_5xx)`

	placeholders := make([]string, len(entries))
	args := make([]interface{}, 0, len(entries)*cols)

	for i, e := range entries {
		placeholders[i] = "(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)"
		// path_hash is passed as []byte — the MySQL driver writes it to BINARY(16) natively
		args = append(args,
			e.key.BucketStart, e.key.Host, e.key.IP,
			e.key.PathHash[:], // [16]byte → []byte
			e.val.Path,
			e.val.Total,
			e.val.C401, e.val.C403, e.val.C404, e.val.C429, e.val.C5xx,
		)
	}

	var sb strings.Builder
	sb.WriteString("INSERT INTO arb_host_ip_path_10s ")
	sb.WriteString(colNames)
	sb.WriteString(" VALUES ")
	sb.WriteString(strings.Join(placeholders, ", "))
	sb.WriteString(` ON DUPLICATE KEY UPDATE ` +
		`total = total + VALUES(total), ` +
		`c_401 = c_401 + VALUES(c_401), ` +
		`c_403 = c_403 + VALUES(c_403), ` +
		`c_404 = c_404 + VALUES(c_404), ` +
		`c_429 = c_429 + VALUES(c_429), ` +
		`c_5xx = c_5xx + VALUES(c_5xx)`)

	return sb.String(), args
}

// ---------------------------------------------------------------------------
// Exported test helpers
// ---------------------------------------------------------------------------

// BuildHostIPBatchesForTest exposes buildHostIPBatches for unit testing.
func BuildHostIPBatchesForTest(rows map[HostIPKey]*HostIPCounters) []struct {
	SQL  string
	Args []interface{}
} {
	batches := buildHostIPBatches(rows)
	out := make([]struct {
		SQL  string
		Args []interface{}
	}, len(batches))
	for i, b := range batches {
		out[i] = struct {
			SQL  string
			Args []interface{}
		}{SQL: b.SQL, Args: b.Args}
	}
	return out
}

// BuildHostIPPathBatchesForTest exposes buildHostIPPathBatches for unit testing.
func BuildHostIPPathBatchesForTest(rows map[HostIPPathKey]*PathCounters) []struct {
	SQL  string
	Args []interface{}
} {
	batches := buildHostIPPathBatches(rows)
	out := make([]struct {
		SQL  string
		Args []interface{}
	}, len(batches))
	for i, b := range batches {
		out[i] = struct {
			SQL  string
			Args []interface{}
		}{SQL: b.SQL, Args: b.Args}
	}
	return out
}
