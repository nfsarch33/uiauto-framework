# Coverage and Integration Status

Snapshot of the framework's test posture after the unit-test push and the
docker-compose-driven integration validation. All numbers below are
reproducible via the make targets listed at the end.

## Headline numbers

| Scope                                | Mode                  | Coverage |
| ------------------------------------ | --------------------- | -------- |
| `uiauto-framework` weighted total    | `-short` (no Chrome)  | 80.4 %   |
| `uiauto-framework` weighted total    | docker-compose stack  | 81.4 %   |
| Downstream scenarios repo            | `-short`              | 87.4 %   |

The `-short` number is what runs in CI and on a developer laptop without
Docker. The docker-compose number is what runs locally when the integration
stack is brought up.

The downstream scenarios repo number is reported here for context only --
that repo is target-specific and lives outside the framework. The framework
itself stays generic (see `make lint-no-target-strings`).

## `uiauto-framework` per-package (`-short`)

| Package                      | Coverage |
| ---------------------------- | -------- |
| `cmd/ui-agent`               | 80.8 %   |
| `internal/doctor`            | 82.1 %   |
| `pkg/domheal`                | 93.8 %   |
| `pkg/evolver`                | 86.3 %   |
| `pkg/llm`                    | 87.9 %   |
| `pkg/uiauto`                 | 64.9 %   |
| `pkg/uiauto/accessibility`   | 94.9 %   |
| `pkg/uiauto/action`          | 100.0 %  |
| `pkg/uiauto/aiwright`        | 87.4 %   |
| `pkg/uiauto/bandits`         | 95.8 %   |
| `pkg/uiauto/budget`          | 97.5 %   |
| `pkg/uiauto/comparison`      | 96.0 %   |
| `pkg/uiauto/config`          | 88.2 %   |
| `pkg/uiauto/endurance`       | 86.9 %   |
| `pkg/uiauto/frame`           | 96.6 %   |
| `pkg/uiauto/mutation`        | 91.9 %   |
| `pkg/uiauto/omniparser`      | 90.5 %   |
| `pkg/uiauto/parallel`        | 95.9 %   |
| `pkg/uiauto/playwright`      | 100.0 %  |
| `pkg/uiauto/plugin`          | 95.2 %   |
| `pkg/uiauto/registry`        | 97.9 %   |
| `pkg/uiauto/regression`      | 94.6 %   |
| `pkg/uiauto/signal`          | 94.1 %   |
| `pkg/uiauto/store`           | 85.0 %   |
| `pkg/uiauto/visual`          | 92.6 %   |

`pkg/uiauto` sits below the others because it owns the live-browser
orchestrator types (`BrowserAgent`, `MemberAgent.RunTask`, `ModelRouter.smartPath`,
`ModelRouter.vlmPath`, `DiscoveryMode.Scan`) whose meaningful coverage requires
a real Chrome and an external LLM. Those paths are exercised by the
docker-compose run, which lifts the package-level number further and
contributes the +1.0 % delta on the weighted total.

## Downstream scenarios repo per-package (`-short`)

Reported for context. None of these packages live in the framework; they
are target-specific consumers of the public framework APIs.

| Package                              | Coverage |
| ------------------------------------ | -------- |
| `auth`                               | 84.2 %   |
| `call-harness`                       | 84.9 %   |
| `scripts/sync-nl-from-playwright`    | 87.5 %   |
| `scripts/sync-nl-from-app-frame`     | 89.2 %   |
| `scripts/sync-nl-from-browser-tests` | 88.9 %   |

`scenarios/` in that repo is data-only (JSON + a thin loader) so it has no
executable statements; the harness validates the JSON by loading it through
the framework's `ScenarioLoader` plugin seam.

## Integration test stack

`docker-compose.integration.yml` brings up three services on isolated
loopback ports so it can co-exist with a developer's debug Chrome on 9222:

| Service           | Image                                 | Host port | Purpose                                     |
| ----------------- | ------------------------------------- | --------- | ------------------------------------------- |
| `chrome`          | `chromedp/headless-shell:stable`      | 9333      | CDP target for `BrowserAgent` integration   |
| `postgres`        | `postgres:16-alpine`                  | 5440      | `PostgresPatternStore` integration suite    |
| `omniparser-stub` | `wiremock/wiremock:3.9.1`             | 7861      | OmniParser HTTP API responses               |

The wiremock fixtures under `test/fixtures/omniparser-stub/mappings/`
mirror OmniParser V2's `/probe/`, `/parse`, and `/parse-ocr` shapes, so the
GPU-bound vision service is only required when running the full visual
verification suite against a real OmniParser instance.

## How to reproduce

```bash
# Unit + plugin tests, no external services
make test
make test-coverage

# Spin the integration stack up, run the full suite, tear it down
make test-integration

# Bring the stack up only (useful when iterating on a single test)
make test-integration-up
make test-integration-down
```

The `lint-no-target-strings` Makefile guard runs as part of `make lint` and
fails the build if the framework picks up any company-specific identifiers --
that material belongs in the downstream scenarios repo.
