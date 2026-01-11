package pack

import (
	"fmt"
	"strings"
)

// ExpandPolicies expands a PolicyPack into a list of ExpandedPolicy entries
// Each policy entry is expanded to one or more host-level policies
func ExpandPolicies(pack *PolicyPack, killswitchPublicHost, gatekeeperPublicHost string) ([]ExpandedPolicy, error) {
	var expanded []ExpandedPolicy

	ksHostLower := strings.ToLower(killswitchPublicHost)
	gkHostLower := strings.ToLower(gatekeeperPublicHost)

	for _, policy := range pack.Policies {
		var hosts []string

		// Determine hosts for this policy
		if len(policy.Hosts) > 0 {
			// Use explicit hosts
			hosts = policy.Hosts
		} else if policy.ExpandCommonDomains {
			// Expand common domains with subdomain
			for _, domain := range pack.CommonDomains {
				host := fmt.Sprintf("%s.%s", policy.Subdomain, domain)
				hosts = append(hosts, host)
			}
		}

		// Determine required services
		killswitchRequired := false
		gatekeeperRequired := false
		for _, service := range policy.RequiredServices {
			switch strings.ToLower(service) {
			case "killswitch":
				killswitchRequired = true
			case "gatekeeper":
				gatekeeperRequired = true
			}
		}

		// Create expanded policy for each host
		for _, host := range hosts {
			hostLower := strings.ToLower(host)

			// Apply anti-recursion constraints
			ksRequired := killswitchRequired
			gkRequired := gatekeeperRequired

			if hostLower == ksHostLower {
				ksRequired = false
			}
			if hostLower == gkHostLower {
				gkRequired = false
			}

			expanded = append(expanded, ExpandedPolicy{
				Host:               hostLower,
				KillswitchRequired: ksRequired,
				GatekeeperRequired: gkRequired,
				ManagedKey:         policy.Key,
				ManagedName:        policy.Name,
				ManagedDescription: policy.Description,
				ManagedVersion:     pack.Version,
				ManagedPack:        pack.Pack,
			})
		}
	}

	return expanded, nil
}
