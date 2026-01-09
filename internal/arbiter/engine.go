package arbiter

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/domostack/arbiter/internal/downstream"
	"github.com/domostack/arbiter/internal/policycache"
	"github.com/google/uuid"
)

// Decision represents an authorization decision
type Decision struct {
	Status       int
	Decision     string // allow, unauth, forbid, killswitch, error
	Reason       string
	Source       string // host, none, forced
	Policy       string // host:<host> or none
	TraceID      string
	IdentityHeaders map[string]string
	KillswitchHeaders map[string]string
	MissingHeaders   []string // List of missing required headers
	TotalLatencyMs    float64 // Total latency for the entire check
	KillswitchLatencyMs float64 // Latency for killswitch check (0 if not called)
	GatekeeperLatencyMs float64 // Latency for gatekeeper check (0 if not called)
}

// Engine orchestrates authorization decisions
type Engine struct {
	cache    *policycache.Cache
	client   *downstream.Client
	ksHost   string // Killswitch public host (for forced constraints)
	gkHost   string // Gatekeeper public host (for forced constraints)
}

// NewEngine creates a new decision engine
func NewEngine(cache *policycache.Cache, client *downstream.Client, killswitchPublicHost, gatekeeperPublicHost string) *Engine {
	return &Engine{
		cache:  cache,
		client: client,
		ksHost: strings.ToLower(killswitchPublicHost),
		gkHost: strings.ToLower(gatekeeperPublicHost),
	}
}

