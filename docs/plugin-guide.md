# Plugin guide

The framework's `pkg/uiauto/plugin` package defines four extension seams. Each
ships with a default no-op or built-in implementation, so you only adopt a
seam when you need it.

## ActionRegistry

Register custom action types beyond the built-ins (click, type, verify, wait,
frame, evaluate, read).

```go
import "github.com/nfsarch33/uiauto-framework/pkg/uiauto/plugin"

reg := plugin.NewActionRegistry()
_ = reg.Register("dwell", func(ctx context.Context, sel, val string) error {
    time.Sleep(2 * time.Second)
    return nil
})
```

The framework consults `ActionRegistry` only for unknown action types; built-in
types stay inside `LightExecutor`.

## ScenarioLoader

Parse scenarios from any source.

```go
loader := plugin.NewJSONScenarioLoader()
scenarios, err := loader.Load("scenarios/my-flows.json")
```

Implement the `ScenarioLoader` interface to read YAML, Cucumber `.feature`
files, Playwright `.spec.ts` test titles, or anything else.

## AuthProvider

Run target-specific authentication before the demo loop starts.

```go
type myAuth struct{ token string }

func (m *myAuth) Authenticate(ctx context.Context) error {
    return os.Setenv("APP_SESSION", m.token)
}

var p plugin.AuthProvider = &myAuth{token: os.Getenv("APP_TOKEN")}
_ = p.Authenticate(ctx)
```

The default `NoopAuthProvider` is fine for public targets like example.com.

## VisualVerifier

Swap the visual scorer.

```go
type clipVerifier struct{ /* ... */ }

func (v *clipVerifier) Verify(ctx context.Context, png []byte, expect string) (plugin.VerificationResult, error) {
    score := computeCLIPScore(png, expect)
    return plugin.VerificationResult{Score: score, Pass: score > 0.7}, nil
}
```

The default `NoopVisualVerifier` always passes; substitute a real verifier
when running the VLM tier.

## Wiring it all up

A downstream consumer typically composes a `MemberAgent` with the plugins it
cares about, then calls `agent.RunTask(ctx, scenarioID, actions)` per step.
See `cmd/ui-agent/demo.go` for a working example.
