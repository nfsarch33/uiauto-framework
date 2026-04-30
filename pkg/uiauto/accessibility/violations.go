package accessibility

// AuditResult holds the complete output from axe.run().
type AuditResult struct {
	Violations      []Violation     `json:"violations"`
	Passes          []Violation     `json:"passes"`
	Incomplete      []Violation     `json:"incomplete"`
	Inapplicable    []Violation     `json:"inapplicable"`
	URL             string          `json:"url"`
	Timestamp       string          `json:"timestamp"`
	TestEngine      TestEngine      `json:"testEngine"`
	TestRunner      TestRunner      `json:"testRunner"`
	TestEnvironment TestEnvironment `json:"testEnvironment"`
}

// Violation represents a single axe-core rule violation or pass.
type Violation struct {
	ID          string          `json:"id"`
	Impact      string          `json:"impact"`
	Tags        []string        `json:"tags"`
	Description string          `json:"description"`
	Help        string          `json:"help"`
	HelpURL     string          `json:"helpUrl"`
	Nodes       []ViolationNode `json:"nodes"`
}

// ViolationNode describes a specific DOM element that triggered a rule.
type ViolationNode struct {
	HTML           string   `json:"html"`
	Impact         string   `json:"impact"`
	Target         []string `json:"target"`
	FailureSummary string   `json:"failureSummary"`
}

// TestEngine identifies the axe-core version.
type TestEngine struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// TestRunner identifies the test harness.
type TestRunner struct {
	Name string `json:"name"`
}

// TestEnvironment captures the browser context.
type TestEnvironment struct {
	UserAgent    string `json:"userAgent"`
	WindowWidth  int    `json:"windowWidth"`
	WindowHeight int    `json:"windowHeight"`
}

// Summary returns a count of violations grouped by impact level.
func (r *AuditResult) Summary() map[string]int {
	m := make(map[string]int)
	for _, v := range r.Violations {
		m[v.Impact] += len(v.Nodes)
	}
	return m
}

// TotalViolations returns the total number of violation nodes.
func (r *AuditResult) TotalViolations() int {
	total := 0
	for _, v := range r.Violations {
		total += len(v.Nodes)
	}
	return total
}

// CriticalAndSerious returns only violations with "critical" or "serious" impact.
func (r *AuditResult) CriticalAndSerious() []Violation {
	var result []Violation
	for _, v := range r.Violations {
		if v.Impact == "critical" || v.Impact == "serious" {
			result = append(result, v)
		}
	}
	return result
}

// HasWCAG2AA checks whether violations include WCAG 2.1 AA tags.
func (v *Violation) HasWCAG2AA() bool {
	for _, tag := range v.Tags {
		if tag == "wcag2aa" || tag == "wcag21aa" {
			return true
		}
	}
	return false
}
