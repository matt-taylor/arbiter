package telemetry

import (
	"regexp"
	"strings"
)

// TODO(security): Percent-decoding of path segments is security-sensitive.
// Malicious clients can use URL encoding to bypass normalization (e.g. %2F for /).
// Phase 1 deliberately does NOT percent-decode; this should be revisited in Phase 2
// with careful consideration of double-encoding attacks and normalization ordering.

var (
	// uuidRe matches standard UUID format: 8-4-4-4-12 hex chars.
	uuidRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

	// numericRe matches purely numeric segments.
	numericRe = regexp.MustCompile(`^[0-9]+$`)

	// tokenRe matches hex or base64url-like tokens of 32+ characters.
	// This is intentionally strict: only collapse segments that are clearly
	// machine-generated identifiers, not human-readable slugs.
	// Matches: hex strings (0-9a-f), base64url strings (A-Za-z0-9_-).
	tokenRe = regexp.MustCompile(`^[0-9a-zA-Z_-]{32,}$`)

	// tokenHexRe refines tokenRe: at least 32 chars AND looks hex-ish or
	// base64url-ish (contains digits mixed with letters, not pure alpha).
	tokenHexLikeRe = regexp.MustCompile(`[0-9]`)
)

// StripQuery removes query parameters and fragments from a URI.
// Returns just the path component. Query params are never stored in telemetry.
func StripQuery(uri string) string {
	// Strip fragment first
	if idx := strings.IndexByte(uri, '#'); idx >= 0 {
		uri = uri[:idx]
	}
	// Strip query params
	if idx := strings.IndexByte(uri, '?'); idx >= 0 {
		uri = uri[:idx]
	}
	if uri == "" {
		return "/"
	}
	return uri
}

// NormalizeHost normalizes a host for telemetry purposes.
// This is telemetry-only normalization and MUST NOT affect policy matching.
//
// Rules:
//   - Lowercase
//   - Strip trailing "."
//   - Strip port
//   - Strip leading "www."
func NormalizeHost(host string) string {
	if host == "" {
		return host
	}

	// Lowercase
	h := strings.ToLower(host)

	// Strip port first (before trailing dot, since "host.:port" has port after dot)
	if bracketIdx := strings.LastIndexByte(h, ']'); bracketIdx >= 0 {
		// IPv6 with brackets: [::1]:8080 → strip port after ]
		if colonIdx := strings.IndexByte(h[bracketIdx:], ':'); colonIdx >= 0 {
			h = h[:bracketIdx+colonIdx]
		}
	} else if colonIdx := strings.LastIndexByte(h, ':'); colonIdx >= 0 {
		// IPv4 or hostname:port — only strip if there's exactly one colon
		// (otherwise it might be bare IPv6 like ::1)
		if strings.Count(h, ":") == 1 {
			h = h[:colonIdx]
		}
	}

	// Strip trailing dot (DNS root) — after port is removed
	h = strings.TrimRight(h, ".")

	// Strip leading "www."
	h = strings.TrimPrefix(h, "www.")

	return h
}

// NormalizePath normalizes a path for telemetry purposes.
// The input should already have query params stripped (via StripQuery).
//
// Rules:
//   - Replace numeric segments with :id
//   - Replace UUID segments with :uuid
//   - Replace 32+ char hex/base64url-like tokens with :token
//   - Replace literal "|" with "_"
//   - Always starts with "/"; empty → "/"
func NormalizePath(path string) string {
	if path == "" {
		return "/"
	}

	// Replace literal pipe with underscore
	path = strings.ReplaceAll(path, "|", "_")

	// Ensure starts with /
	if path[0] != '/' {
		path = "/" + path
	}

	// Split into segments, normalize each
	segments := strings.Split(path, "/")
	for i, seg := range segments {
		if seg == "" {
			continue
		}
		segments[i] = normalizeSegment(seg)
	}

	result := strings.Join(segments, "/")
	if result == "" {
		return "/"
	}
	return result
}

// normalizeSegment normalizes a single path segment.
func normalizeSegment(seg string) string {
	// UUID check (most specific first)
	if uuidRe.MatchString(seg) {
		return ":uuid"
	}

	// Pure numeric → :id
	if numericRe.MatchString(seg) {
		return ":id"
	}

	// Token heuristic: 32+ chars, looks like hex/base64url, AND contains digits
	// (to avoid collapsing real alphabetic slugs like "getting-started-with-go")
	if tokenRe.MatchString(seg) && tokenHexLikeRe.MatchString(seg) {
		return ":token"
	}

	return seg
}
