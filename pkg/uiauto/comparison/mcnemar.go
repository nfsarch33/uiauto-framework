package comparison

import "math"

// McNemar computes McNemar's chi-squared statistic and approximate p-value
// for a 2x2 contingency table of paired binary outcomes.
//
// The test assesses whether discordant pairs (b, c) differ significantly:
//
//	chi2 = (b - c)^2 / (b + c)
//
// where b = OnlyCtrl (control pass, treatment fail)
// and   c = OnlyTreat (control fail, treatment pass).
//
// Uses continuity correction when total discordant < 25.
func McNemar(b, c int) (chiSq float64, pValue float64) {
	n := b + c
	if n == 0 {
		return 0, 1.0
	}

	diff := float64(b - c)

	if n < 25 {
		// Yates continuity correction
		diff = math.Abs(diff) - 0.5
		if diff < 0 {
			diff = 0
		}
		chiSq = (diff * diff) / float64(n)
	} else {
		chiSq = (diff * diff) / float64(n)
	}

	// Approximate p-value from chi-squared distribution (1 df)
	pValue = chiSquaredSurvival(chiSq, 1)
	return chiSq, pValue
}

// chiSquaredSurvival computes the survival function P(X > x) for chi-squared
// with k degrees of freedom using the regularized incomplete gamma function.
func chiSquaredSurvival(x float64, k int) float64 {
	if x <= 0 {
		return 1.0
	}
	return 1.0 - regularizedGammaP(float64(k)/2.0, x/2.0)
}

// regularizedGammaP computes the regularized lower incomplete gamma function
// P(a, x) = gamma(a, x) / Gamma(a) using series expansion.
func regularizedGammaP(a, x float64) float64 {
	if x < 0 {
		return 0
	}
	if x == 0 {
		return 0
	}

	// Series expansion: P(a,x) = e^(-x) * x^a * sum_{n=0}^{inf} x^n / (a*(a+1)*...*(a+n))
	sum := 1.0 / a
	term := 1.0 / a
	for n := 1; n < 200; n++ {
		term *= x / (a + float64(n))
		sum += term
		if math.Abs(term/sum) < 1e-14 {
			break
		}
	}

	logP := -x + a*math.Log(x) - lgamma(a) + math.Log(sum)
	return math.Exp(logP)
}

func lgamma(x float64) float64 {
	v, _ := math.Lgamma(x)
	return v
}
