package downstream

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClient_CheckKillswitch_Allow(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/check" {
			t.Errorf("expected path /api/v1/check, got %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient(server.URL, "http://localhost:3000", 1000, 1000)

	headers := map[string]string{
		"X-Original-Host":   "example.com",
		"X-Original-URI":    "/path",
		"X-Original-Method": "GET",
		"X-Auth-Trace":       "test-trace",
	}

	resp, err := client.CheckKillswitch(context.Background(), headers)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.Status != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.Status)
	}
}

func TestClient_CheckKillswitch_Block(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Killswitch-Rule", "test-rule")
		w.Header().Set("X-Killswitch-Reason", "Blocked")
		w.Header().Set("X-Killswitch-Response-Type", "block")
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	client := NewClient(server.URL, "http://localhost:3000", 1000, 1000)

	headers := map[string]string{
		"X-Original-Host":   "example.com",
		"X-Original-URI":    "/path",
		"X-Original-Method": "GET",
	}

	resp, err := client.CheckKillswitch(context.Background(), headers)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.Status != http.StatusServiceUnavailable {
		t.Errorf("expected status 503, got %d", resp.Status)
	}
	if resp.Rule != "test-rule" {
		t.Errorf("expected rule 'test-rule', got '%s'", resp.Rule)
	}
	if resp.Reason != "Blocked" {
		t.Errorf("expected reason 'Blocked', got '%s'", resp.Reason)
	}
}

func TestClient_AuthorizeGatekeeper_Allow(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/authorize" {
			t.Errorf("expected path /api/v1/authorize, got %s", r.URL.Path)
		}
		w.Header().Set("X-Identity-User-Id", "123")
		w.Header().Set("X-Identity-Email", "user@example.com")
		w.Header().Set("X-Identity-Groups", "admin,users")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient("http://localhost:9090", server.URL, 1000, 1000)

	headers := map[string]string{
		"X-Original-Host":   "example.com",
		"X-Original-URI":    "/path",
		"X-Original-Method": "GET",
		"Cookie":            "gk_sid=test-session",
		"X-Auth-Trace":      "test-trace",
	}

	resp, err := client.AuthorizeGatekeeper(context.Background(), headers)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.Status != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.Status)
	}
	if resp.UserID != "123" {
		t.Errorf("expected user ID '123', got '%s'", resp.UserID)
	}
	if resp.Email != "user@example.com" {
		t.Errorf("expected email 'user@example.com', got '%s'", resp.Email)
	}
	if resp.Groups != "admin,users" {
		t.Errorf("expected groups 'admin,users', got '%s'", resp.Groups)
	}
}

func TestClient_AuthorizeGatekeeper_Unauth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	client := NewClient("http://localhost:9090", server.URL, 1000, 1000)

	headers := map[string]string{
		"X-Original-Host":   "example.com",
		"X-Original-URI":    "/path",
		"X-Original-Method": "GET",
	}

	resp, err := client.AuthorizeGatekeeper(context.Background(), headers)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.Status != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", resp.Status)
	}
}

func TestClient_AuthorizeGatekeeper_Forbid(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()

	client := NewClient("http://localhost:9090", server.URL, 1000, 1000)

	headers := map[string]string{
		"X-Original-Host":   "example.com",
		"X-Original-URI":    "/path",
		"X-Original-Method": "GET",
	}

	resp, err := client.AuthorizeGatekeeper(context.Background(), headers)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.Status != http.StatusForbidden {
		t.Errorf("expected status 403, got %d", resp.Status)
	}
}

func TestClient_HeaderForwarding(t *testing.T) {
	var receivedHeaders map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = make(map[string]string)
		for k, v := range r.Header {
			if len(v) > 0 {
				receivedHeaders[k] = v[0]
			}
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient(server.URL, "http://localhost:3000", 1000, 1000)

	headers := map[string]string{
		"X-Original-Host":   "example.com",
		"X-Original-URI":    "/path",
		"X-Original-Method": "GET",
		"X-Forwarded-For":   "1.2.3.4",
		"X-Real-IP":         "5.6.7.8",
		"User-Agent":        "test-agent",
		"X-Request-Id":      "req-123",
		"X-Auth-Trace":      "trace-456",
	}

	_, err := client.CheckKillswitch(context.Background(), headers)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if receivedHeaders["X-Original-Host"] != "example.com" {
		t.Error("X-Original-Host not forwarded")
	}
	if receivedHeaders["X-Auth-Trace"] != "trace-456" {
		t.Error("X-Auth-Trace not forwarded")
	}
}
