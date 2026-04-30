# uiauto — Self-Healing UI Automation Framework

Production-grade, self-adjusting UI automation for Go. Detects UI drift, heals broken selectors via multi-tier AI models, and learns patterns over time.

## Architecture

```
                          ┌──────────────────┐
                          │   MemberAgent    │  Orchestrator
                          │ (per-target CBs) │
                          └───────┬──────────┘
                    ┌─────────────┼─────────────┐
                    ▼             ▼              ▼
           ┌──────────────┐ ┌──────────┐ ┌─────────────┐
           │ ModelRouter   │ │ SelfHealer│ │PatternTracker│
           │ (tier promo)  │ │ (CB+VLM) │ │(confidence)  │
           └──────┬───────┘ └────┬─────┘ └──────┬──────┘
                  │              │               │
          ┌───────┴──┐    ┌─────┴─────┐    ┌────┴────┐
          ▼          ▼    ▼           ▼    ▼         ▼
    ┌──────────┐ ┌───────────┐ ┌─────────┐ ┌──────────┐
    │LightExec │ │SmartDisc  │ │VLMBridge│ │JSONStore │
    │(fast ops)│ │(LLM find) │ │(vision) │ │(persist) │
    └────┬─────┘ └───────────┘ └─────────┘ └──────────┘
         │
    ┌────┴────┐
    │ Browser │  ← Abstract interface (chromedp, Playwright, mock)
    └─────────┘
```

## Key Components

| Component | File | Purpose |
|---|---|---|
| `Browser` | `browser_iface.go` | Abstract browser interface; swap chromedp/Playwright/mock |
| `BrowserAgent` | `browser.go` | chromedp implementation (Navigate, Click, Type, CaptureDOM) |
| `BrowserPool` | `browser_iface.go` | Bounded pool with acquire/release, session reuse |
| `MemberAgent` | `member_agent.go` | Top-level orchestrator with per-target circuit breakers |
| `ModelRouter` | `model_router.go` | Tier promotion/demotion (Light → Smart → VLM) |
| `LightExecutor` | `light_executor.go` | Fast pattern-based action execution |
| `SmartDiscoverer` | `smart_discoverer.go` | Multi-model LLM element discovery |
| `VLMBridge` | `vlm_bridge.go` | Vision-Language Model verification |
| `SelfHealer` | `self_healer.go` | Multi-tier healing with circuit breakers |
| `PatternTracker` | `pattern_tracker.go` | Learn/remember/adapt UI patterns with confidence |
| `PageWaiter` | `page_waiter.go` | Network idle + DOM stability waits |
| `CircuitBreaker` | `circuit_breaker.go` | Generic circuit breaker (Closed/Open/HalfOpen) |
| `SelfEvaluator` | `self_evaluator.go` | Effectiveness scoring and feedback |
| `LearningLoop` | `learning_loop.go` | Self-improvement cycle: evaluate → mine signals → evolve → feedback |
| `PatternExport` | `learning_loop.go` | Fleet-wide pattern sharing (export/import with confidence merge) |
| `KPIFramework` | `learning_loop.go` | KPI tracking with target/alert thresholds and on-target checks |
| `MetricsCollector` | `metrics_collector.go` | Prometheus metrics export |
| `SOMClient` | `aiwright/client.go` | ai-wright SOM annotation service client |
| `SOMBridge` | `aiwright/bridge.go` | Screenshot → SOM → CSS selector mapping |
| `PixelDiffer` | `visual/pixel.go` | Pixel-level image comparison |
| `SemanticDiffer` | `visual/semantic.go` | VLM-based semantic image comparison |
| `CompositeVerifier` | `visual/scorer.go` | Weighted pixel + semantic visual regression |
| `OmniParserClient` | `omniparser/client.go` | OmniParser V2 UI element parsing |
| `ActionSequence` | `action/sequence.go` | Multi-step workflow with rollback |
| `FrameTree` | `frame/context.go` | iframe hierarchy management |
| `FrameHealer` | `frame/healer.go` | Cross-frame self-healing |
| `JSONFileStore` | `store/sqlite.go` | JSON-file pattern persistence |
| `FallbackStore` | `store/fallback.go` | Circuit-breaker resilient store |
| `SignalEmitter` | `signal/emitter.go` | Debounced operational signal dispatch |
| `Pool` | `parallel/pool.go` | Bounded goroutine pool with context isolation |
| `RegressionSuite` | `regression/suite.go` | Multi-site concurrent regression testing |
| `BudgetRouter` | `budget/router.go` | Cost-aware model tier selection |
| `EnduranceHarness` | `endurance/harness.go` | Continuous mutation+heal cycle runner |
| `Benchmark` | `endurance/benchmark.go` | P50/P95/P99 latency profiling |

