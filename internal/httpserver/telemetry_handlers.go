package httpserver

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"sort"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/rs/zerolog"

	"github.com/domostack/arbiter/internal/telemetry/query"
)

// OverviewThresholds holds configurable thresholds for overview reason-flag logic.
type OverviewThresholds struct {
	ScannerPathThreshold int     // Unique paths to earn SCAN_SINGLE_HOST flag (default 30)
	SprayerHostThreshold int     // Unique hosts to earn SPRAY_HOSTS flag (default 5)
	FlooderMaxPaths      int     // Max unique paths for FLOOD_SINGLE_PATH flag (default 3)
	BurstinessThreshold  float64 // Burstiness ratio to earn BURSTY flag (default 5.0)
	PeakRPSThreshold     float64 // Peak RPS to earn HIGH_PEAK flag (default 10.0)
}

// DefaultOverviewThresholds returns the default thresholds.
func DefaultOverviewThresholds() OverviewThresholds {
	return OverviewThresholds{
		ScannerPathThreshold: 30,
		SprayerHostThreshold: 5,
		FlooderMaxPaths:      3,
		BurstinessThreshold:  5.0,
		PeakRPSThreshold:     10.0,
	}
}

// TelemetryHandlers holds the dependencies for the read-only telemetry API endpoints.
type TelemetryHandlers struct {
	repo       *query.Repository
	logger     zerolog.Logger
	maxWindow  int
	maxLimit   int
	thresholds OverviewThresholds
}

// NewTelemetryHandlers creates a new TelemetryHandlers with default overview thresholds.
func NewTelemetryHandlers(repo *query.Repository, logger zerolog.Logger, maxWindow, maxLimit int) *TelemetryHandlers {
	return &TelemetryHandlers{
		repo:       repo,
		logger:     logger,
		maxWindow:  maxWindow,
		maxLimit:   maxLimit,
		thresholds: DefaultOverviewThresholds(),
	}
}

// NewTelemetryHandlersWithThresholds creates a new TelemetryHandlers with explicit overview thresholds.
func NewTelemetryHandlersWithThresholds(repo *query.Repository, logger zerolog.Logger, maxWindow, maxLimit int, thresholds OverviewThresholds) *TelemetryHandlers {
	return &TelemetryHandlers{
		repo:       repo,
		logger:     logger,
		maxWindow:  maxWindow,
		maxLimit:   maxLimit,
		thresholds: thresholds,
	}
}

// HandleTopIPs handles GET /api/v1/telemetry/hosts/{host}/top-ips
func (th *TelemetryHandlers) HandleTopIPs(w http.ResponseWriter, r *http.Request) {
	reqID := middleware.GetReqID(r.Context())
	host := chi.URLParam(r, "host")
	q := r.URL.Query()

	params, err := query.ParseAndValidate(
		host, "", q.Get("window_minutes"), q.Get("limit"), q.Get("end_ts"),
		th.maxWindow, th.maxLimit,
	)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error(), reqID)
		return
	}

	start, end := computeTimeRange(params)

	items, err := th.repo.TopIPs(r.Context(), params.Host, start, end, params.Limit)
	if err != nil {
		th.logger.Error().Err(err).Str("request_id", reqID).Msg("TopIPs query failed")
		writeJSONError(w, http.StatusInternalServerError, "internal server error", reqID)
		return
	}

	// Build response items with nested status
	type itemResp struct {
		IP     string         `json:"ip"`
		Total  int64          `json:"total"`
		Status map[string]int64 `json:"status"`
	}
	respItems := make([]itemResp, len(items))
	for i, row := range items {
		respItems[i] = itemResp{
			IP:    row.IP,
			Total: row.Total,
			Status: map[string]int64{
				"c_401": row.C401,
				"c_403": row.C403,
				"c_404": row.C404,
				"c_429": row.C429,
				"c_5xx": row.C5xx,
			},
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"host":           params.Host,
		"window_minutes": params.WindowMinutes,
		"start_ts":       start,
		"end_ts":         end,
		"items":          respItems,
	})
}

