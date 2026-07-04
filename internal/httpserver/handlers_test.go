package httpserver

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
	"github.com/domostack/arbiter/internal/arbiter"
	"github.com/domostack/arbiter/internal/downstream"
	"github.com/domostack/arbiter/internal/policycache"
	"github.com/domostack/arbiter/internal/store"
	"github.com/domostack/arbiter/internal/telemetry"
	"github.com/rs/zerolog"
)

func setupTestHandlers(t *testing.T) (*Handlers, store.Store) {
	// Create temporary database
	tmpfile, err := os.CreateTemp("", "test-*.db")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	tmpfile.Close()

	dbPath := "sqlite:///" + tmpfile.Name()
	dbStore, err := store.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	testDB, err := sql.Open("sqlite", tmpfile.Name())
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	_, err = testDB.Exec(`
		CREATE TABLE IF NOT EXISTS host_policies (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			host TEXT NOT NULL UNIQUE,
			killswitch_required INTEGER NOT NULL,
			gatekeeper_required INTEGER NOT NULL,
			notes TEXT,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			managed INTEGER NOT NULL DEFAULT 0,
			managed_pack TEXT NULL,
			managed_key TEXT NULL,
			managed_version INTEGER NULL,
			managed_name TEXT NULL,
			managed_description TEXT NULL,
			managed_at TEXT NULL
		)
	`)
	testDB.Close()
	if err != nil {
		t.Fatalf("failed to create table: %v", err)
	}

	t.Cleanup(func() {
		dbStore.Close()
		os.Remove(tmpfile.Name())
	})

	cache := policycache.NewCache(dbStore, 600)
	client := downstream.NewClient("http://localhost:9090", "http://localhost:3000", 1000, 1000)
	engine := arbiter.NewEngine(cache, client, "", "")
	logger := zerolog.Nop()

	handlers := NewHandlers(engine, cache, dbStore, logger, "", "", telemetry.NoopPublisher{}, "")

	return handlers, dbStore
}

func insertTestPolicy(t *testing.T, dbStore store.Store, host string) int64 {
	policy := &store.HostPolicy{
		Host:                host,
		KillswitchRequired:  true,
		GatekeeperRequired:  false,
		Notes:               stringPtr("Test policy"),
	}
	created, err := dbStore.Create(policy)
	if err != nil {
		t.Fatalf("failed to create test policy: %v", err)
	}
	return created.ID
}

func stringPtr(s string) *string {
	return &s
}

