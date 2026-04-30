package evolver

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/nfsarch33/uiauto-framework/internal/doctor"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultAgentDoctorConfig(t *testing.T) {
	cfg := DefaultAgentDoctorConfig()
	assert.True(t, cfg.CheckDocker)
	assert.True(t, cfg.CheckLLM)
	assert.True(t, cfg.CheckMem0)
	assert.True(t, cfg.CheckPatternStore)
	assert.True(t, cfg.CheckEvolver)
	assert.True(t, cfg.CheckFleet)
	assert.True(t, cfg.CheckGo)
	assert.NotEmpty(t, cfg.LLMHealthURL)
	assert.NotEmpty(t, cfg.Mem0HealthURL)
}

func TestAgentDoctor_RunAll_GoCheck(t *testing.T) {
	cfg := AgentDoctorConfig{
		CheckGo: true,
	}
	doc := NewAgentDoctor(cfg)
	report := doc.RunAll(context.Background())

	require.Len(t, report.Suites, 1)
	suite := report.Suites[0]
	require.Len(t, suite.Checks, 1)
	assert.Equal(t, "go-runtime", suite.Checks[0].Name)
	assert.Equal(t, doctor.StatusPass, suite.Checks[0].Status)
	assert.Contains(t, suite.Checks[0].Message, "Go go")
	assert.Equal(t, "healthy", report.Overall)
	assert.NotEmpty(t, report.Platform)
	assert.NotEmpty(t, report.GoVersion)
}

func TestAgentDoctor_RunAll_FleetCheck(t *testing.T) {
	cfg := AgentDoctorConfig{
		CheckFleet: true,
	}
	doc := NewAgentDoctor(cfg)
	report := doc.RunAll(context.Background())

	require.Len(t, report.Suites, 1)
	suite := report.Suites[0]
	require.Len(t, suite.Checks, 1)
	assert.Equal(t, "fleet", suite.Checks[0].Name)
	assert.Equal(t, doctor.StatusPass, suite.Checks[0].Status)
	assert.Contains(t, suite.Checks[0].Message, "node:")
}

func TestAgentDoctor_RunAll_PatternStore_NotFound(t *testing.T) {
	cfg := AgentDoctorConfig{
		CheckPatternStore: true,
		PatternStorePath:  "/nonexistent/path/patterns",
	}
	doc := NewAgentDoctor(cfg)
	report := doc.RunAll(context.Background())

	require.Len(t, report.Suites, 1)
	suite := report.Suites[0]
	require.Len(t, suite.Checks, 1)
	assert.Equal(t, doctor.StatusWarn, suite.Checks[0].Status)
	assert.Contains(t, suite.Checks[0].Message, "not found")
	assert.Equal(t, "degraded", report.Overall)
}

func TestAgentDoctor_RunAll_PatternStore_Valid(t *testing.T) {
	dir := t.TempDir()
	patternDir := filepath.Join(dir, "patterns")
	require.NoError(t, os.MkdirAll(patternDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(patternDir, "p1.json"), []byte("{}"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(patternDir, "p2.json"), []byte("{}"), 0o644))

	cfg := AgentDoctorConfig{
		CheckPatternStore: true,
		PatternStorePath:  patternDir,
	}
	doc := NewAgentDoctor(cfg)
	report := doc.RunAll(context.Background())

	require.Len(t, report.Suites, 1)
	suite := report.Suites[0]
	require.Len(t, suite.Checks, 1)
	assert.Equal(t, doctor.StatusPass, suite.Checks[0].Status)
	assert.Contains(t, suite.Checks[0].Message, "2 JSON files")
}

func TestAgentDoctor_RunAll_Evolver_NotFound(t *testing.T) {
	cfg := AgentDoctorConfig{
		CheckEvolver:     true,
		CapsuleStorePath: "/nonexistent/capsules",
	}
	doc := NewAgentDoctor(cfg)
	report := doc.RunAll(context.Background())

	require.Len(t, report.Suites, 1)
	suite := report.Suites[0]
	require.Len(t, suite.Checks, 1)
	assert.Equal(t, doctor.StatusWarn, suite.Checks[0].Status)
}

func TestAgentDoctor_RunAll_Evolver_Valid(t *testing.T) {
	dir := t.TempDir()
	capsuleDir := filepath.Join(dir, "capsules")
	require.NoError(t, os.MkdirAll(capsuleDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(capsuleDir, "cap1.json"), []byte("{}"), 0o644))

	cfg := AgentDoctorConfig{
		CheckEvolver:     true,
		CapsuleStorePath: capsuleDir,
	}
	doc := NewAgentDoctor(cfg)
	report := doc.RunAll(context.Background())

	require.Len(t, report.Suites, 1)
	suite := report.Suites[0]
	require.Len(t, suite.Checks, 1)
	assert.Equal(t, doctor.StatusPass, suite.Checks[0].Status)
	assert.Contains(t, suite.Checks[0].Message, "1 capsules")
}

func TestAgentDoctor_RunAll_Concurrent(t *testing.T) {
	dir := t.TempDir()
	patternDir := filepath.Join(dir, "patterns")
	require.NoError(t, os.MkdirAll(patternDir, 0o755))

	cfg := AgentDoctorConfig{
		CheckGo:           true,
		CheckFleet:        true,
		CheckPatternStore: true,
		PatternStorePath:  patternDir,
	}
	doc := NewAgentDoctor(cfg)
	report := doc.RunAll(context.Background())

	require.Len(t, report.Suites, 1)
	suite := report.Suites[0]
	require.Len(t, suite.Checks, 3)
	assert.Equal(t, "healthy", report.Overall)
	assert.Greater(t, report.Duration.Nanoseconds(), int64(0))
}

func TestAgentDoctorReport_ToJSON(t *testing.T) {
	cfg := AgentDoctorConfig{CheckGo: true}
	doc := NewAgentDoctor(cfg)
	report := doc.RunAll(context.Background())

	data, err := report.ToJSON()
	require.NoError(t, err)
	assert.Contains(t, string(data), "go-runtime")
	assert.Contains(t, string(data), "healthy")
}

func TestAgentDoctor_Checks(t *testing.T) {
	cfg := AgentDoctorConfig{CheckGo: true, CheckFleet: true}
	doc := NewAgentDoctor(cfg)
	checks := doc.Checks(context.Background())
	assert.Len(t, checks, 2)
}
