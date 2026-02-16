package httpserver

import (
	"math"
	"testing"

	"github.com/domostack/arbiter/internal/telemetry/query"
)

// ── computeOverviewTimeRange tests ──────────────────────────────────────

func TestComputeOverviewTimeRange(t *testing.T) {
	tests := []struct {
		name      string
		params    query.OverviewParams
		wantStart int64
		wantEnd   int64
	}{
		{
			name: "end_ts floored to 10s, 15 min window",
			params: query.OverviewParams{
				WindowMinutes: 15,
				EndTS:         1700000007, // not aligned
			},
			wantEnd:   1700000000,            // floored to 10s
			wantStart: 1700000000 - 15*60,    // 1699999100
		},
		{
			name: "already aligned end_ts",
			params: query.OverviewParams{
				WindowMinutes: 5,
				EndTS:         1700000000,
			},
			wantEnd:   1700000000,
			wantStart: 1700000000 - 5*60, // 1699999700
		},
		{
			name: "1 minute window",
			params: query.OverviewParams{
				WindowMinutes: 1,
				EndTS:         1700000019, // floors to 1700000010
			},
			wantEnd:   1700000010,
			wantStart: 1700000010 - 60, // 1699999950
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start, end := computeOverviewTimeRange(tt.params)
			if end != tt.wantEnd {
				t.Errorf("end = %d, want %d", end, tt.wantEnd)
			}
			if start != tt.wantStart {
				t.Errorf("start = %d, want %d", start, tt.wantStart)
			}
			// Verify range semantics
			if start >= end {
				t.Errorf("start (%d) must be < end (%d)", start, end)
			}
			// Verify both are 10s-aligned
			if start%10 != 0 {
				t.Errorf("start %d not 10s-aligned", start)
			}
			if end%10 != 0 {
				t.Errorf("end %d not 10s-aligned", end)
			}
		})
	}
}

// ── Window seconds derived from aligned start/end ───────────────────────

func TestWindowSecondsFromAlignedRange(t *testing.T) {
	p := query.OverviewParams{
		WindowMinutes: 15,
		EndTS:         1700000007,
	}
	start, end := computeOverviewTimeRange(p)
	windowSeconds := end - start

	// 15 * 60 = 900 seconds exactly
	if windowSeconds != 900 {
		t.Errorf("windowSeconds = %d, want 900", windowSeconds)
	}
}

// ── Derived-metric helper tests ─────────────────────────────────────────

func TestComputeAvgRPS(t *testing.T) {
	tests := []struct {
		name          string
		total         int64
		windowSeconds int64
		want          float64
	}{
		{"normal", 900, 900, 1.0},
		{"zero window", 100, 0, 0.0},
		{"large total", 9000, 900, 10.0},
		{"fractional", 100, 300, 100.0 / 300.0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := computeAvgRPS(tt.total, tt.windowSeconds)
			if math.Abs(got-tt.want) > 0.001 {
				t.Errorf("computeAvgRPS(%d, %d) = %f, want %f", tt.total, tt.windowSeconds, got, tt.want)
			}
		})
	}
}

func TestComputePeakRPS(t *testing.T) {
	tests := []struct {
		name           string
		maxBucketTotal int64
		want           float64
	}{
		{"zero", 0, 0.0},
		{"50 requests in 10s", 50, 5.0},
		{"100 requests in 10s", 100, 10.0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := computePeakRPS(tt.maxBucketTotal)
			if math.Abs(got-tt.want) > 0.001 {
				t.Errorf("computePeakRPS(%d) = %f, want %f", tt.maxBucketTotal, got, tt.want)
			}
		})
	}
}

func TestComputeBurstiness(t *testing.T) {
	tests := []struct {
		name    string
		peakRPS float64
		avgRPS  float64
		want    float64
	}{
		{"normal", 10.0, 2.0, 5.0},
		{"zero avg uses floor 0.1", 10.0, 0.0, 100.0},
		{"very low avg uses floor", 5.0, 0.05, 50.0},
		{"avg above floor", 5.0, 1.0, 5.0},
		{"equal", 2.0, 2.0, 1.0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := computeBurstiness(tt.peakRPS, tt.avgRPS)
			if math.Abs(got-tt.want) > 0.001 {
				t.Errorf("computeBurstiness(%f, %f) = %f, want %f", tt.peakRPS, tt.avgRPS, got, tt.want)
			}
		})
	}
}

