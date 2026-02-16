package consumer

import (
	"crypto/md5"
	"strings"
)

// ---------------------------------------------------------------------------
// Keys
// ---------------------------------------------------------------------------

// HostIPKey is the composite key for the arb_host_ip_10s rollup table.
type HostIPKey struct {
	BucketStart int64  // epoch seconds, floored to 10s
	Host        string // normalized host
	IP          string
}

// HostIPPathKey is the composite key for the arb_host_ip_path_10s rollup table.
type HostIPPathKey struct {
	BucketStart int64
	Host        string
	IP          string
	PathHash    [16]byte // MD5 of normalized path
}

// ---------------------------------------------------------------------------
// Counters
// ---------------------------------------------------------------------------

// HostIPCounters holds counters for a (bucket, host, ip) rollup row.
type HostIPCounters struct {
	Total   int
	C401    int
	C403    int
	C404    int
	C429    int
	C5xx    int
	MGet    int
	MPost   int
	MPut    int
	MPatch  int
	MDelete int
}

// PathCounters holds counters for a (bucket, host, ip, path) rollup row.
type PathCounters struct {
	Path  string // kept for display; uniqueness is via PathHash in the key
	Total int
	C401  int
	C403  int
	C404  int
	C429  int
	C5xx  int
}

// ---------------------------------------------------------------------------
// Aggregator
// ---------------------------------------------------------------------------

// Aggregator accumulates telemetry events into rollup buckets in memory.
// It is NOT safe for concurrent use; the consumer loop owns it single-threaded.
type Aggregator struct {
	hostIP     map[HostIPKey]*HostIPCounters
	hostIPPath map[HostIPPathKey]*PathCounters

	// pathCounts tracks the number of distinct paths per (bucket, host, ip).
	// Used to enforce the per-bucket path cap.
	pathCounts map[HostIPKey]int

	pathCap int

	// Metrics for the current flush cycle.
	DroppedPaths     int64
	DroppedMalformed int64

	// pendingIDs tracks Redis stream entry IDs to ACK after a successful flush.
	pendingIDs []string
}

// NewAggregator creates a new Aggregator with the given path cap.
func NewAggregator(pathCap int) *Aggregator {
	if pathCap <= 0 {
		pathCap = 50
	}
	return &Aggregator{
		hostIP:     make(map[HostIPKey]*HostIPCounters),
		hostIPPath: make(map[HostIPPathKey]*PathCounters),
		pathCounts: make(map[HostIPKey]int),
		pathCap:    pathCap,
	}
}

// ---------------------------------------------------------------------------
// Bucket rounding
// ---------------------------------------------------------------------------

// BucketStart floors a millisecond-epoch timestamp to the nearest 10-second boundary.
// All timestamps are treated as UTC (epoch is timezone-agnostic).
func BucketStart(tsMs int64) int64 {
	sec := tsMs / 1000
	return (sec / 10) * 10
}

// ---------------------------------------------------------------------------
// Path hashing
// ---------------------------------------------------------------------------

// PathHash computes the MD5 hash of a normalized path, returning a [16]byte
// suitable for the BINARY(16) path_hash column in MariaDB.
func PathHash(path string) [16]byte {
	return md5.Sum([]byte(path))
}

// ---------------------------------------------------------------------------
// Add event
// ---------------------------------------------------------------------------

// Add aggregates a single parsed event into the in-memory maps.
// It always updates the host+IP level row. It updates the path-level row
// only if the per-bucket path cap has not been exceeded.
func (a *Aggregator) Add(bucketStart int64, host, ip, method, path string, status int) {
	hipKey := HostIPKey{BucketStart: bucketStart, Host: host, IP: ip}

	// --- Host+IP level (always) ---
	counters := a.hostIP[hipKey]
	if counters == nil {
		counters = &HostIPCounters{}
		a.hostIP[hipKey] = counters
	}
	counters.Total++
	addStatusCounter(status, &counters.C401, &counters.C403, &counters.C404, &counters.C429, &counters.C5xx)
	addMethodCounter(method, &counters.MGet, &counters.MPost, &counters.MPut, &counters.MPatch, &counters.MDelete)

	// --- Path level (cap-guarded) ---
	pathHash := PathHash(path)
	pathKey := HostIPPathKey{BucketStart: bucketStart, Host: host, IP: ip, PathHash: pathHash}

	// Check if this path is already tracked (existing entry doesn't count against cap)
	_, pathExists := a.hostIPPath[pathKey]
	if !pathExists {
		current := a.pathCounts[hipKey]
		if current >= a.pathCap {
			a.DroppedPaths++
			return
		}
		a.pathCounts[hipKey] = current + 1
	}

	pc := a.hostIPPath[pathKey]
	if pc == nil {
		pc = &PathCounters{Path: path}
		a.hostIPPath[pathKey] = pc
	}
	pc.Total++
	addStatusCounter(status, &pc.C401, &pc.C403, &pc.C404, &pc.C429, &pc.C5xx)
}

// TrackID records a Redis stream entry ID for ACK after flush.
func (a *Aggregator) TrackID(id string) {
	a.pendingIDs = append(a.pendingIDs, id)
}

// ---------------------------------------------------------------------------
// Snapshot + Reset
// ---------------------------------------------------------------------------

// Snapshot holds the aggregated data ready for flushing to the database.
type Snapshot struct {
	HostIP     map[HostIPKey]*HostIPCounters
	HostIPPath map[HostIPPathKey]*PathCounters
	PendingIDs []string

	DroppedPaths     int64
	DroppedMalformed int64
}

// Snapshot takes a snapshot of the current aggregated data and resets the
// aggregator for the next cycle. The returned Snapshot owns the maps; the
// aggregator allocates fresh maps internally.
func (a *Aggregator) Snapshot() Snapshot {
	s := Snapshot{
		HostIP:           a.hostIP,
		HostIPPath:       a.hostIPPath,
		PendingIDs:       a.pendingIDs,
		DroppedPaths:     a.DroppedPaths,
		DroppedMalformed: a.DroppedMalformed,
	}

	// Reset for next cycle
	a.hostIP = make(map[HostIPKey]*HostIPCounters)
	a.hostIPPath = make(map[HostIPPathKey]*PathCounters)
	a.pathCounts = make(map[HostIPKey]int)
	a.pendingIDs = nil
	a.DroppedPaths = 0
	a.DroppedMalformed = 0

	return s
}

// Size returns the total number of entries across both maps.
func (a *Aggregator) Size() int {
	return len(a.hostIP) + len(a.hostIPPath)
}

// PendingCount returns the number of stream entry IDs pending ACK.
func (a *Aggregator) PendingCount() int {
	return len(a.pendingIDs)
}

// ---------------------------------------------------------------------------
// Counter helpers
// ---------------------------------------------------------------------------

func addStatusCounter(status int, c401, c403, c404, c429, c5xx *int) {
	switch {
	case status == 401:
		*c401++
	case status == 403:
		*c403++
	case status == 404:
		*c404++
	case status == 429:
		*c429++
	case status >= 500 && status < 600:
		*c5xx++
	}
}

func addMethodCounter(method string, mGet, mPost, mPut, mPatch, mDelete *int) {
	switch strings.ToUpper(method) {
	case "GET":
		*mGet++
	case "POST":
		*mPost++
	case "PUT":
		*mPut++
	case "PATCH":
		*mPatch++
	case "DELETE":
		*mDelete++
	}
}
