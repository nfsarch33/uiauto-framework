package plugin

import (
	"encoding/json"
	"fmt"
	"os"
)

// Scenario is the framework-level representation of a test scenario. It is
// deliberately small and generic -- target-specific fields (page_objects,
// selectors_used, source) live in tags or metadata maps.
type Scenario struct {
	ID              string            `json:"id"`
	Name            string            `json:"name"`
	NaturalLanguage []string          `json:"natural_language"`
	Selectors       []string          `json:"selectors_used,omitempty"`
	ActionTypes     []string          `json:"action_types,omitempty"`
	ActionValues    []string          `json:"action_values,omitempty"`
	Tags            []string          `json:"tags,omitempty"`
	Metadata        map[string]string `json:"metadata,omitempty"`
}

// ScenarioLoader parses scenarios from a backing source.
type ScenarioLoader interface {
	Load(path string) ([]Scenario, error)
}

// JSONScenarioLoader reads a JSON array of Scenario objects from disk. It is
// the default loader used by the demo command.
type JSONScenarioLoader struct{}

// NewJSONScenarioLoader constructs a default JSON loader.
func NewJSONScenarioLoader() *JSONScenarioLoader { return &JSONScenarioLoader{} }

// Load reads path, unmarshals the array, and returns it. Empty arrays are
// rejected so misconfigured inputs surface early.
func (l *JSONScenarioLoader) Load(path string) ([]Scenario, error) {
	if path == "" {
		return nil, fmt.Errorf("scenario path is empty")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var raw []map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	scenarios := make([]Scenario, 0, len(raw))
	for i, item := range raw {
		buf, _ := json.Marshal(item)
		var s Scenario
		if err := json.Unmarshal(buf, &s); err != nil {
			return nil, fmt.Errorf("scenario[%d]: %w", i, err)
		}
		scenarios = append(scenarios, s)
	}
	if len(scenarios) == 0 {
		return nil, fmt.Errorf("no scenarios found in %s", path)
	}
	return scenarios, nil
}
