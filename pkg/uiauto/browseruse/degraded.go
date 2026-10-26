package browseruse

import "strings"

// Degraded reason labels. They are stable and machine-readable: a consumer
// compares them, it does not parse prose.
const (
	// DegradedTruncated marks a run whose model output was cut off at the
	// completion-token ceiling, so the structured output never arrived.
	DegradedTruncated = "model_output_truncated"
	// DegradedInvalidOutput marks a run whose final action never parsed as
	// the executor's structured AgentOutput.
	DegradedInvalidOutput = "agent_output_invalid"
)

// Degraded reports whether a run's history carries a degradation marker, and
// which markers it carries.
//
// The lane's own ok flag is not the question this answers. The executor loop
// can finish, the lane can report ok=true, and the run can still have
// produced no outcome at all: the model's output was truncated at the
// completion-token ceiling, or the final action was prose where the
// executor's structured AgentOutput was expected, so the action never parsed.
// Grading such a run as a clean pass is the defect this distinguishes.
//
// The markers are the two the executor lane actually emits:
//
//	"Model output was truncated at max_completion_tokens=N; ..."
//	"<n> validation error(s) for AgentOutput\n ..."
//
// A run is degraded when either marker is present in Errors. The returned
// reasons hold one label per distinct marker, in the order the markers first
// appear, so two runs with the same degradation compare equal.
func Degraded(res RunResult) (bool, []string) {
	reasons := []string{}
	for _, e := range res.Errors {
		if strings.Contains(e, "truncated at max_completion_tokens") &&
			!containsReason(reasons, DegradedTruncated) {
			reasons = append(reasons, DegradedTruncated)
		}
		if strings.Contains(e, "validation error") && strings.Contains(e, "for AgentOutput") &&
			!containsReason(reasons, DegradedInvalidOutput) {
			reasons = append(reasons, DegradedInvalidOutput)
		}
	}
	return len(reasons) > 0, reasons
}

func containsReason(reasons []string, want string) bool {
	for _, r := range reasons {
		if r == want {
			return true
		}
	}
	return false
}
