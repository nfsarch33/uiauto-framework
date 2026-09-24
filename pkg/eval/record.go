package eval

// RunRecord is the outcome of one scenario repetition. OK is the
// scenario-level verdict (executor success + any final_contains check);
// Errors carries everything a reviewer needs to re-trace a failure.
type RunRecord struct {
	ScenarioID  string           `json:"scenario_id"`
	Executor    string           `json:"executor"`
	Attempt     int              `json:"attempt"`
	OK          bool             `json:"ok"`
	Steps       int              `json:"steps"`
	DurationSec float64          `json:"duration_s"`
	Errors      []string         `json:"errors,omitempty"`
	Decisions   []DecisionRecord `json:"decisions,omitempty"`
}

// DecisionRecord is one typed decision with its golden comparison, when
// the scenario carries one. Probability is the engine's probability for
// the golden label (choice) or the noul value itself; Brier scoring uses
// the full distribution where the lane reports it.
type DecisionRecord struct {
	Question    string             `json:"question"`
	Type        string             `json:"type"`
	Got         string             `json:"got,omitempty"`
	Want        string             `json:"want,omitempty"`
	Correct     bool               `json:"correct"`
	Confidence  float64            `json:"confidence"`
	Probability float64            `json:"probability"`    // p(golden) for choice; noul value for noul
	Dist        map[string]float64 `json:"dist,omitempty"` // full choice distribution when available
	WantBool    *bool              `json:"want_bool,omitempty"`
	NoulValue   *float64           `json:"noul_value,omitempty"`
}