// HandleTopPaths handles GET /api/v1/telemetry/hosts/{host}/ips/{ip}/top-paths
func (th *TelemetryHandlers) HandleTopPaths(w http.ResponseWriter, r *http.Request) {
	reqID := middleware.GetReqID(r.Context())
	host := chi.URLParam(r, "host")
	ip := chi.URLParam(r, "ip")
	q := r.URL.Query()

	params, err := query.ParseAndValidate(
		host, ip, q.Get("window_minutes"), q.Get("limit"), q.Get("end_ts"),
		th.maxWindow, th.maxLimit,
	)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error(), reqID)
		return
	}

	if params.IP == "" {
		writeJSONError(w, http.StatusBadRequest, "ip is required for top-paths", reqID)
		return
	}

	start, end := computeTimeRange(params)

	items, err := th.repo.TopPaths(r.Context(), params.Host, params.IP, start, end, params.Limit)
	if err != nil {
		th.logger.Error().Err(err).Str("request_id", reqID).Msg("TopPaths query failed")
		writeJSONError(w, http.StatusInternalServerError, "internal server error", reqID)
		return
	}

	// Build response items with nested status
	type itemResp struct {
		Path   string         `json:"path"`
		Total  int64          `json:"total"`
		Status map[string]int64 `json:"status"`
	}
	respItems := make([]itemResp, len(items))
	for i, row := range items {
		respItems[i] = itemResp{
			Path:  row.Path,
			Total: row.Total,
			Status: map[string]int64{
				"c_401": row.C401,
				"c_403": row.C403,
				"c_404": row.C404,
				"c_429": row.C429,
				"c_5xx": row.C5xx,
			},
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"host":           params.Host,
		"ip":             params.IP,
		"window_minutes": params.WindowMinutes,
		"start_ts":       start,
		"end_ts":         end,
		"items":          respItems,
	})
}

// HandleSummary handles GET /api/v1/telemetry/hosts/{host}/summary
func (th *TelemetryHandlers) HandleSummary(w http.ResponseWriter, r *http.Request) {
	reqID := middleware.GetReqID(r.Context())
	host := chi.URLParam(r, "host")
	q := r.URL.Query()

	params, err := query.ParseAndValidate(
		host, "", q.Get("window_minutes"), q.Get("limit"), q.Get("end_ts"),
		th.maxWindow, th.maxLimit,
	)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error(), reqID)
		return
	}

	start, end := computeTimeRange(params)

	summary, err := th.repo.Summary(r.Context(), params.Host, start, end)
	if err != nil {
		th.logger.Error().Err(err).Str("request_id", reqID).Msg("Summary query failed")
		writeJSONError(w, http.StatusInternalServerError, "internal server error", reqID)
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"host":           params.Host,
		"window_minutes": params.WindowMinutes,
		"start_ts":       start,
		"end_ts":         end,
		"total":          summary.Total,
		"unique_ips":     summary.UniqueIPs,
		"status": map[string]int64{
			"c_401": summary.C401,
			"c_403": summary.C403,
			"c_404": summary.C404,
			"c_429": summary.C429,
			"c_5xx": summary.C5xx,
		},
	})
}

// computeTimeRange computes the 10s-aligned [start, end) range from validated params.
func computeTimeRange(p query.Params) (start, end int64) {
	end = (p.EndTS / 10) * 10 // floor to 10s bucket boundary
	start = end - int64(p.WindowMinutes*60)
	start = (start / 10) * 10 // floor to 10s bucket boundary
	return start, end
}

// writeJSONError writes a JSON error response with the given status code.
func writeJSONError(w http.ResponseWriter, status int, msg, requestID string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{
		"error":      msg,
		"request_id": requestID,
	})
}

// writeJSON writes a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// ── Overview time-range helper ──────────────────────────────────────────

// computeOverviewTimeRange computes the 10s-aligned [start, end) range from
// validated OverviewParams. Same alignment semantics as computeTimeRange.
func computeOverviewTimeRange(p query.OverviewParams) (start, end int64) {
	end = (p.EndTS / 10) * 10 // floor to 10s bucket boundary
	start = end - int64(p.WindowMinutes*60)
	start = (start / 10) * 10 // floor to 10s bucket boundary
	return start, end
}

// ── Derived-metric helpers ──────────────────────────────────────────────

