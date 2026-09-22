# Eval engineering: metrics and rubrics

The eval harness (`pkg/eval`, `ui-agent eval`) measures **outcomes**, not
coverage: it runs a suite of scenarios through the execution lanes,
aggregates fixed outcome metrics, scores them against a weighted rubric,
and emits a JSON + markdown report. In CI every service is stubbed, so a
red eval is a framework bug, never model noise. Live runs point the same
suites at the real services and feed the laya probation ledger.

```
suite YAML ─▶ Runner ─▶ RunRecord[] ─▶ Aggregate ─▶ SuiteMetrics ─▶ Rubric.Score ─▶ report.json / report.md
                │                        (accuracy, Brier, rates, percentiles)        (PASS/FAIL gates)
             executors: browser-use (LLM executor lane), laya-decide (typed decisions)
```

## Suites

`eval/suites/*.yaml`; run with:

```sh
ui-agent eval --suite eval/suites/deterministic.yaml \
  --rubric eval/rubrics/default.yaml \
  --browser-use http://127.0.0.1:8091 \
  --laya http://127.0.0.1:8092 \
  --chrome-debug http://127.0.0.1:9333
```

- **`deterministic.yaml`** (CI): fixture pages, WireMock LLM stub
  (browser-use finishes on the first step) and WireMock laya stub (fixed
  decisions). `make eval-smoke` brings the whole stack up and requires a
  PASS verdict — it is the CI outcome gate.
- **Live runs**: same schema, URLs pointing at real services
  (`make laya-image`, `make browser-use-image`, a real LLM gateway via
  `BU_LLM_*`). Golden answers then measure the model, not the harness.
  `--repeat N` measures flake on live stacks.

Scenario fields per executor: `browser-use` takes `task`, `url`,
`max_steps`, `final_contains` (outcome check on the final result);
`laya-decide` takes `url`, `question_set`, `golden` (expected `label`
for choice questions, expected `true` for noul).

## Metrics

| Snapshot key | Definition | Direction |
|---|---|---|
| `task_success_rate` | successful runs / runs (lane ok **and** outcome check) | higher |
| `strict_scenario_rate` | scenarios with ALL repeats ok / scenarios | higher |
| `flake_rate` | repeated scenarios with mixed outcomes / repeated scenarios | zero |
| `mean_steps` | mean agent steps per run | context |
| `mean_duration_s`, `p50_duration_s`, `p95_duration_s` | run latency, nearest-rank percentiles | context |
| `decision_accuracy` | golden decisions answered correctly / golden decisions | higher |
| `brier_score` | mean Brier over golden decisions (below) | lower |
| `low_confidence_rate` | decisions with confidence < 0.5 / decisions | context |
| `<executor>.success_rate`, `.mean_steps`, `.mean_duration_s` | per-lane slice | — |

### Brier score (calibration)

For a choice decision with the full distribution it is the **multiclass
Brier**: `Σ_c (p(c) − 1[c == golden])²`. For a noul decision it is
`(p − y)²` with the golden boolean. Chance level for a binary question is
0.25; a perfectly calibrated confident decision scores 0. This is the
metric behind the probation trust threshold (ADR-0105 §9 frames it as
calibration error ≤ 0.05 over four consecutive weekly ledgers) — the
`decision-calibration` rubric gate enforces the same number at suite
granularity.

### Confidence floor

Decisions carry the engine's own `confidence`. During probation the floor
(0.5) is **recorded** (`low_confidence_rate`), not enforced: the ledger
must be able to separate wrong-and-confident from unsure before any
verdict can be graded.

## Rubrics

`eval/rubrics/*.yaml`; each criterion names a metric snapshot key, an
operator (`>=` / `<=`), a threshold, and a weight. `required: true`
makes it a **gate** — the overall verdict is PASS only when every gate
passes, and `ui-agent eval` then exits non-zero (the CI gate). The score
is `Σ weight(passed) / Σ weight`, reported alongside the verdict.

```yaml
name: default
criteria:
  - {id: task-success, metric: task_success_rate, operator: ">=", threshold: 0.99, weight: 4, required: true}
  - {id: decision-calibration, metric: brier_score, operator: "<=", threshold: 0.05, weight: 2, required: true}
```

A criterion naming a metric the harness did not produce **fails** — a
rubric referencing a missing metric must never pass silently.

## Reports

Each run writes `eval-out/<timestamp>/report.json` (full records: the
machine input for ledgers and trend tooling) and `report.md` (verdict,
metrics table, per-run table for reviewers). Failed runs keep their
records — a failing scenario is data, not a crash.

## Adding scenarios and executors

- New scenario: append to a suite YAML; pick an executor, add golden
  answers where the outcome is gradeable.
- New lane: implement `eval.Executor` (`Name()`, `Run(ctx, scenario,
  attempt) RunRecord`), register it in `cmd/ui-agent/eval.go`, and add
  its metrics via the existing aggregation (executor name automatically
  scopes per-lane metrics).