// Check makes an authorization decision for a request
func (e *Engine) Check(ctx context.Context, headers map[string]string) (*Decision, error) {
	// 1. Validate required headers
	// Use canonical header keys to match Go's http.Header canonicalization
	host := headers["X-Original-Host"]
	uri := headers["X-Original-Uri"]
	method := headers["X-Original-Method"]

	if host == "" || uri == "" || method == "" {
		traceID := e.getTraceID(headers)
		missingHeaders := []string{}
		if host == "" {
			missingHeaders = append(missingHeaders, "X-Original-Host")
		}
		if uri == "" {
			missingHeaders = append(missingHeaders, "X-Original-Uri")
		}
		if method == "" {
			missingHeaders = append(missingHeaders, "X-Original-Method")
		}
		return &Decision{
			Status:   http.StatusInternalServerError,
			Decision: "error",
			Reason:   "missing required header",
			Source:   "none",
			Policy:   "none",
			TraceID:  traceID,
			MissingHeaders: missingHeaders,
			TotalLatencyMs: 0,
			KillswitchLatencyMs: 0,
			GatekeeperLatencyMs: 0,
		}, nil
	}

	// 2. Generate/use trace ID
	traceID := e.getTraceID(headers)

	// Track total latency from the start
	totalStartTime := time.Now()

	// 3. Resolve policy for host
	hostLower := strings.ToLower(host)
	policy, err := e.cache.Get(hostLower)
	if err != nil {
		totalLatencyMs := float64(time.Since(totalStartTime).Nanoseconds()) / 1e6
		return &Decision{
			Status:   http.StatusInternalServerError,
			Decision: "error",
			Reason:   fmt.Sprintf("failed to resolve policy: %v", err),
			Source:   "none",
			Policy:   "none",
			TraceID:  traceID,
			TotalLatencyMs: totalLatencyMs,
			KillswitchLatencyMs: 0,
			GatekeeperLatencyMs: 0,
		}, nil
	}

	// If no policy exists, allow
	if policy == nil {
		totalLatencyMs := float64(time.Since(totalStartTime).Nanoseconds()) / 1e6
		return &Decision{
			Status:  http.StatusOK,
			Decision: "allow",
			Reason:   "no policy for host",
			Source:   "none",
			Policy:   "none",
			TraceID: traceID,
			TotalLatencyMs: totalLatencyMs,
			KillswitchLatencyMs: 0,
			GatekeeperLatencyMs: 0,
		}, nil
	}

	// Apply forced constraints
	killswitchRequired := policy.KillswitchRequired
	gatekeeperRequired := policy.GatekeeperRequired
	source := "host"

	if hostLower == e.ksHost {
		killswitchRequired = false
		source = "forced"
	}
	if hostLower == e.gkHost {
		gatekeeperRequired = false
		source = "forced"
	}

	policyStr := fmt.Sprintf("host:%s", policy.Host)

	var killswitchLatencyMs float64
	var gatekeeperLatencyMs float64

	// 4. Check Killswitch if required
	if killswitchRequired {
		ksHeaders := e.buildKillswitchHeaders(headers, traceID)
		ksResp, err := e.client.CheckKillswitch(ctx, ksHeaders)
		if err != nil {
			// Fail closed on error/timeout, but capture latency if available
			if ksResp != nil {
				killswitchLatencyMs = ksResp.LatencyMs
			}
			totalLatencyMs := float64(time.Since(totalStartTime).Nanoseconds()) / 1e6
			return &Decision{
				Status:   http.StatusInternalServerError,
				Decision: "error",
				Reason:   fmt.Sprintf("killswitch check failed: %v", err),
				Source:   source,
				Policy:   policyStr,
				TraceID:  traceID,
				TotalLatencyMs: totalLatencyMs,
				KillswitchLatencyMs: killswitchLatencyMs,
				GatekeeperLatencyMs: 0,
			}, nil
		}
		killswitchLatencyMs = ksResp.LatencyMs

		if ksResp.Status == http.StatusServiceUnavailable {
			// Killswitch blocked - return 503, do not call Gatekeeper
			ksHeaders := map[string]string{}
			if ksResp.Rule != "" {
				ksHeaders["X-Killswitch-Rule"] = ksResp.Rule
			}
			if ksResp.Reason != "" {
				ksHeaders["X-Killswitch-Reason"] = ksResp.Reason
			}
			if ksResp.ResponseType != "" {
				ksHeaders["X-Killswitch-Response-Type"] = ksResp.ResponseType
			}
			if ksResp.RetryAfter != "" {
				ksHeaders["Retry-After"] = ksResp.RetryAfter
			}

			totalLatencyMs := float64(time.Since(totalStartTime).Nanoseconds()) / 1e6
			return &Decision{
				Status:           http.StatusServiceUnavailable,
				Decision:         "killswitch",
				Reason:           ksResp.Reason,
				Source:           source,
				Policy:           policyStr,
				TraceID:          traceID,
				KillswitchHeaders: ksHeaders,
				TotalLatencyMs: totalLatencyMs,
				KillswitchLatencyMs: killswitchLatencyMs,
				GatekeeperLatencyMs: 0,
			}, nil
		}

		if ksResp.Status != http.StatusOK {
			// Unexpected status - fail closed
			totalLatencyMs := float64(time.Since(totalStartTime).Nanoseconds()) / 1e6
			return &Decision{
				Status:   http.StatusInternalServerError,
				Decision: "error",
				Reason:   fmt.Sprintf("killswitch returned unexpected status: %d", ksResp.Status),
				Source:   source,
				Policy:   policyStr,
				TraceID:  traceID,
				TotalLatencyMs: totalLatencyMs,
				KillswitchLatencyMs: killswitchLatencyMs,
				GatekeeperLatencyMs: 0,
			}, nil
		}
	}

	// 5. Check Gatekeeper if required
	if gatekeeperRequired {
		// #region agent log
		if logFile, err := os.OpenFile("/Users/matthewtaylor/Projects/proxmox/.cursor/debug.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644); err == nil {
			cookieVal := headers["Cookie"]
			if cookieVal == "" {
				cookieVal = "NOT_PRESENT"
			}
			cookiePreview := cookieVal
			if len(cookieVal) > 50 {
				cookiePreview = cookieVal[:50]
			}
			logFile.WriteString(fmt.Sprintf(`{"sessionId":"debug-session","runId":"run1","hypothesisId":"B","location":"engine.go:216","message":"Cookie in originalHeaders before buildGatekeeperHeaders","data":{"cookie_present":%t,"cookie_value_preview":"%s"},"timestamp":%d}`+"\n", cookieVal != "NOT_PRESENT", cookiePreview, time.Now().UnixMilli()))
			logFile.Close()
		}
		// #endregion
		gkHeaders := e.buildGatekeeperHeaders(headers, traceID)
		// #region agent log
		if logFile, err := os.OpenFile("/Users/matthewtaylor/Projects/proxmox/.cursor/debug.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644); err == nil {
			cookieVal := gkHeaders["Cookie"]
			if cookieVal == "" {
				cookieVal = "NOT_PRESENT"
			}
			cookiePreview := cookieVal
			if len(cookieVal) > 50 {
				cookiePreview = cookieVal[:50]
			}
			logFile.WriteString(fmt.Sprintf(`{"sessionId":"debug-session","runId":"run1","hypothesisId":"C","location":"engine.go:217","message":"Cookie in gkHeaders after buildGatekeeperHeaders","data":{"cookie_present":%t,"cookie_value_preview":"%s"},"timestamp":%d}`+"\n", cookieVal != "NOT_PRESENT", cookiePreview, time.Now().UnixMilli()))
			logFile.Close()
		}
		// #endregion
		gkResp, err := e.client.AuthorizeGatekeeper(ctx, gkHeaders)
		if err != nil {
			// Fail closed on error/timeout, but capture latency if available
			if gkResp != nil {
				gatekeeperLatencyMs = gkResp.LatencyMs
			}
			totalLatencyMs := float64(time.Since(totalStartTime).Nanoseconds()) / 1e6
			return &Decision{
				Status:   http.StatusInternalServerError,
				Decision: "error",
				Reason:   fmt.Sprintf("gatekeeper check failed: %v", err),
				Source:   source,
				Policy:   policyStr,
				TraceID:  traceID,
				TotalLatencyMs: totalLatencyMs,
				KillswitchLatencyMs: killswitchLatencyMs,
				GatekeeperLatencyMs: gatekeeperLatencyMs,
			}, nil
		}
		gatekeeperLatencyMs = gkResp.LatencyMs

		if gkResp.Status == http.StatusUnauthorized {
			totalLatencyMs := float64(time.Since(totalStartTime).Nanoseconds()) / 1e6
			return &Decision{
				Status:  http.StatusUnauthorized,
				Decision: "unauth",
				Reason:   "unauthenticated",
				Source:   source,
				Policy:   policyStr,
				TraceID: traceID,
				TotalLatencyMs: totalLatencyMs,
				KillswitchLatencyMs: killswitchLatencyMs,
				GatekeeperLatencyMs: gatekeeperLatencyMs,
			}, nil
		}

		if gkResp.Status == http.StatusForbidden {
			totalLatencyMs := float64(time.Since(totalStartTime).Nanoseconds()) / 1e6
			return &Decision{
				Status:  http.StatusForbidden,
				Decision: "forbid",
				Reason:   "forbidden",
				Source:   source,
				Policy:   policyStr,
				TraceID: traceID,
				TotalLatencyMs: totalLatencyMs,
				KillswitchLatencyMs: killswitchLatencyMs,
				GatekeeperLatencyMs: gatekeeperLatencyMs,
			}, nil
		}

		if gkResp.Status == http.StatusOK {
			// Extract identity headers
			identityHeaders := make(map[string]string)
			if gkResp.UserID != "" {
				identityHeaders["X-Identity-User-Id"] = gkResp.UserID
			}
			if gkResp.Email != "" {
				identityHeaders["X-Identity-Email"] = gkResp.Email
			}
			if gkResp.Groups != "" {
				identityHeaders["X-Identity-Groups"] = gkResp.Groups
			}

			totalLatencyMs := float64(time.Since(totalStartTime).Nanoseconds()) / 1e6
			return &Decision{
				Status:          http.StatusOK,
				Decision:        "allow",
				Reason:          "authorized",
				Source:          source,
				Policy:          policyStr,
				TraceID:         traceID,
				IdentityHeaders: identityHeaders,
				TotalLatencyMs: totalLatencyMs,
				KillswitchLatencyMs: killswitchLatencyMs,
				GatekeeperLatencyMs: gatekeeperLatencyMs,
			}, nil
		}

		// Unexpected status - fail closed
		totalLatencyMs := float64(time.Since(totalStartTime).Nanoseconds()) / 1e6
		return &Decision{
			Status:   http.StatusInternalServerError,
			Decision: "error",
			Reason:   fmt.Sprintf("gatekeeper returned unexpected status: %d", gkResp.Status),
			Source:   source,
			Policy:   policyStr,
			TraceID:  traceID,
			TotalLatencyMs: totalLatencyMs,
			KillswitchLatencyMs: killswitchLatencyMs,
			GatekeeperLatencyMs: gatekeeperLatencyMs,
		}, nil
	}

	// 6. All checks passed or not required
	totalLatencyMs := float64(time.Since(totalStartTime).Nanoseconds()) / 1e6
	return &Decision{
		Status:  http.StatusOK,
		Decision: "allow",
		Reason:   "authorized",
		Source:   source,
		Policy:   policyStr,
		TraceID: traceID,
		TotalLatencyMs: totalLatencyMs,
		KillswitchLatencyMs: killswitchLatencyMs,
		GatekeeperLatencyMs: gatekeeperLatencyMs,
	}, nil
}

