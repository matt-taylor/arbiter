package httpserver

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// blockIPRequest is the expected request body for HandleKillswitchBlockIP.
type blockIPRequest struct {
	IP        string `json:"ip"`
	Method    string `json:"method"`
	Domain    string `json:"domain"`
	Path      string `json:"path"`
	ExpiresAt string `json:"expires_at"`
	Reason    string `json:"reason"`
}

// HandleKillswitchBlockIP proxies an IP block request to Killswitch.
// POST /api/v1/killswitch/block-ip
func (h *Handlers) HandleKillswitchBlockIP(w http.ResponseWriter, r *http.Request) {
	if h.ksBaseURL == "" {
		writeJSONError(w, http.StatusServiceUnavailable, "Killswitch proxy not configured", "")
		return
	}

	// ── Auth: require killswitch:rules:admin scope ──────────────────────
	scopesHeader := r.Header.Get("X-Identity-Scopes")
	if !hasScope(scopesHeader, "killswitch:rules:admin") {
		writeJSONError(w, http.StatusForbidden, "Insufficient permissions: killswitch:rules:admin required", "")
		return
	}

	// ── Parse & validate request body ───────────────────────────────────
	var req blockIPRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("Invalid request body: %v", err), "")
		return
	}

	req.IP = strings.TrimSpace(req.IP)
	if req.IP == "" {
		writeJSONError(w, http.StatusBadRequest, "ip is required", "")
		return
	}

	// Default scope fields to wildcard
	if req.Method == "" {
		req.Method = "*"
	}
	if req.Domain == "" {
		req.Domain = "*"
	}
	if req.Path == "" {
		req.Path = "*"
	}

	// Validate expiration
	if req.ExpiresAt == "" {
		writeJSONError(w, http.StatusBadRequest, "expires_at is required", "")
		return
	}
	expiresTime, err := time.Parse(time.RFC3339, req.ExpiresAt)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("Invalid expires_at format: %v. Use ISO 8601 (e.g. 2025-12-31T23:59:59Z)", err), "")
		return
	}
	maxExpiry := time.Now().Add(72*time.Hour + 1*time.Minute) // 1 min grace
	if expiresTime.After(maxExpiry) {
		writeJSONError(w, http.StatusBadRequest, "Expiration cannot exceed 72 hours from now", "")
		return
	}

	// Validate reason
	req.Reason = strings.TrimSpace(req.Reason)
	if req.Reason == "" {
		writeJSONError(w, http.StatusBadRequest, "reason is required", "")
		return
	}
	if len(req.Reason) > 20 {
		writeJSONError(w, http.StatusBadRequest, "reason must be 20 characters or less", "")
		return
	}

	// ── Build Killswitch request ────────────────────────────────────────
	ksPayload := map[string]interface{}{
		"ip_address":    req.IP,
		"method":        req.Method,
		"domain":        req.Domain,
		"path":          req.Path,
		"action":        "enable",
		"expires_at":    req.ExpiresAt,
		"reason":        req.Reason,
		"response_type": "html",
	}

	payloadBytes, err := json.Marshal(ksPayload)
	if err != nil {
		h.logger.Error().Err(err).Msg("failed to marshal killswitch payload")
		writeJSONError(w, http.StatusInternalServerError, "Internal error", "")
		return
	}

	ksURL := strings.TrimRight(h.ksBaseURL, "/") + "/api/v1/rules/"

	ksReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, ksURL, bytes.NewReader(payloadBytes))
	if err != nil {
		h.logger.Error().Err(err).Msg("failed to create killswitch request")
		writeJSONError(w, http.StatusInternalServerError, "Internal error", "")
		return
	}
	ksReq.Header.Set("Content-Type", "application/json")

	// Forward identity scopes so Killswitch can authorize
	if scopesHeader != "" {
		ksReq.Header.Set("X-Identity-Scopes", scopesHeader)
	}

	// ── Execute request ─────────────────────────────────────────────────
	resp, err := h.ksHTTPClient.Do(ksReq)
	if err != nil {
		h.logger.Error().Err(err).Str("url", ksURL).Msg("killswitch proxy request failed")
		writeJSONError(w, http.StatusBadGateway, fmt.Sprintf("Killswitch unreachable: %v", err), "")
		return
	}
	defer resp.Body.Close()

	// ── Relay response ──────────────────────────────────────────────────
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1 MB limit
	if err != nil {
		h.logger.Error().Err(err).Msg("failed to read killswitch response")
		writeJSONError(w, http.StatusBadGateway, "Failed to read Killswitch response", "")
		return
	}

	h.logger.Info().
		Str("ip", req.IP).
		Str("method", req.Method).
		Str("domain", req.Domain).
		Str("path", req.Path).
		Str("reason", req.Reason).
		Str("expires_at", req.ExpiresAt).
		Int("ks_status", resp.StatusCode).
		Msg("killswitch block-ip proxy complete")

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	w.Write(body)
}

// hasScope checks whether the comma-separated scopes header contains the target.
func hasScope(scopesHeader, target string) bool {
	if scopesHeader == "" {
		return false
	}
	for _, s := range strings.Split(scopesHeader, ",") {
		if strings.TrimSpace(s) == target {
			return true
		}
	}
	return false
}
