package doctor

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"
)

// Status represents the outcome of a single health check.
type Status int

const (
	StatusPass Status = iota
	StatusWarn
	StatusFail
	StatusSkip
)

func (s Status) String() string {
	switch s {
	case StatusPass:
		return "pass"
	case StatusWarn:
		return "warn"
	case StatusFail:
		return "fail"
	case StatusSkip:
		return "skip"
	default:
		return "unknown"
	}
}

func (s Status) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.String())
}

func (s *Status) UnmarshalJSON(data []byte) error {
	var str string
	if err := json.Unmarshal(data, &str); err != nil {
		return err
	}
	switch str {
	case "pass", "ok":
		*s = StatusPass
	case "warn", "warning":
		*s = StatusWarn
	case "fail", "critical":
		*s = StatusFail
	case "skip", "skipped":
		*s = StatusSkip
	default:
		return fmt.Errorf("unknown status: %q", str)
	}
	return nil
}

// Check represents a single health-check result.
type Check struct {
	Name     string        `json:"name"`
	Status   Status        `json:"status"`
	Message  string        `json:"message"`
	Duration time.Duration `json:"duration_ms"`
}

// Suite groups related checks under a named category.
type Suite struct {
	Name     string        `json:"name"`
	Checks   []Check       `json:"checks"`
	Duration time.Duration `json:"duration_ms"`
}

// Overall computes the suite-level status from its checks.
func (s *Suite) Overall() Status {
	worst := StatusPass
	for _, c := range s.Checks {
		if c.Status > worst {
			worst = c.Status
		}
	}
	return worst
}

// PassCount returns the number of passing checks.
func (s *Suite) PassCount() int {
	n := 0
	for _, c := range s.Checks {
		if c.Status == StatusPass {
			n++
		}
	}
	return n
}

// Report aggregates multiple suites into a single diagnostic report.
type Report struct {
	Suites    []Suite       `json:"suites"`
	Overall   string        `json:"overall"` // healthy, degraded, unhealthy
	Timestamp time.Time     `json:"timestamp"`
	Duration  time.Duration `json:"duration_ms"`
	Platform  string        `json:"platform"`
	GoVersion string        `json:"go_version"`
}

// ComputeOverall derives the report-level status from all suites.
func (r *Report) ComputeOverall() {
	hasFail := false
	hasWarn := false
	for _, s := range r.Suites {
		switch s.Overall() {
		case StatusFail:
			hasFail = true
		case StatusWarn:
			hasWarn = true
		}
	}
	switch {
	case hasFail:
		r.Overall = "unhealthy"
	case hasWarn:
		r.Overall = "degraded"
	default:
		r.Overall = "healthy"
	}
}

// TotalChecks returns the total number of checks across all suites.
func (r *Report) TotalChecks() int {
	n := 0
	for _, s := range r.Suites {
		n += len(s.Checks)
	}
	return n
}

// TotalPass returns the total number of passing checks.
func (r *Report) TotalPass() int {
	n := 0
	for _, s := range r.Suites {
		n += s.PassCount()
	}
	return n
}

// ToJSON returns the report as pretty-printed JSON.
func (r *Report) ToJSON() ([]byte, error) {
	return json.MarshalIndent(r, "", "  ")
}

