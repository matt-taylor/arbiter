package httpserver

import (
	"encoding/json"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/rs/zerolog"
	"golang.org/x/time/rate"
)

// limiterEntry holds a rate limiter and the last time it was used.
type limiterEntry struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// TelemetryRateLimiter returns chi middleware that enforces per-client-IP
// rate limiting using a token bucket.
//
// TRUST BOUNDARY: when trustProxy is true, client IP is extracted via
// ClientIP(r) which trusts X-Real-IP / X-Forwarded-For headers. This is
// correct ONLY when Arbiter sits behind NGINX (or equivalent) that
// overwrites/sanitizes those headers. If Arbiter is directly exposed,
// set ARB_TELEMETRY_API_TRUST_PROXY_HEADERS=false so the limiter uses
// r.RemoteAddr only — otherwise attackers can spoof IPs to bypass limits.
func TelemetryRateLimiter(rps float64, burst int, trustProxy bool, logger zerolog.Logger) func(http.Handler) http.Handler {
	var clients sync.Map // map[string]*limiterEntry

	// Background goroutine sweeps stale entries every 5 minutes.
	// Entries with no hits in the last 10 minutes are removed.
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			cutoff := time.Now().Add(-10 * time.Minute)
			clients.Range(func(key, value any) bool {
				entry := value.(*limiterEntry)
				if entry.lastSeen.Before(cutoff) {
					clients.Delete(key)
				}
				return true
			})
		}
	}()

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var clientIP string
			if trustProxy {
				clientIP = ClientIP(r)
			} else {
				// Use r.RemoteAddr only (strip port)
				host, _, err := net.SplitHostPort(r.RemoteAddr)
				if err != nil {
					host = r.RemoteAddr
				}
				clientIP = host
			}

			if clientIP == "" {
				clientIP = "unknown"
			}

			// Get or create limiter for this IP
			val, _ := clients.LoadOrStore(clientIP, &limiterEntry{
				limiter:  rate.NewLimiter(rate.Limit(rps), burst),
				lastSeen: time.Now(),
			})
			entry := val.(*limiterEntry)
			entry.lastSeen = time.Now()

			if !entry.limiter.Allow() {
				reqID := middleware.GetReqID(r.Context())
				logger.Warn().
					Str("client_ip", clientIP).
					Str("request_id", reqID).
					Msg("telemetry API rate limit exceeded")

				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Retry-After", "1")
				w.WriteHeader(http.StatusTooManyRequests)
				json.NewEncoder(w).Encode(map[string]string{
					"error":      "rate limit exceeded",
					"request_id": reqID,
				})
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
