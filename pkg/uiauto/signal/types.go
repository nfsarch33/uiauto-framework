package signal

import (
	"fmt"
	"strings"
	"time"
)

// Severity classifies how critical a signal is.
type Severity int

const (
	SeverityInfo    Severity = iota // routine operational update
	SeveritySuccess                 // positive outcome (heal succeeded, test passed)
	SeverityWarning                 // degradation or drift detected
	SeverityError                   // failure requiring attention
)

func (s Severity) String() string {
	switch s {
	case SeverityInfo:
		return "INFO"
	case SeveritySuccess:
		return "OK"
	case SeverityWarning:
		return "WARN"
	case SeverityError:
		return "ERROR"
	default:
		return "UNKNOWN"
	}
}

// Category groups signals by operational area.
type Category string

const (
	CategoryHeal      Category = "heal"
	CategoryDrift     Category = "drift"
	CategoryTest      Category = "test"
	CategoryCircuit   Category = "circuit"
	CategoryTodo      Category = "todo"
	CategoryMetrics   Category = "metrics"
	CategoryEndurance Category = "endurance"
	CategoryPipeline  Category = "pipeline"
)

// Signal represents an operational event worth notifying about.
type Signal struct {
	ID        string            `json:"id"`
	Timestamp time.Time         `json:"timestamp"`
	Severity  Severity          `json:"severity"`
	Category  Category          `json:"category"`
	Title     string            `json:"title"`
	Detail    string            `json:"detail,omitempty"`
	Tags      map[string]string `json:"tags,omitempty"`
	Source    string            `json:"source,omitempty"`
}

// Brief returns a one-line summary suitable for --brief output.
func (s Signal) Brief() string {
	return fmt.Sprintf("[%s] %s: %s", s.Severity, s.Category, s.Title)
}

// Verbose returns a multi-line detailed representation.
func (s Signal) Verbose() string {
	var b strings.Builder
	fmt.Fprintf(&b, "[%s] %s | %s\n", s.Severity, s.Category, s.Title)
	fmt.Fprintf(&b, "  time: %s\n", s.Timestamp.Format(time.RFC3339))
	if s.Source != "" {
		fmt.Fprintf(&b, "  src:  %s\n", s.Source)
	}
	if s.Detail != "" {
		fmt.Fprintf(&b, "  detail: %s\n", s.Detail)
	}
	for k, v := range s.Tags {
		fmt.Fprintf(&b, "  %s=%s\n", k, v)
	}
	return b.String()
}

// Format returns Brief() or Verbose() based on the brief flag.
func (s Signal) Format(brief bool) string {
	if brief {
		return s.Brief()
	}
	return s.Verbose()
}
