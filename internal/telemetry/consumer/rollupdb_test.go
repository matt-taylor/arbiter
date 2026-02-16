package consumer

import (
	"fmt"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Host+IP upsert SQL
// ---------------------------------------------------------------------------

func TestBuildUpsertSQL_HostIP(t *testing.T) {
	rows := map[HostIPKey]*HostIPCounters{
		{BucketStart: 1700000000, Host: "example.com", IP: "1.2.3.4"}: {
			Total: 10, C401: 1, C403: 2, C404: 0, C429: 3, C5xx: 1,
			MGet: 5, MPost: 2, MPut: 1, MPatch: 1, MDelete: 1,
		},
	}

	batches := BuildHostIPBatchesForTest(rows)
	if len(batches) != 1 {
		t.Fatalf("expected 1 batch, got %d", len(batches))
	}

	b := batches[0]

	// Check SQL structure
	if !strings.HasPrefix(b.SQL, "INSERT INTO arb_host_ip_10s") {
		t.Errorf("SQL should start with INSERT INTO arb_host_ip_10s, got:\n%s", b.SQL)
	}
	if !strings.Contains(b.SQL, "ON DUPLICATE KEY UPDATE") {
		t.Error("SQL should contain ON DUPLICATE KEY UPDATE")
	}
	if !strings.Contains(b.SQL, "total = total + VALUES(total)") {
		t.Error("SQL should contain additive total update")
	}
	if !strings.Contains(b.SQL, "c_401 = c_401 + VALUES(c_401)") {
		t.Error("SQL should contain additive c_401 update")
	}
	if !strings.Contains(b.SQL, "m_get = m_get + VALUES(m_get)") {
		t.Error("SQL should contain additive m_get update")
	}
	if !strings.Contains(b.SQL, "m_delete = m_delete + VALUES(m_delete)") {
		t.Error("SQL should contain additive m_delete update")
	}

	// Check placeholder count: 14 per row, 1 row
	placeholderCount := strings.Count(b.SQL, "?")
	if placeholderCount != 14 {
		t.Errorf("expected 14 placeholders, got %d", placeholderCount)
	}

	// Check args count
	if len(b.Args) != 14 {
		t.Errorf("expected 14 args, got %d", len(b.Args))
	}

	// Verify arg values
	if b.Args[0] != int64(1700000000) {
		t.Errorf("arg[0] (bucket_start) = %v, want 1700000000", b.Args[0])
	}
	if b.Args[1] != "example.com" {
		t.Errorf("arg[1] (host) = %v, want example.com", b.Args[1])
	}
	if b.Args[2] != "1.2.3.4" {
		t.Errorf("arg[2] (ip) = %v, want 1.2.3.4", b.Args[2])
	}
	if b.Args[3] != 10 {
		t.Errorf("arg[3] (total) = %v, want 10", b.Args[3])
	}
}

func TestBuildUpsertSQL_HostIP_MultipleRows(t *testing.T) {
	rows := map[HostIPKey]*HostIPCounters{
		{BucketStart: 1700000000, Host: "a.com", IP: "1.1.1.1"}: {Total: 5},
		{BucketStart: 1700000000, Host: "b.com", IP: "2.2.2.2"}: {Total: 3},
	}

	batches := BuildHostIPBatchesForTest(rows)
	if len(batches) != 1 {
		t.Fatalf("expected 1 batch, got %d", len(batches))
	}

	b := batches[0]
	// 2 rows × 14 args = 28
	if len(b.Args) != 28 {
		t.Errorf("expected 28 args for 2 rows, got %d", len(b.Args))
	}

	// Should have exactly 2 value tuples
	placeholderCount := strings.Count(b.SQL, "?")
	if placeholderCount != 28 {
		t.Errorf("expected 28 placeholders, got %d", placeholderCount)
	}
}

// ---------------------------------------------------------------------------
// Host+IP+Path upsert SQL
// ---------------------------------------------------------------------------

func TestBuildUpsertSQL_HostIPPath(t *testing.T) {
	ph := PathHash("/api/v1/users")
	rows := map[HostIPPathKey]*PathCounters{
		{BucketStart: 1700000000, Host: "example.com", IP: "1.2.3.4", PathHash: ph}: {
			Path: "/api/v1/users", Total: 7, C401: 0, C403: 1, C404: 2, C429: 0, C5xx: 0,
		},
	}

	batches := BuildHostIPPathBatchesForTest(rows)
	if len(batches) != 1 {
		t.Fatalf("expected 1 batch, got %d", len(batches))
	}

	b := batches[0]

	if !strings.HasPrefix(b.SQL, "INSERT INTO arb_host_ip_path_10s") {
		t.Errorf("SQL should start with INSERT INTO arb_host_ip_path_10s, got:\n%s", b.SQL)
	}
	if !strings.Contains(b.SQL, "path_hash") {
		t.Error("SQL should reference path_hash column")
	}
	if !strings.Contains(b.SQL, "ON DUPLICATE KEY UPDATE") {
		t.Error("SQL should contain ON DUPLICATE KEY UPDATE")
	}

	// 11 columns per row
	if len(b.Args) != 11 {
		t.Errorf("expected 11 args, got %d", len(b.Args))
	}

	// Verify path_hash is passed as []byte
	hashArg, ok := b.Args[3].([]byte)
	if !ok {
		t.Fatalf("arg[3] (path_hash) should be []byte, got %T", b.Args[3])
	}
	if len(hashArg) != 16 {
		t.Errorf("path_hash should be 16 bytes, got %d", len(hashArg))
	}

	// Verify path value
	if b.Args[4] != "/api/v1/users" {
		t.Errorf("arg[4] (path) = %v, want /api/v1/users", b.Args[4])
	}
}

// ---------------------------------------------------------------------------
// Batch splitting
// ---------------------------------------------------------------------------

func TestBuildUpsertSQL_BatchSplit(t *testing.T) {
	// Generate more rows than maxRowsPerInsert (500)
	rowCount := maxRowsPerInsert + 100
	rows := make(map[HostIPKey]*HostIPCounters, rowCount)
	for i := 0; i < rowCount; i++ {
		rows[HostIPKey{
			BucketStart: 1700000000,
			Host:        "example.com",
			IP:          fmt.Sprintf("10.0.%d.%d", i/256, i%256),
		}] = &HostIPCounters{Total: 1}
	}

	batches := BuildHostIPBatchesForTest(rows)
	if len(batches) != 2 {
		t.Fatalf("expected 2 batches for %d rows, got %d", rowCount, len(batches))
	}

	// First batch should have maxRowsPerInsert * 14 args
	if len(batches[0].Args) != maxRowsPerInsert*14 {
		t.Errorf("batch 0 args = %d, want %d", len(batches[0].Args), maxRowsPerInsert*14)
	}

	// Second batch should have remaining rows * 14 args
	remaining := rowCount - maxRowsPerInsert
	if len(batches[1].Args) != remaining*14 {
		t.Errorf("batch 1 args = %d, want %d", len(batches[1].Args), remaining*14)
	}
}

func TestBuildUpsertSQL_BatchSplit_Path(t *testing.T) {
	// Same test for path table
	rowCount := maxRowsPerInsert + 50
	rows := make(map[HostIPPathKey]*PathCounters, rowCount)
	for i := 0; i < rowCount; i++ {
		path := fmt.Sprintf("/path/%d", i)
		rows[HostIPPathKey{
			BucketStart: 1700000000,
			Host:        "example.com",
			IP:          "1.2.3.4",
			PathHash:    PathHash(path),
		}] = &PathCounters{Path: path, Total: 1}
	}

	batches := BuildHostIPPathBatchesForTest(rows)
	if len(batches) != 2 {
		t.Fatalf("expected 2 batches for %d rows, got %d", rowCount, len(batches))
	}

	if len(batches[0].Args) != maxRowsPerInsert*11 {
		t.Errorf("batch 0 args = %d, want %d", len(batches[0].Args), maxRowsPerInsert*11)
	}
}

// ---------------------------------------------------------------------------
// Empty map produces no batches
// ---------------------------------------------------------------------------

func TestBuildUpsertSQL_Empty(t *testing.T) {
	batches := BuildHostIPBatchesForTest(map[HostIPKey]*HostIPCounters{})
	if len(batches) != 0 {
		t.Errorf("expected 0 batches for empty map, got %d", len(batches))
	}

	pathBatches := BuildHostIPPathBatchesForTest(map[HostIPPathKey]*PathCounters{})
	if len(pathBatches) != 0 {
		t.Errorf("expected 0 batches for empty path map, got %d", len(pathBatches))
	}
}
