package stats

import (
	"encoding/json"
	"math"
	"os"
	"testing"
)

// golden is testdata/golden.json: the same inputs and expected outputs are
// committed beside every copy of these functions, so two copies cannot drift
// apart without one of their golden tests going red.
type golden struct {
	Wilson []struct {
		K, N      int
		Z, Lo, Hi float64
		Tol       float64
	} `json:"wilson"`
	McNemarExact []struct {
		B, C int
		P    float64
	} `json:"mcnemar_exact"`
	Kappa []struct {
		Table           [2][2]int
		Kappa, TPR, TNR float64
	} `json:"kappa"`
	PassHatK []struct {
		N, C, K int
		P       float64
	} `json:"pass_hat_k"`
	GeoMedian []struct {
		Vectors [][]float64
		OK      bool
		Median  []float64
	} `json:"geo_median"`
}

func loadGolden(t *testing.T) golden {
	t.Helper()
	data, err := os.ReadFile("testdata/golden.json")
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	var g golden
	if err := json.Unmarshal(data, &g); err != nil {
		t.Fatalf("parse golden: %v", err)
	}
	return g
}

func TestGoldenFixture(t *testing.T) {
	g := loadGolden(t)
	const tol = 1e-9
	for _, c := range g.Wilson {
		lo, hi := Wilson(c.K, c.N, c.Z)
		if math.Abs(lo-c.Lo) > c.Tol+tol || math.Abs(hi-c.Hi) > c.Tol+tol {
			t.Errorf("Wilson(%d, %d) = [%.4f, %.4f], golden [%.4f, %.4f]", c.K, c.N, lo, hi, c.Lo, c.Hi)
		}
	}
	for _, c := range g.McNemarExact {
		if p := McNemarExact(c.B, c.C); math.Abs(p-c.P) > tol {
			t.Errorf("McNemarExact(%d, %d) = %v, golden %v", c.B, c.C, p, c.P)
		}
	}
	for _, c := range g.Kappa {
		k, tpr, tnr := Kappa(c.Table)
		if math.Abs(k-c.Kappa) > tol || math.Abs(tpr-c.TPR) > tol || math.Abs(tnr-c.TNR) > tol {
			t.Errorf("Kappa(%v) = %v %v %v, golden %v %v %v", c.Table, k, tpr, tnr, c.Kappa, c.TPR, c.TNR)
		}
	}
	for _, c := range g.PassHatK {
		if p := PassHatK(c.N, c.C, c.K); math.Abs(p-c.P) > tol {
			t.Errorf("PassHatK(%d, %d, %d) = %v, golden %v", c.N, c.C, c.K, p, c.P)
		}
	}
	for _, c := range g.GeoMedian {
		m, ok := GeoMedian(c.Vectors)
		if ok != c.OK {
			t.Errorf("GeoMedian(%v) ok = %v, golden %v", c.Vectors, ok, c.OK)
			continue
		}
		for i := range c.Median {
			if math.Abs(m[i]-c.Median[i]) > 1e-6 {
				t.Errorf("GeoMedian(%v) = %v, golden %v", c.Vectors, m, c.Median)
			}
		}
	}
}
