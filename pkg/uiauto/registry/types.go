package registry

import "time"

// PagePattern holds selectors and metadata for a specific page route.
type PagePattern struct {
	Route       string          `json:"route"`
	Name        string          `json:"name,omitempty"`
	Selectors   []SelectorGroup `json:"selectors"`
	LastUpdated time.Time       `json:"last_updated"`
	HitCount    int64           `json:"hit_count"`
	Confidence  float64         `json:"confidence"`
}

// SelectorGroup represents a named group of selectors for a UI element.
type SelectorGroup struct {
	ElementName string   `json:"element_name"`
	Primary     string   `json:"primary"`
	Fallbacks   []string `json:"fallbacks,omitempty"`
	Strategy    string   `json:"strategy"`
	Stable      bool     `json:"stable"`
}

// PatternMatch is the result of looking up a URL against the registry.
type PatternMatch struct {
	Pattern    *PagePattern
	MatchScore float64
	Route      string
}
