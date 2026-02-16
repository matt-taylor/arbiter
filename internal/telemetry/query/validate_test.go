package query

import (
	"fmt"
	"math"
	"testing"
	"time"
)

func TestParseAndValidate(t *testing.T) {
	maxWindow := 60
	maxLimit := 100

	tests := []struct {
		name      string
		host      string
		ip        string
		window    string
		limit     string
		endTS     string
		wantErr   bool
		errSubstr string // substring expected in error message
		check     func(t *testing.T, p Params)
	}{
		// ── Host validation ─────────────────────────────────────────────
		{
			name:    "missing host",
			host:    "",
			wantErr: true, errSubstr: "host is required",
		},
		{
			name:    "host with port (colon rejected)",
			host:    "example.com:8080",
			wantErr: true, errSubstr: "contains \":\"",
		},
		{
			name:    "host with slash (rejected)",
			host:    "example.com/path",
			wantErr: true, errSubstr: "contains \"/\"",
		},
		{
			name:    "host with percent (rejected)",
			host:    "example%2Ecom",
			wantErr: true, errSubstr: "contains \"%\"",
		},
		{
			name:    "host with question mark (rejected)",
			host:    "example.com?foo",
			wantErr: true, errSubstr: "contains \"?\"",
		},
		{
			name:    "host with hash (rejected)",
			host:    "example.com#frag",
			wantErr: true, errSubstr: "contains \"#\"",
		},
		{
			name:    "host with space",
			host:    "example .com",
			wantErr: true, errSubstr: "whitespace",
		},
		{
			name:    "host exceeds 253 chars",
			host:    string(make([]byte, 254)),
			wantErr: true, errSubstr: "exceeds 253",
		},
		{
			name:    "host label exceeds 63 chars",
			host:    string(repeatByte('a', 64)) + ".com",
			wantErr: true, errSubstr: "exceeds 63",
		},
		{
			name:    "underscore in host (allowed for internal infra)",
			host:    "my_service.internal",
			wantErr: false,
			check: func(t *testing.T, p Params) {
				if p.Host != "my_service.internal" {
					t.Errorf("got Host=%q, want %q", p.Host, "my_service.internal")
				}
			},
		},
		{
			name:    "valid host normalization – www. strip",
			host:    "www.example.com",
			wantErr: false,
			check: func(t *testing.T, p Params) {
				if p.Host != "example.com" {
					t.Errorf("got Host=%q, want %q", p.Host, "example.com")
				}
			},
		},
		{
			name:    "valid host normalization – trailing dot strip",
			host:    "example.com.",
			wantErr: false,
			check: func(t *testing.T, p Params) {
				if p.Host != "example.com" {
					t.Errorf("got Host=%q, want %q", p.Host, "example.com")
				}
			},
		},
		{
			name:    "valid host normalization – uppercase to lowercase",
			host:    "Example.COM",
			wantErr: false,
			check: func(t *testing.T, p Params) {
				if p.Host != "example.com" {
					t.Errorf("got Host=%q, want %q", p.Host, "example.com")
				}
			},
		},

		// ── IP validation ───────────────────────────────────────────────
		{
			name:    "invalid IP",
			host:    "example.com",
			ip:      "not-an-ip",
			wantErr: true, errSubstr: "invalid ip",
		},
		{
			name:    "IPv4 IP",
			host:    "example.com",
			ip:      "1.2.3.4",
			wantErr: false,
			check: func(t *testing.T, p Params) {
				if p.IP != "1.2.3.4" {
					t.Errorf("got IP=%q, want %q", p.IP, "1.2.3.4")
				}
			},
		},
		{
			name:    "IPv6 IP canonical form",
			host:    "example.com",
			ip:      "::1",
			wantErr: false,
			check: func(t *testing.T, p Params) {
				if p.IP != "::1" {
					t.Errorf("got IP=%q, want %q", p.IP, "::1")
				}
			},
		},
		{
			name:    "empty IP is OK (not required for top-ips/summary)",
			host:    "example.com",
			ip:      "",
			wantErr: false,
			check: func(t *testing.T, p Params) {
				if p.IP != "" {
					t.Errorf("got IP=%q, want empty", p.IP)
				}
			},
		},

		// ── WindowMinutes ───────────────────────────────────────────────
		{
			name:    "window defaults to 5 when omitted",
			host:    "example.com",
			wantErr: false,
			check: func(t *testing.T, p Params) {
				if p.WindowMinutes != 5 {
					t.Errorf("got WindowMinutes=%d, want 5", p.WindowMinutes)
				}
			},
		},
		{
			name:    "window clamped to min 1",
			host:    "example.com",
			window:  "0",
			wantErr: false,
			check: func(t *testing.T, p Params) {
				if p.WindowMinutes != 1 {
					t.Errorf("got WindowMinutes=%d, want 1", p.WindowMinutes)
				}
			},
		},
		{
			name:    "window clamped to maxWindow",
			host:    "example.com",
			window:  "999",
			wantErr: false,
			check: func(t *testing.T, p Params) {
				if p.WindowMinutes != maxWindow {
					t.Errorf("got WindowMinutes=%d, want %d", p.WindowMinutes, maxWindow)
				}
			},
		},
		{
			name:    "invalid window string",
			host:    "example.com",
			window:  "abc",
			wantErr: true, errSubstr: "invalid window_minutes",
		},

		// ── Limit ───────────────────────────────────────────────────────
		{
			name:    "limit defaults to 20 when omitted",
			host:    "example.com",
			wantErr: false,
			check: func(t *testing.T, p Params) {
				if p.Limit != 20 {
					t.Errorf("got Limit=%d, want 20", p.Limit)
				}
			},
		},
		{
			name:    "limit clamped to min 1",
			host:    "example.com",
			limit:   "-5",
			wantErr: false,
			check: func(t *testing.T, p Params) {
				if p.Limit != 1 {
					t.Errorf("got Limit=%d, want 1", p.Limit)
				}
			},
		},
		{
			name:    "limit clamped to maxLimit",
			host:    "example.com",
			limit:   "999",
			wantErr: false,
			check: func(t *testing.T, p Params) {
				if p.Limit != maxLimit {
					t.Errorf("got Limit=%d, want %d", p.Limit, maxLimit)
				}
			},
		},
		{
			name:    "invalid limit string",
			host:    "example.com",
			limit:   "xyz",
			wantErr: true, errSubstr: "invalid limit",
		},

		// ── EndTS ───────────────────────────────────────────────────────
		{
			name:    "end_ts omitted defaults to now",
			host:    "example.com",
			endTS:   "",
			wantErr: false,
			check: func(t *testing.T, p Params) {
				now := time.Now().Unix()
				if math.Abs(float64(p.EndTS-now)) > 2 {
					t.Errorf("got EndTS=%d, expected near %d", p.EndTS, now)
				}
			},
		},
		{
			name:    "malformed end_ts rejected",
			host:    "example.com",
			endTS:   "not-a-number",
			wantErr: true, errSubstr: "invalid end_ts",
		},
		{
			name:    "end_ts <= 0 rejected",
			host:    "example.com",
			endTS:   "0",
			wantErr: true, errSubstr: "end_ts must be > 0",
		},
		{
			name:    "future end_ts rejected (beyond 60s skew)",
			host:    "example.com",
			endTS:   fmt.Sprintf("%d", time.Now().Unix()+120),
			wantErr: true, errSubstr: "too far in the future",
		},
		{
			name:    "valid historical end_ts accepted",
			host:    "example.com",
			endTS:   fmt.Sprintf("%d", time.Now().Unix()-3600),
			wantErr: false,
			check: func(t *testing.T, p Params) {
				now := time.Now().Unix()
				if p.EndTS > now || p.EndTS < now-7200 {
					t.Errorf("got EndTS=%d, expected historical value", p.EndTS)
				}
			},
		},

		// ── Defaults applied together ───────────────────────────────────
		{
			name:    "all defaults applied when only host provided",
			host:    "example.com",
			wantErr: false,
			check: func(t *testing.T, p Params) {
				if p.Host != "example.com" {
					t.Errorf("got Host=%q, want %q", p.Host, "example.com")
				}
				if p.WindowMinutes != 5 {
					t.Errorf("got WindowMinutes=%d, want 5", p.WindowMinutes)
				}
				if p.Limit != 20 {
					t.Errorf("got Limit=%d, want 20", p.Limit)
				}
				now := time.Now().Unix()
				if math.Abs(float64(p.EndTS-now)) > 2 {
					t.Errorf("got EndTS=%d, expected near %d", p.EndTS, now)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := ParseAndValidate(tt.host, tt.ip, tt.window, tt.limit, tt.endTS, maxWindow, maxLimit)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tt.errSubstr != "" {
					if got := err.Error(); !contains(got, tt.errSubstr) {
						t.Errorf("error %q does not contain %q", got, tt.errSubstr)
					}
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.check != nil {
				tt.check(t, p)
			}
		})
	}
}

func TestParseOverviewParams(t *testing.T) {
	maxWindow := 60
	maxLimit := 100

	tests := []struct {
		name      string
		window    string
		limit     string
		endTS     string
		wantErr   bool
		errSubstr string
		check     func(t *testing.T, p OverviewParams)
	}{
		// ── WindowMinutes (default 15) ─────────────────────────────────
		{
			name:    "window defaults to 15 when omitted",
			wantErr: false,
			check: func(t *testing.T, p OverviewParams) {
				if p.WindowMinutes != 15 {
					t.Errorf("got WindowMinutes=%d, want 15", p.WindowMinutes)
				}
			},
		},
		{
			name:    "window explicitly set to 5",
			window:  "5",
			wantErr: false,
			check: func(t *testing.T, p OverviewParams) {
				if p.WindowMinutes != 5 {
					t.Errorf("got WindowMinutes=%d, want 5", p.WindowMinutes)
				}
			},
		},
		{
			name:    "window clamped to min 1",
			window:  "0",
			wantErr: false,
			check: func(t *testing.T, p OverviewParams) {
				if p.WindowMinutes != 1 {
					t.Errorf("got WindowMinutes=%d, want 1", p.WindowMinutes)
				}
			},
		},
		{
			name:    "window clamped to maxWindow",
			window:  "999",
			wantErr: false,
			check: func(t *testing.T, p OverviewParams) {
				if p.WindowMinutes != maxWindow {
					t.Errorf("got WindowMinutes=%d, want %d", p.WindowMinutes, maxWindow)
				}
			},
		},
		{
			name:    "invalid window string",
			window:  "abc",
			wantErr: true, errSubstr: "invalid window_minutes",
		},

		// ── Limit ───────────────────────────────────────────────────────
		{
			name:    "limit defaults to 20 when omitted",
			wantErr: false,
			check: func(t *testing.T, p OverviewParams) {
				if p.Limit != 20 {
					t.Errorf("got Limit=%d, want 20", p.Limit)
				}
			},
		},
		{
			name:    "limit clamped to min 1",
			limit:   "-5",
			wantErr: false,
			check: func(t *testing.T, p OverviewParams) {
				if p.Limit != 1 {
					t.Errorf("got Limit=%d, want 1", p.Limit)
				}
			},
		},
		{
			name:    "limit clamped to maxLimit",
			limit:   "999",
			wantErr: false,
			check: func(t *testing.T, p OverviewParams) {
				if p.Limit != maxLimit {
					t.Errorf("got Limit=%d, want %d", p.Limit, maxLimit)
				}
			},
		},
		{
			name:    "invalid limit string",
			limit:   "xyz",
			wantErr: true, errSubstr: "invalid limit",
		},

		// ── EndTS ───────────────────────────────────────────────────────
		{
			name:    "end_ts omitted defaults to now",
			wantErr: false,
			check: func(t *testing.T, p OverviewParams) {
				now := time.Now().Unix()
				if math.Abs(float64(p.EndTS-now)) > 2 {
					t.Errorf("got EndTS=%d, expected near %d", p.EndTS, now)
				}
			},
		},
		{
			name:    "malformed end_ts rejected",
			endTS:   "not-a-number",
			wantErr: true, errSubstr: "invalid end_ts",
		},
		{
			name:    "end_ts <= 0 rejected",
			endTS:   "0",
			wantErr: true, errSubstr: "end_ts must be > 0",
		},
		{
			name:    "future end_ts rejected (beyond 60s skew)",
			endTS:   fmt.Sprintf("%d", time.Now().Unix()+120),
			wantErr: true, errSubstr: "too far in the future",
		},
		{
			name:    "valid historical end_ts accepted",
			endTS:   fmt.Sprintf("%d", time.Now().Unix()-3600),
			wantErr: false,
			check: func(t *testing.T, p OverviewParams) {
				now := time.Now().Unix()
				if p.EndTS > now || p.EndTS < now-7200 {
					t.Errorf("got EndTS=%d, expected historical value", p.EndTS)
				}
			},
		},

		// ── All defaults together ──────────────────────────────────────
		{
			name:    "all defaults applied when no params provided",
			wantErr: false,
			check: func(t *testing.T, p OverviewParams) {
				if p.WindowMinutes != 15 {
					t.Errorf("got WindowMinutes=%d, want 15", p.WindowMinutes)
				}
				if p.Limit != 20 {
					t.Errorf("got Limit=%d, want 20", p.Limit)
				}
				now := time.Now().Unix()
				if math.Abs(float64(p.EndTS-now)) > 2 {
					t.Errorf("got EndTS=%d, expected near %d", p.EndTS, now)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := ParseOverviewParams(tt.window, tt.limit, tt.endTS, maxWindow, maxLimit)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tt.errSubstr != "" {
					if got := err.Error(); !contains(got, tt.errSubstr) {
						t.Errorf("error %q does not contain %q", got, tt.errSubstr)
					}
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.check != nil {
				tt.check(t, p)
			}
		})
	}
}

// repeatByte returns a string of n copies of byte b.
func repeatByte(b byte, n int) []byte {
	out := make([]byte, n)
	for i := range out {
		out[i] = b
	}
	return out
}

// contains checks if s contains substr.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && searchSubstr(s, substr)))
}

func searchSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
