package pack

import (
	"database/sql"
	"os"
	"testing"

	_ "modernc.org/sqlite"
	"github.com/domostack/arbiter/internal/policycache"
	"github.com/domostack/arbiter/internal/store"
)

// setupTestStore creates a test store with the full schema including managed fields
func setupTestStore(t *testing.T) *store.SQLiteStore {
	tmpfile, err := os.CreateTemp("", "test-*.db")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	tmpfile.Close()

	dbPath := "sqlite:///" + tmpfile.Name()
	s, err := store.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	// Create table with managed fields using a separate SQLite connection
	// since we can't access the private db field from another package
	testDB, err := sql.Open("sqlite", tmpfile.Name())
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	defer testDB.Close()

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
	if err != nil {
		t.Fatalf("failed to create table: %v", err)
	}

	t.Cleanup(func() {
		s.Close()
		os.Remove(tmpfile.Name())
	})

	return s
}

func TestApplyPack_CreatesManagedRows(t *testing.T) {
	s := setupTestStore(t)
	cache := policycache.NewCache(s, 0)

	pack := &PolicyPack{
		Version:       1,
		Pack:          "test-pack",
		CommonDomains: []string{"example.com"},
		Policies: []Policy{
			{
				Key:              "test-policy",
				Name:             "Test Policy",
				RequiredServices: []string{"killswitch"},
				Hosts:            []string{"test.example.com"},
			},
		},
	}

	err := ApplyPack(s, cache, pack, "", "")
	if err != nil {
		t.Fatalf("failed to apply pack: %v", err)
	}

	// Verify policy was created
	policy, err := s.GetByHost("test.example.com")
	if err != nil {
		t.Fatalf("failed to get policy: %v", err)
	}
	if policy == nil {
		t.Fatal("policy was not created")
	}
	if !policy.Managed {
		t.Error("policy should be marked as managed")
	}
	if policy.ManagedPack == nil || *policy.ManagedPack != "test-pack" {
		t.Error("policy should have correct managed_pack")
	}
	if !policy.KillswitchRequired {
		t.Error("policy should require killswitch")
	}
}

func TestApplyPack_IsIdempotent(t *testing.T) {
	s := setupTestStore(t)
	cache := policycache.NewCache(s, 0)

	pack := &PolicyPack{
		Version:       1,
		Pack:          "test-pack",
		CommonDomains: []string{"example.com"},
		Policies: []Policy{
			{
				Key:              "test-policy",
				Name:             "Test Policy",
				RequiredServices: []string{"killswitch"},
				Hosts:            []string{"test.example.com"},
			},
		},
	}

	// Apply first time
	err := ApplyPack(s, cache, pack, "", "")
	if err != nil {
		t.Fatalf("failed to apply pack first time: %v", err)
	}

	firstPolicy, _ := s.GetByHost("test.example.com")
	firstID := firstPolicy.ID

	// Apply second time (should be idempotent)
	err = ApplyPack(s, cache, pack, "", "")
	if err != nil {
		t.Fatalf("failed to apply pack second time: %v", err)
	}

	secondPolicy, _ := s.GetByHost("test.example.com")
	if secondPolicy.ID != firstID {
		t.Error("policy ID should not change on re-apply")
	}
}

func TestApplyPack_UpdatesRequiredServices(t *testing.T) {
	s := setupTestStore(t)
	cache := policycache.NewCache(s, 0)

	pack1 := &PolicyPack{
		Version:       1,
		Pack:          "test-pack",
		CommonDomains: []string{"example.com"},
		Policies: []Policy{
			{
				Key:              "test-policy",
				Name:             "Test Policy",
				RequiredServices: []string{"killswitch"},
				Hosts:            []string{"test.example.com"},
			},
		},
	}

	// Apply with only killswitch
	err := ApplyPack(s, cache, pack1, "", "")
	if err != nil {
		t.Fatalf("failed to apply pack: %v", err)
	}

	policy, _ := s.GetByHost("test.example.com")
	if !policy.KillswitchRequired || policy.GatekeeperRequired {
		t.Error("initial policy should only require killswitch")
	}

	// Update pack to require both
	pack2 := &PolicyPack{
		Version:       2,
		Pack:          "test-pack",
		CommonDomains: []string{"example.com"},
		Policies: []Policy{
			{
				Key:              "test-policy",
				Name:             "Test Policy",
				RequiredServices: []string{"killswitch", "gatekeeper"},
				Hosts:            []string{"test.example.com"},
			},
		},
	}

	err = ApplyPack(s, cache, pack2, "", "")
	if err != nil {
		t.Fatalf("failed to apply updated pack: %v", err)
	}

	policy, _ = s.GetByHost("test.example.com")
	if !policy.KillswitchRequired || !policy.GatekeeperRequired {
		t.Error("updated policy should require both services")
	}
}

