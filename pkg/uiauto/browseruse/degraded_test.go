package browseruse

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// TestDegradedOverPayloadShapes: the two payload shapes the live leg
// produced - truncated model output, and a truncated output whose final
// action never parsed as an AgentOutput - are both degraded, and both
// markers are reported.
func TestDegradedOverPayloadShapes(t *testing.T) {
	cases := []struct {
		file    string
		degrade bool
		reasons []string
	}{
		{"testdata/run2_truncated.json", true, []string{DegradedTruncated, DegradedInvalidOutput}},
		{"testdata/run3_invalid_agent_output.json", true, []string{DegradedTruncated, DegradedInvalidOutput}},
	}
	for _, tc := range cases {
		raw, err := os.ReadFile(tc.file)
		if err != nil {
			t.Fatalf("read %s: %v", tc.file, err)
		}
		var res RunResult
		if err := json.Unmarshal(raw, &res); err != nil {
			t.Fatalf("decode %s: %v", tc.file, err)
		}
		degraded, reasons := Degraded(res)
		if degraded != tc.degrade {
			t.Errorf("%s: degraded = %v, want %v", tc.file, degraded, tc.degrade)
		}
		if strings.Join(reasons, ",") != strings.Join(tc.reasons, ",") {
			t.Errorf("%s: reasons = %v, want %v", tc.file, reasons, tc.reasons)
		}
	}
}

// TestDegradedCleanRunIsNotDegraded: a clean run stays clean, and a run that
// failed for an unrelated reason is not silently called degraded.
func TestDegradedCleanRunIsNotDegraded(t *testing.T) {
	cases := []RunResult{
		{OK: true, FinalResult: "form submitted"},
		{OK: false, Errors: []string{"no CDP endpoint"}},
	}
	for _, res := range cases {
		degraded, reasons := Degraded(res)
		if degraded {
			t.Errorf("%+v: degraded = %v, reasons = %v, want not degraded", res, degraded, reasons)
		}
		if len(reasons) != 0 {
			t.Errorf("%+v: reasons = %v, want none", res, reasons)
		}
	}
}
