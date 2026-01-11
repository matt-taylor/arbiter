package pack

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// ParsePackFile parses a YAML policy pack file and returns a validated PolicyPack
func ParsePackFile(filePath string) (*PolicyPack, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	var pack PolicyPack
	if err := yaml.Unmarshal(data, &pack); err != nil {
		return nil, fmt.Errorf("failed to parse YAML: %w", err)
	}

	if err := ValidatePack(&pack); err != nil {
		return nil, fmt.Errorf("validation failed: %w", err)
	}

	return &pack, nil
}

// ValidatePack validates a PolicyPack structure
func ValidatePack(pack *PolicyPack) error {
	if pack.Version < 1 {
		return fmt.Errorf("version must be >= 1, got %d", pack.Version)
	}

	if pack.Pack == "" {
		return fmt.Errorf("pack name is required")
	}

	// Validate each policy
	for i, policy := range pack.Policies {
		if err := ValidatePolicy(&policy, pack); err != nil {
			return fmt.Errorf("policy[%d]: %w", i, err)
		}
	}

	return nil
}

// ValidatePolicy validates a single Policy entry
func ValidatePolicy(policy *Policy, pack *PolicyPack) error {
	if policy.Key == "" {
		return fmt.Errorf("key is required")
	}

	if policy.Name == "" {
		return fmt.Errorf("name is required")
	}

	// Validate service names (empty array is allowed - means no services required)
	validServices := map[string]bool{
		"killswitch": true,
		"gatekeeper": true,
	}
	for _, service := range policy.RequiredServices {
		if !validServices[strings.ToLower(service)] {
			return fmt.Errorf("invalid required_service: %s (must be 'killswitch' or 'gatekeeper')", service)
		}
	}

	// Validate host specification
	hasHosts := len(policy.Hosts) > 0
	hasExpand := policy.ExpandCommonDomains

	if !hasHosts && !hasExpand {
		return fmt.Errorf("must specify either 'hosts' or 'expand_common_domains: true'")
	}

	if hasHosts && hasExpand {
		return fmt.Errorf("cannot specify both 'hosts' and 'expand_common_domains'")
	}

	if hasExpand {
		if len(pack.CommonDomains) == 0 {
			return fmt.Errorf("expand_common_domains requires 'common_domains' to be defined in pack")
		}
		if policy.Subdomain == "" {
			return fmt.Errorf("subdomain is required when expand_common_domains is true")
		}
	}

	return nil
}
