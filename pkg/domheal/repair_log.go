package domheal

import (
	"encoding/json"
	"os"
)

// RepairSuggestion records a selector repair event.
type RepairSuggestion struct {
	ElementType string  `json:"element_type"`
	OldSelector string  `json:"old_selector"`
	NewSelector string  `json:"new_selector"`
	Confidence  float64 `json:"confidence"`
	Method      string  `json:"method"`
}

// RepairLog persists repair suggestions to a JSON file.
type RepairLog struct {
	path string
}

// NewRepairLog creates a repair log at the given file path.
func NewRepairLog(path string) *RepairLog {
	return &RepairLog{path: path}
}

// Append adds a repair suggestion to the log file.
func (rl *RepairLog) Append(suggestion RepairSuggestion) error {
	var entries []RepairSuggestion
	data, err := os.ReadFile(rl.path)
	if err == nil {
		_ = json.Unmarshal(data, &entries)
	}
	entries = append(entries, suggestion)
	out, _ := json.MarshalIndent(entries, "", "  ")
	return os.WriteFile(rl.path, out, 0644)
}

// Read loads all repair suggestions from the log file.
func (rl *RepairLog) Read() ([]RepairSuggestion, error) {
	data, err := os.ReadFile(rl.path)
	if err != nil {
		return nil, err
	}
	var entries []RepairSuggestion
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, err
	}
	return entries, nil
}
