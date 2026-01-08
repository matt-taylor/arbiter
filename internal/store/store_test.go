package store

import (
	"os"
	"testing"
)

func setupTestDB(t *testing.T) *SQLiteStore {
	// Create temporary database
	tmpfile, err := os.CreateTemp("", "test-*.db")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	tmpfile.Close()

	dbPath := "sqlite:///" + tmpfile.Name()
	store, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	// Create table manually for testing
	_, err = store.db.Exec(`
		CREATE TABLE IF NOT EXISTS host_policies (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			host TEXT NOT NULL UNIQUE,
			killswitch_required INTEGER NOT NULL,
			gatekeeper_required INTEGER NOT NULL,
			notes TEXT,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)
	`)
	if err != nil {
		t.Fatalf("failed to create table: %v", err)
	}

	t.Cleanup(func() {
		store.Close()
		os.Remove(tmpfile.Name())
	})

	return store
}

func TestStore_Create(t *testing.T) {
	store := setupTestDB(t)

	policy := &HostPolicy{
		Host:                "example.com",
		KillswitchRequired:  true,
		GatekeeperRequired:  false,
		Notes:               stringPtr("Test policy"),
	}

	created, err := store.Create(policy)
	if err != nil {
		t.Fatalf("failed to create policy: %v", err)
	}

	if created.ID == 0 {
		t.Error("expected non-zero ID")
	}
	if created.Host != "example.com" {
		t.Errorf("expected host 'example.com', got '%s'", created.Host)
	}
	if !created.KillswitchRequired {
		t.Error("expected killswitch_required to be true")
	}
	if created.GatekeeperRequired {
		t.Error("expected gatekeeper_required to be false")
	}
	if created.Notes == nil || *created.Notes != "Test policy" {
		t.Error("expected notes to be set")
	}
}

func TestStore_GetByHost(t *testing.T) {
	store := setupTestDB(t)

	// Create a policy
	policy := &HostPolicy{
		Host:                "example.com",
		KillswitchRequired:  true,
		GatekeeperRequired:  false,
	}
	created, err := store.Create(policy)
	if err != nil {
		t.Fatalf("failed to create policy: %v", err)
	}

	// Get by host (case-insensitive)
	found, err := store.GetByHost("EXAMPLE.COM")
	if err != nil {
		t.Fatalf("failed to get policy: %v", err)
	}

	if found == nil {
		t.Fatal("expected to find policy")
	}
	if found.ID != created.ID {
		t.Errorf("expected ID %d, got %d", created.ID, found.ID)
	}

	// Test non-existent host
	notFound, err := store.GetByHost("nonexistent.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if notFound != nil {
		t.Error("expected nil for non-existent host")
	}
}

func TestStore_GetByID(t *testing.T) {
	store := setupTestDB(t)

	policy := &HostPolicy{
		Host:                "example.com",
		KillswitchRequired:  true,
		GatekeeperRequired:  false,
	}
	created, err := store.Create(policy)
	if err != nil {
		t.Fatalf("failed to create policy: %v", err)
	}

	found, err := store.GetByID(created.ID)
	if err != nil {
		t.Fatalf("failed to get policy: %v", err)
	}

	if found == nil {
		t.Fatal("expected to find policy")
	}
	if found.ID != created.ID {
		t.Errorf("expected ID %d, got %d", created.ID, found.ID)
	}
}

func TestStore_List(t *testing.T) {
	store := setupTestDB(t)

	// Create multiple policies
	hosts := []string{"example.com", "test.com", "demo.com"}
	for _, host := range hosts {
		policy := &HostPolicy{
			Host:                host,
			KillswitchRequired:  true,
			GatekeeperRequired:  false,
		}
		_, err := store.Create(policy)
		if err != nil {
			t.Fatalf("failed to create policy: %v", err)
		}
	}

	policies, err := store.List()
	if err != nil {
		t.Fatalf("failed to list policies: %v", err)
	}

	if len(policies) != len(hosts) {
		t.Errorf("expected %d policies, got %d", len(hosts), len(policies))
	}
}

func TestStore_Update(t *testing.T) {
	store := setupTestDB(t)

	policy := &HostPolicy{
		Host:                "example.com",
		KillswitchRequired:  true,
		GatekeeperRequired:  false,
	}
	created, err := store.Create(policy)
	if err != nil {
		t.Fatalf("failed to create policy: %v", err)
	}

	// Update policy
	update := &HostPolicy{
		Host:                "example.com",
		KillswitchRequired:  false,
		GatekeeperRequired:  true,
		Notes:               stringPtr("Updated notes"),
	}

	updated, err := store.Update(created.ID, update)
	if err != nil {
		t.Fatalf("failed to update policy: %v", err)
	}

	if updated.KillswitchRequired {
		t.Error("expected killswitch_required to be false")
	}
	if !updated.GatekeeperRequired {
		t.Error("expected gatekeeper_required to be true")
	}
	if updated.Notes == nil || *updated.Notes != "Updated notes" {
		t.Error("expected notes to be updated")
	}
}

func TestStore_Delete(t *testing.T) {
	store := setupTestDB(t)

	policy := &HostPolicy{
		Host:                "example.com",
		KillswitchRequired:  true,
		GatekeeperRequired:  false,
	}
	created, err := store.Create(policy)
	if err != nil {
		t.Fatalf("failed to create policy: %v", err)
	}

	// Delete policy
	err = store.Delete(created.ID)
	if err != nil {
		t.Fatalf("failed to delete policy: %v", err)
	}

	// Verify it's gone
	found, err := store.GetByID(created.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found != nil {
		t.Error("expected policy to be deleted")
	}
}

func TestStore_UniqueConstraint(t *testing.T) {
	store := setupTestDB(t)

	policy1 := &HostPolicy{
		Host:                "example.com",
		KillswitchRequired:  true,
		GatekeeperRequired:  false,
	}
	_, err := store.Create(policy1)
	if err != nil {
		t.Fatalf("failed to create first policy: %v", err)
	}

	// Try to create duplicate
	policy2 := &HostPolicy{
		Host:                "example.com",
		KillswitchRequired:  false,
		GatekeeperRequired:  true,
	}
	_, err = store.Create(policy2)
	if err == nil {
		t.Error("expected error for duplicate host")
	}
}

func stringPtr(s string) *string {
	return &s
}
