package domheal

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// RepairStrategy identifies the method used during selector repair.
type RepairStrategy string

// Repair strategies for selector recovery.
const (
	StrategyCSS         RepairStrategy = "css"
	StrategyXPath       RepairStrategy = "xpath"
	StrategyARIA        RepairStrategy = "aria"
	StrategyTextContent RepairStrategy = "text_content"
	StrategyVLM         RepairStrategy = "vlm"
)

// RepairCandidate represents a single candidate selector produced by a repair strategy.
type RepairCandidate struct {
	Selector   string         `json:"selector"`
	Strategy   RepairStrategy `json:"strategy"`
	Confidence float64        `json:"confidence"`
	MatchCount int            `json:"match_count"`
}

// RepairResult is the outcome of a multi-strategy selector repair attempt.
type RepairResult struct {
	OriginalSelector string            `json:"original_selector"`
	BestCandidate    *RepairCandidate  `json:"best_candidate,omitempty"`
	Candidates       []RepairCandidate `json:"candidates"`
	StrategiesTried  int               `json:"strategies_tried"`
	Duration         time.Duration     `json:"duration_ns"`
	Repaired         bool              `json:"repaired"`
}

// SelectorEvaluator tests a selector against a live DOM and returns match count.
type SelectorEvaluator func(ctx context.Context, selector string) (matchCount int, err error)

// VLMSelectorGenerator uses a vision-language model to propose selectors from a screenshot.
type VLMSelectorGenerator func(ctx context.Context, elementDescription string, screenshot []byte) ([]string, error)

// SelectorRepairV2Metrics holds Prometheus counters for repair operations.
type SelectorRepairV2Metrics struct {
	RepairAttempts *prometheus.CounterVec
	RepairSuccess  *prometheus.CounterVec
	RepairDuration *prometheus.HistogramVec
}

// NewSelectorRepairV2Metrics registers repair metrics with the given registerer.
func NewSelectorRepairV2Metrics(reg prometheus.Registerer) *SelectorRepairV2Metrics {
	m := &SelectorRepairV2Metrics{
		RepairAttempts: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "domheal",
			Subsystem: "selector_repair",
			Name:      "attempts_total",
			Help:      "Total selector repair attempts by strategy.",
		}, []string{"strategy"}),
		RepairSuccess: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "domheal",
			Subsystem: "selector_repair",
			Name:      "success_total",
			Help:      "Successful selector repairs by strategy.",
		}, []string{"strategy"}),
		RepairDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "domheal",
			Subsystem: "selector_repair",
			Name:      "duration_seconds",
			Help:      "Duration of selector repair operations by strategy.",
			Buckets:   []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5},
		}, []string{"strategy"}),
	}
	reg.MustRegister(m.RepairAttempts, m.RepairSuccess, m.RepairDuration)
	return m
}

// SelectorRepairV2 implements a multi-strategy selector repair engine.
// Fallback chain: CSS variations -> XPath -> ARIA attributes -> text content -> VLM.
type SelectorRepairV2 struct {
	repairLog *RepairLog
	metrics   *SelectorRepairV2Metrics
	logger    *slog.Logger
	vlmGen    VLMSelectorGenerator

	// CSS alternation rules: class fragments, data-attribute patterns
	cssAlternations []CSSAlternation

	mu         sync.RWMutex
	repairHist map[string]RepairResult
}

// CSSAlternation defines a CSS selector variation pattern.
type CSSAlternation struct {
	Name     string
	Generate func(original string) []string
}

// NewSelectorRepairV2 creates a repair engine with the default strategy chain.
func NewSelectorRepairV2(repairLog *RepairLog, logger *slog.Logger) *SelectorRepairV2 {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &SelectorRepairV2{
		repairLog:       repairLog,
		logger:          logger,
		repairHist:      make(map[string]RepairResult),
		cssAlternations: defaultCSSAlternations(),
	}
}

// WithMetrics attaches Prometheus metrics to the repair engine.
func (sr *SelectorRepairV2) WithMetrics(m *SelectorRepairV2Metrics) {
	sr.metrics = m
}

// WithVLM attaches a VLM-based selector generator for the final fallback tier.
func (sr *SelectorRepairV2) WithVLM(gen VLMSelectorGenerator) {
	sr.vlmGen = gen
}

