package eval

// MeanBrier computes the mean Brier score over all golden decisions in
// the records. For choice decisions with a full distribution it is the
// multiclass Brier: sum over classes of (p(c) - 1[c==golden])^2. For noul
// decisions it is (p - y)^2 with the golden boolean. Lower is better;
// 0.25 is chance-level for a binary question.
func MeanBrier(records []RunRecord) float64 {
	sum, n := 0.0, 0
	for _, r := range records {
		for _, d := range r.Decisions {
			switch d.Type {
			case "choice":
				if d.Want == "" {
					continue
				}
				sum += choiceBrier(d)
				n++
			case "noul":
				if d.WantBool == nil || d.NoulValue == nil {
					continue
				}
				y := 0.0
				if *d.WantBool {
					y = 1
				}
				diff := *d.NoulValue - y
				sum += diff * diff
				n++
			}
		}
	}
	if n == 0 {
		return 0
	}
	return sum / float64(n)
}

// choiceBrier: with a distribution, multiclass over its classes; without
// one, binary on p(golden).
func choiceBrier(d DecisionRecord) float64 {
	if len(d.Dist) == 0 {
		diff := d.Probability - 1
		return diff * diff
	}
	sum := 0.0
	for label, p := range d.Dist {
		y := 0.0
		if label == d.Want {
			y = 1
		}
		diff := p - y
		sum += diff * diff
	}
	return sum
}
