// Package stats holds small evaluation statistics: the Wilson score interval,
// the exact McNemar test, Cohen's kappa with TPR and TNR, the pass^k estimator
// and the geometric median. Expected values live in testdata/golden.json.
package stats

import "math"

// Wilson returns the Wilson score interval for k successes in n trials at the
// normal quantile z (1.96 for 95%). n == 0 is the uninformative [0, 1].
func Wilson(k, n int, z float64) (lo, hi float64) {
	if n <= 0 {
		return 0, 1
	}
	p, nf := float64(k)/float64(n), float64(n)
	z2 := z * z
	den := 1 + z2/nf
	center := (p + z2/(2*nf)) / den
	half := z * math.Sqrt(p*(1-p)/nf+z2/(4*nf*nf)) / den
	return math.Max(0, center-half), math.Min(1, center+half)
}

// McNemarExact is the two-sided exact McNemar test: the binomial probability of
// a split at least as uneven as (b, c) when each discordant pair is a fair coin.
// No discordant pairs is p = 1.
func McNemarExact(b, c int) float64 {
	n := b + c
	if n == 0 {
		return 1
	}
	k := b
	if c < k {
		k = c
	}
	tail := 0.0
	for i := 0; i <= k; i++ {
		tail += math.Exp(lnChoose(n, i) - float64(n)*math.Ln2)
	}
	return math.Min(1, 2*tail)
}

// Kappa is Cohen's kappa for a 2x2 agreement table t[human][judge], index 0 =
// pass and 1 = fail, with the judge's true-positive rate (human pass, judge
// pass) and true-negative rate (human fail, judge fail). An empty table, or one
// whose chance agreement is 1, gives kappa 0.
func Kappa(t [2][2]int) (kappa, tpr, tnr float64) {
	a, b, c, d := float64(t[0][0]), float64(t[0][1]), float64(t[1][0]), float64(t[1][1])
	n := a + b + c + d
	if n == 0 {
		return 0, 0, 0
	}
	po := (a + d) / n
	pe := ((a+b)*(a+c) + (c+d)*(b+d)) / (n * n)
	if pe < 1 {
		kappa = (po - pe) / (1 - pe)
	}
	if a+b > 0 {
		tpr = a / (a + b)
	}
	if c+d > 0 {
		tnr = d / (c + d)
	}
	return kappa, tpr, tnr
}

// PassHatK is the unbiased pass^k estimator: the chance that k attempts drawn
// without replacement from n attempts with c passes all pass, C(c,k)/C(n,k).
func PassHatK(n, c, k int) float64 {
	if k <= 0 || n <= 0 || k > n || c < k {
		return 0
	}
	return math.Exp(lnChoose(c, k) - lnChoose(n, k))
}

// GeoMedian is the geometric median (Weiszfeld iteration) of the non-void
// vectors; a nil vector is a void judge and is dropped. Fewer than two valid
// vectors is void (ok == false): one judge is not a panel.
func GeoMedian(vs [][]float64) (median []float64, ok bool) {
	var pts [][]float64
	for _, v := range vs {
		if v != nil {
			pts = append(pts, v)
		}
	}
	if len(pts) < 2 {
		return nil, false
	}
	dim := len(pts[0])
	m := make([]float64, dim)
	for _, p := range pts {
		for i := range m {
			m[i] += p[i] / float64(len(pts))
		}
	}
	const eps = 1e-9
	for iter := 0; iter < 200; iter++ {
		num, den := make([]float64, dim), 0.0
		for _, p := range pts {
			d := dist(p, m)
			if d < eps {
				return append([]float64(nil), p...), true // the median sits on a sample
			}
			for i := range num {
				num[i] += p[i] / d
			}
			den += 1 / d
		}
		next := make([]float64, dim)
		for i := range next {
			next[i] = num[i] / den
		}
		moved := dist(next, m)
		m = next
		if moved < eps {
			break
		}
	}
	return m, true
}

func dist(a, b []float64) float64 {
	s := 0.0
	for i := range a {
		s += (a[i] - b[i]) * (a[i] - b[i])
	}
	return math.Sqrt(s)
}

func lnChoose(n, k int) float64 {
	a, _ := math.Lgamma(float64(n + 1))
	b, _ := math.Lgamma(float64(k + 1))
	c, _ := math.Lgamma(float64(n - k + 1))
	return a - b - c
}
