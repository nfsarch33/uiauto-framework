package evolver

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSignalDetector_NoSignalWithFewPoints(t *testing.T) {
	sd := NewSignalDetector(2.0, 100, nil)
	for i := 0; i < 4; i++ {
		sig := sd.Record("latency", 100.0)
		assert.Nil(t, sig)
	}
	assert.Equal(t, 4, sd.HistoryLen("latency"))
}

func TestSignalDetector_DetectsAnomaly(t *testing.T) {
	sd := NewSignalDetector(2.0, 100, nil)
	for i := 0; i < 20; i++ {
		sd.Record("latency", 100.0+float64(i%3))
	}

	sig := sd.Record("latency", 500.0)
	require.NotNil(t, sig)
	assert.Equal(t, SignalImprovement, sig.Type)
	assert.Equal(t, "latency", sig.Metric)
	assert.Greater(t, sig.Confidence, 0.0)
}

func TestSignalDetector_DetectsRegression(t *testing.T) {
	sd := NewSignalDetector(2.0, 100, nil)
	for i := 0; i < 20; i++ {
		sd.Record("success_rate", 95.0+float64(i%2))
	}

	sig := sd.Record("success_rate", 50.0)
	require.NotNil(t, sig)
	assert.Equal(t, SignalRegression, sig.Type)
}

func TestSignalDetector_NoSignalForNormalValues(t *testing.T) {
	sd := NewSignalDetector(2.0, 100, nil)
	for i := 0; i < 20; i++ {
		sd.Record("cpu", 50.0+float64(i%5))
	}
	sig := sd.Record("cpu", 52.0)
	assert.Nil(t, sig)
}

func TestSignalDetector_HistoryTruncation(t *testing.T) {
	sd := NewSignalDetector(2.0, 15, nil)
	for i := 0; i < 30; i++ {
		sd.Record("mem", float64(i))
	}
	assert.Equal(t, 15, sd.HistoryLen("mem"))
}

func TestMeanStdDev(t *testing.T) {
	mean, std := meanStdDev([]float64{10, 10, 10, 10})
	assert.Equal(t, 10.0, mean)
	assert.Equal(t, 0.0, std)

	mean2, std2 := meanStdDev([]float64{})
	assert.Equal(t, 0.0, mean2)
	assert.Equal(t, 0.0, std2)
}
