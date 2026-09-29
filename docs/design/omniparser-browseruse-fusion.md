# OmniParser + browser-use fusion lane — design note

Status: spike (prototype behind a flag). Operator goal: make the framework
competitive on web automation — OmniParser perceives, browser-use acts,
the planning model reasons through the router, and the typed-decision
service decides where it is calibrated.

## Problem

The plain browser-use lane navigates by DOM-derived page state alone.
On pages where the DOM is misleading (canvas-heavy UIs, obfuscated class
names, overlays) the model receives no grounded notion of *what is on the
screen and where*. OmniParser V2 already returns exactly that: a set of
interactable elements with bounding boxes, text and a confidence score —
but nothing consumes it as an action candidate set.

## Design

```
page ─▶ CDP screenshot (ctx-aware) ─▶ OmniParser V2 ─▶ []Candidate{label, bbox, conf}
                                                  │
                            conf ≥ threshold ──────┴──── conf < threshold
                                  │                           │
             grounded candidate set injected        DOM fallback: plain
             into the browser-use task (the lane      browser-use task,
             still plans and acts; the hints cut       unchanged
             the search space and the step count)
                                  │                           │
                                  └──────── both ─────────────┘
                                              │
                                    browser-use acts (CDP attach,
                                    never launches a browser)
```

- **Candidate set**: OmniParser's `elements[]` filtered to
  `interactable`, mapped to `{id, type, text, bbox, confidence}`,
  ordered by confidence, by `Executor.Ground(ctx, screen)
  ([]Candidate, Outcome, error)`. The candidate list is bounded (top 25)
  and rendered into the task as a compact numbered list with viewport
  coordinates — the model can quote `"the element numbered 3"` and the
  lane can also resolve a candidate to a click point later.
- **DOM fallback**: if the OmniParser service is unreachable, returns
  zero interactable elements, or every candidate is under the confidence
  threshold (default 0.35, configurable), the executor runs the plain
  task with no hints. Fallback is a first-class outcome, recorded in the
  run record as `grounding_reason: <Outcome>` (the constants in the
  taxonomy below), never an error.
- **Decision seam**: the typed-decision service remains where calibrated
  action selection happens (checkout-style pages); the fusion lane uses
  it unchanged. This spike does not alter decision routing.

## Outcome taxonomy

The executor's Outcome constants, exactly as the code defines them (each
is a `grounding_reason` value):

| Outcome | Signal | Handling |
|---|---|---|
| `grounded` | candidates cleared the threshold | task enriched with the hint block; `enrichment_bytes` records the rendered length |
| `capture_failed` | screenshot capture error / ctx deadline | DOM fallback; grounding_error recorded |
| `grounding_unavailable` | OmniParser HTTP error / timeout | DOM fallback; grounding_error recorded |
| `grounding_empty` | 0 interactable elements | DOM fallback; record |
| `grounding_low_confidence` | best candidate < threshold | DOM fallback; record |

## Observational classes (NOT executor outcomes — never appear as
`grounding_reason`; the eval table shows them as run shapes)

| Class | Signal | Where it shows |
|---|---|---|
| `hint_ignored` | grounded run, model does not use the candidates | visible as plain-lane step counts in the eval table |
| `wrong_element` | model acts on a non-candidate | steps succeed/fail as the plain lane (not emitted as a report field in this spike) |
| `lane_failure` | browser-use run errors | existing lane error contract (`ok=false` + errors) |

## Prototype scope (this spike)

- `pkg/uiauto/fusion`: `Ground(screen) ([]Candidate, outcome)`,
  `Enrich(task, candidates) task`, `Executor` implementing the eval
  harness's executor interface — same `RunRecord` shape as the plain
  browser-use executor, plus `grounding` in the evidence map.
- Flag: the executor registers under `"browser-use-fusion"` in
  `ui-agent eval`; nothing changes for existing suites until a scenario
  names it.
- Integration test in the #39 shape: WireMock OmniParser stub + the
  navigate-then-done LLM scenario (A → B, reset), asserting both pages
  in the distinct URL history — the grounded hint must not break the
  deterministic loop.
- Eval: healthy-page set, fusion vs plain, table in `docs/eval.md`.

## Not in scope

Platform-side bridges and endpoints are out of scope for this repo: the
fusion executor is a framework-side executor, adds no endpoints, and only
enriches the task text of the existing run contract.

## Risks

- Prompt injection via page text reaching the planning model: candidates
  carry OmniParser's OCR text; the enrichment wraps it in a quoted,
  numbered block and states that page text is data, not instructions.
  (The taxonomy records when a run appears to follow page-injected
  directives — follow-up work, out of spike scope.)
- Prompt growth from hints: the bounded candidate list keeps the
  enrichment small, and the run record carries the MEASURED size in
  evidence (enrichment_bytes). Token counts are NOT yet measured —
  measure in a live-model run before quoting any number.