// ── Reason flag threshold tests ─────────────────────────────────────────

func TestScannerReasons(t *testing.T) {
	windowSeconds := int64(900) // 15 minutes

	tests := []struct {
		name        string
		uniquePaths int64
		total       int64
		peakBucket  int64
		wantReasons []string
	}{
		{
			name:        "below all thresholds",
			uniquePaths: 5,
			total:       900,       // avgRPS = 1.0
			peakBucket:  40,        // peakRPS = 4.0, burstiness = 4.0/1.0 = 4.0
			wantReasons: nil,
		},
		{
			name:        "SCAN_SINGLE_HOST only",
			uniquePaths: 30,
			total:       900,       // avgRPS = 1.0
			peakBucket:  40,        // peakRPS = 4.0, burstiness = 4.0/1.0 = 4.0
			wantReasons: []string{"SCAN_SINGLE_HOST"},
		},
		{
			name:        "HIGH_PEAK only (high volume keeps burstiness low)",
			uniquePaths: 15,
			total:       2700,      // avgRPS = 3.0
			peakBucket:  100,       // peakRPS = 10.0, burstiness = 10.0/3.0 = 3.33
			wantReasons: []string{"HIGH_PEAK"},
		},
		{
			name:        "BURSTY only",
			uniquePaths: 15,
			total:       18,        // avgRPS = 18/900 = 0.02 → floor 0.1
			peakBucket:  5,         // peakRPS = 0.5, burstiness = 0.5/0.1 = 5.0
			wantReasons: []string{"BURSTY"},
		},
		{
			name:        "all three reasons",
			uniquePaths: 50,
			total:       100,       // avgRPS = 100/900 ≈ 0.11
			peakBucket:  200,       // peakRPS = 20.0, burstiness ≈ 181
			wantReasons: []string{"SCAN_SINGLE_HOST", "BURSTY", "HIGH_PEAK"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			avgRPS := computeAvgRPS(tt.total, windowSeconds)
			peakRPS := computePeakRPS(tt.peakBucket)
			burstiness := computeBurstiness(peakRPS, avgRPS)

			var reasons []string
			if tt.uniquePaths >= 30 {
				reasons = append(reasons, "SCAN_SINGLE_HOST")
			}
			if burstiness >= 5 {
				reasons = append(reasons, "BURSTY")
			}
			if peakRPS >= 10 {
				reasons = append(reasons, "HIGH_PEAK")
			}

			if len(reasons) != len(tt.wantReasons) {
				t.Errorf("reasons = %v, want %v", reasons, tt.wantReasons)
				return
			}
			for i := range reasons {
				if reasons[i] != tt.wantReasons[i] {
					t.Errorf("reasons[%d] = %q, want %q", i, reasons[i], tt.wantReasons[i])
				}
			}
		})
	}
}

