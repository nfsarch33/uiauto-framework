package eval

// MeanBriers computes the two Brier means and their input counts over
// graded decisions, kept
// separate because their scales differ: multiclass choice Brier ranges
// 0..2, binary noul Brier 0..1. A suite-level mean mixing the two would
// be uninterpretable, so no combined score exists.
//
// Multiclass Brier for one choice decision is the sum over the classes in
// Dist UNION {Want} of (p(c) - 1[c == Want])^2, with p(c) = 0 for a class
// the distribution omits -- the golden class is always scored, so a model
// that leaves it out pays the full penalty. A nil Dist is scored as a
// one-hot on Got (0 when correct, 2 when wrong). Binary (noul) Brier is
// (p - y)^2 with the golden boolean.
func MeanBriers(records []RunRecord) (choice, noul float64, choiceN, noulN int) {
	choiceSum, choiceN, noulSum, noulN := 0.0, 0, 0.0, 0
	for _, r := range records {
		for _, d := range r.Decisions {
			switch {
			case d.Type == "choice" && d.Want != "":
				choiceSum += multiclassBrier(d)
				choiceN++
			case d.Type == "noul" && d.WantBool != nil && d.NoulValue != nil:
				y := 0.0
				if *d.WantBool {
					y = 1
				}
				diff := *d.NoulValue - y
				noulSum += diff * diff
				noulN++
			}
		}
	}
	if choiceN > 0 {
		choice = choiceSum / float64(choiceN)
	}
	if noulN > 0 {
		noul = noulSum / float64(noulN)
	}
	return choice, noul, choiceN, noulN
}

// multiclassBrier scores one graded choice decision.
func multiclassBrier(d DecisionRecord) float64 {
	dist := d.Dist
	if len(dist) == 0 {
		// No distribution reported (nil, or empty -- an empty map is what
		// a report round-trip turns nil into): treat as one-hot on the
		// model's pick, so live and reloaded records score identically.
		dist = map[string]float64{d.Got: 1}
	}
	classes := make(map[string]struct{}, len(dist)+1)
	for c := range dist {
		classes[c] = struct{}{}
	}
	classes[d.Want] = struct{}{} // the golden class always scores
	sum := 0.0
	for c := range classes {
		target := 0.0
		if c == d.Want {
			target = 1
		}
		diff := dist[c] - target // dist[c] is 0 when absent
		sum += diff * diff
	}
	return sum
}