func TestHandlers_HandleListPolicies(t *testing.T) {
	handlers, dbStore := setupTestHandlers(t)

	// Create a test policy
	insertTestPolicy(t, dbStore, "example.com")

	req := httptest.NewRequest("GET", "/api/v1/policies", nil)
	w := httptest.NewRecorder()

	handlers.HandleListPolicies(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var policies []store.HostPolicy
	if err := json.NewDecoder(w.Body).Decode(&policies); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(policies) < 1 {
		t.Errorf("expected at least 1 policy, got %d", len(policies))
	}
}

func TestHandlers_HandleCreatePolicy(t *testing.T) {
	handlers, _ := setupTestHandlers(t)

	policy := map[string]interface{}{
		"host":                 "test.com",
		"killswitch_required":  true,
		"gatekeeper_required":  false,
		"notes":                "Test policy",
	}

	body, _ := json.Marshal(policy)
	req := httptest.NewRequest("POST", "/api/v1/policies", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handlers.HandleCreatePolicy(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("expected status 201, got %d", w.Code)
	}

	var created store.HostPolicy
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if created.Host != "test.com" {
		t.Errorf("expected host 'test.com', got '%s'", created.Host)
	}
	if !created.KillswitchRequired {
		t.Error("expected killswitch_required to be true")
	}
}

func TestHandlers_HandleGetPolicy(t *testing.T) {
	handlers, dbStore := setupTestHandlers(t)

	// Create a test policy
	id := insertTestPolicy(t, dbStore, "example.com")

	req := httptest.NewRequest("GET", "/api/v1/policies/1", nil)
	w := httptest.NewRecorder()

	handlers.HandleGetPolicy(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var policy store.HostPolicy
	if err := json.NewDecoder(w.Body).Decode(&policy); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if policy.ID != id {
		t.Errorf("expected ID %d, got %d", id, policy.ID)
	}
}

func TestHandlers_HandleUpdatePolicy(t *testing.T) {
	handlers, dbStore := setupTestHandlers(t)

	// Create a test policy
	id := insertTestPolicy(t, dbStore, "example.com")

	update := map[string]interface{}{
		"host":                 "example.com",
		"killswitch_required":  false,
		"gatekeeper_required":  true,
		"notes":                "Updated policy",
	}

	body, _ := json.Marshal(update)
	req := httptest.NewRequest("PATCH", "/api/v1/policies/1", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handlers.HandleUpdatePolicy(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var updated store.HostPolicy
	if err := json.NewDecoder(w.Body).Decode(&updated); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if updated.ID != id {
		t.Errorf("expected ID %d, got %d", id, updated.ID)
	}
	if updated.KillswitchRequired {
		t.Error("expected killswitch_required to be false")
	}
	if !updated.GatekeeperRequired {
		t.Error("expected gatekeeper_required to be true")
	}
}

func TestHandlers_HandleDeletePolicy(t *testing.T) {
	handlers, dbStore := setupTestHandlers(t)

	// Create a test policy
	id := insertTestPolicy(t, dbStore, "example.com")

	req := httptest.NewRequest("DELETE", "/api/v1/policies/1", nil)
	w := httptest.NewRecorder()

	handlers.HandleDeletePolicy(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("expected status 204, got %d", w.Code)
	}

	// Verify it's deleted
	found, err := dbStore.GetByID(id)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found != nil {
		t.Error("expected policy to be deleted")
	}
}

func TestHandlers_HandleEffective(t *testing.T) {
	handlers, dbStore := setupTestHandlers(t)

	// Create a test policy
	insertTestPolicy(t, dbStore, "example.com")

	req := httptest.NewRequest("GET", "/api/v1/effective?host=example.com", nil)
	w := httptest.NewRecorder()

	handlers.HandleEffective(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var effective map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&effective); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if effective["host"] != "example.com" {
		t.Errorf("expected host 'example.com', got '%v'", effective["host"])
	}
}

func TestHandlers_HandleHealthz(t *testing.T) {
	handlers, _ := setupTestHandlers(t)

	t.Setenv("SERVICE_NAME", "arbiter")
	t.Setenv("GIT_REVISION", "abc1234def5678901234567890abcdef12345678")

	req := httptest.NewRequest("GET", "/healthz", nil)
	w := httptest.NewRecorder()

	handlers.HandleHealthz(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("expected application/json, got %q", ct)
	}

	var body map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to decode JSON: %v", err)
	}
	for _, key := range []string{"status", "service", "git_revision", "checks"} {
		if _, ok := body[key]; !ok {
			t.Errorf("missing required key %q", key)
		}
	}
	if body["status"] != "healthy" {
		t.Errorf("expected status healthy, got %v", body["status"])
	}
	if body["service"] != "arbiter" {
		t.Errorf("expected service arbiter, got %v", body["service"])
	}
}

func TestHandlers_HandleHealthz_MissingServiceName(t *testing.T) {
	handlers, _ := setupTestHandlers(t)

	t.Setenv("SERVICE_NAME", "")
	t.Setenv("GIT_REVISION", "abc1234def5678901234567890abcdef12345678")

	req := httptest.NewRequest("GET", "/healthz", nil)
	w := httptest.NewRecorder()

	handlers.HandleHealthz(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
	var body map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to decode JSON: %v", err)
	}
	if body["status"] != "degraded" {
		t.Errorf("expected degraded, got %v", body["status"])
	}
	if body["service"] != "unknown" {
		t.Errorf("expected unknown service, got %v", body["service"])
	}
}

func TestHandlers_HandleHealthz_DatabaseFailure(t *testing.T) {
	handlers, dbStore := setupTestHandlers(t)
	_ = dbStore.Close()

	t.Setenv("SERVICE_NAME", "arbiter")
	t.Setenv("GIT_REVISION", "abc1234def5678901234567890abcdef12345678")

	req := httptest.NewRequest("GET", "/healthz", nil)
	w := httptest.NewRecorder()

	handlers.HandleHealthz(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status 503, got %d", w.Code)
	}
	var body map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to decode JSON: %v", err)
	}
	if body["status"] != "unhealthy" {
		t.Errorf("expected unhealthy, got %v", body["status"])
	}
}

func TestHandlers_HandleReadyz(t *testing.T) {
	handlers, _ := setupTestHandlers(t)

	req := httptest.NewRequest("GET", "/readyz", nil)
	w := httptest.NewRecorder()

	handlers.HandleReadyz(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
}
