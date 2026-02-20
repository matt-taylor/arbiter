package query

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// RepositoryConfig holds configurable thresholds for overview queries.
type RepositoryConfig struct {
	ScannerNoiseFloor   int // Min unique paths to be a scanner candidate (default 10)
	ScannerCandidateCap int // Max scanner candidates from Stage 1 SQL (default 200)
	ScannerEnrichBatch  int // Max candidates per enrichment query batch (default 100)
}

// DefaultRepositoryConfig returns the default thresholds.
func DefaultRepositoryConfig() RepositoryConfig {
	return RepositoryConfig{
		ScannerNoiseFloor:   10,
		ScannerCandidateCap: 200,
		ScannerEnrichBatch:  100,
	}
}

// Repository provides read-only access to the telemetry rollup tables in MariaDB.
//
// The Repository does NOT own the *sql.DB connection — the caller (cmd/main)
// is responsible for calling db.Close(). This avoids double-close confusion.
type Repository struct {
	db  *sql.DB
	cfg RepositoryConfig
}

// NewRepository wraps an existing *sql.DB for querying rollup tables using default thresholds.
func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db, cfg: DefaultRepositoryConfig()}
}

// NewRepositoryWithConfig wraps an existing *sql.DB with explicit thresholds.
func NewRepositoryWithConfig(db *sql.DB, cfg RepositoryConfig) *Repository {
	return &Repository{db: db, cfg: cfg}
}

