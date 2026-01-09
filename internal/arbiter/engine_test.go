package arbiter

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/domostack/arbiter/internal/downstream"
	"github.com/domostack/arbiter/internal/policycache"
	"github.com/domostack/arbiter/internal/store"
)

type mockCache struct {
	policies map[string]*store.HostPolicy
}

func (m *mockCache) Get(host string) (*store.HostPolicy, error) {
	return m.policies[host], nil
}

func (m *mockCache) GetAll() ([]*store.HostPolicy, error) {
	policies := make([]*store.HostPolicy, 0, len(m.policies))
	for _, p := range m.policies {
		policies = append(policies, p)
	}
	return policies, nil
}

func (m *mockCache) Invalidate() {}

func newMockCache() *mockCache {
	return &mockCache{
		policies: make(map[string]*store.HostPolicy),
	}
}

func TestEngine_Check_NoPolicy(t *testing.T) {
	cache := newMockCache()
	client := downstream.NewClient("http://localhost:9090", "http://localhost:3000", 1000, 1000)
	engine := NewEngine(cache, client, "killswitch.example.com", "gatekeeper.example.com")

	headers := map[string]string{
		"X-Original-Host":   "example.com",
		"X-Original-Uri":     "/path",
		"X-Original-Method":  "GET",
		"X-Request-Id":       "test-request-id",
	}

	decision, err := engine.Check(context.Background(), headers)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if decision.Status != http.StatusOK {
		t.Errorf("expected status 200, got %d", decision.Status)
	}
	if decision.Decision != "allow" {
		t.Errorf("expected decision 'allow', got '%s'", decision.Decision)
	}
	if decision.Source != "none" {
		t.Errorf("expected source 'none', got '%s'", decision.Source)
	}
	if decision.TraceID != "test-request-id" {
		t.Errorf("expected trace ID 'test-request-id', got '%s'", decision.TraceID)
	}
}

func TestEngine_Check_MissingHeaders(t *testing.T) {
	cache := newMockCache()
	client := downstream.NewClient("http://localhost:9090", "http://localhost:3000", 1000, 1000)
	engine := NewEngine(cache, client, "", "")

	headers := map[string]string{
		"X-Original-Host": "example.com",
		// Missing X-Original-URI and X-Original-Method
	}

	decision, err := engine.Check(context.Background(), headers)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if decision.Status != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", decision.Status)
	}
	if decision.Decision != "error" {
		t.Errorf("expected decision 'error', got '%s'", decision.Decision)
	}
	if decision.Reason != "missing required header" {
		t.Errorf("expected reason 'missing required header', got '%s'", decision.Reason)
	}
}

func TestEngine_Check_ForcedConstraints(t *testing.T) {
	cache := newMockCache()
	client := downstream.NewClient("http://localhost:9090", "http://localhost:3000", 1000, 1000)
	engine := NewEngine(cache, client, "killswitch.example.com", "gatekeeper.example.com")

	// Create a policy that requires both checks
	policy := &store.HostPolicy{
		Host:                "killswitch.example.com",
		KillswitchRequired:  true,
		GatekeeperRequired:  true,
	}
	cache.policies["killswitch.example.com"] = policy

	headers := map[string]string{
		"X-Original-Host":   "killswitch.example.com",
		"X-Original-Uri":     "/path",
		"X-Original-Method": "GET",
	}

	decision, err := engine.Check(context.Background(), headers)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if decision.Source != "forced" {
		t.Errorf("expected source 'forced', got '%s'", decision.Source)
	}
	// Killswitch should be forced to false, so it shouldn't be called
	// Since both are forced off, should allow
	if decision.Status != http.StatusOK {
		t.Errorf("expected status 200, got %d", decision.Status)
	}
}

