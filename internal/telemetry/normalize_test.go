package telemetry

import "testing"

func TestStripQuery(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"no query", "/api/v1/users", "/api/v1/users"},
		{"with query", "/api/v1/users?page=1&limit=10", "/api/v1/users"},
		{"with fragment", "/docs/section#heading", "/docs/section"},
		{"with query and fragment", "/search?q=test#results", "/search"},
		{"empty string", "", "/"},
		{"just question mark", "/path?", "/path"},
		{"root with query", "/?token=secret", "/"},
		{"sensitive token in query", "/api/v1/data?api_key=abc123secret", "/api/v1/data"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := StripQuery(tt.in)
			if got != tt.want {
				t.Errorf("StripQuery(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestNormalizeHost(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"simple", "example.com", "example.com"},
		{"uppercase", "EXAMPLE.COM", "example.com"},
		{"mixed case", "Example.Com", "example.com"},
		{"trailing dot", "example.com.", "example.com"},
		{"multiple trailing dots", "example.com...", "example.com"},
		{"with port", "example.com:8080", "example.com"},
		{"with www", "www.example.com", "example.com"},
		{"with WWW uppercase", "WWW.example.com", "example.com"},
		{"www and port", "www.example.com:443", "example.com"},
		{"www trailing dot and port", "www.example.com.:8080", "example.com"},
		{"all combined", "WWW.Example.COM.:9090", "example.com"},
		{"just www dot", "www.", "www"},
		{"ipv4 with port", "192.168.1.1:8080", "192.168.1.1"},
		{"ipv4 no port", "192.168.1.1", "192.168.1.1"},
		// IPv6 with brackets
		{"ipv6 bracket with port", "[::1]:8080", "[::1]"},
		{"ipv6 bracket no port", "[::1]", "[::1]"},
		// Bare IPv6 (no brackets, multiple colons) — should NOT strip
		{"bare ipv6", "::1", "::1"},
		{"subdomain with www", "www.sub.example.com", "sub.example.com"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeHost(tt.in)
			if got != tt.want {
				t.Errorf("NormalizeHost(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestNormalizePath(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		// Basic
		{"empty", "", "/"},
		{"root", "/", "/"},
		{"simple path", "/api/v1/health", "/api/v1/health"},

		// Numeric IDs
		{"numeric id", "/api/v1/users/123", "/api/v1/users/:id"},
		{"numeric id zero", "/api/v1/items/0", "/api/v1/items/:id"},
		{"large numeric id", "/api/v1/orders/9999999999", "/api/v1/orders/:id"},
		{"multiple numeric ids", "/api/v1/users/42/posts/99", "/api/v1/users/:id/posts/:id"},

		// UUIDs
		{"uuid", "/api/v1/sessions/550e8400-e29b-41d4-a716-446655440000", "/api/v1/sessions/:uuid"},
		{"uuid uppercase", "/api/v1/sessions/550E8400-E29B-41D4-A716-446655440000", "/api/v1/sessions/:uuid"},

		// Tokens (32+ hex/base64url with digits)
		{"hex token 32 chars", "/api/v1/verify/a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4", "/api/v1/verify/:token"},
		{"base64url token", "/api/v1/reset/aB3dEf_gH1jKlM-nO2pQrStUvWxYz0123", "/api/v1/reset/:token"},
		{"long hex token", "/callback/aabbccdd11223344aabbccdd11223344aabbccdd", "/callback/:token"},

		// Slugs that must NOT be collapsed
		{"short slug", "/blog/my-post", "/blog/my-post"},
		{"long alpha slug", "/blog/getting-started-with-golang", "/blog/getting-started-with-golang"},
		{"slug with numbers", "/blog/top-10-tips", "/blog/top-10-tips"},
		{"short hex-like", "/api/v1/abcdef12", "/api/v1/abcdef12"},
		{"31 char token too short", "/api/v1/a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3", "/api/v1/a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3"},

		// Pipe replacement
		{"pipe in segment", "/api/v1/filter|sort", "/api/v1/filter_sort"},
		{"pipe in path", "/a|b/c|d", "/a_b/c_d"},

		// Mixed
		{"mixed ids and path", "/api/v1/users/42/posts/550e8400-e29b-41d4-a716-446655440000/comments/7",
			"/api/v1/users/:id/posts/:uuid/comments/:id"},

		// No leading slash
		{"no leading slash", "api/v1/users", "/api/v1/users"},

		// Trailing slash preserved
		{"trailing slash", "/api/v1/users/", "/api/v1/users/"},

		// Pure alpha long string (no digits) — must NOT collapse
		{"pure alpha 32+", "/api/v1/abcdefghijklmnopqrstuvwxyzabcdefgh", "/api/v1/abcdefghijklmnopqrstuvwxyzabcdefgh"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizePath(tt.in)
			if got != tt.want {
				t.Errorf("NormalizePath(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
