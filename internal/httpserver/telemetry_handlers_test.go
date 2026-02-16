package httpserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/rs/zerolog"

	"github.com/domostack/arbiter/internal/telemetry/query"
)

// mockDB implements a minimal *sql.DB replacement for handler tests.
// We test handlers by wiring a real chi router but using a nil repo
// (to test validation paths) or by testing computeTimeRange directly.

func newTestRouter(th *TelemetryHandlers) *chi.Mux {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Route("/api/v1/telemetry", func(r chi.Router) {
		r.Get("/hosts/{host}/top-ips", th.HandleTopIPs)
		r.Get("/hosts/{host}/ips/{ip}/top-paths", th.HandleTopPaths)
		r.Get("/hosts/{host}/summary", th.HandleSummary)
	})
	return r
}

func TestComputeTimeRange(t *testing.T) {
	tests := []struct {
		name          string
		endTS         int64
		windowMinutes int
		wantStart     int64
		wantEnd       int64
	}{
		{
			name:          "5 minute window, already aligned",
			endTS:         1700000300,
			windowMinutes: 5,
			wantStart:     1700000000,
			wantEnd:       1700000300,
		},
		{
			name:          "5 minute window, not aligned",
			endTS:         1700000307,
			windowMinutes: 5,
			wantStart:     1700000000,
			wantEnd:       1700000300,
		},
		{
			name:          "1 minute window",
			endTS:         1700000060,
			windowMinutes: 1,
			wantStart:     1700000000,
			wantEnd:       1700000060,
		},
		{
			name:          "60 minute window",
			endTS:         1700003600,
			windowMinutes: 60,
			wantStart:     1700000000,
			wantEnd:       1700003600,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := query.Params{
				EndTS:         tt.endTS,
				WindowMinutes: tt.windowMinutes,
			}
			start, end := computeTimeRange(p)
			if start != tt.wantStart {
				t.Errorf("start = %d, want %d", start, tt.wantStart)
			}
			if end != tt.wantEnd {
				t.Errorf("end = %d, want %d", end, tt.wantEnd)
			}
		})
	}
}

func TestHandleTopIPs_ValidationError(t *testing.T) {
	logger := zerolog.Nop()
	th := NewTelemetryHandlers(nil, logger, 60, 100)
	router := newTestRouter(th)

	// Host with colon (port) should be rejected
	req := httptest.NewRequest("GET", "/api/v1/telemetry/hosts/example.com:8080/top-ips", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp["error"] == "" {
		t.Error("expected error field in response")
	}
	if resp["request_id"] == "" {
		t.Error("expected request_id field in response")
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("expected Content-Type application/json, got %q", ct)
	}
}

func TestHandleTopPaths_MissingIP(t *testing.T) {
	logger := zerolog.Nop()
	th := NewTelemetryHandlers(nil, logger, 60, 100)

	// Directly call with chi context that has host but empty ip
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Get("/api/v1/telemetry/hosts/{host}/ips/{ip}/top-paths", th.HandleTopPaths)

	// IP with invalid value should fail validation
	req := httptest.NewRequest("GET", "/api/v1/telemetry/hosts/example.com/ips/not-an-ip/top-paths", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleSummary_ValidationError(t *testing.T) {
	logger := zerolog.Nop()
	th := NewTelemetryHandlers(nil, logger, 60, 100)
	router := newTestRouter(th)

	// Invalid window_minutes
	req := httptest.NewRequest("GET", "/api/v1/telemetry/hosts/example.com/summary?window_minutes=abc", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp["error"] == "" {
		t.Error("expected error field in response")
	}
}

func TestHandleTopIPs_EndTSChangesOutput(t *testing.T) {
	// Verify that providing end_ts deterministically changes the start_ts/end_ts
	// in the response. We can't call the full handler without a real DB, so test
	// via computeTimeRange.
	p1 := query.Params{EndTS: 1700000300, WindowMinutes: 5}
	start1, end1 := computeTimeRange(p1)

	p2 := query.Params{EndTS: 1700100300, WindowMinutes: 5}
	start2, end2 := computeTimeRange(p2)

	if start1 == start2 {
		t.Error("different end_ts should produce different start_ts")
	}
	if end1 == end2 {
		t.Error("different end_ts should produce different end_ts")
	}

	// Same end_ts, different window
	p3 := query.Params{EndTS: 1700000300, WindowMinutes: 10}
	start3, end3 := computeTimeRange(p3)

	if end1 != end3 {
		t.Error("same end_ts should produce same end_ts regardless of window")
	}
	if start1 == start3 {
		t.Error("different window should produce different start_ts")
	}
}

func TestWriteJSONError(t *testing.T) {
	w := httptest.NewRecorder()
	writeJSONError(w, http.StatusBadRequest, "test error", "req-123")

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("expected Content-Type application/json, got %q", ct)
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if resp["error"] != "test error" {
		t.Errorf("expected error 'test error', got %q", resp["error"])
	}
	if resp["request_id"] != "req-123" {
		t.Errorf("expected request_id 'req-123', got %q", resp["request_id"])
	}
}