// ToMarkdown renders the report as a markdown table.
func (r *Report) ToMarkdown() string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# Doctor Report — %s\n\n", r.Overall))
	sb.WriteString(fmt.Sprintf("- **Timestamp**: %s\n", r.Timestamp.Format(time.RFC3339)))
	sb.WriteString(fmt.Sprintf("- **Duration**: %dms\n", r.Duration.Milliseconds()))
	sb.WriteString(fmt.Sprintf("- **Platform**: %s\n", r.Platform))
	sb.WriteString(fmt.Sprintf("- **Checks**: %d/%d passed\n\n", r.TotalPass(), r.TotalChecks()))

	for _, s := range r.Suites {
		sb.WriteString(fmt.Sprintf("## %s (%d/%d)\n\n", s.Name, s.PassCount(), len(s.Checks)))
		sb.WriteString("| Check | Status | Message | Duration |\n")
		sb.WriteString("|-------|--------|---------|----------|\n")
		for _, c := range s.Checks {
			sb.WriteString(fmt.Sprintf("| %s | %s | %s | %s |\n",
				c.Name, c.Status, c.Message, c.Duration.Round(time.Microsecond)))
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

// CheckFunc is a function that performs a single health check.
type CheckFunc func(ctx context.Context) Check

// SuiteSpec defines a suite to be run by the Runner.
type SuiteSpec struct {
	Name   string
	Checks []NamedCheck
}

// NamedCheck pairs a check name with its function.
type NamedCheck struct {
	Name string
	Fn   CheckFunc
}

// Runner executes suites concurrently with per-check timeouts.
type Runner struct {
	CheckTimeout time.Duration
	SuiteTimeout time.Duration
}

// DefaultRunner returns a runner with sensible defaults.
func DefaultRunner() *Runner {
	return &Runner{
		CheckTimeout: 10 * time.Second,
		SuiteTimeout: 30 * time.Second,
	}
}

// RunSuite executes all checks in a suite concurrently.
func (r *Runner) RunSuite(ctx context.Context, spec SuiteSpec) Suite {
	start := time.Now()
	suiteCtx, cancel := context.WithTimeout(ctx, r.SuiteTimeout)
	defer cancel()

	results := make([]Check, len(spec.Checks))
	var wg sync.WaitGroup

	for i, nc := range spec.Checks {
		wg.Add(1)
		go func(idx int, named NamedCheck) {
			defer wg.Done()
			checkCtx, checkCancel := context.WithTimeout(suiteCtx, r.CheckTimeout)
			defer checkCancel()
			results[idx] = named.Fn(checkCtx)
			if results[idx].Name == "" {
				results[idx].Name = named.Name
			}
		}(i, nc)
	}

	wg.Wait()
	return Suite{
		Name:     spec.Name,
		Checks:   results,
		Duration: time.Since(start),
	}
}

// RunAll executes multiple suites and produces a Report.
func (r *Runner) RunAll(ctx context.Context, specs []SuiteSpec) *Report {
	start := time.Now()
	report := &Report{
		Timestamp: start,
		Platform:  fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH),
		GoVersion: runtime.Version(),
	}

	suites := make([]Suite, len(specs))
	var wg sync.WaitGroup

	for i, spec := range specs {
		wg.Add(1)
		go func(idx int, s SuiteSpec) {
			defer wg.Done()
			suites[idx] = r.RunSuite(ctx, s)
		}(i, spec)
	}

	wg.Wait()
	report.Suites = suites
	report.Duration = time.Since(start)
	report.ComputeOverall()
	return report
}

// --- Assertion Helpers ---

// AssertTrue creates a check that passes when condition is true.
func AssertTrue(name string, condition bool, detail string) Check {
	start := time.Now()
	status := StatusPass
	if !condition {
		status = StatusFail
	}
	return Check{Name: name, Status: status, Message: detail, Duration: time.Since(start)}
}

// AssertFileExists checks that a file or directory exists.
func AssertFileExists(name, path string) Check {
	start := time.Now()
	_, err := os.Stat(path)
	if err != nil {
		return Check{Name: name, Status: StatusFail, Message: "not found: " + path, Duration: time.Since(start)}
	}
	return Check{Name: name, Status: StatusPass, Message: "exists: " + path, Duration: time.Since(start)}
}

// AssertDirWritable checks that a directory exists and is writable.
func AssertDirWritable(name, dir string) Check {
	start := time.Now()
	info, err := os.Stat(dir)
	if err != nil {
		return Check{Name: name, Status: StatusFail, Message: "not found: " + dir, Duration: time.Since(start)}
	}
	if !info.IsDir() {
		return Check{Name: name, Status: StatusFail, Message: "not a directory: " + dir, Duration: time.Since(start)}
	}
	probe := dir + "/.doctor_probe"
	if err := os.WriteFile(probe, []byte("ok"), 0o644); err != nil {
		return Check{Name: name, Status: StatusFail, Message: "not writable: " + err.Error(), Duration: time.Since(start)}
	}
	os.Remove(probe)
	return Check{Name: name, Status: StatusPass, Message: "writable: " + dir, Duration: time.Since(start)}
}

// AssertHTTPHealth checks that a URL responds with 2xx/3xx.
func AssertHTTPHealth(ctx context.Context, name, url string) Check {
	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Check{Name: name, Status: StatusFail, Message: "bad URL: " + err.Error(), Duration: time.Since(start)}
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return Check{Name: name, Status: StatusFail, Message: "unreachable: " + err.Error(), Duration: time.Since(start)}
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 200 && resp.StatusCode < 400 {
		return Check{Name: name, Status: StatusPass, Message: fmt.Sprintf("healthy (status=%d)", resp.StatusCode), Duration: time.Since(start)}
	}
	return Check{Name: name, Status: StatusWarn, Message: fmt.Sprintf("status %d", resp.StatusCode), Duration: time.Since(start)}
}

// AssertCommandExists checks that a CLI command is on PATH.
func AssertCommandExists(name, cmd string) Check {
	start := time.Now()
	path, err := exec.LookPath(cmd)
	if err != nil {
		return Check{Name: name, Status: StatusWarn, Message: cmd + " not found", Duration: time.Since(start)}
	}
	return Check{Name: name, Status: StatusPass, Message: "found: " + path, Duration: time.Since(start)}
}

// AssertGoroutineCount checks that the number of active goroutines is below a threshold.
func AssertGoroutineCount(name string, warnAt, failAt int) Check {
	start := time.Now()
	n := runtime.NumGoroutine()
	msg := fmt.Sprintf("%d goroutines active", n)
	switch {
	case n >= failAt:
		return Check{Name: name, Status: StatusFail, Message: msg + fmt.Sprintf(" (>= %d)", failAt), Duration: time.Since(start)}
	case n >= warnAt:
		return Check{Name: name, Status: StatusWarn, Message: msg + fmt.Sprintf(" (>= %d)", warnAt), Duration: time.Since(start)}
	default:
		return Check{Name: name, Status: StatusPass, Message: msg, Duration: time.Since(start)}
	}
}

// AssertDirFileCount checks that a directory has at least min files with the given extension.
func AssertDirFileCount(name, dir, ext string, min int) Check {
	start := time.Now()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return Check{Name: name, Status: StatusWarn, Message: "cannot read: " + dir, Duration: time.Since(start)}
	}
	count := 0
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ext) {
			count++
		}
	}
	status := StatusPass
	if count < min {
		status = StatusWarn
	}
	return Check{Name: name, Status: status, Message: fmt.Sprintf("%d %s files in %s", count, ext, dir), Duration: time.Since(start)}
}
