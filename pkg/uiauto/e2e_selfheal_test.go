package uiauto

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/nfsarch33/uiauto-framework/pkg/domheal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// SiteMutation describes a simulated DOM mutation for a specific CMS site.
type SiteMutation struct {
	Site        string
	PageID      string
	PatternID   string
	OrigHTML    string
	MutatedHTML string
	Selector    string
	Desc        string
}

func d2lMutations() []SiteMutation {
	return []SiteMutation{
		{
			Site: "D2L Brightspace", PageID: "d2l_grades", PatternID: "grade_table",
			OrigHTML:    `<div id="content"><table class="d2l-table"><thead><tr><th>Assignment</th><th>Grade</th></tr></thead><tbody><tr><td>Quiz 1</td><td>85</td></tr></tbody></table></div>`,
			MutatedHTML: `<div id="content"><div class="d2l-grid" role="table"><div class="d2l-row" role="row"><span role="columnheader">Assignment</span><span role="columnheader">Grade</span></div><div class="d2l-row" role="row"><span>Quiz 1</span><span>85</span></div></div></div>`,
			Selector:    "table.d2l-table", Desc: "D2L grade table restructured from table to div grid",
		},
		{
			Site: "D2L Brightspace", PageID: "d2l_nav", PatternID: "course_nav",
			OrigHTML:    `<nav id="main-nav"><ul class="nav-list"><li><a href="/content">Content</a></li><li><a href="/grades">Grades</a></li></ul></nav>`,
			MutatedHTML: `<aside id="side-nav" role="navigation"><div class="nav-container"><a class="nav-link" href="/content">Content</a><a class="nav-link" href="/grades">Grades</a></div></aside>`,
			Selector:    "nav#main-nav ul.nav-list", Desc: "D2L navigation from nav/ul to aside/div",
		},
		{
			Site: "D2L Brightspace", PageID: "d2l_submit", PatternID: "submit_btn",
			OrigHTML:    `<form id="assignment-form"><div class="actions"><button type="submit" class="d2l-button-primary">Submit</button></div></form>`,
			MutatedHTML: `<form id="assignment-form"><div class="d2l-action-bar"><d2l-button primary type="submit">Submit Assignment</d2l-button></div></form>`,
			Selector:    "button.d2l-button-primary", Desc: "D2L submit button changed to web component",
		},
	}
}

func wordpressMutations() []SiteMutation {
	return []SiteMutation{
		{
			Site: "WordPress", PageID: "wp_menu", PatternID: "main_menu",
			OrigHTML:    `<header><nav class="main-navigation"><ul id="primary-menu"><li><a href="/">Home</a></li><li><a href="/shop">Shop</a></li></ul></nav></header>`,
			MutatedHTML: `<header><nav class="site-navigation" aria-label="Main"><div class="menu-container"><a class="menu-item" href="/">Home</a><a class="menu-item" href="/shop">Shop</a></div></nav></header>`,
			Selector:    "ul#primary-menu", Desc: "WordPress menu from ul to div links",
		},
		{
			Site: "WordPress", PageID: "wp_product", PatternID: "add_to_cart",
			OrigHTML:    `<div class="product"><button class="single_add_to_cart_button button">Add to cart</button></div>`,
			MutatedHTML: `<div class="product"><form class="cart"><button type="submit" name="add-to-cart" class="wp-block-button__link">Add to cart</button></form></div>`,
			Selector:    "button.single_add_to_cart_button", Desc: "WooCommerce add-to-cart button class change",
		},
	}
}

func universityCatalogMutations() []SiteMutation {
	return []SiteMutation{
		{
			Site: "University Catalog", PageID: "catalog_search", PatternID: "search_input",
			OrigHTML:    `<div class="search-bar"><input type="text" id="catalog-search" placeholder="Search courses..."></div>`,
			MutatedHTML: `<div class="search-container" role="search"><label for="q">Search</label><input type="search" id="q" name="q" placeholder="Find courses..."></div>`,
			Selector:    "input#catalog-search", Desc: "Catalog search input ID and type changed",
		},
		{
			Site: "University Catalog", PageID: "catalog_results", PatternID: "course_list",
			OrigHTML:    `<div id="results"><ul class="course-list"><li class="course-item"><h3>CS101</h3><p>Intro to CS</p></li></ul></div>`,
			MutatedHTML: `<div id="results"><div class="courses-grid"><article class="course-card"><h2>CS101</h2><p>Intro to CS</p></article></div></div>`,
			Selector:    "ul.course-list", Desc: "Catalog results from ul list to CSS grid cards",
		},
	}
}

func allMutations() []SiteMutation {
	var all []SiteMutation
	all = append(all, d2lMutations()...)
	all = append(all, wordpressMutations()...)
	all = append(all, universityCatalogMutations()...)
	return all
}

