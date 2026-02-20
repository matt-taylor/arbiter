package query

// IPRow represents a single row in the "top IPs" response.
type IPRow struct {
	IP    string `json:"ip"`
	Total int64  `json:"total"`
	C401  int64  `json:"c_401"`
	C403  int64  `json:"c_403"`
	C404  int64  `json:"c_404"`
	C429  int64  `json:"c_429"`
	C5xx  int64  `json:"c_5xx"`
}

// PathRow represents a single row in the "top paths" response.
type PathRow struct {
	Path  string `json:"path"`
	Total int64  `json:"total"`
	C401  int64  `json:"c_401"`
	C403  int64  `json:"c_403"`
	C404  int64  `json:"c_404"`
	C429  int64  `json:"c_429"`
	C5xx  int64  `json:"c_5xx"`
}

// SummaryRow represents the aggregate summary for a host.
type SummaryRow struct {
	Total     int64 `json:"total"`
	C401      int64 `json:"c_401"`
	C403      int64 `json:"c_403"`
	C404      int64 `json:"c_404"`
	C429      int64 `json:"c_429"`
	C5xx      int64 `json:"c_5xx"`
	UniqueIPs int64 `json:"unique_ips"`
}

// ── Overview DB-level rows (no derived metrics) ─────────────────────────

// OverviewHostRow is returned by the top-hosts aggregate query.
type OverviewHostRow struct {
	Host      string
	Total     int64
	UniqueIPs int64
}

// HostPeakRow is returned by the top-hosts peak-bucket query.
type HostPeakRow struct {
	Host           string
	MaxBucketTotal int64 // MAX of per-bucket SUM(total) across IPs
}

// ScannerCandidateRow is returned by the scanner Stage 1 candidate query.
type ScannerCandidateRow struct {
	Host        string
	IP          string
	UniquePaths int64
}

// ScannerEnrichRow is returned by the scanner Stage 2 enrichment query.
type ScannerEnrichRow struct {
	Host            string
	IP              string
	Total           int64
	PeakBucketTotal int64 // MAX(total) per 10s bucket for this host+ip
}

// SprayerRow is returned by the sprayer aggregate query.
type SprayerRow struct {
	IP          string
	Total       int64
	UniqueHosts int64
}

// SprayerPeakRow is returned by the sprayer peak-bucket query.
type SprayerPeakRow struct {
	IP             string
	MaxBucketTotal int64 // MAX of per-bucket SUM(total) across hosts
}

// ── Overview API-level response items ───────────────────────────────────

// TopHostItem is an element in the top-hosts response items array.
type TopHostItem struct {
	Host      string  `json:"host"`
	Total     int64   `json:"total"`
	UniqueIPs int64   `json:"unique_ips"`
	AvgRPS    float64 `json:"avg_rps"`
	PeakRPS   float64 `json:"peak_rps"`
}

// SuspiciousScannerItem is an element in the suspicious-scanners response items array.
type SuspiciousScannerItem struct {
	Host        string   `json:"host"`
	IP          string   `json:"ip"`
	UniquePaths int64    `json:"unique_paths"`
	Total       int64    `json:"total"`
	AvgRPS      float64  `json:"avg_rps"`
	PeakRPS     float64  `json:"peak_rps"`
	Burstiness  float64  `json:"burstiness"`
	Reasons     []string `json:"reasons"`
}

// SuspiciousSprayerItem is an element in the suspicious-sprayers response items array.
type SuspiciousSprayerItem struct {
	IP          string   `json:"ip"`
	UniqueHosts int64    `json:"unique_hosts"`
	Total       int64    `json:"total"`
	AvgRPS      float64  `json:"avg_rps"`
	PeakRPS     float64  `json:"peak_rps"`
	Burstiness  float64  `json:"burstiness"`
	Reasons     []string `json:"reasons"`
}

// ── Flooder DB-level rows ───────────────────────────────────────────────

// FlooderCandidateRow is returned by the flooder Stage 1 candidate query.
// Each row is a (host, ip, path) triple with high total requests to that path.
type FlooderCandidateRow struct {
	Host  string
	IP    string
	Path  string
	Total int64
}

// FlooderEnrichRow is returned by the flooder Stage 2 enrichment query.
// It provides the unique path count and peak bucket total for a (host, ip) pair.
type FlooderEnrichRow struct {
	Host            string
	IP              string
	UniquePaths     int64
	PeakBucketTotal int64 // MAX(total) per 10s bucket for this host+ip
}

// ── Flooder API-level response item ─────────────────────────────────────

// SuspiciousFlooderItem is an element in the suspicious-flooders response items array.
type SuspiciousFlooderItem struct {
	Host        string   `json:"host"`
	IP          string   `json:"ip"`
	Path        string   `json:"path"`
	UniquePaths int64    `json:"unique_paths"`
	Total       int64    `json:"total"`
	AvgRPS      float64  `json:"avg_rps"`
	PeakRPS     float64  `json:"peak_rps"`
	Burstiness  float64  `json:"burstiness"`
	Reasons     []string `json:"reasons"`
}
