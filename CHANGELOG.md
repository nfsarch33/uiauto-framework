# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project follows [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added

- browser-use lane: `BU_LLM_AGENT_HEADER` / `BU_LLM_AGENT_ID` send a caller-identity header with every LLM request (default `X-Helixon-Agent: browser-use`), so per-agent gateway policies see this lane as itself instead of sniffing the OpenAI client User-Agent.

### Added

- Laya decision-layer hardening: rune-safe state cap, answer schema
  validation at the client boundary (choice ∈ criteria, type match,
  populated values), `--min-confidence` recorded floor with
  `confidence_below_floor` evidence, refusal to decide on an empty page
  state, `no_error_continue` healthy-page option, `/healthz` version
  reporting, and pinned container deps (`laya==0.3.5`, `torch==2.14.0`
  CPU).
- browser-use executor lane: containerized service (CDP-attach only,
  never launches a browser), typed Go client, `ui-agent browser-use-run`
  command, and a deterministic WireMock LLM stub for CI e2e.
- Eval engineering: `pkg/eval` + `ui-agent eval` — suites, outcome
  metrics (task success, flake, latency percentiles, decision accuracy,
  Brier calibration), weighted rubrics with gates, JSON + markdown
  reports, `make eval-smoke` CI outcome gate, and `docs/eval.md`.

### Fixed

- `NewMetrics` panicked on duplicate registration (`go test -count>1`,
  in-process serve restarts); it is now idempotent per registerer.
- Circuit-breaker Prometheus collectors were only registered on the
  first caller's registry; they now register on every passed registerer.

### Added

- Extracted a generic Go UI automation framework with `ui-agent` CLI,
  CDP/chromedp browser automation, self-healing selector tiers, OmniParser
  visual grounding, and natural-language scenario execution.
- Added plugin seams for custom actions, scenario loaders, authentication, and
  visual verification.
- Added Docker Compose integration testing with headless Chrome, Postgres, and
  an OmniParser-compatible stub.
- Added public example scenarios and documentation for architecture, scenario
  format, plugin extension, and coverage reproduction.
- Added open-source community, security, CI, and release assets.

### Changed

- Raised short-mode unit coverage above the 80 percent readiness target.

### Fixed

- Fixed `pkg/domheal` circuit breaker cooldown measurement to use monotonic
  elapsed time instead of truncated Unix seconds, which let the breaker
  half-open early at wall-second boundaries and stall past its cooldown when
  NTP stepped the wall clock. Its unit test now injects a deterministic clock
  instead of sleeping, removing an intermittent full-suite failure.

### Security

- Added a forbidden target-specific string lint guard so the framework remains
  generic and downstream scenario data stays outside the public module.
