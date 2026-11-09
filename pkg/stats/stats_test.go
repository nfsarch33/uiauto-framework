package stats

import (
	"math"
	"testing"
)

func near(got, want, tol float64) bool { return math.Abs(got-want) <= tol }

// TestWilson_21of25: the 95% Wilson interval for 21 of 25.
// MUTANT: use the normal (Wald) interval instead and the bounds miss by > 0.01.
func TestWilson_21of25(t *testing.T) {
	lo, hi := Wilson(21, 25, 1.96)
	if !near(lo, 0.6535, 5e-5) || !near(hi, 0.9360, 5e-5) {
		t.Fatalf("Wilson(21, 25) = [%.4f, %.4f], want [0.6535, 0.9360]", lo, hi)
	}
	if lo, hi := Wilson(0, 0, 1.96); lo != 0 || hi != 1 {
		t.Fatalf("Wilson(0, 0) = [%v, %v], want the uninformative [0, 1]", lo, hi)
	}
}

// TestMcNemarExact: the two-sided exact binomial test on discordant pairs.
// MUTANT: drop the doubling (one-sided p) and (4, 0) reads 0.0625.
func TestMcNemarExact(t *testing.T) {
	for _, tc := range []struct {
		b, c int
		want float64
	}{{4, 0, 0.125}, {6, 0, 0.03125}, {0, 6, 0.03125}, {0, 0, 1}, {3, 3, 1}} {
		if got := McNemarExact(tc.b, tc.c); !near(got, tc.want, 1e-12) {
			t.Fatalf("McNemarExact(%d, %d) = %v, want %v", tc.b, tc.c, got, tc.want)
		}
	}
}

// TestKappa_TwoByTwo: Cohen's kappa for judge versus human labels, with the
// judge's TPR and TNR against the human label.
// MUTANT: drop the chance-agreement term and kappa reads 0.70 (raw agreement).
func TestKappa_TwoByTwo(t *testing.T) {
	k, tpr, tnr := Kappa([2][2]int{{20, 5}, {10, 15}})
	if !near(k, 0.40, 1e-12) || !near(tpr, 0.80, 1e-12) || !near(tnr, 0.60, 1e-12) {
		t.Fatalf("Kappa = %v, TPR %v, TNR %v; want 0.40, 0.80, 0.60", k, tpr, tnr)
	}
}

// TestPassHatK: the unbiased pass^k estimator C(c,k)/C(n,k).
// MUTANT: use (c/n)^k and it reads 0.512.
func TestPassHatK(t *testing.T) {
	if got := PassHatK(5, 4, 3); !near(got, 0.4, 1e-12) {
		t.Fatalf("PassHatK(5, 4, 3) = %v, want 0.4", got)
	}
	if got := PassHatK(5, 2, 3); got != 0 {
		t.Fatalf("PassHatK(5, 2, 3) = %v, want 0 (fewer passes than k)", got)
	}
}

// TestGeoMedian_VoidHandling: a void judge (nil vector) is dropped, two valid
// vectors are combined, and fewer than two valid vectors is a void result.
// MUTANT: treat a nil vector as zeros and the median is pulled toward the origin.
func TestGeoMedian_VoidHandling(t *testing.T) {
	m, ok := GeoMedian([][]float64{{1, 0}, nil, {3, 0}})
	if !ok || !near(m[0], 2, 1e-6) || !near(m[1], 0, 1e-6) {
		t.Fatalf("GeoMedian with one void = %v ok=%v, want [2 0] ok=true", m, ok)
	}
	if _, ok := GeoMedian([][]float64{{1, 0}, nil, nil}); ok {
		t.Fatal("one valid vector must be void, not a median")
	}
	m, ok = GeoMedian([][]float64{{0, 0}, {0, 0}, {10, 0}})
	if !ok || !near(m[0], 0, 1e-6) {
		t.Fatalf("GeoMedian resists the outlier: got %v, want [0 0]", m)
	}
}