// Repair attempts to find a working replacement for a broken selector using
// the multi-strategy fallback chain. The evaluator tests candidates against live DOM.
func (sr *SelectorRepairV2) Repair(ctx context.Context, elementType, brokenSelector string, evaluator SelectorEvaluator) RepairResult {
	start := time.Now()
	result := RepairResult{
		OriginalSelector: brokenSelector,
	}

	strategies := []struct {
		name     RepairStrategy
		generate func() []string
	}{
		{StrategyCSS, func() []string { return sr.generateCSSVariants(brokenSelector) }},
		{StrategyXPath, func() []string { return sr.generateXPathVariants(brokenSelector) }},
		{StrategyARIA, func() []string { return sr.generateARIACandidates(elementType) }},
		{StrategyTextContent, func() []string { return sr.generateTextContentCandidates(elementType) }},
	}

	for _, strat := range strategies {
		result.StrategiesTried++
		candidates := strat.generate()
		sr.recordAttempt(strat.name)

		for _, candidate := range candidates {
			count, err := evaluator(ctx, candidate)
			if err != nil {
				continue
			}
			if count > 0 {
				rc := RepairCandidate{
					Selector:   candidate,
					Strategy:   strat.name,
					Confidence: strategyConfidence(strat.name, count),
					MatchCount: count,
				}
				result.Candidates = append(result.Candidates, rc)

				if result.BestCandidate == nil || rc.Confidence > result.BestCandidate.Confidence {
					best := rc
					result.BestCandidate = &best
				}
			}
		}

		if result.BestCandidate != nil && result.BestCandidate.Confidence >= 0.8 {
			break
		}
	}

	// VLM fallback: only if no high-confidence candidate found and VLM generator is wired
	if (result.BestCandidate == nil || result.BestCandidate.Confidence < 0.8) && sr.vlmGen != nil {
		result.StrategiesTried++
		sr.recordAttempt(StrategyVLM)

		vlmSelectors, vlmErr := sr.vlmGen(ctx, elementType, nil)
		if vlmErr == nil {
			for _, candidate := range vlmSelectors {
				count, err := evaluator(ctx, candidate)
				if err != nil {
					continue
				}
				if count > 0 {
					rc := RepairCandidate{
						Selector:   candidate,
						Strategy:   StrategyVLM,
						Confidence: strategyConfidence(StrategyVLM, count),
						MatchCount: count,
					}
					result.Candidates = append(result.Candidates, rc)

					if result.BestCandidate == nil || rc.Confidence > result.BestCandidate.Confidence {
						best := rc
						result.BestCandidate = &best
					}
				}
			}
		} else {
			sr.logger.Warn("VLM selector generation failed",
				"element", elementType,
				"error", vlmErr,
			)
		}
	}

	result.Duration = time.Since(start)
	result.Repaired = result.BestCandidate != nil

	if result.Repaired {
		sr.recordSuccess(result.BestCandidate.Strategy)
		sr.logRepairSuggestion(elementType, brokenSelector, result.BestCandidate)
	}

	sr.mu.Lock()
	sr.repairHist[elementType+":"+brokenSelector] = result
	sr.mu.Unlock()

	sr.logger.Info("selector repair completed",
		"element", elementType,
		"repaired", result.Repaired,
		"strategies_tried", result.StrategiesTried,
		"candidates_found", len(result.Candidates),
		"duration", result.Duration,
	)

	return result
}

// RepairHistory returns the repair history for diagnostics.
func (sr *SelectorRepairV2) RepairHistory() map[string]RepairResult {
	sr.mu.RLock()
	defer sr.mu.RUnlock()
	out := make(map[string]RepairResult, len(sr.repairHist))
	for k, v := range sr.repairHist {
		out[k] = v
	}
	return out
}

func (sr *SelectorRepairV2) generateCSSVariants(original string) []string {
	var variants []string
	for _, alt := range sr.cssAlternations {
		variants = append(variants, alt.Generate(original)...)
	}
	return variants
}

func (sr *SelectorRepairV2) generateXPathVariants(cssSelector string) []string {
	tag, classes, attrs := parseCSSSelector(cssSelector)
	var xpaths []string

	if tag != "" && len(classes) > 0 {
		xpaths = append(xpaths, fmt.Sprintf("//%s[contains(@class, '%s')]", tag, classes[0]))
	}
	if tag != "" {
		xpaths = append(xpaths, fmt.Sprintf("//%s", tag))
	}
	for _, attr := range attrs {
		if tag != "" {
			xpaths = append(xpaths, fmt.Sprintf("//%s[@%s]", tag, attr))
		}
	}
	if len(classes) > 0 {
		xpaths = append(xpaths, fmt.Sprintf("//*[contains(@class, '%s')]", classes[0]))
	}

	return xpaths
}

