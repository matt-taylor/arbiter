package policycache

import (
	"sync"
	"testing"
	"time"

	"github.com/domostack/arbiter/internal/store"
)

type mockStore struct {
	policies map[string]*store.HostPolicy
	mu       sync.Mutex
}

func (m *mockStore) GetByHost(host string) (*store.HostPolicy, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.policies[host], nil
}

func (m *mockStore) GetByID(id int64) (*store.HostPolicy, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, p := range m.policies {
		if p.ID == id {
			return p, nil
		}
	}
	return nil, nil
}

func (m *mockStore) List() ([]*store.HostPolicy, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	policies := make([]*store.HostPolicy, 0, len(m.policies))
	for _, p := range m.policies {
		policies = append(policies, p)
	}
	return policies, nil
}

func (m *mockStore) Create(policy *store.HostPolicy) (*store.HostPolicy, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	policy.ID = int64(len(m.policies) + 1)
	m.policies[policy.Host] = policy
	return policy, nil
}

func (m *mockStore) Update(id int64, policy *store.HostPolicy) (*store.HostPolicy, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, p := range m.policies {
		if p.ID == id {
			p.Host = policy.Host
			p.KillswitchRequired = policy.KillswitchRequired
			p.GatekeeperRequired = policy.GatekeeperRequired
			p.Notes = policy.Notes
			return p, nil
		}
	}
	return nil, nil
}

func (m *mockStore) Delete(id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for host, p := range m.policies {
		if p.ID == id {
			delete(m.policies, host)
			return nil
		}
	}
	return nil
}

func (m *mockStore) Close() error {
	return nil
}

func newMockStore() *mockStore {
	return &mockStore{
		policies: make(map[string]*store.HostPolicy),
	}
}

func TestCache_Get(t *testing.T) {
	mockStore := newMockStore()
	cache := NewCache(mockStore, 1*time.Minute)

	// Create a policy in store
	policy := &store.HostPolicy{
		Host:                "example.com",
		KillswitchRequired:  true,
		GatekeeperRequired:  false,
	}
	mockStore.Create(policy)

	// First get should load from store
	found, err := cache.Get("example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found == nil {
		t.Fatal("expected to find policy")
	}
	if found.Host != "example.com" {
		t.Errorf("expected host 'example.com', got '%s'", found.Host)
	}

	// Second get should use cache
	found2, err := cache.Get("example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found2 == nil {
		t.Fatal("expected to find policy")
	}
	if found2.Host != found.Host {
		t.Error("expected same policy from cache")
	}
}

func TestCache_Get_NotFound(t *testing.T) {
	mockStore := newMockStore()
	cache := NewCache(mockStore, 1*time.Minute)

	// Get non-existent policy
	found, err := cache.Get("nonexistent.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found != nil {
		t.Error("expected nil for non-existent policy")
	}
}

func TestCache_TTL_Expiration(t *testing.T) {
	mockStore := newMockStore()
	cache := NewCache(mockStore, 100*time.Millisecond)

	// Create a policy
	policy := &store.HostPolicy{
		Host:                "example.com",
		KillswitchRequired:  true,
		GatekeeperRequired:  false,
	}
	mockStore.Create(policy)

	// First get loads from store
	_, err := cache.Get("example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Wait for TTL to expire
	time.Sleep(150 * time.Millisecond)

	// Update policy in store
	updated := &store.HostPolicy{
		Host:                "example.com",
		KillswitchRequired:  false,
		GatekeeperRequired:  true,
	}
	mockStore.Update(1, updated)

	// Get should reload from store
	found, err := cache.Get("example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found == nil {
		t.Fatal("expected to find policy")
	}
	if found.KillswitchRequired {
		t.Error("expected killswitch_required to be false after reload")
	}
}

func TestCache_Invalidate(t *testing.T) {
	mockStore := newMockStore()
	cache := NewCache(mockStore, 1*time.Minute)

	// Create and cache a policy
	policy := &store.HostPolicy{
		Host:                "example.com",
		KillswitchRequired:  true,
		GatekeeperRequired:  false,
	}
	mockStore.Create(policy)
	cache.Get("example.com")

	// Invalidate cache
	cache.Invalidate()

	// Update policy in store
	updated := &store.HostPolicy{
		Host:                "example.com",
		KillswitchRequired:  false,
		GatekeeperRequired:  true,
	}
	mockStore.Update(1, updated)

	// Get should reload from store (cache was invalidated)
	found, err := cache.Get("example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found == nil {
		t.Fatal("expected to find policy")
	}
	if found.KillswitchRequired {
		t.Error("expected killswitch_required to be false after invalidation and reload")
	}
}

func TestCache_Concurrency(t *testing.T) {
	mockStore := newMockStore()
	cache := NewCache(mockStore, 1*time.Minute)

	// Create multiple policies
	for i := 0; i < 10; i++ {
		policy := &store.HostPolicy{
			Host:                "example.com",
			KillswitchRequired:  true,
			GatekeeperRequired:  false,
		}
		mockStore.Create(policy)
	}

	// Concurrent reads
	var wg sync.WaitGroup
	numGoroutines := 100
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			_, err := cache.Get("example.com")
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		}()
	}

	wg.Wait()
}

func TestCache_GetAll(t *testing.T) {
	mockStore := newMockStore()
	cache := NewCache(mockStore, 1*time.Minute)

	// Create multiple policies
	hosts := []string{"example.com", "test.com", "demo.com"}
	for _, host := range hosts {
		policy := &store.HostPolicy{
			Host:                host,
			KillswitchRequired:  true,
			GatekeeperRequired:  false,
		}
		mockStore.Create(policy)
	}

	policies, err := cache.GetAll()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(policies) != len(hosts) {
		t.Errorf("expected %d policies, got %d", len(hosts), len(policies))
	}
}
