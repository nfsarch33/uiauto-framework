package main

import (
	"os"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
)

// The hosting-panel dry run: the scenario must load and every selector it
// drives must resolve against the fixture page, with the action arrays in
// the format's parallel shape. This is the CI-green leg of the
// hosting-panel ticket — the live leg drives the real signed-in panel
// through the shared CDP profile and records its evidence on the ticket,
// never in this repo.
func TestHostingPanelScenarioDryRun(t *testing.T) {
	scenarios, err := loadScenarios("../../examples/hosting-panel/scenario.json")
	if err != nil {
		t.Fatalf("load scenarios: %v", err)
	}
	if len(scenarios) != 1 {
		t.Fatalf("scenarios = %d, want 1", len(scenarios))
	}
	sc := scenarios[0]

	fixture, err := os.ReadFile("../../examples/hosting-panel/index.html")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	if err := resolveScenarioSelectors(string(fixture), sc); err != nil {
		t.Fatalf("dry run: %v", err)
	}
}

// Mutant this kills: a selector that no longer matches the fixture (the
// panel redesigns, the fixture drifts) must fail the dry run — a green
// run on a broken selector would mean the dry run checks nothing.
func TestHostingPanelScenarioDryRunCatchesSelectorDrift(t *testing.T) {
	scenarios, err := loadScenarios("../../examples/hosting-panel/scenario.json")
	if err != nil {
		t.Fatalf("load scenarios: %v", err)
	}
	fixture, err := os.ReadFile("../../examples/hosting-panel/index.html")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	broken := strings.Replace(string(fixture), `id="ssh-key-add"`, `id="ssh-key-submit"`, 1)
	if broken == string(fixture) {
		t.Fatal("mutant did not apply: the add-key id was not found to rename")
	}
	if err := resolveScenarioSelectors(broken, scenarios[0]); err == nil {
		t.Fatal("dry run passed a fixture whose add-key selector no longer resolves")
	}
}

// resolveScenarioSelectors parses the fixture and applies the scenario
// format's contract: parallel arrays of equal step count, every selector
// resolving to at least one element, typing steps landing on inputs and
// clicking steps landing on activatable elements.
func resolveScenarioSelectors(fixtureHTML string, sc NLScenario) error {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(fixtureHTML))
	if err != nil {
		return err
	}
	if len(sc.NaturalLanguage) == 0 || len(sc.SelectorsUsed) != len(sc.NaturalLanguage) {
		return errScenarioShape{reason: "natural_language and selectors_used must be parallel and non-empty"}
	}
	if len(sc.ActionTypes) > len(sc.NaturalLanguage) || len(sc.ActionValues) > len(sc.NaturalLanguage) {
		return errScenarioShape{reason: "action arrays longer than the step list"}
	}
	for i, sel := range sc.SelectorsUsed {
		kind := "click"
		if i < len(sc.ActionTypes) {
			kind = sc.ActionTypes[i]
		}
		selDoc := doc.Find(sel)
		if selDoc.Length() == 0 {
			return errScenarioShape{reason: "step " + sc.NaturalLanguage[i] + ": selector " + sel + " matches nothing in the fixture"}
		}
		switch kind {
		case "type":
			if selDoc.Is("input, textarea") == false {
				return errScenarioShape{reason: "step " + sc.NaturalLanguage[i] + ": typing targets must be inputs or textareas (" + sel + ")"}
			}
		case "click":
			if selDoc.Is("button, a, input[type=submit], input[type=button]") == false {
				return errScenarioShape{reason: "step " + sc.NaturalLanguage[i] + ": clicking targets must be buttons or links (" + sel + ")"}
			}
		case "wait":
			value := ""
			if i < len(sc.ActionValues) {
				value = sc.ActionValues[i]
			}
			if value != "" && doc.Find(value).Length() == 0 {
				return errScenarioShape{reason: "step " + sc.NaturalLanguage[i] + ": wait selector " + value + " matches nothing in the fixture"}
			}
		case "verify", "read", "frame", "evaluate":
			// existence already asserted above
		default:
			return errScenarioShape{reason: "step " + sc.NaturalLanguage[i] + ": unknown action type " + kind}
		}
	}
	return nil
}

type errScenarioShape struct {
	reason string
}

func (e errScenarioShape) Error() string { return "scenario shape: " + e.reason }