// getTraceID extracts or generates a trace ID
func (e *Engine) getTraceID(headers map[string]string) string {
	if traceID := headers["X-Request-Id"]; traceID != "" {
		return traceID
	}
	return uuid.New().String()
}

// buildKillswitchHeaders builds headers for Killswitch request
func (e *Engine) buildKillswitchHeaders(originalHeaders map[string]string, traceID string) map[string]string {
	headers := make(map[string]string)

	// Required canonical headers
	headers["X-Original-Host"] = originalHeaders["X-Original-Host"]
	headers["X-Original-Uri"] = originalHeaders["X-Original-Uri"]
	headers["X-Original-Method"] = originalHeaders["X-Original-Method"]

	// Trace header
	headers["X-Auth-Trace"] = traceID

	// Optional headers if present
	if v := originalHeaders["X-Forwarded-For"]; v != "" {
		headers["X-Forwarded-For"] = v
	}
	if v := originalHeaders["X-Real-IP"]; v != "" {
		headers["X-Real-IP"] = v
	}
	if v := originalHeaders["User-Agent"]; v != "" {
		headers["User-Agent"] = v
	}
	if v := originalHeaders["X-Request-Id"]; v != "" {
		headers["X-Request-Id"] = v
	}

	return headers
}

// buildGatekeeperHeaders builds headers for Gatekeeper request
func (e *Engine) buildGatekeeperHeaders(originalHeaders map[string]string, traceID string) map[string]string {
	headers := make(map[string]string)

	// Required canonical headers
	headers["X-Original-Host"] = originalHeaders["X-Original-Host"]
	headers["X-Original-Uri"] = originalHeaders["X-Original-Uri"]
	headers["X-Original-Method"] = originalHeaders["X-Original-Method"]

	// Trace header
	headers["X-Auth-Trace"] = traceID

	// Cookie verbatim if present
	if v := originalHeaders["Cookie"]; v != "" {
		headers["Cookie"] = v
	}

	// Optional headers if present
	if v := originalHeaders["X-Forwarded-For"]; v != "" {
		headers["X-Forwarded-For"] = v
	}
	if v := originalHeaders["X-Real-IP"]; v != "" {
		headers["X-Real-IP"] = v
	}
	if v := originalHeaders["User-Agent"]; v != "" {
		headers["User-Agent"] = v
	}
	if v := originalHeaders["X-Request-Id"]; v != "" {
		headers["X-Request-Id"] = v
	}

	return headers
}
