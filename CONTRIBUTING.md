# Contributing to uiauto-framework

Thank you for your interest in contributing. This guide covers everything you
need to know to get a working development environment, propose changes, and
submit them upstream.

## Code of Conduct

Participation in this project is governed by the
[Contributor Covenant](CODE_OF_CONDUCT.md). By participating, you agree to
uphold its terms.

## Project scope

`uiauto-framework` is a deliberately generic library. Anything tied to a
specific website, product, or company belongs in a downstream consumer that
implements the plugin seams in [`pkg/uiauto/plugin`](pkg/uiauto/plugin).

CI enforces this with `make lint-no-target-strings`. PRs that introduce
target-specific strings (company names, product names, internal hosts) will
fail until they are removed and re-routed through the plugin layer.

## Development environment

Prerequisites:

- Go 1.24+
- Chrome or Chromium (only required for live-browser tests)
- Docker + Docker Compose v2 (only required for `make test-integration`)

```bash
git clone https://github.com/<your-fork>/uiauto-framework.git
cd uiauto-framework
make build
make test
```

`make test` runs the short suite, which skips browser- and database-backed
paths so it stays under five minutes on a stock laptop.

For the full suite (browser + Postgres + OmniParser stub), run:

```bash
make test-integration
```

This brings up the docker compose stack defined in
[`docker-compose.integration.yml`](docker-compose.integration.yml), runs every
test without `-short`, and tears the stack back down.

## Branching and commits

- Cut a feature branch from `main`: `git checkout -b feat/<short-topic>`.
- Use [Conventional Commits](https://www.conventionalcommits.org/) for commit
  messages: `feat:`, `fix:`, `refactor:`, `test:`, `docs:`, `chore:`, `ci:`.
- Keep commits focused. One logical change per commit makes review and
  bisection painless.

## Style and quality gates

Before you push:

```bash
make vet
make lint        # vet + the no-target-strings guard
make test
```

Optional but recommended:

```bash
make lint-go     # golangci-lint (pulls binary if missing)
make govulncheck # govulncheck against go.sum
make ossready    # validates that all OSS community files exist
```

CI runs the equivalent of `make lint && make test` on every PR. The full
integration suite runs nightly on `main` to keep flakes from masking
regressions.

## Tests

- New code requires tests. Aim to keep the affected package above 80 percent
  coverage; check with `make test-coverage`.
- Use the existing fakes in `pkg/uiauto/*_test.go` instead of reaching for a
  real browser when the unit under test does not need one.
- Browser-backed tests must skip cleanly under `-short` via
  `skipWithoutBrowser(t)`.

## Documentation

- Update [`docs/architecture.md`](docs/architecture.md) when you add or move
  a public component.
- Update [`docs/scenario-format.md`](docs/scenario-format.md) when scenario
  JSON gains or loses fields.
- Update [`CHANGELOG.md`](CHANGELOG.md) under the `## [Unreleased]` section.

## Pull request checklist

- [ ] Conventional Commit subject.
- [ ] `make lint && make test` is green locally.
- [ ] New or changed behaviour is covered by a test.
- [ ] Public API changes are documented in `docs/` and noted in `CHANGELOG.md`.
- [ ] No target-specific strings in `pkg/`, `cmd/`, `examples/`, or `docs/`.

## Reporting bugs

Open an issue using the [Bug report](.github/ISSUE_TEMPLATE/bug_report.yml)
template. Include the exact `ui-agent` invocation, the relevant scenario JSON,
and the smallest possible reproduction.

## Reporting security issues

Please do **not** file public issues for security vulnerabilities. Follow
[`SECURITY.md`](SECURITY.md) for the disclosure process.

## Releases

`uiauto-framework` follows [Semantic Versioning](https://semver.org/). Tagged
releases trigger
[`.github/workflows/release.yml`](.github/workflows/release.yml), which
publishes checksummed `ui-agent` binaries via `goreleaser`.