// computeAvgRPS returns total / windowSeconds, or 0 if window is zero.
func computeAvgRPS(total int64, windowSeconds int64) float64 {
	if windowSeconds <= 0 {
		return 0
	}
	return float64(total) / float64(windowSeconds)
}

// computePeakRPS returns maxBucketTotal / 10.0 (each bucket is 10 seconds).
func computePeakRPS(maxBucketTotal int64) float64 {
	return float64(maxBucketTotal) / 10.0
}

// computeBurstiness returns peakRPS / max(avgRPS, 0.1).
func computeBurstiness(peakRPS, avgRPS float64) float64 {
	return peakRPS / math.Max(avgRPS, 0.1)
}

// roundFloat rounds f to n decimal places.
func roundFloat(f float64, n int) float64 {
	pow := math.Pow(10, float64(n))
	return math.Round(f*pow) / pow
}

// ── HandleOverviewTopHosts ──────────────────────────────────────────────

// HandleOverviewTopHosts handles GET /api/v1/telemetry/overview/top-hosts
func (th *TelemetryHandlers) HandleOverviewTopHosts(w http.ResponseWriter, r *http.Request) {
	reqID := middleware.GetReqID(r.Context())
	q := r.URL.Query()

	params, err := query.ParseOverviewParams(
		q.Get("window_minutes"), q.Get("limit"), q.Get("end_ts"),
		th.maxWindow, th.maxLimit,
	)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error(), reqID)
		return
	}

	start, end := computeOverviewTimeRange(params)
	windowSeconds := end - start

	// Query 1: top hosts
	hosts, err := th.repo.OverviewTopHosts(r.Context(), start, end, params.Limit)
	if err != nil {
		th.logger.Error().Err(err).Str("request_id", reqID).Msg("OverviewTopHosts query failed")
		writeJSONError(w, http.StatusInternalServerError, "internal server error", reqID)
		return
	}

	// Query 2: peak buckets (skip if no hosts)
	peakMap := make(map[string]int64)
	if len(hosts) > 0 {
		hostNames := make([]string, len(hosts))
		for i, h := range hosts {
			hostNames[i] = h.Host
		}
		peaks, err := th.repo.OverviewTopHostsPeak(r.Context(), start, end, hostNames)
		if err != nil {
			th.logger.Error().Err(err).Str("request_id", reqID).Msg("OverviewTopHostsPeak query failed")
			writeJSONError(w, http.StatusInternalServerError, "internal server error", reqID)
			return
		}
		for _, p := range peaks {
			peakMap[p.Host] = p.MaxBucketTotal
		}
	}

	// Build response items
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

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"window_minutes": params.WindowMinutes,
		"start_ts":       start,
		"end_ts":         end,
		"items":          items,
	})
}

// ── HandleOverviewSuspiciousScanners ────────────────────────────────────

