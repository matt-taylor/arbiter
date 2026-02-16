package query

import (
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/domostack/arbiter/internal/telemetry"
)

// hostCharsetRe allows alphanumeric, dots, underscores, and hyphens.
// Underscores are permitted for internal infrastructure hostnames (e.g. my_service.internal).
var hostCharsetRe = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)

// Params holds validated and normalized query parameters for telemetry endpoints.
type Params struct {
	Host          string // validated + normalized (matches rollup rows)
	IP            string // only for top-paths; validated via net.ParseIP
	WindowMinutes int
	Limit         int
	EndTS         int64 // epoch seconds; defaults to time.Now().Unix() if omitted
}

// ParseAndValidate parses and validates raw query parameters for telemetry endpoints.
// It returns validated Params or an error describing the first validation failure.
func ParseAndValidate(host, ip, windowStr, limitStr, endTSStr string, maxWindow, maxLimit int) (Params, error) {
	var p Params

	// ── Host: Step A – Strict input validation ──────────────────────────
	host = strings.TrimSpace(host)
	if host == "" {
		return p, fmt.Errorf("host is required")
	}
	if len(host) > 253 {
		return p, fmt.Errorf("host exceeds 253 characters")
	}
	// Reject dangerous characters (ports, query strings, path traversal)
	for _, ch := range host {
		switch ch {
		case '/', '?', '#', '%', ':':
			return p, fmt.Errorf("invalid host: contains %q", string(ch))
		}
		if ch <= ' ' || ch == '\t' || ch == '\n' || ch == '\r' {
			return p, fmt.Errorf("invalid host: contains whitespace")
		}
	}
	if !hostCharsetRe.MatchString(host) {
		return p, fmt.Errorf("invalid host: must match [a-zA-Z0-9._-]")
	}
	// Validate label lengths (each segment separated by '.')
	labels := strings.Split(host, ".")
	for _, label := range labels {
		if len(label) > 63 {
			return p, fmt.Errorf("invalid host: label %q exceeds 63 characters", label)
		}
	}

	// ── Host: Step B – Normalize (same transform the consumer applied) ──
	p.Host = telemetry.NormalizeHost(host)
	if p.Host == "" {
		return p, fmt.Errorf("host is empty after normalization")
	}

	// ── IP (optional, required only for top-paths) ──────────────────────
	ip = strings.TrimSpace(ip)
	if ip != "" {
		parsed := net.ParseIP(ip)
		if parsed == nil {
			return p, fmt.Errorf("invalid ip: %q", ip)
		}
		p.IP = parsed.String() // canonical form
	}

	// ── WindowMinutes ───────────────────────────────────────────────────
	if windowStr == "" {
		p.WindowMinutes = 5
	} else {
		w, err := strconv.Atoi(windowStr)
		if err != nil {
			return p, fmt.Errorf("invalid window_minutes: %q", windowStr)
		}
		if w < 1 {
			w = 1
		}
		if w > maxWindow {
			w = maxWindow
		}
		p.WindowMinutes = w
	}

	// ── Limit ───────────────────────────────────────────────────────────
	if limitStr == "" {
		p.Limit = 20
	} else {
		l, err := strconv.Atoi(limitStr)
		if err != nil {
			return p, fmt.Errorf("invalid limit: %q", limitStr)
		}
		if l < 1 {
			l = 1
		}
		if l > maxLimit {
			l = maxLimit
		}
		p.Limit = l
	}

	// ── EndTS ───────────────────────────────────────────────────────────
	now := time.Now().Unix()
	if endTSStr == "" {
		p.EndTS = now
	} else {
		ts, err := strconv.ParseInt(endTSStr, 10, 64)
		if err != nil {
			return p, fmt.Errorf("invalid end_ts: %q", endTSStr)
		}
		if ts <= 0 {
			return p, fmt.Errorf("end_ts must be > 0")
		}
		if ts > now+60 {
			return p, fmt.Errorf("end_ts is too far in the future")
		}
		p.EndTS = ts
	}

	return p, nil
}

// OverviewParams holds validated query parameters for overview endpoints
// that do not require a host or IP.
type OverviewParams struct {
	WindowMinutes int
	Limit         int
	EndTS         int64 // epoch seconds; defaults to time.Now().Unix() if omitted
}

// ParseOverviewParams parses and validates query parameters for overview endpoints.
// Unlike ParseAndValidate, no host or IP is required, and window_minutes defaults to 15.
func ParseOverviewParams(windowStr, limitStr, endTSStr string, maxWindow, maxLimit int) (OverviewParams, error) {
	var p OverviewParams

	// ── WindowMinutes (default 15) ─────────────────────────────────────
	if windowStr == "" {
		p.WindowMinutes = 15
	} else {
		w, err := strconv.Atoi(windowStr)
		if err != nil {
			return p, fmt.Errorf("invalid window_minutes: %q", windowStr)
		}
		if w < 1 {
			w = 1
		}
		if w > maxWindow {
			w = maxWindow
		}
		p.WindowMinutes = w
	}

	// ── Limit ───────────────────────────────────────────────────────────
	if limitStr == "" {
		p.Limit = 20
	} else {
		l, err := strconv.Atoi(limitStr)
		if err != nil {
			return p, fmt.Errorf("invalid limit: %q", limitStr)
		}
		if l < 1 {
			l = 1
		}
		if l > maxLimit {
			l = maxLimit
		}
		p.Limit = l
	}

	// ── EndTS ───────────────────────────────────────────────────────────
	now := time.Now().Unix()
	if endTSStr == "" {
		p.EndTS = now
	} else {
		ts, err := strconv.ParseInt(endTSStr, 10, 64)
		if err != nil {
			return p, fmt.Errorf("invalid end_ts: %q", endTSStr)
		}
		if ts <= 0 {
			return p, fmt.Errorf("end_ts must be > 0")
		}
		if ts > now+60 {
			return p, fmt.Errorf("end_ts is too far in the future")
		}
		p.EndTS = ts
	}

	return p, nil
}
