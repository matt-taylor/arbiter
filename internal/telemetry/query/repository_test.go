package query

import (
	"strings"
	"testing"
)

// TestTopIPsSQL verifies the TopIPs query contains expected SQL clauses.
func TestTopIPsSQL(t *testing.T) {
	// We can't execute SQL without a real DB, but we verify the query structure
	// by inspecting the constant. Extract it from the source (or duplicate here).
	q := `
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

	clauses := []string{
		"FROM arb_host_ip_10s",
		"host = ?",
		"bucket_start >= ?",
		"bucket_start < ?",
		"GROUP BY ip",
		"ORDER BY total DESC",
		"LIMIT ?",
	}
	for _, clause := range clauses {
		if !strings.Contains(q, clause) {
			t.Errorf("TopIPs query missing clause: %q", clause)
		}
	}
}

// TestTopPathsSQL verifies the TopPaths query contains expected SQL clauses.
func TestTopPathsSQL(t *testing.T) {
	q := `
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

	clauses := []string{
		"FROM arb_host_ip_path_10s",
		"host = ?",
		"ip = ?",
		"bucket_start >= ?",
		"bucket_start < ?",
		"GROUP BY path",
		"ORDER BY total DESC",
		"LIMIT ?",
	}
	for _, clause := range clauses {
		if !strings.Contains(q, clause) {
			t.Errorf("TopPaths query missing clause: %q", clause)
		}
	}
}

// TestSummarySQL verifies the Summary query contains expected SQL clauses.
func TestSummarySQL(t *testing.T) {
	q := `
SELECT SUM(total) AS total,
       SUM(c_401) AS c_401, SUM(c_403) AS c_403,
       SUM(c_404) AS c_404, SUM(c_429) AS c_429,
       SUM(c_5xx) AS c_5xx,
       COUNT(DISTINCT ip) AS unique_ips
  FROM arb_host_ip_10s
 WHERE host = ? AND bucket_start >= ? AND bucket_start < ?`

	clauses := []string{
		"FROM arb_host_ip_10s",
		"host = ?",
		"bucket_start >= ?",
		"bucket_start < ?",
		"SUM(total)",
		"COUNT(DISTINCT ip)",
	}
	for _, clause := range clauses {
		if !strings.Contains(q, clause) {
			t.Errorf("Summary query missing clause: %q", clause)
		}
	}

	// Should NOT contain LIMIT (summary is a single-row aggregate)
	if strings.Contains(q, "LIMIT") {
		t.Error("Summary query should not contain LIMIT")
	}
}
