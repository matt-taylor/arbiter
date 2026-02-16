package consumer

import (
	"encoding/json"
	"testing"
)

// ---------------------------------------------------------------------------
// ParseStreamEvent — valid payloads
// ---------------------------------------------------------------------------

func TestParseStreamEvent_Valid(t *testing.T) {
	evt := WireEvent{
		V:              1,
		TsMs:           1700000005000,
		IP:             "1.2.3.4",
		Host:           "example.com",
		HostRaw:        "WWW.Example.COM",
		Method:         "GET",
		Path:           "/api/v1/users/:id",
		PathRaw:        "/api/v1/users/42",
		Status:         200,
		Decision:       "allow",
		EngineDecision: "allow",
	}

	data, err := json.Marshal(evt)
	if err != nil {
		t.Fatalf("failed to marshal test event: %v", err)
	}

	values := map[string]interface{}{
		"event": string(data),
	}

	got, err := ParseStreamEvent(values)
	if err != nil {
		t.Fatalf("ParseStreamEvent returned error: %v", err)
	}

	if got.V != 1 {
		t.Errorf("v = %d, want 1", got.V)
	}
	if got.TsMs != 1700000005000 {
		t.Errorf("ts_ms = %d, want 1700000005000", got.TsMs)
	}
	if got.IP != "1.2.3.4" {
		t.Errorf("ip = %q, want %q", got.IP, "1.2.3.4")
	}
	if got.Host != "example.com" {
		t.Errorf("host = %q, want %q", got.Host, "example.com")
	}
	if got.Method != "GET" {
		t.Errorf("method = %q, want %q", got.Method, "GET")
	}
	if got.Path != "/api/v1/users/:id" {
		t.Errorf("path = %q, want %q", got.Path, "/api/v1/users/:id")
	}
	if got.Status != 200 {
		t.Errorf("status = %d, want 200", got.Status)
	}
}

func TestParseStreamEvent_Valid_ByteSlice(t *testing.T) {
	// Test that []byte event field also works
	evt := WireEvent{V: 1, TsMs: 1700000000000, Host: "a.com", IP: "1.1.1.1", Method: "POST", Path: "/", Status: 201}
	data, _ := json.Marshal(evt)

	values := map[string]interface{}{
		"event": data, // []byte instead of string
	}

	got, err := ParseStreamEvent(values)
	if err != nil {
		t.Fatalf("ParseStreamEvent returned error for []byte event: %v", err)
	}
	if got.Host != "a.com" {
		t.Errorf("host = %q, want %q", got.Host, "a.com")
	}
}

// ---------------------------------------------------------------------------
// ParseStreamEvent — malformed payloads
// ---------------------------------------------------------------------------

func TestParseStreamEvent_Malformed(t *testing.T) {
	tests := []struct {
		name        string
		values      map[string]interface{}
		errContains string
	}{
		{
			name:        "missing event field",
			values:      map[string]interface{}{"other": "data"},
			errContains: "missing 'event' field",
		},
		{
			name:        "event field is integer",
			values:      map[string]interface{}{"event": 12345},
			errContains: "unexpected type",
		},
		{
			name:        "event field is nil",
			values:      map[string]interface{}{"event": nil},
			errContains: "unexpected type",
		},
		{
			name:        "invalid JSON",
			values:      map[string]interface{}{"event": "not json at all"},
			errContains: "JSON unmarshal",
		},
		{
			name:        "empty JSON object",
			values:      map[string]interface{}{"event": "{}"},
			errContains: "unsupported event version 0",
		},
		{
			name:        "wrong version",
			values:      map[string]interface{}{"event": `{"v":2,"ts_ms":1700000000000}`},
			errContains: "unsupported event version 2",
		},
		{
			name:        "empty string",
			values:      map[string]interface{}{"event": ""},
			errContains: "JSON unmarshal",
		},
		{
			name:        "empty values map",
			values:      map[string]interface{}{},
			errContains: "missing 'event' field",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseStreamEvent(tt.values)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if tt.errContains != "" {
				if got := err.Error(); !contains(got, tt.errContains) {
					t.Errorf("error = %q, want it to contain %q", got, tt.errContains)
				}
			}
		})
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && containsSubstring(s, substr)
}

func containsSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Config: DSN parameter injection
// ---------------------------------------------------------------------------

func TestEnsureDSNParams(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			"bare DSN",
			"user:pass@tcp(127.0.0.1:3306)/arbiter_telemetry",
			"user:pass@tcp(127.0.0.1:3306)/arbiter_telemetry?parseTime=true&loc=UTC",
		},
		{
			"has parseTime only",
			"user:pass@tcp(127.0.0.1:3306)/db?parseTime=true",
			"user:pass@tcp(127.0.0.1:3306)/db?parseTime=true&loc=UTC",
		},
		{
			"has loc=UTC only",
			"user:pass@tcp(127.0.0.1:3306)/db?loc=UTC",
			"user:pass@tcp(127.0.0.1:3306)/db?loc=UTC&parseTime=true",
		},
		{
			"has both already",
			"user:pass@tcp(127.0.0.1:3306)/db?parseTime=true&loc=UTC",
			"user:pass@tcp(127.0.0.1:3306)/db?parseTime=true&loc=UTC",
		},
		{
			"has both in different order",
			"user:pass@tcp(127.0.0.1:3306)/db?loc=UTC&parseTime=true",
			"user:pass@tcp(127.0.0.1:3306)/db?loc=UTC&parseTime=true",
		},
		{
			"has other params",
			"user:pass@tcp(127.0.0.1:3306)/db?charset=utf8mb4",
			"user:pass@tcp(127.0.0.1:3306)/db?charset=utf8mb4&parseTime=true&loc=UTC",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ensureDSNParams(tt.in)
			if got != tt.want {
				t.Errorf("ensureDSNParams(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