// HandleOverviewSuspiciousScanners handles GET /api/v1/telemetry/overview/suspicious-scanners
func (th *TelemetryHandlers) HandleOverviewSuspiciousScanners(w http.ResponseWriter, r *http.Request) {
	reqID := middleware.GetReqID(r.Context())
	q := r.URL.Query()

	params, err := query.ParseOverviewParams(
		q.Get("window_minutes"), q.Get("limit"), q.Get("end_ts"),
		th.maxWindow, th.maxLimit,
	)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error(), reqID)
		return
	}

	start, end := computeOverviewTimeRange(params)
	windowSeconds := end - start

	// Stage 1: candidates
	candidates, err := th.repo.OverviewScannerCandidates(r.Context(), start, end)
	if err != nil {
		th.logger.Error().Err(err).Str("request_id", reqID).Msg("OverviewScannerCandidates query failed")
		writeJSONError(w, http.StatusInternalServerError, "internal server error", reqID)
		return
	}

	// Stage 2: enrich (skip if no candidates)
	type enrichKey struct{ Host, IP string }
	enrichMap := make(map[enrichKey]query.ScannerEnrichRow)
	if len(candidates) > 0 {
		enriched, err := th.repo.OverviewScannerEnrich(r.Context(), start, end, candidates)
		if err != nil {
			th.logger.Error().Err(err).Str("request_id", reqID).Msg("OverviewScannerEnrich query failed")
			writeJSONError(w, http.StatusInternalServerError, "internal server error", reqID)
			return
		}
		for _, e := range enriched {
			enrichMap[enrichKey{e.Host, e.IP}] = e
		}
	}

	// Merge candidates + enrichment, compute derived metrics
	items := make([]query.SuspiciousScannerItem, 0, len(candidates))
	for _, c := range candidates {
		e := enrichMap[enrichKey{c.Host, c.IP}]
		avgRPS := computeAvgRPS(e.Total, windowSeconds)
		peakRPS := computePeakRPS(e.PeakBucketTotal)
		burstiness := computeBurstiness(peakRPS, avgRPS)

		reasons := make([]string, 0)
		if c.UniquePaths >= int64(th.thresholds.ScannerPathThreshold) {
			reasons = append(reasons, "SCAN_SINGLE_HOST")
		}
		if burstiness >= th.thresholds.BurstinessThreshold {
			reasons = append(reasons, "BURSTY")
		}
		if peakRPS >= th.thresholds.PeakRPSThreshold {
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

	// Sort: unique_paths DESC, peak_rps DESC, total DESC
	sort.Slice(items, func(i, j int) bool {
		if items[i].UniquePaths != items[j].UniquePaths {
			return items[i].UniquePaths > items[j].UniquePaths
		}
		if items[i].PeakRPS != items[j].PeakRPS {
			return items[i].PeakRPS > items[j].PeakRPS
		}
		return items[i].Total > items[j].Total
	})

	// Apply final limit
	if len(items) > params.Limit {
		items = items[:params.Limit]
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"window_minutes": params.WindowMinutes,
		"start_ts":       start,
		"end_ts":         end,
		"items":          items,
	})
}

// ── HandleOverviewSuspiciousSprayers ────────────────────────────────────

// HandleOverviewSuspiciousSprayers handles GET /api/v1/telemetry/overview/suspicious-sprayers
func (th *TelemetryHandlers) HandleOverviewSuspiciousSprayers(w http.ResponseWriter, r *http.Request) {
	reqID := middleware.GetReqID(r.Context())
	q := r.URL.Query()

	params, err := query.ParseOverviewParams(
		q.Get("window_minutes"), q.Get("limit"), q.Get("end_ts"),
		th.maxWindow, th.maxLimit,
	)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error(), reqID)
		return
	}

	start, end := computeOverviewTimeRange(params)
	windowSeconds := end - start

	// Query 1: sprayer candidates
	sprayers, err := th.repo.OverviewSuspiciousSprayers(r.Context(), start, end, params.Limit)
	if err != nil {
		th.logger.Error().Err(err).Str("request_id", reqID).Msg("OverviewSuspiciousSprayers query failed")
		writeJSONError(w, http.StatusInternalServerError, "internal server error", reqID)
		return
	}

	// Query 2: peak buckets (skip if no sprayers)
	peakMap := make(map[string]int64)
	if len(sprayers) > 0 {
		ips := make([]string, len(sprayers))
		for i, s := range sprayers {
			ips[i] = s.IP
		}
		peaks, err := th.repo.OverviewSprayersPeak(r.Context(), start, end, ips)
		if err != nil {
			th.logger.Error().Err(err).Str("request_id", reqID).Msg("OverviewSprayersPeak query failed")
			writeJSONError(w, http.StatusInternalServerError, "internal server error", reqID)
			return
		}
		for _, p := range peaks {
			peakMap[p.IP] = p.MaxBucketTotal
		}
	}

	// Build response items
	items := make([]query.SuspiciousSprayerItem, len(sprayers))
	for i, s := range sprayers {
		avgRPS := computeAvgRPS(s.Total, windowSeconds)
		peakRPS := computePeakRPS(peakMap[s.IP])
		burstiness := computeBurstiness(peakRPS, avgRPS)

		reasons := make([]string, 0)
		if s.UniqueHosts >= int64(th.thresholds.SprayerHostThreshold) {
			reasons = append(reasons, "SPRAY_HOSTS")
		}
		if burstiness >= th.thresholds.BurstinessThreshold {
			reasons = append(reasons, "BURSTY")
		}
		if peakRPS >= th.thresholds.PeakRPSThreshold {
			reasons = append(reasons, "HIGH_PEAK")
		}

		items[i] = query.SuspiciousSprayerItem{
			IP:          s.IP,
			UniqueHosts: s.UniqueHosts,
			Total:       s.Total,
			AvgRPS:      roundFloat(avgRPS, 2),
			PeakRPS:     roundFloat(peakRPS, 2),
			Burstiness:  roundFloat(burstiness, 2),
			Reasons:     reasons,
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"window_minutes": params.WindowMinutes,
		"start_ts":       start,
		"end_ts":         end,
		"items":          items,
	})
}

