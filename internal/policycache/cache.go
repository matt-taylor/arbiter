package policycache

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/domostack/arbiter/internal/store"
)

// cacheState represents an immutable cache state
type cacheState struct {
	expiresAt time.Time
	policies  map[string]*store.HostPolicy
}

// Cache provides thread-safe caching of host policies
type Cache struct {
	state    atomic.Pointer[cacheState]
	store    store.Store
	ttl      time.Duration
	reloadMu sync.Mutex
}


// NewCache creates a new policy cache
func NewCache(s store.Store, ttl time.Duration) *Cache {
	return &Cache{
		store: s,
		ttl:   ttl,
	}
}

// Get retrieves a policy by host, loading from store if cache miss or expired
func (c *Cache) Get(host string) (*store.HostPolicy, error) {
	state := c.state.Load()

	// Check if cache is valid
	if state != nil && time.Now().Before(state.expiresAt) {
		if policy, ok := state.policies[host]; ok {
			return policy, nil
		}
		// Policy not in cache, but cache is valid - return nil (no policy)
		return nil, nil
	}

	// Cache miss or expired - reload
	return c.reloadAndGet(host)
}

// reloadAndGet reloads the cache and returns the requested policy
func (c *Cache) reloadAndGet(host string) (*store.HostPolicy, error) {
	// Single-flight: only one goroutine reloads at a time
	c.reloadMu.Lock()
	defer c.reloadMu.Unlock()

	// Double-check: another goroutine might have reloaded while we waited
	state := c.state.Load()
	if state != nil && time.Now().Before(state.expiresAt) {
		if policy, ok := state.policies[host]; ok {
			return policy, nil
		}
		return nil, nil
	}

	// Load all policies from store
	policies, err := c.store.List()
	if err != nil {
		return nil, err
	}

	// Build new cache state
	policiesMap := make(map[string]*store.HostPolicy, len(policies))
	for _, policy := range policies {
		policiesMap[policy.Host] = policy
	}

	newState := &cacheState{
		expiresAt: time.Now().Add(c.ttl),
		policies:  policiesMap,
	}

	// Atomically swap the cache state
	c.state.Store(newState)

	// Return the requested policy
	if policy, ok := policiesMap[host]; ok {
		return policy, nil
	}
	return nil, nil
}

// Invalidate marks the cache as invalid (next read will reload)
func (c *Cache) Invalidate() {
	// Atomically set to nil/expired state
	c.state.Store(nil)
}

// GetAll returns all policies from cache (or reloads if needed)
func (c *Cache) GetAll() ([]*store.HostPolicy, error) {
	state := c.state.Load()

	// Check if cache is valid
	if state != nil && time.Now().Before(state.expiresAt) {
		policies := make([]*store.HostPolicy, 0, len(state.policies))
		for _, policy := range state.policies {
			policies = append(policies, policy)
		}
		return policies, nil
	}

	// Cache miss or expired - reload
	c.reloadMu.Lock()
	defer c.reloadMu.Unlock()

	// Double-check
	state = c.state.Load()
	if state != nil && time.Now().Before(state.expiresAt) {
		policies := make([]*store.HostPolicy, 0, len(state.policies))
		for _, policy := range state.policies {
			policies = append(policies, policy)
		}
		return policies, nil
	}

	// Load all policies from store
	policies, err := c.store.List()
	if err != nil {
		return nil, err
	}

	// Build new cache state
	policiesMap := make(map[string]*store.HostPolicy, len(policies))
	for _, policy := range policies {
		policiesMap[policy.Host] = policy
	}

	newState := &cacheState{
		expiresAt: time.Now().Add(c.ttl),
		policies:  policiesMap,
	}

	// Atomically swap the cache state
	c.state.Store(newState)

	return policies, nil
}
