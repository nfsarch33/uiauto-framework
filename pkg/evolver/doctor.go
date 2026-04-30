package evolver

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/nfsarch33/uiauto-framework/internal/doctor"
)

// AgentDoctorConfig configures agent health checks.
type AgentDoctorConfig struct {
	CheckDocker       bool
	CheckLLM          bool
	CheckMem0         bool
	CheckPatternStore bool
	CheckEvolver      bool
	CheckFleet        bool
	CheckGo           bool

	LLMHealthURL     string
	Mem0HealthURL    string
	PatternStorePath string
	CapsuleStorePath string
}

// DefaultAgentDoctorConfig returns a config with all checks enabled.
func DefaultAgentDoctorConfig() AgentDoctorConfig {
	return AgentDoctorConfig{
		CheckDocker:       true,
		CheckLLM:          true,
		CheckMem0:         true,
		CheckPatternStore: true,
		CheckEvolver:      true,
		CheckFleet:        true,
		CheckGo:           true,
		LLMHealthURL:      "http://localhost:18789/health",
		Mem0HealthURL:     "http://localhost:8019/health",
		PatternStorePath:  "data/patterns",
		CapsuleStorePath:  "data/evolver/capsules",
	}
}

// AgentDoctor runs diagnostic checks for IronClaw agent infrastructure.
type AgentDoctor struct {
	cfg    AgentDoctorConfig
	runner *doctor.Runner
}

// NewAgentDoctor creates a new agent doctor.
func NewAgentDoctor(cfg AgentDoctorConfig) *AgentDoctor {
	return &AgentDoctor{
		cfg:    cfg,
		runner: doctor.DefaultRunner(),
	}
}

// RunAll runs all enabled checks concurrently using the shared doctor.Runner.
func (d *AgentDoctor) RunAll(ctx context.Context) *doctor.Report {
	spec := d.buildSuiteSpec()
	report := d.runner.RunAll(ctx, []doctor.SuiteSpec{spec})
	return report
}

// Checks returns individual check results (convenience for callers needing flat access).
func (d *AgentDoctor) Checks(ctx context.Context) []doctor.Check {
	report := d.RunAll(ctx)
	if len(report.Suites) == 0 {
		return nil
	}
	return report.Suites[0].Checks
}

func (d *AgentDoctor) buildSuiteSpec() doctor.SuiteSpec {
	var checks []doctor.NamedCheck

	type checkEntry struct {
		enabled bool
		name    string
		fn      doctor.CheckFunc
	}

	entries := []checkEntry{
		{d.cfg.CheckDocker, "docker", d.checkDocker},
		{d.cfg.CheckLLM, "llm-router", d.checkLLM},
		{d.cfg.CheckMem0, "mem0", d.checkMem0},
		{d.cfg.CheckPatternStore, "pattern-store", d.checkPatternStore},
		{d.cfg.CheckEvolver, "evolver", d.checkEvolver},
		{d.cfg.CheckFleet, "fleet", d.checkFleet},
		{d.cfg.CheckGo, "go-runtime", d.checkGo},
	}

	for _, e := range entries {
		if e.enabled {
			checks = append(checks, doctor.NamedCheck{Name: e.name, Fn: e.fn})
		}
	}

	return doctor.SuiteSpec{Name: "evolver", Checks: checks}
}

func (d *AgentDoctor) checkDocker(ctx context.Context) doctor.Check {
	c := doctor.AssertCommandExists("docker", "docker")
	if c.Status != doctor.StatusPass {
		c.Status = doctor.StatusWarn
		c.Message = "Docker daemon not reachable; container sandbox unavailable"
	}
	return c
}

func (d *AgentDoctor) checkLLM(ctx context.Context) doctor.Check {
	return doctor.AssertHTTPHealth(ctx, "llm-router", d.cfg.LLMHealthURL)
}

func (d *AgentDoctor) checkMem0(ctx context.Context) doctor.Check {
	url := d.cfg.Mem0HealthURL
	if url == "" {
		return doctor.Check{Name: "mem0", Status: doctor.StatusWarn, Message: "Mem0 health URL not configured"}
	}
	c := doctor.AssertHTTPHealth(ctx, "mem0", url)
	if c.Status == doctor.StatusFail {
		c.Status = doctor.StatusWarn
		c.Message = "Mem0 unreachable (fleet pattern sharing degraded)"
	}
	return c
}

func (d *AgentDoctor) checkPatternStore(_ context.Context) doctor.Check {
	start := time.Now()
	path := d.cfg.PatternStorePath
	info, err := os.Stat(path)
	if err != nil {
		return doctor.Check{Name: "pattern-store", Status: doctor.StatusWarn, Message: "pattern store directory not found: " + path, Duration: time.Since(start)}
	}
	if !info.IsDir() {
		return doctor.Check{Name: "pattern-store", Status: doctor.StatusFail, Message: "pattern store path is not a directory: " + path, Duration: time.Since(start)}
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return doctor.Check{Name: "pattern-store", Status: doctor.StatusFail, Message: "cannot read pattern store: " + err.Error(), Duration: time.Since(start)}
	}
	jsonCount := 0
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".json" {
			jsonCount++
		}
	}
	return doctor.Check{Name: "pattern-store", Status: doctor.StatusPass, Message: fmt.Sprintf("pattern store: %d JSON files in %s", jsonCount, path), Duration: time.Since(start)}
}

func (d *AgentDoctor) checkEvolver(_ context.Context) doctor.Check {
	start := time.Now()
	path := d.cfg.CapsuleStorePath
	info, err := os.Stat(path)
	if err != nil {
		return doctor.Check{Name: "evolver", Status: doctor.StatusWarn, Message: "capsule store not found: " + path, Duration: time.Since(start)}
	}
	if !info.IsDir() {
		return doctor.Check{Name: "evolver", Status: doctor.StatusFail, Message: "capsule store path is not a directory", Duration: time.Since(start)}
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return doctor.Check{Name: "evolver", Status: doctor.StatusWarn, Message: "cannot read capsule store", Duration: time.Since(start)}
	}
	capsuleCount := 0
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".json" {
			capsuleCount++
		}
	}
	return doctor.Check{Name: "evolver", Status: doctor.StatusPass, Message: fmt.Sprintf("evolver capsule store: %d capsules", capsuleCount), Duration: time.Since(start)}
}

func (d *AgentDoctor) checkFleet(_ context.Context) doctor.Check {
	start := time.Now()
	hostname, err := os.Hostname()
	if err != nil {
		return doctor.Check{Name: "fleet", Status: doctor.StatusWarn, Message: "cannot determine hostname", Duration: time.Since(start)}
	}
	return doctor.Check{Name: "fleet", Status: doctor.StatusPass, Message: fmt.Sprintf("node: %s (%s/%s)", hostname, runtime.GOOS, runtime.GOARCH), Duration: time.Since(start)}
}

func (d *AgentDoctor) checkGo(_ context.Context) doctor.Check {
	start := time.Now()
	return doctor.Check{
		Name:   "go-runtime",
		Status: doctor.StatusPass,
		Message: fmt.Sprintf("Go %s on %s/%s (NumCPU=%d, NumGoroutine=%d)",
			runtime.Version(), runtime.GOOS, runtime.GOARCH,
			runtime.NumCPU(), runtime.NumGoroutine()),
		Duration: time.Since(start),
	}
}
