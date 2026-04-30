package uiauto

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync/atomic"
	"time"
)

// JudgeTestCase defines one ground-truth case for VLM accuracy evaluation.
type JudgeTestCase struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	Screenshot  []byte `json:"-"`
	Expected    bool   `json:"expected"` // ground truth
}

// JudgeCaseResult records the judge's output for one test case.
type JudgeCaseResult struct {
	CaseID      string        `json:"case_id"`
	Expected    bool          `json:"expected"`
	Predicted   bool          `json:"predicted"`
	Confidence  float64       `json:"confidence"`
	Explanation string        `json:"explanation"`
	Correct     bool          `json:"correct"`
	Latency     time.Duration `json:"latency_ns"`
}

// JudgeEvalReport aggregates accuracy results.
type JudgeEvalReport struct {
	TotalCases    int               `json:"total_cases"`
	Correct       int               `json:"correct"`
	Accuracy      float64           `json:"accuracy"`
	TruePositive  int               `json:"true_positive"`
	FalsePositive int               `json:"false_positive"`
	TrueNegative  int               `json:"true_negative"`
	FalseNegative int               `json:"false_negative"`
	Precision     float64           `json:"precision"`
	Recall        float64           `json:"recall"`
	F1Score       float64           `json:"f1_score"`
	AvgConfidence float64           `json:"avg_confidence"`
	AvgLatencyMs  float64           `json:"avg_latency_ms"`
	Results       []JudgeCaseResult `json:"results"`
}

// JudgeEvalMetrics tracks running accuracy statistics.
type JudgeEvalMetrics struct {
	TotalEvals    int64
	CorrectEvals  int64
	TotalLatMs    int64
	AvgConfidence float64
}

// VLMJudgeEvaluator runs accuracy evaluation of the VLM-as-judge pipeline.
type VLMJudgeEvaluator struct {
	vlm     *VLMBridge
	Metrics *JudgeEvalMetrics
	logger  *slog.Logger

	confThreshold float64
}

// NewVLMJudgeEvaluator creates an evaluator with the given confidence threshold.
func NewVLMJudgeEvaluator(vlm *VLMBridge, confThreshold float64, logger *slog.Logger) *VLMJudgeEvaluator {
	if confThreshold <= 0 {
		confThreshold = 0.6
	}
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &VLMJudgeEvaluator{
		vlm:           vlm,
		Metrics:       &JudgeEvalMetrics{},
		logger:        logger,
		confThreshold: confThreshold,
	}
}

// EvaluateCase runs a single test case through the VLM judge.
func (e *VLMJudgeEvaluator) EvaluateCase(ctx context.Context, tc JudgeTestCase) (JudgeCaseResult, error) {
	start := time.Now()
	atomic.AddInt64(&e.Metrics.TotalEvals, 1)

	judgment, err := e.vlm.VLMAsJudge(ctx, tc.Description, tc.Screenshot)
	if err != nil {
		return JudgeCaseResult{
			CaseID:   tc.ID,
			Expected: tc.Expected,
			Correct:  false,
			Latency:  time.Since(start),
		}, fmt.Errorf("vlm judge failed for case %s: %w", tc.ID, err)
	}

	predicted := judgment.Present && judgment.Confidence >= e.confThreshold
	correct := predicted == tc.Expected
	if correct {
		atomic.AddInt64(&e.Metrics.CorrectEvals, 1)
	}
	latency := time.Since(start)
	atomic.AddInt64(&e.Metrics.TotalLatMs, int64(latency/time.Millisecond))

	return JudgeCaseResult{
		CaseID:      tc.ID,
		Expected:    tc.Expected,
		Predicted:   predicted,
		Confidence:  judgment.Confidence,
		Explanation: judgment.Explanation,
		Correct:     correct,
		Latency:     latency,
	}, nil
}

// RunBatch evaluates all test cases and returns an accuracy report.
func (e *VLMJudgeEvaluator) RunBatch(ctx context.Context, cases []JudgeTestCase) (*JudgeEvalReport, error) {
	report := &JudgeEvalReport{
		TotalCases: len(cases),
		Results:    make([]JudgeCaseResult, 0, len(cases)),
	}

	var totalConf float64
	var totalLat time.Duration

	for _, tc := range cases {
		result, err := e.EvaluateCase(ctx, tc)
		if err != nil {
			e.logger.Warn("judge eval case failed", "case", tc.ID, "error", err)
			report.Results = append(report.Results, result)
			continue
		}

		report.Results = append(report.Results, result)
		totalConf += result.Confidence
		totalLat += result.Latency

		if result.Correct {
			report.Correct++
		}

		switch {
		case result.Expected && result.Predicted:
			report.TruePositive++
		case !result.Expected && result.Predicted:
			report.FalsePositive++
		case !result.Expected && !result.Predicted:
			report.TrueNegative++
		default:
			report.FalseNegative++
		}
	}

	if report.TotalCases > 0 {
		report.Accuracy = float64(report.Correct) / float64(report.TotalCases)
		report.AvgConfidence = totalConf / float64(report.TotalCases)
		report.AvgLatencyMs = float64(totalLat.Milliseconds()) / float64(report.TotalCases)
	}

	tp := float64(report.TruePositive)
	fp := float64(report.FalsePositive)
	fn := float64(report.FalseNegative)

	if tp+fp > 0 {
		report.Precision = tp / (tp + fp)
	}
	if tp+fn > 0 {
		report.Recall = tp / (tp + fn)
	}
	if report.Precision+report.Recall > 0 {
		report.F1Score = 2 * (report.Precision * report.Recall) / (report.Precision + report.Recall)
	}

	return report, nil
}

// CurrentAccuracy returns the running accuracy [0.0, 1.0].
func (e *VLMJudgeEvaluator) CurrentAccuracy() float64 {
	total := atomic.LoadInt64(&e.Metrics.TotalEvals)
	if total == 0 {
		return 0
	}
	return float64(atomic.LoadInt64(&e.Metrics.CorrectEvals)) / float64(total)
}

// PassesTarget checks if the current accuracy meets the given target (e.g., 0.8 for 80%).
func (e *VLMJudgeEvaluator) PassesTarget(target float64) bool {
	return e.CurrentAccuracy() >= target
}

// PassesF1Target checks whether the latest batch F1 score meets the given target.
func (e *VLMJudgeEvaluator) PassesF1Target(report *JudgeEvalReport, target float64) bool {
	return report != nil && report.F1Score >= target
}