// ── HandleOverviewSuspiciousFlooders ────────────────────────────────────

// HandleOverviewSuspiciousFlooders handles GET /api/v1/telemetry/overview/suspicious-flooders
func (th *TelemetryHandlers) HandleOverviewSuspiciousFlooders(w http.ResponseWriter, r *http.Request) {
	reqID := middleware.GetReqID(r.Context())
	q := r.URL.Query()

	params, err := query.ParseOverviewParams(
		q.Get("window_minutes"), q.Get("limit"), q.Get("end_ts"),
		th.maxWindow, th.maxLimit,
	)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error(), reqID)
		return
	}

	start, end := computeOverviewTimeRange(params)
	windowSeconds := end - start

	// Stage 1: candidates — (host, ip, path) triples with high single-path totals
	candidates, err := th.repo.OverviewFlooderCandidates(r.Context(), start, end)
	if err != nil {
		th.logger.Error().Err(err).Str("request_id", reqID).Msg("OverviewFlooderCandidates query failed")
		writeJSONError(w, http.StatusInternalServerError, "internal server error", reqID)
		return
	}

	// Stage 2: enrich with unique_paths + peak (skip if no candidates)
	type enrichKey struct{ Host, IP string }
	enrichMap := make(map[enrichKey]query.FlooderEnrichRow)
	if len(candidates) > 0 {
		enriched, err := th.repo.OverviewFlooderEnrich(r.Context(), start, end, candidates)
		if err != nil {
			th.logger.Error().Err(err).Str("request_id", reqID).Msg("OverviewFlooderEnrich query failed")
			writeJSONError(w, http.StatusInternalServerError, "internal server error", reqID)
			return
		}
		for _, e := range enriched {
			enrichMap[enrichKey{e.Host, e.IP}] = e
		}
	}

	// Merge candidates + enrichment, compute derived metrics
	flooderItems := make([]query.SuspiciousFlooderItem, 0, len(candidates))
	for _, c := range candidates {
		e, ok := enrichMap[enrichKey{c.Host, c.IP}]
		if !ok {
			// IP did not pass the unique_paths filter — skip
			continue
		}

		avgRPS := computeAvgRPS(c.Total, windowSeconds)
		peakRPS := computePeakRPS(e.PeakBucketTotal)
		burstiness := computeBurstiness(peakRPS, avgRPS)

		reasons := make([]string, 0)
		if e.UniquePaths <= int64(th.thresholds.FlooderMaxPaths) {
			reasons = append(reasons, "FLOOD_SINGLE_PATH")
		}
		if burstiness >= th.thresholds.BurstinessThreshold {
			reasons = append(reasons, "BURSTY")
		}
		if peakRPS >= th.thresholds.PeakRPSThreshold {
			reasons = append(reasons, "HIGH_PEAK")
		}

		flooderItems = append(flooderItems, query.SuspiciousFlooderItem{
			Host:        c.Host,
			IP:          c.IP,
			Path:        c.Path,
			UniquePaths: e.UniquePaths,
			Total:       c.Total,
			AvgRPS:      roundFloat(avgRPS, 2),
			PeakRPS:     roundFloat(peakRPS, 2),
			Burstiness:  roundFloat(burstiness, 2),
			Reasons:     reasons,
		})
	}

	// Sort: total DESC, peak_rps DESC
	sort.Slice(flooderItems, func(i, j int) bool {
		if flooderItems[i].Total != flooderItems[j].Total {
			return flooderItems[i].Total > flooderItems[j].Total
		}
		return flooderItems[i].PeakRPS > flooderItems[j].PeakRPS
	})

	// Apply final limit
	if len(flooderItems) > params.Limit {
		flooderItems = flooderItems[:params.Limit]
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"window_minutes": params.WindowMinutes,
		"start_ts":       start,
		"end_ts":         end,
		"items":          flooderItems,
	})
}