// TopIPs returns the top IPs by total request count for a given host
// within the time range [start, end). Both start and end are 10s-aligned
// epoch seconds.
func (r *Repository) TopIPs(ctx context.Context, host string, start, end int64, limit int) ([]IPRow, error) {
	const q = `
SELECT ip,
       SUM(total) AS total,
       SUM(c_401) AS c_401, SUM(c_403) AS c_403,
       SUM(c_404) AS c_404, SUM(c_429) AS c_429,
       SUM(c_5xx) AS c_5xx
  FROM arb_host_ip_10s
 WHERE host = ? AND bucket_start >= ? AND bucket_start < ?
 GROUP BY ip
 ORDER BY total DESC
 LIMIT ?`

	rows, err := r.db.QueryContext(ctx, q, host, start, end, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []IPRow
	for rows.Next() {
		var row IPRow
		if err := rows.Scan(&row.IP, &row.Total, &row.C401, &row.C403, &row.C404, &row.C429, &row.C5xx); err != nil {
			return nil, err
		}
		result = append(result, row)
	}
	return result, rows.Err()
}

// TopPaths returns the top paths by total request count for a given host and IP
// within the time range [start, end). Both start and end are 10s-aligned
// epoch seconds.
func (r *Repository) TopPaths(ctx context.Context, host, ip string, start, end int64, limit int) ([]PathRow, error) {
	const q = `
SELECT path,
       SUM(total) AS total,
       SUM(c_401) AS c_401, SUM(c_403) AS c_403,
       SUM(c_404) AS c_404, SUM(c_429) AS c_429,
       SUM(c_5xx) AS c_5xx
  FROM arb_host_ip_path_10s
 WHERE host = ? AND ip = ? AND bucket_start >= ? AND bucket_start < ?
 GROUP BY path
 ORDER BY total DESC
 LIMIT ?`

	rows, err := r.db.QueryContext(ctx, q, host, ip, start, end, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []PathRow
	for rows.Next() {
		var row PathRow
		if err := rows.Scan(&row.Path, &row.Total, &row.C401, &row.C403, &row.C404, &row.C429, &row.C5xx); err != nil {
			return nil, err
		}
		result = append(result, row)
	}
	return result, rows.Err()
}

// Summary returns aggregate counters and unique IP count for a given host
// within the time range [start, end). Both start and end are 10s-aligned
// epoch seconds.
//
// Cost note: COUNT(DISTINCT ip) cost grows with the window size because it
// must scan every qualifying row to de-duplicate. The MaxWindowMinutes default
// of 60 keeps this reasonable. If the hard cap is raised beyond 180 minutes,
// verify query latency against production data volumes.
func (r *Repository) Summary(ctx context.Context, host string, start, end int64) (*SummaryRow, error) {
	const q = `
SELECT SUM(total) AS total,
       SUM(c_401) AS c_401, SUM(c_403) AS c_403,
       SUM(c_404) AS c_404, SUM(c_429) AS c_429,
       SUM(c_5xx) AS c_5xx,
       COUNT(DISTINCT ip) AS unique_ips
  FROM arb_host_ip_10s
 WHERE host = ? AND bucket_start >= ? AND bucket_start < ?`

	var row SummaryRow
	// SUM returns NULL when there are no matching rows, so we scan into sql.NullInt64.
	var total, c401, c403, c404, c429, c5xx, uniqueIPs sql.NullInt64
	err := r.db.QueryRowContext(ctx, q, host, start, end).Scan(
		&total, &c401, &c403, &c404, &c429, &c5xx, &uniqueIPs,
	)
	if err != nil {
		return nil, err
	}

	row.Total = total.Int64
	row.C401 = c401.Int64
	row.C403 = c403.Int64
	row.C404 = c404.Int64
	row.C429 = c429.Int64
	row.C5xx = c5xx.Int64
	row.UniqueIPs = uniqueIPs.Int64

	return &row, nil
}

// ── Overview Methods ────────────────────────────────────────────────────

// OverviewTopHosts returns the top hosts by total request count within [start, end).
// This is Query 1 of the two-query top-hosts pattern.
func (r *Repository) OverviewTopHosts(ctx context.Context, start, end int64, limit int) ([]OverviewHostRow, error) {
	const q = `
SELECT host, SUM(total) AS total, COUNT(DISTINCT ip) AS unique_ips
  FROM arb_host_ip_10s
 WHERE bucket_start >= ? AND bucket_start < ?
 GROUP BY host
 ORDER BY total DESC
 LIMIT ?`

	rows, err := r.db.QueryContext(ctx, q, start, end, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []OverviewHostRow
	for rows.Next() {
		var row OverviewHostRow
		if err := rows.Scan(&row.Host, &row.Total, &row.UniqueIPs); err != nil {
			return nil, err
		}
		result = append(result, row)
	}
	return result, rows.Err()
}

// OverviewTopHostsPeak returns the peak bucket total for each of the given hosts
// within [start, end). This is Query 2; it must only be called with the hosts
// returned by OverviewTopHosts (empty hosts slice → skip call).
func (r *Repository) OverviewTopHostsPeak(ctx context.Context, start, end int64, hosts []string) ([]HostPeakRow, error) {
	if len(hosts) == 0 {
		return nil, nil
	}

	// Build host IN (?, ?, ...) placeholders
	placeholders := make([]string, len(hosts))
	args := []interface{}{start, end}
	for i, h := range hosts {
		placeholders[i] = "?"
		args = append(args, h)
	}

	q := fmt.Sprintf(`
SELECT host, MAX(bucket_total) AS max_bucket_total
  FROM (
    SELECT host, bucket_start, SUM(total) AS bucket_total
      FROM arb_host_ip_10s
     WHERE bucket_start >= ? AND bucket_start < ?
       AND host IN (%s)
     GROUP BY host, bucket_start
  ) sub
 GROUP BY host`, strings.Join(placeholders, ","))

	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []HostPeakRow
	for rows.Next() {
		var row HostPeakRow
		if err := rows.Scan(&row.Host, &row.MaxBucketTotal); err != nil {
			return nil, err
		}
		result = append(result, row)
	}
	return result, rows.Err()
}

// NOTE: scannerNoiseFloor, scannerCandidateCap, and scannerEnrichBatchSize
// are now configurable via RepositoryConfig fields on the Repository struct.
// Defaults are set in DefaultRepositoryConfig().

// OverviewScannerCandidates returns scanner candidates (host+ip pairs with many
// unique paths) within [start, end). Stage 1 of the two-stage scanner pattern.
func (r *Repository) OverviewScannerCandidates(ctx context.Context, start, end int64) ([]ScannerCandidateRow, error) {
	const q = `
SELECT host, ip, COUNT(DISTINCT path_hash) AS unique_paths
  FROM arb_host_ip_path_10s
 WHERE bucket_start >= ? AND bucket_start < ?
 GROUP BY host, ip
HAVING COUNT(DISTINCT path_hash) >= ?
 ORDER BY unique_paths DESC
 LIMIT ?`

	rows, err := r.db.QueryContext(ctx, q, start, end, r.cfg.ScannerNoiseFloor, r.cfg.ScannerCandidateCap)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []ScannerCandidateRow
	for rows.Next() {
		var row ScannerCandidateRow
		if err := rows.Scan(&row.Host, &row.IP, &row.UniquePaths); err != nil {
			return nil, err
		}
		result = append(result, row)
	}
	return result, rows.Err()
}

// OverviewScannerEnrich enriches scanner candidates with total + peak from
// arb_host_ip_10s using a UNION ALL derived-table JOIN. Stage 2 of the scanner pattern.
// Batches candidates in chunks of cfg.ScannerEnrichBatch to keep query size manageable.
func (r *Repository) OverviewScannerEnrich(ctx context.Context, start, end int64, candidates []ScannerCandidateRow) ([]ScannerEnrichRow, error) {
	if len(candidates) == 0 {
		return nil, nil
	}

	var allResults []ScannerEnrichRow

	for batchStart := 0; batchStart < len(candidates); batchStart += r.cfg.ScannerEnrichBatch {
		batchEnd := batchStart + r.cfg.ScannerEnrichBatch
		if batchEnd > len(candidates) {
			batchEnd = len(candidates)
		}
		batch := candidates[batchStart:batchEnd]

		rows, err := r.enrichScannerBatch(ctx, start, end, batch)
		if err != nil {
			return nil, err
		}
		allResults = append(allResults, rows...)
	}

	return allResults, nil
}

// enrichScannerBatch executes a single enrichment query for a batch of candidates.
func (r *Repository) enrichScannerBatch(ctx context.Context, start, end int64, batch []ScannerCandidateRow) ([]ScannerEnrichRow, error) {
	// Build UNION ALL derived table: SELECT ? AS host, ? AS ip UNION ALL ...
	unionParts := make([]string, len(batch))
	args := make([]interface{}, 0, len(batch)*2+2)
	for i, c := range batch {
		if i == 0 {
			unionParts[i] = "SELECT ? AS host, ? AS ip"
		} else {
			unionParts[i] = "UNION ALL SELECT ?, ?"
		}
		args = append(args, c.Host, c.IP)
	}
	// Append start, end for the time range
	args = append(args, start, end)

	q := fmt.Sprintf(`
SELECT c.host, c.ip, SUM(a.total) AS total, MAX(a.total) AS peak_bucket_total
  FROM (
    %s
  ) c
  INNER JOIN arb_host_ip_10s a
    ON a.host = c.host AND a.ip = c.ip
   AND a.bucket_start >= ? AND a.bucket_start < ?
 GROUP BY c.host, c.ip`, strings.Join(unionParts, "\n    "))

	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []ScannerEnrichRow
	for rows.Next() {
		var row ScannerEnrichRow
		if err := rows.Scan(&row.Host, &row.IP, &row.Total, &row.PeakBucketTotal); err != nil {
			return nil, err
		}
		result = append(result, row)
	}
	return result, rows.Err()
}

// OverviewSuspiciousSprayers returns the top sprayer IPs by unique host count
// within [start, end). Query 1 of the two-query sprayer pattern.
func (r *Repository) OverviewSuspiciousSprayers(ctx context.Context, start, end int64, limit int) ([]SprayerRow, error) {
	const q = `
SELECT ip, SUM(total) AS total, COUNT(DISTINCT host) AS unique_hosts
  FROM arb_host_ip_10s
 WHERE bucket_start >= ? AND bucket_start < ?
 GROUP BY ip
 ORDER BY unique_hosts DESC, total DESC
 LIMIT ?`

	rows, err := r.db.QueryContext(ctx, q, start, end, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []SprayerRow
	for rows.Next() {
		var row SprayerRow
		if err := rows.Scan(&row.IP, &row.Total, &row.UniqueHosts); err != nil {
			return nil, err
		}
		result = append(result, row)
	}
	return result, rows.Err()
}

// OverviewSprayersPeak returns the peak bucket total for each of the given IPs
// within [start, end). Query 2; skip call if ips is empty.
func (r *Repository) OverviewSprayersPeak(ctx context.Context, start, end int64, ips []string) ([]SprayerPeakRow, error) {
	if len(ips) == 0 {
		return nil, nil
	}

	placeholders := make([]string, len(ips))
	args := []interface{}{start, end}
	for i, ip := range ips {
		placeholders[i] = "?"
		args = append(args, ip)
	}

	q := fmt.Sprintf(`
SELECT ip, MAX(bucket_total) AS max_bucket_total
  FROM (
    SELECT ip, bucket_start, SUM(total) AS bucket_total
      FROM arb_host_ip_10s
     WHERE bucket_start >= ? AND bucket_start < ?
       AND ip IN (%s)
     GROUP BY ip, bucket_start
  ) sub
 GROUP BY ip`, strings.Join(placeholders, ","))

	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []SprayerPeakRow
	for rows.Next() {
		var row SprayerPeakRow
		if err := rows.Scan(&row.IP, &row.MaxBucketTotal); err != nil {
			return nil, err
		}
		result = append(result, row)
	}
	return result, rows.Err()
}
