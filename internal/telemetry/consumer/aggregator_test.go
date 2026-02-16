package consumer

import (
	"fmt"
	"testing"
)

// ---------------------------------------------------------------------------
// BucketStart (10-second rounding)
// ---------------------------------------------------------------------------

func TestBucketRounding(t *testing.T) {
	tests := []struct {
		name    string
		tsMs    int64
		wantSec int64
	}{
		{"exact 10s boundary", 1700000000_000, 1700000000},
		{"1ms after boundary", 1700000000_001, 1700000000},
		{"9.999s after boundary", 1700000009_999, 1700000000},
		{"next boundary", 1700000010_000, 1700000010},
		{"mid-bucket", 1700000005_500, 1700000000},
		{"zero", 0, 0},
		{"sub-second", 500, 0},
		{"large value", 1739664000_000, 1739664000}, // 2026-02-16 00:00:00 UTC
		{"large value +7s", 1739664007_000, 1739664000},
		{"large value +10s", 1739664010_000, 1739664010},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BucketStart(tt.tsMs)
			if got != tt.wantSec {
				t.Errorf("BucketStart(%d) = %d, want %d", tt.tsMs, got, tt.wantSec)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Counter aggregation
// ---------------------------------------------------------------------------

func TestAggregateCounters(t *testing.T) {
	agg := NewAggregator(50)

	bucket := int64(1700000000)
	host := "example.com"
	ip := "1.2.3.4"

	// Add several events with different statuses and methods
	agg.Add(bucket, host, ip, "GET", "/api/v1/users", 200)
	agg.Add(bucket, host, ip, "GET", "/api/v1/users", 200)
	agg.Add(bucket, host, ip, "POST", "/api/v1/users", 401)
	agg.Add(bucket, host, ip, "PUT", "/api/v1/users/:id", 403)
	agg.Add(bucket, host, ip, "GET", "/api/v1/items", 404)
	agg.Add(bucket, host, ip, "DELETE", "/api/v1/users/:id", 429)
	agg.Add(bucket, host, ip, "PATCH", "/api/v1/users/:id", 500)
	agg.Add(bucket, host, ip, "GET", "/api/v1/health", 502)

	// Check host+IP level
	hipKey := HostIPKey{BucketStart: bucket, Host: host, IP: ip}
	counters := agg.hostIP[hipKey]
	if counters == nil {
		t.Fatal("expected host+IP counters to exist")
	}

	if counters.Total != 8 {
		t.Errorf("total = %d, want 8", counters.Total)
	}
	if counters.C401 != 1 {
		t.Errorf("c_401 = %d, want 1", counters.C401)
	}
	if counters.C403 != 1 {
		t.Errorf("c_403 = %d, want 1", counters.C403)
	}
	if counters.C404 != 1 {
		t.Errorf("c_404 = %d, want 1", counters.C404)
	}
	if counters.C429 != 1 {
		t.Errorf("c_429 = %d, want 1", counters.C429)
	}
	if counters.C5xx != 2 {
		t.Errorf("c_5xx = %d, want 2", counters.C5xx)
	}
	if counters.MGet != 4 {
		t.Errorf("m_get = %d, want 4", counters.MGet)
	}
	if counters.MPost != 1 {
		t.Errorf("m_post = %d, want 1", counters.MPost)
	}
	if counters.MPut != 1 {
		t.Errorf("m_put = %d, want 1", counters.MPut)
	}
	if counters.MPatch != 1 {
		t.Errorf("m_patch = %d, want 1", counters.MPatch)
	}
	if counters.MDelete != 1 {
		t.Errorf("m_delete = %d, want 1", counters.MDelete)
	}
}

func TestAggregateCounters_MultipleBucketsAndIPs(t *testing.T) {
	agg := NewAggregator(50)

	agg.Add(1700000000, "a.com", "1.1.1.1", "GET", "/", 200)
	agg.Add(1700000000, "a.com", "2.2.2.2", "GET", "/", 200)
	agg.Add(1700000010, "a.com", "1.1.1.1", "GET", "/", 200)

	if len(agg.hostIP) != 3 {
		t.Errorf("expected 3 host+IP entries, got %d", len(agg.hostIP))
	}

	// Each should have total=1
	for k, v := range agg.hostIP {
		if v.Total != 1 {
			t.Errorf("key %+v: total = %d, want 1", k, v.Total)
		}
	}
}

// ---------------------------------------------------------------------------
// Path cap enforcement
// ---------------------------------------------------------------------------

func TestPathCapEnforcement(t *testing.T) {
	cap := 5
	agg := NewAggregator(cap)

	bucket := int64(1700000000)
	host := "example.com"
	ip := "10.0.0.1"

	// Add exactly cap distinct paths — all should be tracked
	for i := 0; i < cap; i++ {
		path := fmt.Sprintf("/path/%d", i)
		agg.Add(bucket, host, ip, "GET", path, 200)
	}

	if len(agg.hostIPPath) != cap {
		t.Errorf("expected %d path entries, got %d", cap, len(agg.hostIPPath))
	}
	if agg.DroppedPaths != 0 {
		t.Errorf("expected 0 dropped paths, got %d", agg.DroppedPaths)
	}

	// Add one more distinct path — should be dropped
	agg.Add(bucket, host, ip, "GET", "/path/overflow", 404)

	if len(agg.hostIPPath) != cap {
		t.Errorf("expected still %d path entries after overflow, got %d", cap, len(agg.hostIPPath))
	}
	if agg.DroppedPaths != 1 {
		t.Errorf("expected 1 dropped path, got %d", agg.DroppedPaths)
	}

	// Host+IP level should still have counted ALL events (cap+1)
	hipKey := HostIPKey{BucketStart: bucket, Host: host, IP: ip}
	if agg.hostIP[hipKey].Total != cap+1 {
		t.Errorf("host+IP total = %d, want %d", agg.hostIP[hipKey].Total, cap+1)
	}

	// Add more to an existing tracked path — should still work (not capped)
	agg.Add(bucket, host, ip, "GET", "/path/0", 200)

	ph := PathHash("/path/0")
	pathKey := HostIPPathKey{BucketStart: bucket, Host: host, IP: ip, PathHash: ph}
	if agg.hostIPPath[pathKey].Total != 2 {
		t.Errorf("existing path total = %d, want 2", agg.hostIPPath[pathKey].Total)
	}
	// DroppedPaths should not have increased
	if agg.DroppedPaths != 1 {
		t.Errorf("dropped paths should still be 1, got %d", agg.DroppedPaths)
	}
}

func TestPathCapEnforcement_DifferentBucketsIndependent(t *testing.T) {
	agg := NewAggregator(2)

	// Fill bucket A to cap
	agg.Add(1700000000, "a.com", "1.1.1.1", "GET", "/p1", 200)
	agg.Add(1700000000, "a.com", "1.1.1.1", "GET", "/p2", 200)
	// Bucket A overflow
	agg.Add(1700000000, "a.com", "1.1.1.1", "GET", "/p3", 200)

	// Bucket B should be independent
	agg.Add(1700000010, "a.com", "1.1.1.1", "GET", "/p3", 200)

	if agg.DroppedPaths != 1 {
		t.Errorf("dropped paths = %d, want 1", agg.DroppedPaths)
	}

	// Bucket B should have 1 path entry
	ph := PathHash("/p3")
	pathKey := HostIPPathKey{BucketStart: 1700000010, Host: "a.com", IP: "1.1.1.1", PathHash: ph}
	if agg.hostIPPath[pathKey] == nil {
		t.Error("expected bucket B to track /p3")
	}
}

// ---------------------------------------------------------------------------
// Path hash determinism
// ---------------------------------------------------------------------------

func TestPathHashDeterminism(t *testing.T) {
	path := "/api/v1/users/:id/posts"

	h1 := PathHash(path)
	h2 := PathHash(path)
	if h1 != h2 {
		t.Errorf("PathHash not deterministic: %x != %x", h1, h2)
	}

	// Different paths produce different hashes
	h3 := PathHash("/api/v1/items")
	if h1 == h3 {
		t.Error("different paths produced the same hash (extremely unlikely MD5 collision)")
	}
}

func TestPathHash_EmptyPath(t *testing.T) {
	h1 := PathHash("")
	h2 := PathHash("/")
	if h1 == h2 {
		t.Error("empty string and '/' produced the same hash")
	}
}

// ---------------------------------------------------------------------------
// Snapshot + Reset
// ---------------------------------------------------------------------------

func TestSnapshot_ResetsAggregator(t *testing.T) {
	agg := NewAggregator(50)

	agg.Add(1700000000, "a.com", "1.1.1.1", "GET", "/", 200)
	agg.TrackID("12345-0")
	agg.DroppedMalformed = 3

	snap := agg.Snapshot()

	// Snapshot has the data
	if len(snap.HostIP) != 1 {
		t.Errorf("snapshot hostIP len = %d, want 1", len(snap.HostIP))
	}
	if len(snap.PendingIDs) != 1 {
		t.Errorf("snapshot pendingIDs len = %d, want 1", len(snap.PendingIDs))
	}
	if snap.DroppedMalformed != 3 {
		t.Errorf("snapshot droppedMalformed = %d, want 3", snap.DroppedMalformed)
	}

	// Aggregator is reset
	if len(agg.hostIP) != 0 {
		t.Errorf("aggregator hostIP should be empty after snapshot, got %d", len(agg.hostIP))
	}
	if len(agg.hostIPPath) != 0 {
		t.Errorf("aggregator hostIPPath should be empty after snapshot, got %d", len(agg.hostIPPath))
	}
	if agg.PendingCount() != 0 {
		t.Errorf("aggregator pendingIDs should be empty after snapshot, got %d", agg.PendingCount())
	}
	if agg.DroppedMalformed != 0 {
		t.Errorf("aggregator droppedMalformed should be 0 after snapshot, got %d", agg.DroppedMalformed)
	}
}

func TestSize(t *testing.T) {
	agg := NewAggregator(50)
	if agg.Size() != 0 {
		t.Errorf("empty aggregator size = %d, want 0", agg.Size())
	}

	agg.Add(1700000000, "a.com", "1.1.1.1", "GET", "/", 200)
	// 1 host+IP entry + 1 path entry = 2
	if agg.Size() != 2 {
		t.Errorf("size = %d, want 2", agg.Size())
	}
}
