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
page ─▶ CDP screenshot ─▶ OmniParser V2 ─▶ []Candidate{label, bbox, conf}
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
  ordered by confidence. The candidate list is bounded (top 25) and
  rendered into the task as a compact numbered list with viewport
  coordinates — the model can quote `"the element numbered 3"` and the
  lane can also resolve a candidate to a click point later.
- **DOM fallback**: if the OmniParser service is unreachable, returns
  zero interactable elements, or every candidate is under the confidence
  threshold (default 0.35, configurable), the executor runs the plain
  task with no hints. Fallback is a first-class outcome, recorded in the
  run record (`grounding: "fallback"`), never an error.
- **Decision seam**: the typed-decision service remains where calibrated
  action selection happens (checkout-style pages); the fusion lane uses
  it unchanged. This spike does not alter decision routing.

## Failure taxonomy

| Class | Signal | Handling |
|---|---|---|
| `grounding_unavailable` | OmniParser HTTP error / timeout | DOM fallback; record |
| `grounding_empty` | 0 interactable elements | DOM fallback; record |
| `grounding_low_confidence` | best candidate < threshold | DOM fallback; record |
| `hint_ignored` | grounded run, model does not use the candidates | visible as plain-lane step counts in the eval table |
| `wrong_element` | model acts on a non-candidate | steps succeed/fail as the plain lane; taxonomy field in the report |
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

## Not in scope (agreed interface with the platform lane)

The platform browser-tool bridge (separate ticket) exposes the lane to
the platform's agents over HTTP. The fusion executor is a framework-side
executor and does not add endpoints; the interface point is the existing
`POST /run {task,url,...}` contract plus the task-text enrichment, which
needs no service change. Interface agreement is recorded as a comment on
the bridge ticket.

## Risks

- Prompt injection via page text reaching the planning model: candidates
  carry OmniParser's OCR text; the enrichment wraps it in a quoted,
  numbered block and states that page text is data, not instructions.
  (The taxonomy records when a run appears to follow page-injected
  directives — follow-up work, out of spike scope.)
- Token growth from hints: bounded candidate list keeps the enrichment
  under ~400 tokens; measured in the eval table.