func TestEngine_Check_KillswitchOnly_Allow(t *testing.T) {
	// Create mock Killswitch server
	ksServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ksServer.Close()

	cache := newMockCache()
	client := downstream.NewClient(ksServer.URL, "http://localhost:3000", 1000, 1000)
	engine := NewEngine(cache, client, "", "")

	policy := &store.HostPolicy{
		Host:                "example.com",
		KillswitchRequired:  true,
		GatekeeperRequired:  false,
	}
	cache.policies["example.com"] = policy

	headers := map[string]string{
		"X-Original-Host":   "example.com",
		"X-Original-Uri":    "/path",
		"X-Original-Method": "GET",
	}

	decision, err := engine.Check(context.Background(), headers)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if decision.Status != http.StatusOK {
		t.Errorf("expected status 200, got %d", decision.Status)
	}
	if decision.Decision != "allow" {
		t.Errorf("expected decision 'allow', got '%s'", decision.Decision)
	}
}

func TestEngine_Check_KillswitchOnly_Block(t *testing.T) {
	// Create mock Killswitch server that blocks
	ksServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Killswitch-Rule", "test-rule")
		w.Header().Set("X-Killswitch-Reason", "Blocked by rule")
		w.Header().Set("X-Killswitch-Response-Type", "block")
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer ksServer.Close()

	cache := newMockCache()
	client := downstream.NewClient(ksServer.URL, "http://localhost:3000", 1000, 1000)
	engine := NewEngine(cache, client, "", "")

	policy := &store.HostPolicy{
		Host:                "example.com",
		KillswitchRequired:  true,
		GatekeeperRequired:  false,
	}
	cache.policies["example.com"] = policy

	headers := map[string]string{
		"X-Original-Host":   "example.com",
		"X-Original-Uri":    "/path",
		"X-Original-Method": "GET",
	}

	decision, err := engine.Check(context.Background(), headers)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if decision.Status != http.StatusServiceUnavailable {
		t.Errorf("expected status 503, got %d", decision.Status)
	}
	if decision.Decision != "killswitch" {
		t.Errorf("expected decision 'killswitch', got '%s'", decision.Decision)
	}
	if decision.KillswitchHeaders["X-Killswitch-Rule"] != "test-rule" {
		t.Error("expected killswitch rule header")
	}
}

func TestEngine_Check_GatekeeperOnly_Unauth(t *testing.T) {
	// Create mock Gatekeeper server
	gkServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer gkServer.Close()

	cache := newMockCache()
	client := downstream.NewClient("http://localhost:9090", gkServer.URL, 1000, 1000)
	engine := NewEngine(cache, client, "", "")

	policy := &store.HostPolicy{
		Host:                "example.com",
		KillswitchRequired:  false,
		GatekeeperRequired:  true,
	}
	cache.policies["example.com"] = policy

	headers := map[string]string{
		"X-Original-Host":   "example.com",
		"X-Original-Uri":    "/path",
		"X-Original-Method": "GET",
	}

	decision, err := engine.Check(context.Background(), headers)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if decision.Status != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", decision.Status)
	}
	if decision.Decision != "unauth" {
		t.Errorf("expected decision 'unauth', got '%s'", decision.Decision)
	}
}

func TestEngine_Check_GatekeeperOnly_Forbid(t *testing.T) {
	// Create mock Gatekeeper server
	gkServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer gkServer.Close()

	cache := newMockCache()
	client := downstream.NewClient("http://localhost:9090", gkServer.URL, 1000, 1000)
	engine := NewEngine(cache, client, "", "")

	policy := &store.HostPolicy{
		Host:                "example.com",
		KillswitchRequired:  false,
		GatekeeperRequired:  true,
	}
	cache.policies["example.com"] = policy

	headers := map[string]string{
		"X-Original-Host":   "example.com",
		"X-Original-Uri":    "/path",
		"X-Original-Method": "GET",
	}

	decision, err := engine.Check(context.Background(), headers)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if decision.Status != http.StatusForbidden {
		t.Errorf("expected status 403, got %d", decision.Status)
	}
	if decision.Decision != "forbid" {
		t.Errorf("expected decision 'forbid', got '%s'", decision.Decision)
	}
}

