package domheal

import (
	"log/slog"
	"regexp"
	"sort"
	"strings"
	"sync"
)

// DOMFingerprint is an enhanced structural signature that includes content heuristics
// for locating elements after full DOM restructures.
type DOMFingerprint struct {
	StructuralSignature
	TextPatterns  []string       `json:"text_patterns"`
	DataAttrs     map[string]int `json:"data_attrs"`
	IDFingerprint string         `json:"id_fingerprint"`
	RoleMap       map[string]int `json:"role_map"`
	FormElements  int            `json:"form_elements"`
	LinkDensity   float64        `json:"link_density"`
}

var (
	dataAttrRe = regexp.MustCompile(`data-([a-z0-9-]+)\s*=`)
	idAttrRe   = regexp.MustCompile(`\s+id\s*=\s*["']([^"']+)["']`)
	roleAttrRe = regexp.MustCompile(`role\s*=\s*["']([^"']+)["']`)
	formTagRe  = regexp.MustCompile(`<\s*(input|select|textarea|button)[^>]*>`)
	anchorRe   = regexp.MustCompile(`<\s*a\b[^>]*>`)
)

// ParseDOMFingerprint builds a comprehensive fingerprint from raw HTML,
// including text patterns, data attributes, ARIA roles, and form density.
func ParseDOMFingerprint(html string) DOMFingerprint {
	fp := DOMFingerprint{
		StructuralSignature: ParseStructuralSignature(html),
		DataAttrs:           make(map[string]int),
		RoleMap:             make(map[string]int),
	}
	if html == "" {
		return fp
	}

	lower := strings.ToLower(html)

	for _, m := range dataAttrRe.FindAllStringSubmatch(lower, -1) {
		fp.DataAttrs[m[1]]++
	}

	ids := idAttrRe.FindAllStringSubmatch(lower, -1)
	idList := make([]string, 0, len(ids))
	for _, m := range ids {
		idList = append(idList, m[1])
	}
	sort.Strings(idList)
	fp.IDFingerprint = strings.Join(idList, ",")

	for _, m := range roleAttrRe.FindAllStringSubmatch(lower, -1) {
		fp.RoleMap[m[1]]++
	}

	fp.FormElements = len(formTagRe.FindAllString(lower, -1))

	anchorCount := len(anchorRe.FindAllString(lower, -1))
	if fp.TotalNodes > 0 {
		fp.LinkDensity = float64(anchorCount) / float64(fp.TotalNodes)
	}

	fp.TextPatterns = ExtractTextPatterns(html)

	return fp
}

// ExtractTextPatterns pulls short, distinctive text snippets from visible content.
// These act as content anchors that survive structural changes.
func ExtractTextPatterns(html string) []string {
	stripped := stripHTMLTags(html)
	stripped = strings.Join(strings.Fields(stripped), " ")

	if len(stripped) < 20 {
		return nil
	}

	words := strings.Fields(stripped)
	patterns := make(map[string]bool)
	// Slide a 4-word window to capture distinctive phrases (capped at 20).
	for i := 0; i+3 < len(words) && len(patterns) < 20; i += 4 {
		phrase := strings.Join(words[i:i+4], " ")
		if len(phrase) >= 15 && len(phrase) <= 80 {
			patterns[strings.ToLower(phrase)] = true
		}
	}

	out := make([]string, 0, len(patterns))
	for p := range patterns {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// DOMFingerprintSimilarity computes a weighted similarity score between two fingerprints.
// Weights: structure (0.4), classes (0.15), data-attrs (0.15), roles (0.1), text (0.2).
func DOMFingerprintSimilarity(a, b DOMFingerprint) float64 {
	structSim := StructuralSimilarity(a.StructuralSignature, b.StructuralSignature)
	classSim := StringSimilarity(a.ClassFingerprint, b.ClassFingerprint)
	dataSim := MapSimilarity(a.DataAttrs, b.DataAttrs)
	roleSim := MapSimilarity(a.RoleMap, b.RoleMap)
	textSim := TextPatternOverlap(a.TextPatterns, b.TextPatterns)

	return 0.40*structSim + 0.15*classSim + 0.15*dataSim + 0.10*roleSim + 0.20*textSim
}

// StringSimilarity computes Jaccard similarity between two comma-separated strings.
func StringSimilarity(a, b string) float64 {
	if a == b {
		return 1.0
	}
	if a == "" || b == "" {
		return 0.0
	}
	aSet := make(map[string]bool)
	for _, c := range strings.Split(a, ",") {
		aSet[strings.TrimSpace(c)] = true
	}
	bSet := make(map[string]bool)
	for _, c := range strings.Split(b, ",") {
		bSet[strings.TrimSpace(c)] = true
	}
	inter := 0
	for k := range aSet {
		if bSet[k] {
			inter++
		}
	}
	union := len(aSet) + len(bSet) - inter
	return float64(inter) / float64(union)
}

// MapSimilarity computes min/max Jaccard similarity between two frequency maps.
func MapSimilarity(a, b map[string]int) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 1.0
	}
	allKeys := make(map[string]bool)
	for k := range a {
		allKeys[k] = true
	}
	for k := range b {
		allKeys[k] = true
	}
	inter := 0
	union := 0
	for k := range allKeys {
		ca, cb := a[k], b[k]
		if ca < cb {
			inter += ca
			union += cb
		} else {
			inter += cb
			union += ca
		}
	}
	return float64(inter) / float64(union)
}