func TestE2ESelfHeal_FullPipeline_MultiSite(t *testing.T) {
	mutations := allMutations()
	require.GreaterOrEqual(t, len(mutations), 7)

	dir := t.TempDir()
	tracker, err := NewPatternTracker(dir+"/patterns.json", dir)
	require.NoError(t, err)
	ctx := context.Background()

	pp := NewPatternPipeline(tracker, nil)

	healed := 0
	detected := 0
	for _, m := range mutations {
		err := tracker.RegisterPattern(ctx, m.PatternID, m.Selector, m.Desc, m.OrigHTML)
		require.NoError(t, err)

		pp.CheckAndAlert(ctx, m.PageID, m.PatternID, m.OrigHTML)
		drifted, _ := pp.CheckAndAlert(ctx, m.PageID, m.PatternID, m.MutatedHTML)
		if drifted {
			detected++
		}

		origFP := domheal.ParseDOMFingerprint(m.OrigHTML)
		mutFP := domheal.ParseDOMFingerprint(m.MutatedHTML)
		sim := domheal.DOMFingerprintSimilarity(origFP, mutFP)
		sev := ClassifyDriftSeverity(sim)

		t.Logf("[%s] %s: drift=%v sim=%.3f sev=%s",
			m.Site, m.PatternID, drifted, sim, sev)

		if drifted {
			healed++
		}
	}

	assert.GreaterOrEqual(t, detected, len(mutations)-1,
		"should detect drift for nearly all mutations")
	t.Logf("Total: %d mutations, %d detected, %d processed", len(mutations), detected, healed)
}

func TestE2ESelfHeal_SeverityClassification(t *testing.T) {
	mutations := allMutations()
	for _, m := range mutations {
		origFP := domheal.ParseDOMFingerprint(m.OrigHTML)
		mutFP := domheal.ParseDOMFingerprint(m.MutatedHTML)
		sim := domheal.DOMFingerprintSimilarity(origFP, mutFP)
		sev := ClassifyDriftSeverity(sim)

		assert.NotEmpty(t, string(sev), "severity should not be empty for %s", m.PatternID)
		switch sev {
		case DriftSeverityLow, DriftSeverityMedium, DriftSeverityHigh, DriftSeverityCritical:
		default:
			t.Errorf("unexpected severity %q for %s (sim=%.3f)", sev, m.PatternID, sim)
		}
	}
}

func TestE2ESelfHeal_AlertPersistenceLifecycle(t *testing.T) {
	dir := t.TempDir()
	tracker, err := NewPatternTracker(dir+"/p.json", dir)
	require.NoError(t, err)
	ctx := context.Background()

	var alertFired bool
	pp := NewPatternPipeline(tracker, nil)
	pp.alerts = NewInMemoryDriftAlertStore(func(alert DriftAlert) {
		alertFired = true
	})
	alerts := pp.alerts

	mutation := d2lMutations()[0]
	err = tracker.RegisterPattern(ctx, mutation.PatternID, mutation.Selector, mutation.Desc, mutation.OrigHTML)
	require.NoError(t, err)

	pp.CheckAndAlert(ctx, mutation.PageID, mutation.PatternID, mutation.OrigHTML)
	drifted, _ := pp.CheckAndAlert(ctx, mutation.PageID, mutation.PatternID, mutation.MutatedHTML)
	assert.True(t, drifted)
	assert.True(t, alertFired)

	unresolved := alerts.Unresolved()
	require.GreaterOrEqual(t, len(unresolved), 1)

	alerts.Resolve(unresolved[0].ID)
	remaining := alerts.Unresolved()
	assert.Less(t, len(remaining), len(unresolved))
}

func TestE2ESelfHeal_PatternPersistence(t *testing.T) {
	dir := t.TempDir()
	patternFile := dir + "/patterns.json"

	tracker1, err := NewPatternTracker(patternFile, dir)
	require.NoError(t, err)
	ctx := context.Background()

	m := d2lMutations()[0]
	err = tracker1.RegisterPattern(ctx, m.PatternID, m.Selector, m.Desc, m.OrigHTML)
	require.NoError(t, err)

	tracker2, err := NewPatternTracker(patternFile, dir)
	require.NoError(t, err)

	p, found := tracker2.store.Get(ctx, m.PatternID)
	require.True(t, found, "pattern should persist across tracker instances")
	assert.Equal(t, m.Selector, p.Selector)
	assert.Equal(t, m.Desc, p.Description)
}

func TestE2ESelfHeal_PhaseTransitionDuringMutation(t *testing.T) {
	pt := NewPhaseTracker(2, 3, 2)
	assert.Equal(t, PhaseDiscovery, pt.CurrentPhase())

	// Smart successes move to cruise
	pt.RecordSuccess(TierSmart)
	pt.RecordSuccess(TierSmart)
	assert.Equal(t, PhaseCruise, pt.CurrentPhase())

	// Simulated mutations cause light failures -> escalation
	pt.RecordFailure(TierLight)
	pt.RecordFailure(TierLight)
	assert.Equal(t, PhaseEscalation, pt.CurrentPhase())

	// VLM resolves -> back to discovery
	pt.RecordSuccess(TierVLM)
	assert.Equal(t, PhaseDiscovery, pt.CurrentPhase())

	history := pt.History()
	assert.GreaterOrEqual(t, len(history), 3)
}

