package httpserver

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/domostack/arbiter/internal/arbiter"
	"github.com/domostack/arbiter/internal/policycache"
	"github.com/domostack/arbiter/internal/store"
	"github.com/rs/zerolog"
)

// Handlers contains all HTTP handlers
type Handlers struct {
	engine  *arbiter.Engine
	cache   *policycache.Cache
	store   store.Store
	logger  zerolog.Logger
	ksHost  string
	gkHost  string
}

// NewHandlers creates a new handlers instance
func NewHandlers(engine *arbiter.Engine, cache *policycache.Cache, store store.Store, logger zerolog.Logger, killswitchPublicHost, gatekeeperPublicHost string) *Handlers {
	return &Handlers{
		engine: engine,
		cache:  cache,
		store:  store,
		logger: logger,
		ksHost: killswitchPublicHost,
		gkHost: gatekeeperPublicHost,
	}
}

// HandleCheck handles GET /api/v1/check
func (h *Handlers) HandleCheck(w http.ResponseWriter, r *http.Request) {
	// Extract headers from request
	// Use CanonicalHeaderKey to ensure consistent header key format
	headers := make(map[string]string)
	for k, v := range r.Header {
		if len(v) > 0 {
			headers[http.CanonicalHeaderKey(k)] = v[0]
		}
	}

	// Make decision
	decision, err := h.engine.Check(r.Context(), headers)
	if err != nil {
		h.logger.Error().Err(err).Msg("decision engine error")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	// Log detailed decision result
	method := headers["X-Original-Method"]
	originalHost := headers["X-Original-Host"]
	policyHost := headers["X-Policy-Host"]
	path := headers["X-Original-Uri"]

	logEvent := h.logger.Info().
		Str("method", method).
		Str("original_host", originalHost).
		Str("policy_host", policyHost).
		Str("path", path).
		Str("decision", decision.Decision).
		Int("status", decision.Status).
		Str("reason", decision.Reason).
		Str("source", decision.Source).
		Str("policy", decision.Policy).
		Str("trace_id", decision.TraceID).
		Float64("total_latency_ms", decision.TotalLatencyMs)

	// Add latency details if available
	if decision.KillswitchLatencyMs > 0 {
		logEvent = logEvent.Float64("killswitch_latency_ms", decision.KillswitchLatencyMs)
	}
	if decision.GatekeeperLatencyMs > 0 {
		logEvent = logEvent.Float64("gatekeeper_latency_ms", decision.GatekeeperLatencyMs)
	}

	logEvent.Msg("arbiter check result")

	// Set response headers
	w.Header().Set("X-Auth-Decision", decision.Decision)
	w.Header().Set("X-Auth-Reason", decision.Reason)
	w.Header().Set("X-Auth-Source", decision.Source)
	w.Header().Set("X-Auth-Policy", decision.Policy)
	w.Header().Set("X-Auth-Trace", decision.TraceID)

	// Set missing headers if present
	if len(decision.MissingHeaders) > 0 {
		w.Header().Set("X-MISSING-HEADERS", strings.Join(decision.MissingHeaders, ","))
	}

	// Set identity headers if present
	for k, v := range decision.IdentityHeaders {
		w.Header().Set(k, v)
	}

	// Set killswitch headers if present
	for k, v := range decision.KillswitchHeaders {
		w.Header().Set(k, v)
	}

	w.WriteHeader(decision.Status)
}

// HandleListPolicies handles GET /api/v1/policies
func (h *Handlers) HandleListPolicies(w http.ResponseWriter, r *http.Request) {
	policies, err := h.cache.GetAll()
	if err != nil {
		h.logger.Error().Err(err).Msg("failed to list policies")
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(policies)
}

// HandleCreatePolicy handles POST /api/v1/policies
func (h *Handlers) HandleCreatePolicy(w http.ResponseWriter, r *http.Request) {
	var policy store.HostPolicy
	if err := json.NewDecoder(r.Body).Decode(&policy); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validate host
	policy.Host = strings.ToLower(strings.TrimSpace(policy.Host))
	if policy.Host == "" {
		http.Error(w, "Host is required", http.StatusBadRequest)
		return
	}

	created, err := h.store.Create(&policy)
	if err != nil {
		h.logger.Error().Err(err).Msg("failed to create policy")
		if strings.Contains(err.Error(), "UNIQUE constraint") {
			http.Error(w, "Policy already exists for this host", http.StatusConflict)
			return
		}
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Invalidate cache
	h.cache.Invalidate()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(created)
}

// HandleGetPolicy handles GET /api/v1/policies/{id}
func (h *Handlers) HandleGetPolicy(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid policy ID", http.StatusBadRequest)
		return
	}

	policy, err := h.store.GetByID(id)
	if err != nil {
		h.logger.Error().Err(err).Msg("failed to get policy")
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	if policy == nil {
		http.Error(w, "Policy not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(policy)
}

// HandleUpdatePolicy handles PATCH /api/v1/policies/{id}
func (h *Handlers) HandleUpdatePolicy(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid policy ID", http.StatusBadRequest)
		return
	}

	var policy store.HostPolicy
	if err := json.NewDecoder(r.Body).Decode(&policy); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validate host
	policy.Host = strings.ToLower(strings.TrimSpace(policy.Host))
	if policy.Host == "" {
		http.Error(w, "Host is required", http.StatusBadRequest)
		return
	}

	updated, err := h.store.Update(id, &policy)
	if err != nil {
		h.logger.Error().Err(err).Msg("failed to update policy")
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Invalidate cache
	h.cache.Invalidate()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(updated)
}

// HandleDeletePolicy handles DELETE /api/v1/policies/{id}
func (h *Handlers) HandleDeletePolicy(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid policy ID", http.StatusBadRequest)
		return
	}

	if err := h.store.Delete(id); err != nil {
		h.logger.Error().Err(err).Msg("failed to delete policy")
		if strings.Contains(err.Error(), "not found") {
			http.Error(w, "Policy not found", http.StatusNotFound)
			return
		}
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Invalidate cache
	h.cache.Invalidate()

	w.WriteHeader(http.StatusNoContent)
}

// HandleEffective handles GET /api/v1/effective?host=...
func (h *Handlers) HandleEffective(w http.ResponseWriter, r *http.Request) {
	host := r.URL.Query().Get("host")
	if host == "" {
		http.Error(w, "host parameter is required", http.StatusBadRequest)
		return
	}

	hostLower := strings.ToLower(host)
	policy, err := h.cache.Get(hostLower)
	if err != nil {
		h.logger.Error().Err(err).Msg("failed to get effective policy")
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	type EffectivePolicy struct {
		Host                string `json:"host"`
		KillswitchRequired  bool   `json:"killswitch_required"`
		GatekeeperRequired  bool   `json:"gatekeeper_required"`
		Source              string `json:"source"`
		ForcedKillswitch    bool   `json:"forced_killswitch"`
		ForcedGatekeeper    bool   `json:"forced_gatekeeper"`
		KillswitchPublicHost string `json:"killswitch_public_host,omitempty"`
		GatekeeperPublicHost string `json:"gatekeeper_public_host,omitempty"`
	}

	effective := EffectivePolicy{
		Host:                hostLower,
		KillswitchRequired:  false,
		GatekeeperRequired:  false,
		Source:              "none",
		ForcedKillswitch:    false,
		ForcedGatekeeper:    false,
	}

	if policy != nil {
		effective.KillswitchRequired = policy.KillswitchRequired
		effective.GatekeeperRequired = policy.GatekeeperRequired
		effective.Source = "host"

		// Apply forced constraints
		if hostLower == strings.ToLower(h.ksHost) {
			effective.KillswitchRequired = false
			effective.ForcedKillswitch = true
			effective.Source = "forced"
		}
		if hostLower == strings.ToLower(h.gkHost) {
			effective.GatekeeperRequired = false
			effective.ForcedGatekeeper = true
			effective.Source = "forced"
		}
	}

	if h.ksHost != "" {
		effective.KillswitchPublicHost = h.ksHost
	}
	if h.gkHost != "" {
		effective.GatekeeperPublicHost = h.gkHost
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(effective)
}

// HandleHealthz handles GET /healthz
func (h *Handlers) HandleHealthz(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

// HandleReadyz handles GET /readyz
func (h *Handlers) HandleReadyz(w http.ResponseWriter, r *http.Request) {
	// Simple check: try to list policies (tests DB connectivity)
	_, err := h.store.List()
	if err != nil {
		h.logger.Error().Err(err).Msg("readiness check failed")
		http.Error(w, "Not ready", http.StatusServiceUnavailable)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

// HandleTestCheck handles POST /api/v1/test/check
// This endpoint is for UI testing and always returns HTTP 200 with decision in JSON body
func (h *Handlers) HandleTestCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Method not allowed. Use POST",
		})
		return
	}

	// Parse request body
	var reqBody struct {
		Host   string `json:"host"`
		Method string `json:"method"`
		URI    string `json:"uri"`
	}

	if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
		nginxHeaders := map[string]string{
			"X-Auth-Decision": "error",
			"X-Auth-Reason":   fmt.Sprintf("Invalid request body: %v", err),
			"X-Auth-Source":   "none",
			"X-Auth-Policy":   "none",
			"X-Auth-Trace":    "",
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"decision":              "error",
			"status":                500,
			"reason":                fmt.Sprintf("Invalid request body: %v", err),
			"source":                "none",
			"policy":                "none",
			"trace_id":              "",
			"normalized":           map[string]string{"host": "", "method": "", "uri": ""},
			"latency_ms":            0.0,
			"total_latency_ms":      0.0,
			"killswitch_latency_ms": 0.0,
			"gatekeeper_latency_ms": 0.0,
			"nginx_headers":        nginxHeaders,
		})
		return
	}

	// Fail-closed: missing required fields
	if reqBody.Host == "" || reqBody.Method == "" || reqBody.URI == "" {
		nginxHeaders := map[string]string{
			"X-Auth-Decision": "error",
			"X-Auth-Reason":   "Missing required field: host, method, or uri",
			"X-Auth-Source":   "none",
			"X-Auth-Policy":   "none",
			"X-Auth-Trace":    "",
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"decision":              "error",
			"status":                500,
			"reason":                "Missing required field: host, method, or uri",
			"source":                "none",
			"policy":                "none",
			"trace_id":              "",
			"normalized":           map[string]string{"host": "", "method": "", "uri": ""},
			"latency_ms":            0.0,
			"total_latency_ms":      0.0,
			"killswitch_latency_ms": 0.0,
			"gatekeeper_latency_ms": 0.0,
			"nginx_headers":        nginxHeaders,
		})
		return
	}

	// Build headers map for engine.Check - extract from request like real endpoint
	headers := make(map[string]string)
	// Extract all headers from request (like HandleCheck does)
	for k, v := range r.Header {
		if len(v) > 0 {
			headers[http.CanonicalHeaderKey(k)] = v[0]
		}
	}
	// Override with values from request body (required fields)
	// Normalize host: strip "www." prefix if present (case-insensitive)
	normalizedHost := normalizeHostForTester(strings.ToLower(strings.TrimSpace(reqBody.Host)))
	headers["X-Original-Host"] = normalizedHost
	// If X-Policy-Host is not provided in headers, use the normalized host from body
	if headers["X-Policy-Host"] == "" {
		headers["X-Policy-Host"] = normalizedHost
	} else {
		// Normalize X-Policy-Host if it was provided in headers
		headers["X-Policy-Host"] = normalizeHostForTester(headers["X-Policy-Host"])
	}
	headers["X-Original-Method"] = strings.ToUpper(strings.TrimSpace(reqBody.Method))
	headers["X-Original-Uri"] = reqBody.URI

	// Measure evaluation latency
	startTime := time.Now()
	decision, err := h.engine.Check(r.Context(), headers)
	evaluationLatency := time.Since(startTime)
	latencyMs := float64(evaluationLatency.Nanoseconds()) / 1e6

	if err != nil {
		// Build NGINX headers for error case
		nginxHeaders := make(map[string]string)
		nginxHeaders["X-Auth-Decision"] = "error"
		nginxHeaders["X-Auth-Reason"] = fmt.Sprintf("Decision engine error: %v", err)
		nginxHeaders["X-Auth-Source"] = "none"
		nginxHeaders["X-Auth-Policy"] = "none"
		nginxHeaders["X-Auth-Trace"] = ""

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"decision":              "error",
			"status":                500,
			"reason":                fmt.Sprintf("Decision engine error: %v", err),
			"source":                "none",
			"policy":                "none",
			"trace_id":              "",
			"normalized":           map[string]string{
				"host":   headers["X-Original-Host"],
				"method": headers["X-Original-Method"],
				"uri":    headers["X-Original-Uri"],
			},
			"latency_ms":            latencyMs,
			"total_latency_ms":      latencyMs,
			"killswitch_latency_ms": 0.0,
			"gatekeeper_latency_ms": 0.0,
			"nginx_headers":        nginxHeaders,
		})
		return
	}

	// Build response with decision details
	response := map[string]interface{}{
		"decision":   decision.Decision,
		"status":     decision.Status,
		"reason":     decision.Reason,
		"source":     decision.Source,
		"policy":     decision.Policy,
		"trace_id":   decision.TraceID,
		"normalized": map[string]string{
			"host":   headers["X-Original-Host"],
			"method": headers["X-Original-Method"],
			"uri":    headers["X-Original-Uri"],
		},
		"latency_ms":              decision.TotalLatencyMs,
		"total_latency_ms":        decision.TotalLatencyMs,
		"killswitch_latency_ms":   decision.KillswitchLatencyMs,
		"gatekeeper_latency_ms":   decision.GatekeeperLatencyMs,
	}

	// Build all headers that would be sent back to NGINX (matching HandleCheck behavior)
	nginxHeaders := make(map[string]string)
	nginxHeaders["X-Auth-Decision"] = decision.Decision
	nginxHeaders["X-Auth-Reason"] = decision.Reason
	nginxHeaders["X-Auth-Source"] = decision.Source
	nginxHeaders["X-Auth-Policy"] = decision.Policy
	nginxHeaders["X-Auth-Trace"] = decision.TraceID

	// Add missing headers if present
	if len(decision.MissingHeaders) > 0 {
		nginxHeaders["X-MISSING-HEADERS"] = strings.Join(decision.MissingHeaders, ",")
	}

	// Add identity headers if present
	for k, v := range decision.IdentityHeaders {
		nginxHeaders[k] = v
	}

	// Add killswitch headers if present
	for k, v := range decision.KillswitchHeaders {
		nginxHeaders[k] = v
	}

	response["nginx_headers"] = nginxHeaders

	// Include identity headers if present (for backward compatibility)
	if len(decision.IdentityHeaders) > 0 {
		response["identity_headers"] = decision.IdentityHeaders
	}

	// Include killswitch headers if present (for backward compatibility)
	if len(decision.KillswitchHeaders) > 0 {
		response["killswitch_headers"] = decision.KillswitchHeaders
	}

	// Always return HTTP 200 with decision details in JSON
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// normalizeHostForTester strips "www." prefix from host if present (case-insensitive)
func normalizeHostForTester(host string) string {
	if host == "" {
		return host
	}
	hostLower := strings.ToLower(host)
	if strings.HasPrefix(hostLower, "www.") {
		return host[4:]
	}
	return host
}
