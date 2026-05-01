# Observability

`ui-agent demo` writes one run directory per scenario under `--output-dir`
(default: `~/uiauto/tests`).

Each run contains:

- `screenshots/step-XX-raw.png` -- the raw browser screenshot captured before a
  step runs.
- `screenshots/step-XX-annotated.png` -- OmniParser or local annotation output.
- `results/demo-results.json` -- per-step execution results.
- `results/demo-metrics.json` -- aggregate latency, tier, and heal-path summary.
- `results/evolution-trace.json` -- compact entries for downstream learning
  loops.

## Per-step telemetry

Important fields in `demo-results.json`:

| Field | Meaning |
|---|---|
| `page_url` | Browser URL at the time the step started. |
| `raw_screenshot_path` | Path to the raw screenshot. |
| `screenshot_url` | Path to the annotated screenshot artifact. |
| `elements_detected` | Number of visual elements returned by the parser. |
| `ocr_text_count` | Text-bearing OCR elements returned by fallback mode. |
| `omni_mode` | `visual` for primary parsing or `ocr` for OCR fallback. |
| `fallback_reason` | Why the visual path fell back or remained empty. |
| `tier` | Execution tier serving the step. |
| `visual_score` | Reserved for pluggable VLM judge scores. |

## Blank-page guard

Runs fail fast when the active browser tab is blank (`about:blank` or the
browser new-tab page). If a scenario provides `--url`, the agent navigates
first, then verifies that the active tab is a real target page before capturing
screenshots.

## Parser fallback

Web application screenshots can produce zero icon detections when the parser is
tuned for icon-heavy surfaces. The client therefore tries `/parse-ocr` when
`/parse` returns no elements. The result records `omni_mode=ocr` and
`fallback_reason=visual_zero_elements` so downstream reports can distinguish a
healthy OCR fallback from a broken visual pipeline.
