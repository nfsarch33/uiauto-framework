package evolver

import (
	"io"
	"log/slog"
	"math"
	"sync"
	"time"
)

// ML-specific signal type constants (extend SignalType from signal_miner.go).
const (
	SignalAnomaly     SignalType = "anomaly"
	SignalImprovement SignalType = "improvement"
	SignalRegression  SignalType = "regression"
)

// MLSignal represents a detected behavioral signal from agent metrics.
type MLSignal struct {
	ID         string     `json:"id"`
	Type       SignalType `json:"type"`
	Metric     string     `json:"metric"`
	Value      float64    `json:"value"`
	Baseline   float64    `json:"baseline"`
	Deviation  float64    `json:"deviation"`
	Confidence float64    `json:"confidence"`
	DetectedAt time.Time  `json:"detected_at"`
	AgentID    string     `json:"agent_id,omitempty"`
}

// SignalDetector uses statistical methods to detect behavioral anomalies.
type SignalDetector struct {
	mu      sync.RWMutex
	history map[string][]float64 // metric -> recent values
	maxHist int
	zThresh float64
	logger  *slog.Logger
}

// NewSignalDetector creates a detector with the given z-score threshold.
func NewSignalDetector(zThreshold float64, maxHistory int, logger *slog.Logger) *SignalDetector {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	if maxHistory < 10 {
		maxHistory = 100
	}
	return &SignalDetector{
		history: make(map[string][]float64),
		maxHist: maxHistory,
		zThresh: zThreshold,
		logger:  logger,
	}
}

// Record adds a metric observation and returns a signal if anomalous.
func (sd *SignalDetector) Record(metric string, value float64) *MLSignal {
	sd.mu.Lock()
	defer sd.mu.Unlock()

	hist := sd.history[metric]
	hist = append(hist, value)
	if len(hist) > sd.maxHist {
		hist = hist[len(hist)-sd.maxHist:]
	}
	sd.history[metric] = hist

	if len(hist) < 5 {
		return nil
	}

	mean, stddev := meanStdDev(hist[:len(hist)-1])
	if stddev == 0 {
		return nil
	}

	z := (value - mean) / stddev
	absZ := math.Abs(z)

	if absZ < sd.zThresh {
		return nil
	}

	sig := &MLSignal{
		ID:         metric + "-" + time.Now().Format("20060102150405"),
		Metric:     metric,
		Value:      value,
		Baseline:   mean,
		Deviation:  z,
		Confidence: math.Min(absZ/sd.zThresh, 1.0),
		DetectedAt: time.Now(),
	}

	if z > 0 {
		sig.Type = SignalImprovement
	} else {
		sig.Type = SignalRegression
	}

	sd.logger.Info("signal detected",
		"metric", metric,
		"type", string(sig.Type),
		"z", z,
	)
	return sig
}

// HistoryLen returns the number of observations for a metric.
func (sd *SignalDetector) HistoryLen(metric string) int {
	sd.mu.RLock()
	defer sd.mu.RUnlock()
	return len(sd.history[metric])
}

func meanStdDev(vals []float64) (float64, float64) {
	n := float64(len(vals))
	if n == 0 {
		return 0, 0
	}
	var sum float64
	for _, v := range vals {
		sum += v
	}
	mean := sum / n

	var variance float64
	for _, v := range vals {
		d := v - mean
		variance += d * d
	}
	variance /= n
	return mean, math.Sqrt(variance)
}