// HandleOverviewConfig handles GET /api/v1/telemetry/overview/config
// Returns descriptions, current thresholds, and reason-flag explanations
// for each suspicious-activity detection type. No DB queries — reads from config.
func (th *TelemetryHandlers) HandleOverviewConfig(w http.ResponseWriter, r *http.Request) {
	repoCfg := th.repo.Config()
	t := th.thresholds

	type reasonFlag struct {
		Flag        string `json:"flag"`
		Description string `json:"description"`
	}

	type detectionConfig struct {
		Description string            `json:"description"`
		Thresholds  map[string]interface{} `json:"thresholds"`
		ReasonFlags []reasonFlag      `json:"reason_flags"`
	}

	scanners := detectionConfig{
		Description: "IPs scanning many unique paths on a single host — indicates directory enumeration, vulnerability probing, or scraping.",
		Thresholds: map[string]interface{}{
			"noise_floor":           repoCfg.ScannerNoiseFloor,
			"candidate_cap":         repoCfg.ScannerCandidateCap,
			"path_threshold":        t.ScannerPathThreshold,
			"burstiness_threshold":  t.BurstinessThreshold,
			"peak_rps_threshold":    t.PeakRPSThreshold,
		},
		ReasonFlags: []reasonFlag{
			{Flag: "SCAN_SINGLE_HOST", Description: fmt.Sprintf("IP hit ≥ %d unique paths on a single host", t.ScannerPathThreshold)},
			{Flag: "BURSTY", Description: fmt.Sprintf("Peak-to-average RPS ratio ≥ %.1f", t.BurstinessThreshold)},
			{Flag: "HIGH_PEAK", Description: fmt.Sprintf("Peak RPS ≥ %.1f", t.PeakRPSThreshold)},
		},
	}

	sprayers := detectionConfig{
		Description: "IPs hitting many unique hosts — indicates broad reconnaissance, credential stuffing, or spray attacks across domains.",
		Thresholds: map[string]interface{}{
			"host_threshold":        t.SprayerHostThreshold,
			"burstiness_threshold":  t.BurstinessThreshold,
			"peak_rps_threshold":    t.PeakRPSThreshold,
		},
		ReasonFlags: []reasonFlag{
			{Flag: "SPRAY_HOSTS", Description: fmt.Sprintf("IP hit ≥ %d unique hosts", t.SprayerHostThreshold)},
			{Flag: "BURSTY", Description: fmt.Sprintf("Peak-to-average RPS ratio ≥ %.1f", t.BurstinessThreshold)},
			{Flag: "HIGH_PEAK", Description: fmt.Sprintf("Peak RPS ≥ %.1f", t.PeakRPSThreshold)},
		},
	}

	flooders := detectionConfig{
		Description: "IPs hammering the same endpoint on a single host — indicates brute-force login attempts, API abuse, or DDoS.",
		Thresholds: map[string]interface{}{
			"min_total":             repoCfg.FlooderMinTotal,
			"candidate_cap":         repoCfg.FlooderCandidateCap,
			"max_paths":             t.FlooderMaxPaths,
			"burstiness_threshold":  t.BurstinessThreshold,
			"peak_rps_threshold":    t.PeakRPSThreshold,
		},
		ReasonFlags: []reasonFlag{
			{Flag: "FLOOD_SINGLE_PATH", Description: fmt.Sprintf("IP hit ≤ %d unique paths on the host (focused on one endpoint)", t.FlooderMaxPaths)},
			{Flag: "BURSTY", Description: fmt.Sprintf("Peak-to-average RPS ratio ≥ %.1f", t.BurstinessThreshold)},
			{Flag: "HIGH_PEAK", Description: fmt.Sprintf("Peak RPS ≥ %.1f", t.PeakRPSThreshold)},
		},
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"scanners": scanners,
		"sprayers": sprayers,
		"flooders": flooders,
	})
}
