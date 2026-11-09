package comparison

import (
	"math"

	"github.com/nfsarch33/uiauto-framework/pkg/stats"
)

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

	// The p-value is the exact two-sided binomial test on the discordant pairs
	// (pkg/stats): the chi-squared approximation is poor exactly where it is used,
	// on small discordant counts. chiSq stays the reported test statistic.
	return chiSq, stats.McNemarExact(b, c)
}