func (sr *SelectorRepairV2) generateARIACandidates(elementType string) []string {
	ariaMap := map[string][]string{
		"navigation":      {"[role='navigation']", "nav", "[aria-label='navigation']"},
		"main_content":    {"[role='main']", "main", "#main-content", "[aria-label='main content']"},
		"sidebar":         {"[role='complementary']", "aside", "[aria-label='sidebar']"},
		"search":          {"[role='search']", "[type='search']", "[aria-label='search']"},
		"heading":         {"[role='heading']", "h1, h2, h3", "[aria-level]"},
		"button":          {"[role='button']", "button", "[type='submit']"},
		"link":            {"[role='link']", "a[href]"},
		"form":            {"[role='form']", "form"},
		"list":            {"[role='list']", "ul, ol"},
		"listitem":        {"[role='listitem']", "li"},
		"table":           {"[role='table']", "table"},
		"dialog":          {"[role='dialog']", "[role='alertdialog']"},
		"menu":            {"[role='menu']", "[role='menubar']"},
		"tab":             {"[role='tab']", "[role='tablist']"},
		"content_links":   {"a[href*='content']", "a[href*='module']", "a.d2l-link"},
		"module_list":     {".d2l-content-list", "[data-key='content']", ".d2l-le-content"},
		"grade_table":     {"[role='grid']", "[role='table']", "table", ".d2l-grades-grid", "[aria-label='Grades']"},
		"submit_button":   {"[role='button']", "button[type='submit']", "[data-action='submit']", "button"},
		"assignment":      {"[role='article']", "article", ".d2l-card", "[data-type='assignment']"},
		"course_nav":      {"[role='navigation']", "nav", ".d2l-nav", "[aria-label*='course']"},
		"file_upload":     {"[type='file']", "[role='button']", "[data-action='upload']"},
		"discussion_post": {"[role='article']", "article", ".d2l-discussion", "[data-type='post']"},
		"calendar_event":  {"[role='listitem']", "li", ".d2l-calendar-event", "[data-type='event']"},
		"announcement":    {"[role='article']", "article", ".d2l-announcement", "[data-type='announcement']"},
	}

	normalized := strings.ToLower(elementType)
	normalizedNoUnderscore := strings.ReplaceAll(normalized, "_", " ")

	// Exact match first to avoid shorter keys stealing via substring containment
	if selectors, ok := ariaMap[normalized]; ok {
		return selectors
	}

	// Fuzzy match: sort keys longest-first so more specific keys win
	keys := make([]string, 0, len(ariaMap))
	for k := range ariaMap {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return len(keys[i]) > len(keys[j]) })

	for _, key := range keys {
		keyNoUnderscore := strings.ReplaceAll(key, "_", " ")
		if strings.Contains(normalized, key) || strings.Contains(key, normalized) ||
			strings.Contains(normalizedNoUnderscore, keyNoUnderscore) || strings.Contains(keyNoUnderscore, normalizedNoUnderscore) {
			return ariaMap[key]
		}
	}
	return nil
}

func (sr *SelectorRepairV2) generateTextContentCandidates(elementType string) []string {
	textMap := map[string][]string{
		"login_button":    {`button:has-text("Sign In")`, `button:has-text("Log In")`, `[type="submit"]`},
		"submit":          {`button:has-text("Submit")`, `[type="submit"]`},
		"submit_button":   {`button:has-text("Submit")`, `button:has-text("Save")`, `[type="submit"]`},
		"content_links":   {`a:has-text("Content")`, `a:has-text("Module")`},
		"assignment_link": {`a:has-text("Assignment")`, `a:has-text("Submission")`},
		"quiz_link":       {`a:has-text("Quiz")`, `a:has-text("Test")`},
		"grades_link":     {`a:has-text("Grades")`, `a:has-text("Grade")`},
		"discussion_post": {`a:has-text("Discussion")`, `a:has-text("Forum")`},
		"announcement":    {`a:has-text("Announcement")`, `div:has-text("Important")`},
		"file_upload":     {`button:has-text("Upload")`, `button:has-text("Browse")`},
		"calendar_event":  {`a:has-text("Event")`, `a:has-text("Due")`},
		"course_nav":      {`a:has-text("Course")`, `nav:has-text("Modules")`},
	}

	normalized := strings.ToLower(elementType)
	for key, selectors := range textMap {
		if strings.Contains(normalized, key) || strings.Contains(key, normalized) {
			return selectors
		}
	}
	return nil
}

