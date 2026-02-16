package httpserver

import (
	"net"
	"net/http"
	"strings"
)

// ClientIP extracts the client IP address from the request using a consistent
// trust chain aligned with NGINX reverse-proxy headers.
//
// Priority:
//  1. X-Real-Ip header (set by NGINX proxy_set_header)
//  2. First entry in X-Forwarded-For (comma-separated, trimmed)
//  3. r.RemoteAddr (with port stripped)
//
// Returns empty string if no IP can be determined.
func ClientIP(r *http.Request) string {
	// 1. X-Real-Ip (canonical header access via r.Header.Get)
	if ip := strings.TrimSpace(r.Header.Get("X-Real-Ip")); ip != "" {
		return stripPort(ip)
	}

	// 2. X-Forwarded-For — use the first (leftmost) entry
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		first := xff
		if idx := strings.IndexByte(xff, ','); idx >= 0 {
			first = xff[:idx]
		}
		if ip := strings.TrimSpace(first); ip != "" {
			return stripPort(ip)
		}
	}

	// 3. RemoteAddr fallback
	if r.RemoteAddr != "" {
		return stripPort(r.RemoteAddr)
	}

	return ""
}

// stripPort removes the port component from an address.
// Handles both IPv4 (1.2.3.4:8080) and IPv6 ([::1]:8080) formats.
func stripPort(addr string) string {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		// No port present or parse error — return as-is
		return addr
	}
	return host
}