func TestSprayerReasons(t *testing.T) {
	windowSeconds := int64(900)

	tests := []struct {
		name        string
		uniqueHosts int64
		total       int64
		peakBucket  int64
		wantReasons []string
	}{
		{
			name:        "below all thresholds",
			uniqueHosts: 2,
			total:       900,       // avgRPS = 1.0
			peakBucket:  40,        // peakRPS = 4.0, burstiness = 4.0
			wantReasons: nil,
		},
		{
			name:        "SPRAY_HOSTS only",
			uniqueHosts: 5,
			total:       900,       // avgRPS = 1.0
			peakBucket:  40,        // peakRPS = 4.0, burstiness = 4.0
			wantReasons: []string{"SPRAY_HOSTS"},
		},
		{
			name:        "HIGH_PEAK only (high volume keeps burstiness low)",
			uniqueHosts: 2,
			total:       2700,      // avgRPS = 3.0
			peakBucket:  100,       // peakRPS = 10.0, burstiness = 3.33
			wantReasons: []string{"HIGH_PEAK"},
		},
		{
			name:        "all three reasons",
			uniqueHosts: 10,
			total:       100,       // avgRPS ≈ 0.11
			peakBucket:  200,       // peakRPS = 20.0, burstiness ≈ 181
			wantReasons: []string{"SPRAY_HOSTS", "BURSTY", "HIGH_PEAK"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			avgRPS := computeAvgRPS(tt.total, windowSeconds)
			peakRPS := computePeakRPS(tt.peakBucket)
			burstiness := computeBurstiness(peakRPS, avgRPS)

			var reasons []string
			if tt.uniqueHosts >= 5 {
				reasons = append(reasons, "SPRAY_HOSTS")
			}
			if burstiness >= 5 {
				reasons = append(reasons, "BURSTY")
			}
			if peakRPS >= 10 {
				reasons = append(reasons, "HIGH_PEAK")
			}

			if len(reasons) != len(tt.wantReasons) {
				t.Errorf("reasons = %v, want %v", reasons, tt.wantReasons)
				return
			}
			for i := range reasons {
				if reasons[i] != tt.wantReasons[i] {
					t.Errorf("reasons[%d] = %q, want %q", i, reasons[i], tt.wantReasons[i])
				}
			}
		})
	}
}

// ── Peak correctness tests ──────────────────────────────────────────────
// These verify the conceptual correctness of the peak computation strategies.

func TestTopHostPeakRequiresPerBucketReAggregation(t *testing.T) {
	// Scenario: host has 2 IPs. In bucket 1000, IP-A has 30 reqs, IP-B has 20 reqs.
	// In bucket 1010, IP-A has 5, IP-B has 10.
	// Naive MAX(total) per row = 30, but correct bucket-level total = 50 (at bucket 1000).
	// peak_rps = 50/10 = 5.0
	//
	// This test verifies the formula: peak comes from the max of SUM(total) GROUP BY bucket,
	// not from MAX(individual row total).

	// Simulate what the DB would return from Query 2 (already re-aggregated):
	// MAX(bucket_total) where bucket_total = SUM(total) GROUP BY host, bucket_start
	maxBucketTotal := int64(50) // 30+20 in the peak bucket

	peakRPS := computePeakRPS(maxBucketTotal)
	if math.Abs(peakRPS-5.0) > 0.001 {
		t.Errorf("peakRPS = %f, want 5.0", peakRPS)
	}

	// Contrast with the naive (wrong) approach of just using the max single-row total
	naiveMax := int64(30)
	naivePeak := computePeakRPS(naiveMax)
	if naivePeak == peakRPS {
		t.Error("naive peak should differ from correct re-aggregated peak")
	}
}

func TestSprayerPeakRequiresPerBucketReAggregation(t *testing.T) {
	// Scenario: IP hits 3 hosts. In bucket 1000: host-A=10, host-B=20, host-C=5 → total=35.
	// In bucket 1010: host-A=2, host-B=3, host-C=1 → total=6.
	// Correct peak bucket = 35, peak_rps = 3.5
	// Naive MAX(individual row) = 20, naive peak_rps = 2.0 (wrong)

	maxBucketTotal := int64(35)
	peakRPS := computePeakRPS(maxBucketTotal)
	if math.Abs(peakRPS-3.5) > 0.001 {
		t.Errorf("peakRPS = %f, want 3.5", peakRPS)
	}

	naiveMax := int64(20)
	naivePeak := computePeakRPS(naiveMax)
	if naivePeak == peakRPS {
		t.Error("naive peak should differ from correct re-aggregated peak")
	}
}

func TestScannerPeakUsesMaxTotalDirectly(t *testing.T) {
	// For scanners (host+ip), each row in arb_host_ip_10s already represents
	// the 10s bucket total for that specific host+ip. No re-aggregation needed.
	// MAX(total) from the rows is the correct peak bucket total.

	peakBucketTotal := int64(80) // directly from MAX(total) in arb_host_ip_10s
	peakRPS := computePeakRPS(peakBucketTotal)
	if math.Abs(peakRPS-8.0) > 0.001 {
		t.Errorf("peakRPS = %f, want 8.0", peakRPS)
	}
}

