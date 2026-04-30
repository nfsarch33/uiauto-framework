package config

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
)

// TargetConfig defines a test target site for the UI automation harness.
type TargetConfig struct {
	Name        string            `json:"name"`
	BaseURL     string            `json:"base_url"`
	Description string            `json:"description,omitempty"`
	Auth        *AuthConfig       `json:"auth,omitempty"`
	Pages       []PageConfig      `json:"pages"`
	Tags        []string          `json:"tags,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

// AuthConfig holds optional authentication details for a target.
type AuthConfig struct {
	Type     string `json:"type"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
	TokenEnv string `json:"token_env,omitempty"`
}

// PageConfig defines a specific page within a target site.
type PageConfig struct {
	Name      string            `json:"name"`
	Path      string            `json:"path"`
	Selectors map[string]string `json:"selectors"`
	Actions   []ActionConfig    `json:"actions,omitempty"`
}

// ActionConfig describes a user interaction step to execute on a page.
type ActionConfig struct {
	Type     string `json:"type"`
	Selector string `json:"selector"`
	Value    string `json:"value,omitempty"`
	WaitMs   int    `json:"wait_ms,omitempty"`
}

// Validate checks that the target config has required fields and valid URLs.
func (t *TargetConfig) Validate() error {
	if t.Name == "" {
		return fmt.Errorf("target name is required")
	}
	if t.BaseURL == "" {
		return fmt.Errorf("target base_url is required")
	}
	if _, err := url.Parse(t.BaseURL); err != nil {
		return fmt.Errorf("invalid base_url %q: %w", t.BaseURL, err)
	}
	if len(t.Pages) == 0 {
		return fmt.Errorf("target must have at least one page")
	}
	for i, p := range t.Pages {
		if p.Name == "" {
			return fmt.Errorf("page[%d].name is required", i)
		}
		if p.Path == "" {
			return fmt.Errorf("page[%d].path is required", i)
		}
	}
	return nil
}

// FullURL returns the complete URL for a page.
func (t *TargetConfig) FullURL(page PageConfig) string {
	return t.BaseURL + page.Path
}

// LoadTargets reads a JSON file containing an array of TargetConfig.
func LoadTargets(path string) ([]TargetConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading targets file: %w", err)
	}

	var targets []TargetConfig
	if err := json.Unmarshal(data, &targets); err != nil {
		return nil, fmt.Errorf("parsing targets JSON: %w", err)
	}

	for i := range targets {
		if err := targets[i].Validate(); err != nil {
			return nil, fmt.Errorf("target[%d] %q: %w", i, targets[i].Name, err)
		}
	}

	return targets, nil
}

// LoadTarget reads a single TargetConfig from a JSON file.
func LoadTarget(path string) (*TargetConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading target file: %w", err)
	}

	var target TargetConfig
	if err := json.Unmarshal(data, &target); err != nil {
		return nil, fmt.Errorf("parsing target JSON: %w", err)
	}

	if err := target.Validate(); err != nil {
		return nil, err
	}

	return &target, nil
}