## V2 Features

- **Confidence-weighted pattern matching**: `FindBestMatch` blends structural similarity (70%) with stored confidence (30%) and uses an adaptive threshold
- **Per-target circuit breakers**: `MemberAgent` tracks failure rates per target element; open circuits cause graceful skip instead of task failure
- **Healer circuit breakers**: `SelfHealer` protects Smart LLM and VLM tiers independently, preventing expensive calls when backends are unhealthy
- **Degraded mode**: Agent continues operating with reduced capability when circuits open, logged via `IsDegraded()` and metrics
- **Browser abstraction**: `Browser` interface decouples all components from chromedp; `BrowserPool` manages session reuse and concurrency
- **Self-improving learning loop**: `LearningLoop` connects SelfEvaluator → SignalMiner → EvolutionEngine → PatternTracker for closed-loop improvement
- **Fleet pattern sharing**: `PatternExport`/`ImportPatterns` serializes learned patterns with confidence-based merge for multi-agent coordination
- **KPI-driven operation**: `KPIFramework` tracks 6 agent KPIs (action success, cache hit, heal success, cost, overall score, heal frequency) with alerts

## Usage

### Basic (chromedp)

```go
agent, err := NewMemberAgent(MemberAgentConfig{
    Headless:    true,
    PatternFile: "patterns.json",
    LLMProvider: myLLM,
    Logger:      zap.NewExample(),
})
defer agent.Close()

ctx := context.Background()
agent.Navigate("https://example.com/login")
agent.RegisterPattern(ctx, "submit", "#submit-btn", "submit button")

result := agent.RunTask(ctx, "login", []Action{
    {TargetID: "submit", Type: "click"},
})
// result.Status == TaskCompleted (or TaskFailed with HealResults)
```

### With Browser Pool

```go
pool := NewBrowserPool(BrowserPoolConfig{
    MaxSize: 4,
    Factory: ChromeDPFactory(true),
})
defer pool.CloseAll()

browser, _ := pool.Acquire(ctx)
defer pool.Release(browser)
browser.Navigate("https://example.com")
html, _ := browser.CaptureDOM()
```

### Observability

```go
metrics := agent.Metrics()
// metrics.Degraded       — true if any target circuit is open
// metrics.TargetCBs      — per-target circuit breaker stats
// metrics.SmartCB        — Smart LLM circuit breaker stats
// metrics.VLMCB          — VLM circuit breaker stats
// metrics.Executor       — action counts, cache hit/miss
// metrics.Router         — tier usage, convergence
// metrics.Healer         — heal attempts, successes, failures
```

## Testing

```bash
# Unit tests (no browser needed)
go test -short ./internal/uiauto/

# Integration tests (requires Chrome)
UIAUTO_INTEGRATION=1 go test -v ./internal/uiauto/

# Fuzz tests
go test -fuzz=FuzzPatternFingerprint -fuzztime=30s ./internal/uiauto/
```

## Self-Healing Flow

1. **Execute action** via `LightExecutor` using cached selector
2. **Selector fails** → `SelfHealer.Heal()` triggered
3. **L1: Structural match** — `domheal` compares DOM fingerprints
4. **L2: Light LLM retry** — re-parse DOM with lightweight model
5. **L3: Smart LLM discovery** — full LLM analysis (circuit-breaker protected)
6. **L4: VLM verification** — screenshot + vision model (circuit-breaker protected)
7. **Pattern updated** → confidence boosted on success, decayed on failure
8. **Circuit breaker** trips if tier fails repeatedly → tier skipped until cooldown
