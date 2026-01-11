package pack

import (
	"fmt"
	"time"

	"github.com/domostack/arbiter/internal/policycache"
	"github.com/domostack/arbiter/internal/store"
)

// ApplyPack applies a policy pack to the database
// It performs idempotent upserts and deletes managed policies not in the pack
func ApplyPack(
	s store.Store,
	cache *policycache.Cache,
	pack *PolicyPack,
	killswitchPublicHost,
	gatekeeperPublicHost string,
) error {
	// Get the underlying SQLite store for transaction support
	sqliteStore, ok := s.(*store.SQLiteStore)
	if !ok {
		return fmt.Errorf("store must be SQLiteStore for pack apply")
	}

	// Expand policies
	expanded, err := ExpandPolicies(pack, killswitchPublicHost, gatekeeperPublicHost)
	if err != nil {
		return fmt.Errorf("failed to expand policies: %w", err)
	}

	// Build desired hosts map for quick lookup
	desiredHosts := make(map[string]ExpandedPolicy)
	for _, exp := range expanded {
		desiredHosts[exp.Host] = exp
	}

	// Start transaction
	tx, err := sqliteStore.BeginTx()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Load existing managed policies for this pack
	existing, err := sqliteStore.GetManagedByPackTx(tx, pack.Pack)
	if err != nil {
		return fmt.Errorf("failed to load existing managed policies: %w", err)
	}

	existingHosts := make(map[string]*store.HostPolicy)
	for _, pol := range existing {
		existingHosts[pol.Host] = pol
	}

	now := time.Now().UTC().Format(time.RFC3339)

	// Upsert desired policies
	for _, exp := range expanded {
		existing, exists := existingHosts[exp.Host]

		if exists {
			// Check for collisions
			if !existing.Managed {
				return fmt.Errorf("host %s already exists as unmanaged policy, cannot apply pack", exp.Host)
			}
			if existing.ManagedPack != nil && *existing.ManagedPack != pack.Pack {
				return fmt.Errorf("host %s is managed by pack %s, cannot apply pack %s", exp.Host, *existing.ManagedPack, pack.Pack)
			}

			// Update existing managed policy
			err := sqliteStore.UpdateManagedPolicyTx(tx, existing.ID, &store.HostPolicy{
				KillswitchRequired: exp.KillswitchRequired,
				GatekeeperRequired:  exp.GatekeeperRequired,
				Notes:               stringPtrOrNil(exp.ManagedDescription), // Show description as notes in UI
				Managed:             true,
				ManagedPack:         &pack.Pack,
				ManagedKey:          &exp.ManagedKey,
				ManagedVersion:      &pack.Version,
				ManagedName:         &exp.ManagedName,
				ManagedDescription:  stringPtrOrNil(exp.ManagedDescription),
				ManagedAt:           &now,
			})
			if err != nil {
				return fmt.Errorf("failed to update policy for host %s: %w", exp.Host, err)
			}
		} else {
			// Check if host exists
			existingByHost, err := sqliteStore.GetByHostTx(tx, exp.Host)
			if err != nil {
				return fmt.Errorf("failed to check existing host: %w", err)
			}
			
			if existingByHost != nil {
				// Host exists - check what to do
				if !existingByHost.Managed {
					return fmt.Errorf("host %s already exists as unmanaged policy, cannot apply pack", exp.Host)
				}
				if existingByHost.ManagedPack != nil && *existingByHost.ManagedPack != pack.Pack {
					return fmt.Errorf("host %s is managed by pack %s, cannot apply pack %s", exp.Host, *existingByHost.ManagedPack, pack.Pack)
				}
				
				// Host exists and is managed by this pack (but wasn't in existingHosts map - should UPDATE)
				// This can happen if the managed_key changed or there's a data inconsistency
				err := sqliteStore.UpdateManagedPolicyTx(tx, existingByHost.ID, &store.HostPolicy{
					KillswitchRequired: exp.KillswitchRequired,
					GatekeeperRequired:  exp.GatekeeperRequired,
					Notes:               stringPtrOrNil(exp.ManagedDescription), // Show description as notes in UI
					Managed:             true,
					ManagedPack:         &pack.Pack,
					ManagedKey:          &exp.ManagedKey,
					ManagedVersion:      &pack.Version,
					ManagedName:         &exp.ManagedName,
					ManagedDescription:  stringPtrOrNil(exp.ManagedDescription),
					ManagedAt:           &now,
				})
				if err != nil {
					return fmt.Errorf("failed to update policy for host %s: %w", exp.Host, err)
				}
				continue
			}

			// Host doesn't exist - insert new managed policy
			err = sqliteStore.CreateManagedPolicyTx(tx, &store.HostPolicy{
				Host:                exp.Host,
				KillswitchRequired: exp.KillswitchRequired,
				GatekeeperRequired:  exp.GatekeeperRequired,
				Notes:               stringPtrOrNil(exp.ManagedDescription), // Show description as notes in UI
				Managed:             true,
				ManagedPack:         &pack.Pack,
				ManagedKey:          &exp.ManagedKey,
				ManagedVersion:      &pack.Version,
				ManagedName:         &exp.ManagedName,
				ManagedDescription:  stringPtrOrNil(exp.ManagedDescription),
				ManagedAt:           &now,
			})
			if err != nil {
				return fmt.Errorf("failed to create policy for host %s: %w", exp.Host, err)
			}
		}
	}

	// Reconcile deletions: delete managed policies for this pack not in desired set
	for host, existing := range existingHosts {
		if _, inDesired := desiredHosts[host]; !inDesired {
			err := sqliteStore.DeleteTx(tx, existing.ID)
			if err != nil {
				return fmt.Errorf("failed to delete policy for host %s: %w", host, err)
			}
		}
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	// Invalidate cache
	cache.Invalidate()

	return nil
}

func stringPtrOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