func TestEngine_Check_GatekeeperOnly_Allow(t *testing.T) {
	// Create mock Gatekeeper server
	gkServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Identity-User-Id", "123")
		w.Header().Set("X-Identity-Email", "user@example.com")
		w.Header().Set("X-Identity-Groups", "admin,users")
		w.WriteHeader(http.StatusOK)
	}))
	defer gkServer.Close()

	cache := newMockCache()
	client := downstream.NewClient("http://localhost:9090", gkServer.URL, 1000, 1000)
	engine := NewEngine(cache, client, "", "")

	policy := &store.HostPolicy{
		Host:                "example.com",
		KillswitchRequired:  false,
		GatekeeperRequired:  true,
	}
	cache.policies["example.com"] = policy

	headers := map[string]string{
		"X-Original-Host":   "example.com",
		"X-Original-Uri":    "/path",
		"X-Original-Method": "GET",
	}

	decision, err := engine.Check(context.Background(), headers)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if decision.Status != http.StatusOK {
		t.Errorf("expected status 200, got %d", decision.Status)
	}
	if decision.Decision != "allow" {
		t.Errorf("expected decision 'allow', got '%s'", decision.Decision)
	}
	if decision.IdentityHeaders["X-Identity-User-Id"] != "123" {
		t.Error("expected identity user ID header")
	}
	if decision.IdentityHeaders["X-Identity-Email"] != "user@example.com" {
		t.Error("expected identity email header")
	}
	if decision.IdentityHeaders["X-Identity-Groups"] != "admin,users" {
		t.Error("expected identity groups header")
	}
}

func TestEngine_Check_BothChecks(t *testing.T) {
	// Create mock servers
	ksServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ksServer.Close()

	gkServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Identity-User-Id", "123")
		w.Header().Set("X-Identity-Email", "user@example.com")
		w.WriteHeader(http.StatusOK)
	}))
	defer gkServer.Close()

	cache := newMockCache()
	client := downstream.NewClient(ksServer.URL, gkServer.URL, 1000, 1000)
	engine := NewEngine(cache, client, "", "")

	policy := &store.HostPolicy{
		Host:                "example.com",
		KillswitchRequired:  true,
		GatekeeperRequired:  true,
	}
	cache.policies["example.com"] = policy

	headers := map[string]string{
		"X-Original-Host":   "example.com",
		"X-Original-Uri":    "/path",
		"X-Original-Method": "GET",
	}

	decision, err := engine.Check(context.Background(), headers)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if decision.Status != http.StatusOK {
		t.Errorf("expected status 200, got %d", decision.Status)
	}
	if decision.Decision != "allow" {
		t.Errorf("expected decision 'allow', got '%s'", decision.Decision)
	}
	if len(decision.IdentityHeaders) == 0 {
		t.Error("expected identity headers")
	}
}

func TestEngine_Check_KillswitchBlock_NoGatekeeperCall(t *testing.T) {
	// Create mock Killswitch server that blocks
	ksServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer ksServer.Close()

	// Gatekeeper should not be called
	gkCalled := false
	gkServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gkCalled = true
		w.WriteHeader(http.StatusOK)
	}))
	defer gkServer.Close()

	cache := newMockCache()
	client := downstream.NewClient(ksServer.URL, gkServer.URL, 1000, 1000)
	engine := NewEngine(cache, client, "", "")

	policy := &store.HostPolicy{
		Host:                "example.com",
		KillswitchRequired:  true,
		GatekeeperRequired:  true,
	}
	cache.policies["example.com"] = policy

	headers := map[string]string{
		"X-Original-Host":   "example.com",
		"X-Original-Uri":    "/path",
		"X-Original-Method": "GET",
	}

	decision, err := engine.Check(context.Background(), headers)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if decision.Status != http.StatusServiceUnavailable {
		t.Errorf("expected status 503, got %d", decision.Status)
	}
	if gkCalled {
		t.Error("gatekeeper should not be called when killswitch blocks")
	}
}

func TestEngine_Check_TraceID_Generation(t *testing.T) {
	cache := newMockCache()
	client := downstream.NewClient("http://localhost:9090", "http://localhost:3000", 1000, 1000)
	engine := NewEngine(cache, client, "", "")

	headers := map[string]string{
		"X-Original-Host":   "example.com",
		"X-Original-Uri":     "/path",
		"X-Original-Method":  "GET",
		// No X-Request-Id
	}

	decision, err := engine.Check(context.Background(), headers)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if decision.TraceID == "" {
		t.Error("expected trace ID to be generated")
	}
	if len(decision.TraceID) < 10 {
		t.Error("expected trace ID to be a reasonable length")
	}
}
