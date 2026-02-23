package security

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// ProjectSecurityConfig represents a project-level .security.yaml file.
type ProjectSecurityConfig struct {
	Rules     []CustomRule `yaml:"rules"`
	Overrides []Override   `yaml:"overrides"`
	CI        CIConfig     `yaml:"ci"`
}

// CustomRule defines a project-specific regex-based security rule.
type CustomRule struct {
	ID       string `yaml:"id"`
	Pattern  string `yaml:"pattern"`
	Severity string `yaml:"severity"`
	Title    string `yaml:"title"`
	Category string `yaml:"category"`
}

// Override changes the severity of a specific finding by ID.
type Override struct {
	FindingID string `yaml:"finding_id"`
	Severity  string `yaml:"severity"`
	Reason    string `yaml:"reason"`
}

// CIConfig holds CI/CD-specific settings from .security.yaml.
type CIConfig struct {
	FailOn string `yaml:"fail_on"`
}

// LoadProjectConfig reads and parses .security.yaml from the given directory.
// Returns nil if the file does not exist.
func LoadProjectConfig(dir string) (*ProjectSecurityConfig, error) {
	data, err := os.ReadFile(filepath.Join(dir, ".security.yaml"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading .security.yaml: %w", err)
	}

	if strings.TrimSpace(string(data)) == "" {
		return nil, nil
	}

	var cfg ProjectSecurityConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing .security.yaml: %w", err)
	}

	// Validate severity strings against known values.
	for i, r := range cfg.Rules {
		if r.Severity != "" && SeverityRank(Severity(r.Severity)) == 0 {
			return nil, fmt.Errorf(".security.yaml: rule %q has invalid severity %q (must be critical, high, medium, low, or info)", r.ID, r.Severity)
		}
		if r.ID == "" {
			return nil, fmt.Errorf(".security.yaml: rule at index %d is missing required id field", i)
		}
	}
	for _, o := range cfg.Overrides {
		if o.Severity != "" && SeverityRank(Severity(o.Severity)) == 0 {
			return nil, fmt.Errorf(".security.yaml: override for %q has invalid severity %q (must be critical, high, medium, low, or info)", o.FindingID, o.Severity)
		}
	}

	return &cfg, nil
}

// ApplyOverrides mutates the severity of findings that match override rules.
// Returns the number of findings modified.
func ApplyOverrides(findings []Finding, overrides []Override) int {
	if len(findings) == 0 || len(overrides) == 0 {
		return 0
	}
	overrideMap := make(map[string]Override, len(overrides))
	for _, o := range overrides {
		overrideMap[o.FindingID] = o
	}

	count := 0
	for i := range findings {
		if o, ok := overrideMap[findings[i].ID]; ok {
			findings[i].Severity = Severity(o.Severity)
			count++
		}
	}
	return count
}