func (sr *SelectorRepairV2) logRepairSuggestion(elementType, broken string, candidate *RepairCandidate) {
	if sr.repairLog == nil {
		return
	}
	_ = sr.repairLog.Append(RepairSuggestion{
		ElementType: elementType,
		OldSelector: broken,
		NewSelector: candidate.Selector,
		Confidence:  candidate.Confidence,
		Method:      string(candidate.Strategy),
	})
}

func (sr *SelectorRepairV2) recordAttempt(strategy RepairStrategy) {
	if sr.metrics == nil {
		return
	}
	sr.metrics.RepairAttempts.With(prometheus.Labels{"strategy": string(strategy)}).Inc()
}

func (sr *SelectorRepairV2) recordSuccess(strategy RepairStrategy) {
	if sr.metrics == nil {
		return
	}
	sr.metrics.RepairSuccess.With(prometheus.Labels{"strategy": string(strategy)}).Inc()
}

func strategyConfidence(strategy RepairStrategy, matchCount int) float64 {
	base := map[RepairStrategy]float64{
		StrategyCSS:         0.95,
		StrategyXPath:       0.85,
		StrategyARIA:        0.80,
		StrategyTextContent: 0.70,
		StrategyVLM:         0.60,
	}
	conf := base[strategy]

	// Penalize selectors matching too many elements
	if matchCount > 10 {
		conf *= 0.8
	} else if matchCount > 5 {
		conf *= 0.9
	}
	// Reward exact-match selectors
	if matchCount == 1 {
		conf = minFloat64(conf*1.05, 1.0)
	}

	return conf
}

func defaultCSSAlternations() []CSSAlternation {
	return []CSSAlternation{
		{
			Name: "class_partial",
			Generate: func(original string) []string {
				_, classes, _ := parseCSSSelector(original)
				var out []string
				for _, cls := range classes {
					out = append(out, "."+cls)
					parts := strings.Split(cls, "-")
					if len(parts) > 1 {
						out = append(out, "[class*='"+parts[0]+"']")
						out = append(out, "[class*='"+parts[len(parts)-1]+"']")
					}
				}
				return out
			},
		},
		{
			Name: "data_attribute",
			Generate: func(original string) []string {
				_, _, attrs := parseCSSSelector(original)
				var out []string
				for _, attr := range attrs {
					if strings.HasPrefix(attr, "data-") {
						out = append(out, "["+attr+"]")
					}
				}
				return out
			},
		},
		{
			Name: "id_variation",
			Generate: func(original string) []string {
				if !strings.Contains(original, "#") {
					return nil
				}
				parts := strings.SplitN(original, "#", 2)
				if len(parts) < 2 {
					return nil
				}
				id := strings.Split(parts[1], " ")[0]
				id = strings.Split(id, ".")[0]
				return []string{
					"#" + id,
					"[id='" + id + "']",
					"[id*='" + id + "']",
				}
			},
		},
	}
}

// parseCSSSelector extracts tag, classes, and attributes from a CSS selector string.
func parseCSSSelector(sel string) (tag string, classes []string, attrs []string) {
	sel = strings.TrimSpace(sel)
	if sel == "" {
		return
	}

	// Extract tag
	for i, ch := range sel {
		if ch == '.' || ch == '#' || ch == '[' || ch == ' ' || ch == ':' {
			tag = sel[:i]
			sel = sel[i:]
			break
		}
		if i == len(sel)-1 {
			tag = sel
			return
		}
	}

	// Extract classes
	for _, part := range strings.Split(sel, ".") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		end := strings.IndexAny(part, " #[:>+~")
		if end == -1 {
			end = len(part)
		}
		cls := part[:end]
		if cls != "" && cls != tag {
			classes = append(classes, cls)
		}
	}

	// Extract attributes
	for {
		start := strings.Index(sel, "[")
		if start == -1 {
			break
		}
		end := strings.Index(sel[start:], "]")
		if end == -1 {
			break
		}
		attr := sel[start+1 : start+end]
		attr = strings.Split(attr, "=")[0]
		attr = strings.TrimSpace(attr)
		attrs = append(attrs, attr)
		sel = sel[start+end+1:]
	}

	return
}

func minFloat64(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
