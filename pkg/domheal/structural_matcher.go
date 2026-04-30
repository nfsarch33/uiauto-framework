package domheal

import (
	"log/slog"
	"regexp"
	"strings"
)

// HTMLNode represents a simplified DOM node for structural comparison.
type HTMLNode struct {
	Tag        string     `json:"tag"`
	Classes    []string   `json:"classes,omitempty"`
	Attributes []string   `json:"attributes,omitempty"`
	Children   []HTMLNode `json:"children,omitempty"`
}

// StructuralSignature is a compact fingerprint of a page's DOM structure.
type StructuralSignature struct {
	TagCounts        map[string]int `json:"tag_counts"`
	MaxDepth         int            `json:"max_depth"`
	TotalNodes       int            `json:"total_nodes"`
	ClassFingerprint string         `json:"class_fingerprint"`
}

// StructuralSimilarity computes Jaccard similarity between two tag-count maps.
// Returns a value in [0, 1] where 1 means identical structure.
func StructuralSimilarity(a, b StructuralSignature) float64 {
	if a.TotalNodes == 0 && b.TotalNodes == 0 {
		return 1.0
	}

	allTags := make(map[string]bool)
	for tag := range a.TagCounts {
		allTags[tag] = true
	}
	for tag := range b.TagCounts {
		allTags[tag] = true
	}

	intersection := 0
	union := 0
	for tag := range allTags {
		ca := a.TagCounts[tag]
		cb := b.TagCounts[tag]
		if ca < cb {
			intersection += ca
			union += cb
		} else {
			intersection += cb
			union += ca
		}
	}

	return float64(intersection) / float64(union)
}

// ParseStructuralSignature builds a signature from raw HTML by extracting
// tag names, class lists, and nesting depth via a lightweight regex scanner.
// This avoids a full DOM parser dependency for the structural comparison use case.
func ParseStructuralSignature(html string) StructuralSignature {
	sig := StructuralSignature{
		TagCounts: make(map[string]int),
	}
	if html == "" {
		return sig
	}

	tagRe := regexp.MustCompile(`<\s*([a-zA-Z][a-zA-Z0-9]*)[^>]*>`)
	classRe := regexp.MustCompile(`class\s*=\s*["']([^"']+)["']`)
	closeRe := regexp.MustCompile(`</\s*([a-zA-Z][a-zA-Z0-9]*)\s*>`)

	depth := 0
	classSet := make(map[string]bool)

	openMatches := tagRe.FindAllStringSubmatch(html, -1)
	closeMatches := closeRe.FindAllStringSubmatch(html, -1)

	for _, m := range openMatches {
		tag := strings.ToLower(m[0])
		tagName := strings.ToLower(m[1])
		sig.TagCounts[tagName]++
		sig.TotalNodes++
		depth++
		if depth > sig.MaxDepth {
			sig.MaxDepth = depth
		}

		if cm := classRe.FindStringSubmatch(tag); cm != nil {
			for _, cls := range strings.Fields(cm[1]) {
				classSet[cls] = true
			}
		}
	}

	for _, m := range closeMatches {
		_ = m[1]
		depth--
		if depth < 0 {
			depth = 0
		}
	}

	classes := make([]string, 0, len(classSet))
	for cls := range classSet {
		classes = append(classes, cls)
	}
	sig.ClassFingerprint = strings.Join(classes, ",")

	return sig
}

// StructuralMatcher tracks known page signatures and detects structural drift.
type StructuralMatcher struct {
	known     map[string]StructuralSignature
	threshold float64
	logger    *slog.Logger
}

// NewStructuralMatcher creates a matcher with a minimum similarity threshold.
// Pages scoring below threshold are flagged as structurally drifted.
func NewStructuralMatcher(threshold float64, logger *slog.Logger) *StructuralMatcher {
	if threshold <= 0 {
		threshold = 0.7
	}
	return &StructuralMatcher{
		known:     make(map[string]StructuralSignature),
		threshold: threshold,
		logger:    logger,
	}
}

// StructuralMatchResult reports the outcome of a structural comparison.
type StructuralMatchResult struct {
	PageID     string  `json:"page_id"`
	Similarity float64 `json:"similarity"`
	Drifted    bool    `json:"drifted"`
	IsNew      bool    `json:"is_new"`
}

// CheckAndUpdate compares current HTML structure against the stored baseline.
func (sm *StructuralMatcher) CheckAndUpdate(pageID, html string) StructuralMatchResult {
	current := ParseStructuralSignature(html)
	prev, exists := sm.known[pageID]
	sm.known[pageID] = current

	if !exists {
		return StructuralMatchResult{
			PageID:     pageID,
			Similarity: 1.0,
			Drifted:    false,
			IsNew:      true,
		}
	}

	sim := StructuralSimilarity(prev, current)
	drifted := sim < sm.threshold

	if drifted && sm.logger != nil {
		sm.logger.Warn("structural drift detected",
			"page_id", pageID,
			"similarity", sim,
			"threshold", sm.threshold,
			"prev_nodes", prev.TotalNodes,
			"curr_nodes", current.TotalNodes,
		)
	}

	return StructuralMatchResult{
		PageID:     pageID,
		Similarity: sim,
		Drifted:    drifted,
		IsNew:      false,
	}
}

// KnownSignatures returns a copy of all stored page signatures.
func (sm *StructuralMatcher) KnownSignatures() map[string]StructuralSignature {
	out := make(map[string]StructuralSignature, len(sm.known))
	for k, v := range sm.known {
		out[k] = v
	}
	return out
}

// ClosestMatchResult reports the best structural match from known pages.
type ClosestMatchResult struct {
	PageID     string  `json:"page_id"`
	Similarity float64 `json:"similarity"`
	Found      bool    `json:"found"`
}

// FindClosestMatch searches all known page signatures for the one most
// structurally similar to the given HTML. Returns the best match above
// the matcher's threshold.
func (sm *StructuralMatcher) FindClosestMatch(html string) ClosestMatchResult {
	current := ParseStructuralSignature(html)
	best := ClosestMatchResult{}

	for id, sig := range sm.known {
		sim := StructuralSimilarity(current, sig)
		if sim > best.Similarity {
			best.PageID = id
			best.Similarity = sim
			best.Found = true
		}
	}

	if best.Similarity < sm.threshold {
		best.Found = false
	}

	return best
}

// stripHTMLTags removes HTML tags from a string, leaving only text content.
func stripHTMLTags(s string) string {
	tagRe := regexp.MustCompile(`<[^>]*>`)
	return tagRe.ReplaceAllString(s, " ")
}
