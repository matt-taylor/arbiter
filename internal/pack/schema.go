package pack

// PolicyPack represents the root structure of a policy pack YAML file
type PolicyPack struct {
	Version       int       `yaml:"version"`
	Pack          string    `yaml:"pack"`
	CommonDomains []string  `yaml:"common_domains"`
	Policies      []Policy  `yaml:"policies"`
}

// Policy represents a single policy entry in the pack
type Policy struct {
	Key                string   `yaml:"key"`
	Name               string   `yaml:"name"`
	Description        string   `yaml:"description,omitempty"`
	RequiredServices   []string `yaml:"required_services"`
	Hosts              []string `yaml:"hosts,omitempty"`
	ExpandCommonDomains bool    `yaml:"expand_common_domains,omitempty"`
	Subdomain          string   `yaml:"subdomain,omitempty"`
}

// ExpandedPolicy represents a policy expanded to a specific host
type ExpandedPolicy struct {
	Host                string
	KillswitchRequired  bool
	GatekeeperRequired  bool
	ManagedKey          string
	ManagedName         string
	ManagedDescription  string
	ManagedVersion      int
	ManagedPack         string
}