func TestApplyPack_DeletesRemovedPolicies(t *testing.T) {
	s := setupTestStore(t)
	cache := policycache.NewCache(s, 0)

	pack1 := &PolicyPack{
		Version:       1,
		Pack:          "test-pack",
		CommonDomains: []string{"example.com"},
		Policies: []Policy{
			{
				Key:              "policy-1",
				Name:             "Policy 1",
				RequiredServices: []string{"killswitch"},
				Hosts:            []string{"host1.example.com"},
			},
			{
				Key:              "policy-2",
				Name:             "Policy 2",
				RequiredServices: []string{"killswitch"},
				Hosts:            []string{"host2.example.com"},
			},
		},
	}

	// Apply with two policies
	err := ApplyPack(s, cache, pack1, "", "")
	if err != nil {
		t.Fatalf("failed to apply pack: %v", err)
	}

	// Verify both exist
	if p1, _ := s.GetByHost("host1.example.com"); p1 == nil {
		t.Error("host1 should exist")
	}
	if p2, _ := s.GetByHost("host2.example.com"); p2 == nil {
		t.Error("host2 should exist")
	}

	// Apply with only one policy
	pack2 := &PolicyPack{
		Version:       2,
		Pack:          "test-pack",
		CommonDomains: []string{"example.com"},
		Policies: []Policy{
			{
				Key:              "policy-1",
				Name:             "Policy 1",
				RequiredServices: []string{"killswitch"},
				Hosts:            []string{"host1.example.com"},
			},
		},
	}

	err = ApplyPack(s, cache, pack2, "", "")
	if err != nil {
		t.Fatalf("failed to apply updated pack: %v", err)
	}

	// Verify host1 still exists, host2 is deleted
	if p1, _ := s.GetByHost("host1.example.com"); p1 == nil {
		t.Error("host1 should still exist")
	}
	if p2, _ := s.GetByHost("host2.example.com"); p2 != nil {
		t.Error("host2 should be deleted")
	}
}

func TestApplyPack_CollisionWithUnmanaged(t *testing.T) {
	s := setupTestStore(t)
	cache := policycache.NewCache(s, 0)

	// Create unmanaged policy
	unmanaged := &store.HostPolicy{
		Host:               "test.example.com",
		KillswitchRequired: false,
		GatekeeperRequired: false,
		Managed:            false,
	}
	_, err := s.Create(unmanaged)
	if err != nil {
		t.Fatalf("failed to create unmanaged policy: %v", err)
	}

	pack := &PolicyPack{
		Version:       1,
		Pack:          "test-pack",
		CommonDomains: []string{"example.com"},
		Policies: []Policy{
			{
				Key:              "test-policy",
				Name:             "Test Policy",
				RequiredServices: []string{"killswitch"},
				Hosts:            []string{"test.example.com"},
			},
		},
	}

	err = ApplyPack(s, cache, pack, "", "")
	if err == nil {
		t.Fatal("should fail with collision error")
	}
	if err.Error() == "" || !contains(err.Error(), "unmanaged") {
		t.Errorf("error should mention unmanaged policy, got: %v", err)
	}
}

func TestApplyPack_CollisionWithDifferentPack(t *testing.T) {
	s := setupTestStore(t)
	cache := policycache.NewCache(s, 0)

	// Create managed policy from different pack
	pack1Name := "pack-1"
	pack1 := &PolicyPack{
		Version:       1,
		Pack:          pack1Name,
		CommonDomains: []string{"example.com"},
		Policies: []Policy{
			{
				Key:              "test-policy",
				Name:             "Test Policy",
				RequiredServices: []string{"killswitch"},
				Hosts:            []string{"test.example.com"},
			},
		},
	}

	err := ApplyPack(s, cache, pack1, "", "")
	if err != nil {
		t.Fatalf("failed to apply first pack: %v", err)
	}

	// Try to apply different pack with same host
	pack2 := &PolicyPack{
		Version:       1,
		Pack:          "pack-2",
		CommonDomains: []string{"example.com"},
		Policies: []Policy{
			{
				Key:              "test-policy",
				Name:             "Test Policy",
				RequiredServices: []string{"killswitch"},
				Hosts:            []string{"test.example.com"},
			},
		},
	}

	err = ApplyPack(s, cache, pack2, "", "")
	if err == nil {
		t.Fatal("should fail with collision error")
	}
	if err.Error() == "" || !contains(err.Error(), "managed by pack") {
		t.Errorf("error should mention different pack, got: %v", err)
	}
}

func TestApplyPack_AntiRecursionConstraints(t *testing.T) {
	s := setupTestStore(t)
	cache := policycache.NewCache(s, 0)

	pack := &PolicyPack{
		Version:       1,
		Pack:          "test-pack",
		CommonDomains: []string{"example.com"},
		Policies: []Policy{
			{
				Key:              "killswitch-policy",
				Name:             "Killswitch Policy",
				RequiredServices: []string{"killswitch", "gatekeeper"},
				Hosts:            []string{"killswitch.example.com"},
			},
		},
	}

	// Apply with killswitch host - should force killswitch_required=false
	err := ApplyPack(s, cache, pack, "killswitch.example.com", "")
	if err != nil {
		t.Fatalf("failed to apply pack: %v", err)
	}

	policy, _ := s.GetByHost("killswitch.example.com")
	if policy.KillswitchRequired {
		t.Error("killswitch_required should be forced to false for killswitch host")
	}
	if !policy.GatekeeperRequired {
		t.Error("gatekeeper_required should still be true")
	}

	// Test gatekeeper host
	pack2 := &PolicyPack{
		Version:       1,
		Pack:          "test-pack-2",
		CommonDomains: []string{"example.com"},
		Policies: []Policy{
			{
				Key:              "gatekeeper-policy",
				Name:             "Gatekeeper Policy",
				RequiredServices: []string{"killswitch", "gatekeeper"},
				Hosts:            []string{"gatekeeper.example.com"},
			},
		},
	}

	err = ApplyPack(s, cache, pack2, "", "gatekeeper.example.com")
	if err != nil {
		t.Fatalf("failed to apply pack: %v", err)
	}

	policy2, _ := s.GetByHost("gatekeeper.example.com")
	if policy2.GatekeeperRequired {
		t.Error("gatekeeper_required should be forced to false for gatekeeper host")
	}
	if !policy2.KillswitchRequired {
		t.Error("killswitch_required should still be true")
	}
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