func TestE2ESelfHeal_FallbackChainWithMutations(t *testing.T) {
	mutations := d2lMutations()
	handoffs := NewInMemoryHandoffStore()
	lb := NewLatencyBudget(DefaultTierBudgets())
	fc := NewFallbackChain(
		DefaultFallbackChain(),
		func(tier ModelTier) bool { return true },
		WithFallbackBudget(lb),
		WithFallbackHandoffs(handoffs),
	)

	for _, m := range mutations {
		origFP := domheal.ParseDOMFingerprint(m.OrigHTML)
		mutFP := domheal.ParseDOMFingerprint(m.MutatedHTML)
		sim := domheal.DOMFingerprintSimilarity(origFP, mutFP)

		tier, err := fc.Execute(context.Background(), m.PatternID, func(ctx context.Context, tier ModelTier) error {
			switch tier {
			case TierLight:
				if sim < 0.5 {
					return &healError{msg: "light cannot handle major DOM change"}
				}
				return nil
			case TierSmart:
				if sim < 0.2 {
					return &healError{msg: "smart cannot handle critical change"}
				}
				return nil
			default:
				return nil
			}
		})
		require.NoError(t, err)

		t.Logf("[%s] %s: sim=%.3f -> tier=%s", m.Site, m.PatternID, sim, tier)
	}

	recent := handoffs.Recent(50)
	t.Logf("Total handoffs recorded: %d", len(recent))
}

type healError struct {
	msg string
}

func (e *healError) Error() string { return e.msg }

func TestE2ESelfHeal_MetricsAccumulation(t *testing.T) {
	mutations := allMutations()
	dir := t.TempDir()
	tracker, err := NewPatternTracker(dir+"/p.json", dir)
	require.NoError(t, err)
	ctx := context.Background()

	pp := NewPatternPipeline(tracker, nil)

	driftCount := 0
	for _, m := range mutations {
		_ = tracker.RegisterPattern(ctx, m.PatternID, m.Selector, m.Desc, m.OrigHTML)
		pp.CheckAndAlert(ctx, m.PageID, m.PatternID, m.OrigHTML)
		drifted, _ := pp.CheckAndAlert(ctx, m.PageID, m.PatternID, m.MutatedHTML)
		if drifted {
			driftCount++
		}
	}

	t.Logf("Drift count: %d / %d", driftCount, len(mutations))
	t.Logf("Unresolved alerts: %d", len(pp.alerts.Unresolved()))

	assert.Greater(t, driftCount, 0)
	assert.GreaterOrEqual(t, len(pp.alerts.Unresolved()), 1)
}

func TestE2ESelfHeal_SPA_DynamicDOM(t *testing.T) {
	origHTML := `<div id="app"><div class="spa-view" data-route="/dashboard"><h1>Dashboard</h1><div class="widgets"><div class="widget" data-id="sales">$1,234</div></div></div></div>`
	mutatedHTML := `<div id="app"><main class="view-container" data-route="/dashboard"><h1>Dashboard</h1><section class="widget-grid"><article class="widget-card" data-widget="sales"><span class="amount">$1,234</span></article></section></main></div>`

	origFP := domheal.ParseDOMFingerprint(origHTML)
	mutFP := domheal.ParseDOMFingerprint(mutatedHTML)
	sim := domheal.DOMFingerprintSimilarity(origFP, mutFP)

	assert.Less(t, sim, 0.8, "SPA DOM mutation should have noticeable structural change")

	sev := ClassifyDriftSeverity(sim)
	t.Logf("SPA mutation: sim=%.3f sev=%s", sim, sev)
	assert.NotEqual(t, DriftSeverityLow, sev, "SPA mutation should be more than low severity")
}

func TestE2ESelfHeal_DriftAlertJSON(t *testing.T) {
	alert := DriftAlert{
		ID:          1,
		PageID:      "d2l_grades",
		PatternID:   "grade_table",
		Severity:    DriftSeverityHigh,
		OldSelector: "table.d2l-table",
		NewSelector: "div.d2l-grid",
		Similarity:  0.35,
		Resolved:    false,
		CreatedAt:   time.Now(),
	}
	data, err := json.Marshal(alert)
	require.NoError(t, err)

	var decoded DriftAlert
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, alert.PageID, decoded.PageID)
	assert.Equal(t, alert.Severity, decoded.Severity)
	assert.InDelta(t, alert.Similarity, decoded.Similarity, 0.001)
}