// TextPatternOverlap computes Jaccard overlap between two text pattern slices.
func TextPatternOverlap(a, b []string) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 1.0
	}
	if len(a) == 0 || len(b) == 0 {
		return 0.0
	}
	aSet := make(map[string]bool, len(a))
	for _, p := range a {
		aSet[p] = true
	}
	match := 0
	for _, p := range b {
		if aSet[p] {
			match++
		}
	}
	union := len(a) + len(b) - match
	return float64(match) / float64(union)
}

// FingerprintMatcher extends StructuralMatcher with content-based fingerprinting.
type FingerprintMatcher struct {
	known     map[string]DOMFingerprint
	threshold float64
	logger    *slog.Logger
	mu        sync.RWMutex
}

// NewFingerprintMatcher creates a matcher using the V2 DOMFingerprint algorithm.
func NewFingerprintMatcher(threshold float64, logger *slog.Logger) *FingerprintMatcher {
	if threshold <= 0 {
		threshold = 0.7
	}
	return &FingerprintMatcher{
		known:     make(map[string]DOMFingerprint),
		threshold: threshold,
		logger:    logger,
	}
}

// CheckAndUpdate compares current HTML against stored DOMFingerprint baseline.
func (fm *FingerprintMatcher) CheckAndUpdate(pageID, html string) StructuralMatchResult {
	current := ParseDOMFingerprint(html)
	fm.mu.Lock()
	prev, exists := fm.known[pageID]
	fm.known[pageID] = current
	fm.mu.Unlock()

	if !exists {
		return StructuralMatchResult{
			PageID:     pageID,
			Similarity: 1.0,
			IsNew:      true,
		}
	}

	sim := DOMFingerprintSimilarity(prev, current)
	drifted := sim < fm.threshold

	if drifted && fm.logger != nil {
		fm.logger.Warn("DOM fingerprint drift detected",
			"page_id", pageID,
			"similarity", sim,
			"threshold", fm.threshold,
		)
	}

	return StructuralMatchResult{
		PageID:     pageID,
		Similarity: sim,
		Drifted:    drifted,
		IsNew:      false,
	}
}

// FindClosestMatch searches all known fingerprints for the most similar one.
func (fm *FingerprintMatcher) FindClosestMatch(html string) ClosestMatchResult {
	current := ParseDOMFingerprint(html)
	best := ClosestMatchResult{}

	fm.mu.RLock()
	defer fm.mu.RUnlock()

	for id, sig := range fm.known {
		sim := DOMFingerprintSimilarity(current, sig)
		if sim > best.Similarity {
			best.PageID = id
			best.Similarity = sim
			best.Found = true
		}
	}

	if best.Similarity < fm.threshold {
		best.Found = false
	}

	return best
}

// KnownCount returns the number of stored fingerprints.
func (fm *FingerprintMatcher) KnownCount() int {
	fm.mu.RLock()
	defer fm.mu.RUnlock()
	return len(fm.known)
}

// Threshold returns the matcher's threshold.
func (fm *FingerprintMatcher) Threshold() float64 {
	return fm.threshold
}
