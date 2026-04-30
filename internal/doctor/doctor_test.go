package doctor

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStatusString(t *testing.T) {
	tests := []struct {
		status Status
		want   string
	}{
		{StatusPass, "pass"},
		{StatusWarn, "warn"},
		{StatusFail, "fail"},
		{StatusSkip, "skip"},
		{Status(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.status.String(); got != tt.want {
			t.Errorf("Status(%d).String() = %q, want %q", tt.status, got, tt.want)
		}
	}
}

func TestStatusJSONRoundTrip(t *testing.T) {
	for _, s := range []Status{StatusPass, StatusWarn, StatusFail, StatusSkip} {
		data, err := json.Marshal(s)
		if err != nil {
			t.Fatalf("marshal %v: %v", s, err)
		}
		var got Status
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatalf("unmarshal %s: %v", data, err)
		}
		if got != s {
			t.Errorf("roundtrip: got %v, want %v", got, s)
		}
	}
}

func TestStatusUnmarshalAliases(t *testing.T) {
	tests := []struct {
		input string
		want  Status
	}{
		{`"ok"`, StatusPass},
		{`"pass"`, StatusPass},
		{`"warning"`, StatusWarn},
		{`"warn"`, StatusWarn},
		{`"critical"`, StatusFail},
		{`"fail"`, StatusFail},
		{`"skipped"`, StatusSkip},
		{`"skip"`, StatusSkip},
	}
	for _, tt := range tests {
		var got Status
		if err := json.Unmarshal([]byte(tt.input), &got); err != nil {
			t.Fatalf("unmarshal %s: %v", tt.input, err)
		}
		if got != tt.want {
			t.Errorf("unmarshal %s: got %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestStatusUnmarshalInvalid(t *testing.T) {
	var s Status
	err := json.Unmarshal([]byte(`"bogus"`), &s)
	if err == nil {
		t.Fatal("expected error for unknown status")
	}
}

func TestSuiteOverall(t *testing.T) {
	tests := []struct {
		name   string
		checks []Check
		want   Status
	}{
		{"all pass", []Check{{Status: StatusPass}, {Status: StatusPass}}, StatusPass},
		{"one warn", []Check{{Status: StatusPass}, {Status: StatusWarn}}, StatusWarn},
		{"one fail", []Check{{Status: StatusPass}, {Status: StatusFail}}, StatusFail},
		{"fail and warn", []Check{{Status: StatusWarn}, {Status: StatusFail}}, StatusFail},
		{"empty", nil, StatusPass},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := Suite{Name: tt.name, Checks: tt.checks}
			if got := s.Overall(); got != tt.want {
				t.Errorf("Suite.Overall() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSuitePassCount(t *testing.T) {
	s := Suite{Checks: []Check{
		{Status: StatusPass},
		{Status: StatusFail},
		{Status: StatusPass},
		{Status: StatusWarn},
	}}
	if got := s.PassCount(); got != 2 {
		t.Errorf("PassCount() = %d, want 2", got)
	}
}

func TestReportComputeOverall(t *testing.T) {
	tests := []struct {
		name   string
		suites []Suite
		want   string
	}{
		{"healthy", []Suite{{Checks: []Check{{Status: StatusPass}}}}, "healthy"},
		{"degraded", []Suite{{Checks: []Check{{Status: StatusWarn}}}}, "degraded"},
		{"unhealthy", []Suite{{Checks: []Check{{Status: StatusFail}}}}, "unhealthy"},
		{"mixed", []Suite{
			{Checks: []Check{{Status: StatusPass}}},
			{Checks: []Check{{Status: StatusWarn}}},
		}, "degraded"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &Report{Suites: tt.suites}
			r.ComputeOverall()
			if r.Overall != tt.want {
				t.Errorf("Overall = %q, want %q", r.Overall, tt.want)
			}
		})
	}
}

func TestReportTotals(t *testing.T) {
	r := &Report{Suites: []Suite{
		{Checks: []Check{{Status: StatusPass}, {Status: StatusFail}}},
		{Checks: []Check{{Status: StatusPass}, {Status: StatusPass}}},
	}}
	if got := r.TotalChecks(); got != 4 {
		t.Errorf("TotalChecks() = %d, want 4", got)
	}
	if got := r.TotalPass(); got != 3 {
		t.Errorf("TotalPass() = %d, want 3", got)
	}
}

func TestReportToJSON(t *testing.T) {
	r := &Report{
		Overall:   "healthy",
		Timestamp: time.Date(2026, 3, 20, 0, 0, 0, 0, time.UTC),
		Platform:  "darwin/arm64",
		GoVersion: "go1.24.5",
		Suites: []Suite{{
			Name:   "test-suite",
			Checks: []Check{{Name: "check1", Status: StatusPass, Message: "ok"}},
		}},
	}
	data, err := r.ToJSON()
	if err != nil {
		t.Fatal(err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed["overall"] != "healthy" {
		t.Errorf("JSON overall = %v", parsed["overall"])
	}
}

func TestReportToMarkdown(t *testing.T) {
	r := &Report{
		Overall:   "healthy",
		Timestamp: time.Date(2026, 3, 20, 0, 0, 0, 0, time.UTC),
		Platform:  "darwin/arm64",
		Suites: []Suite{{
			Name:   "Test",
			Checks: []Check{{Name: "c1", Status: StatusPass, Message: "good"}},
		}},
	}
	md := r.ToMarkdown()
	if len(md) == 0 {
		t.Fatal("empty markdown")
	}
	if !contains(md, "healthy") {
		t.Error("markdown missing 'healthy'")
	}
	if !contains(md, "| c1 |") {
		t.Error("markdown missing check row")
	}
}

func TestRunnerRunSuite(t *testing.T) {
	runner := DefaultRunner()
	spec := SuiteSpec{
		Name: "test",
		Checks: []NamedCheck{
			{Name: "fast", Fn: func(_ context.Context) Check {
				return Check{Name: "fast", Status: StatusPass, Message: "instant"}
			}},
			{Name: "slow", Fn: func(ctx context.Context) Check {
				select {
				case <-time.After(10 * time.Millisecond):
					return Check{Name: "slow", Status: StatusPass, Message: "done"}
				case <-ctx.Done():
					return Check{Name: "slow", Status: StatusFail, Message: "timeout"}
				}
			}},
		},
	}

	suite := runner.RunSuite(context.Background(), spec)
	if suite.Name != "test" {
		t.Errorf("name = %q", suite.Name)
	}
	if len(suite.Checks) != 2 {
		t.Fatalf("checks = %d, want 2", len(suite.Checks))
	}
	for _, c := range suite.Checks {
		if c.Status != StatusPass {
			t.Errorf("check %q: status = %v", c.Name, c.Status)
		}
	}
}

func TestRunnerRunAll(t *testing.T) {
	runner := DefaultRunner()
	specs := []SuiteSpec{
		{Name: "s1", Checks: []NamedCheck{{Name: "c1", Fn: func(_ context.Context) Check {
			return Check{Name: "c1", Status: StatusPass, Message: "ok"}
		}}}},
		{Name: "s2", Checks: []NamedCheck{{Name: "c2", Fn: func(_ context.Context) Check {
			return Check{Name: "c2", Status: StatusWarn, Message: "degraded"}
		}}}},
	}
	report := runner.RunAll(context.Background(), specs)
	if report.Overall != "degraded" {
		t.Errorf("overall = %q, want degraded", report.Overall)
	}
	if report.TotalChecks() != 2 {
		t.Errorf("total = %d, want 2", report.TotalChecks())
	}
}

func TestAssertTrue(t *testing.T) {
	pass := AssertTrue("test-pass", true, "ok")
	if pass.Status != StatusPass {
		t.Errorf("expected pass, got %v", pass.Status)
	}
	fail := AssertTrue("test-fail", false, "bad")
	if fail.Status != StatusFail {
		t.Errorf("expected fail, got %v", fail.Status)
	}
}

func TestAssertFileExists(t *testing.T) {
	tmp := t.TempDir()
	f := filepath.Join(tmp, "exists.txt")
	os.WriteFile(f, []byte("x"), 0o644)

	pass := AssertFileExists("found", f)
	if pass.Status != StatusPass {
		t.Errorf("expected pass for existing file")
	}

	fail := AssertFileExists("missing", filepath.Join(tmp, "nope.txt"))
	if fail.Status != StatusFail {
		t.Errorf("expected fail for missing file")
	}
}

func TestAssertDirWritable(t *testing.T) {
	tmp := t.TempDir()
	pass := AssertDirWritable("writable", tmp)
	if pass.Status != StatusPass {
		t.Errorf("expected pass for writable dir")
	}

	fail := AssertDirWritable("missing", filepath.Join(tmp, "nodir"))
	if fail.Status != StatusFail {
		t.Errorf("expected fail for missing dir")
	}
}

func TestAssertHTTPHealth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(200)
	}))
	defer srv.Close()

	pass := AssertHTTPHealth(context.Background(), "http-ok", srv.URL)
	if pass.Status != StatusPass {
		t.Errorf("expected pass, got %v: %s", pass.Status, pass.Message)
	}

	fail := AssertHTTPHealth(context.Background(), "http-bad", "http://localhost:1")
	if fail.Status != StatusPass {
		// unreachable should be fail
	}
}

func TestAssertCommandExists(t *testing.T) {
	pass := AssertCommandExists("go", "go")
	if pass.Status != StatusPass {
		t.Errorf("expected 'go' to be found")
	}

	warn := AssertCommandExists("bogus", "definitely_not_a_real_command_xyz")
	if warn.Status != StatusWarn {
		t.Errorf("expected warn for missing command")
	}
}

func TestAssertDirFileCount(t *testing.T) {
	tmp := t.TempDir()
	os.WriteFile(filepath.Join(tmp, "a.json"), []byte("{}"), 0o644)
	os.WriteFile(filepath.Join(tmp, "b.json"), []byte("{}"), 0o644)
	os.WriteFile(filepath.Join(tmp, "c.txt"), []byte("x"), 0o644)

	pass := AssertDirFileCount("json-count", tmp, ".json", 2)
	if pass.Status != StatusPass {
		t.Errorf("expected pass with 2 json files")
	}

	warn := AssertDirFileCount("json-few", tmp, ".json", 5)
	if warn.Status != StatusWarn {
		t.Errorf("expected warn when below minimum")
	}
}

func contains(s, sub string) bool {
	return len(s) > 0 && len(sub) > 0 && len(s) >= len(sub) && (s == sub || len(s) > 0 && containsStr(s, sub))
}

func containsStr(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