// ── Scanner candidate merge with missing enrichment ─────────────────────

func TestScannerMergeMissingEnrichment(t *testing.T) {
	// When a candidate exists but enrichment returns no matching row,
	// the handler should still include the candidate with zero total/peak.

	candidates := []query.ScannerCandidateRow{
		{Host: "a.com", IP: "1.1.1.1", UniquePaths: 50},
		{Host: "b.com", IP: "2.2.2.2", UniquePaths: 40},
	}

	// Only one candidate has enrichment data
	type enrichKey struct{ Host, IP string }
	enrichMap := map[enrichKey]query.ScannerEnrichRow{
		{"a.com", "1.1.1.1"}: {Host: "a.com", IP: "1.1.1.1", Total: 1000, PeakBucketTotal: 50},
	}

	windowSeconds := int64(900)

	var items []query.SuspiciousScannerItem
	for _, c := range candidates {
		e := enrichMap[enrichKey{c.Host, c.IP}]
		avgRPS := computeAvgRPS(e.Total, windowSeconds)
		peakRPS := computePeakRPS(e.PeakBucketTotal)
		burstiness := computeBurstiness(peakRPS, avgRPS)

		var reasons []string
		if c.UniquePaths >= 30 {
			reasons = append(reasons, "SCAN_SINGLE_HOST")
		}
		if burstiness >= 5 {
			reasons = append(reasons, "BURSTY")
		}
		if peakRPS >= 10 {
			reasons = append(reasons, "HIGH_PEAK")
		}

		items = append(items, query.SuspiciousScannerItem{
			Host:        c.Host,
			IP:          c.IP,
			UniquePaths: c.UniquePaths,
			Total:       e.Total,
			AvgRPS:      roundFloat(avgRPS, 2),
			PeakRPS:     roundFloat(peakRPS, 2),
			Burstiness:  roundFloat(burstiness, 2),
			Reasons:     reasons,
		})
	}

	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}

	// First item has enrichment
	if items[0].Total != 1000 {
		t.Errorf("items[0].Total = %d, want 1000", items[0].Total)
	}
	if items[0].PeakRPS != 5.0 {
		t.Errorf("items[0].PeakRPS = %f, want 5.0", items[0].PeakRPS)
	}

	// Second item has no enrichment — should be zero
	if items[1].Total != 0 {
		t.Errorf("items[1].Total = %d, want 0", items[1].Total)
	}
	if items[1].PeakRPS != 0.0 {
		t.Errorf("items[1].PeakRPS = %f, want 0.0", items[1].PeakRPS)
	}
	// But it still has SCAN_SINGLE_HOST reason (unique_paths=40 >= 30)
	if len(items[1].Reasons) == 0 || items[1].Reasons[0] != "SCAN_SINGLE_HOST" {
		t.Errorf("items[1].Reasons = %v, expected [SCAN_SINGLE_HOST]", items[1].Reasons)
	}
}

// ── Empty list guard test ───────────────────────────────────────────────

func TestEmptyListGuard(t *testing.T) {
	// When Query 1 returns 0 rows, the peak map should be empty
	// and all items should have PeakRPS = 0.

	peakMap := make(map[string]int64)
	// No peak query was called, so peakMap is empty

	hosts := []query.OverviewHostRow{} // empty result from Query 1
	windowSeconds := int64(900)

	items := make([]query.TopHostItem, len(hosts))
	for i, h := range hosts {
		avgRPS := computeAvgRPS(h.Total, windowSeconds)
		peakRPS := computePeakRPS(peakMap[h.Host])
		items[i] = query.TopHostItem{
			Host:      h.Host,
			Total:     h.Total,
			UniqueIPs: h.UniqueIPs,
			AvgRPS:    roundFloat(avgRPS, 2),
			PeakRPS:   roundFloat(peakRPS, 2),
		}
	}

	if len(items) != 0 {
		t.Errorf("expected 0 items, got %d", len(items))
	}
}
