package registry

import (
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// PagePatternRegistry maps URL route patterns to their known UI selectors.
// Thread-safe for concurrent reads and writes.
type PagePatternRegistry struct {
	mu       sync.RWMutex
	patterns map[string]*routeEntry
}

type routeEntry struct {
	regex   *regexp.Regexp
	pattern *PagePattern
}

// New creates an empty registry.
func New() *PagePatternRegistry {
	return &PagePatternRegistry{
		patterns: make(map[string]*routeEntry),
	}
}

// Register adds or updates a page pattern for the given route.
// The route can be a regex pattern (e.g., `/products/\d+`) or a literal path.
func (r *PagePatternRegistry) Register(pattern PagePattern) error {
	re, err := regexp.Compile(pattern.Route)
	if err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if existing, ok := r.patterns[pattern.Route]; ok {
		existing.pattern = &pattern
		existing.pattern.LastUpdated = time.Now()
	} else {
		pattern.LastUpdated = time.Now()
		r.patterns[pattern.Route] = &routeEntry{
			regex:   re,
			pattern: &pattern,
		}
	}
	return nil
}

// Lookup finds the best matching pattern for a given URL path.
// Returns nil if no pattern matches.
func (r *PagePatternRegistry) Lookup(urlPath string) *PatternMatch {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var matches []PatternMatch
	for route, entry := range r.patterns {
		if entry.regex.MatchString(urlPath) {
			score := matchScore(route, urlPath)
			matches = append(matches, PatternMatch{
				Pattern:    entry.pattern,
				MatchScore: score,
				Route:      route,
			})
		}
	}

	if len(matches) == 0 {
		return nil
	}

	sort.Slice(matches, func(i, j int) bool {
		return matches[i].MatchScore > matches[j].MatchScore
	})

	entry := r.patterns[matches[0].Route]
	atomic.AddInt64(&entry.pattern.HitCount, 1)

	return &matches[0]
}

// All returns a copy of all registered patterns.
func (r *PagePatternRegistry) All() []PagePattern {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]PagePattern, 0, len(r.patterns))
	for _, entry := range r.patterns {
		result = append(result, *entry.pattern)
	}
	return result
}

// Remove deletes a pattern by route key.
func (r *PagePatternRegistry) Remove(route string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, existed := r.patterns[route]
	delete(r.patterns, route)
	return existed
}

// Size returns the number of registered patterns.
func (r *PagePatternRegistry) Size() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.patterns)
}

// matchScore ranks how specific a route match is.
// Exact matches score highest; longer literal prefixes score higher.
func matchScore(route, urlPath string) float64 {
	if route == urlPath {
		return 1.0
	}
	if strings.HasPrefix(route, "^") && strings.HasSuffix(route, "$") {
		return 0.9
	}
	literalPart := regexp.MustCompile(`[^\\()\[\]*+?{}|^$.]`).FindAllString(route, -1)
	if len(literalPart) == 0 {
		return 0.1
	}
	return float64(len(strings.Join(literalPart, ""))) / float64(len(urlPath))
}
