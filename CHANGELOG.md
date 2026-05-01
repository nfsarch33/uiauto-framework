# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project follows [Semantic Versioning](https://semver.org/).

## [Unreleased]

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

### Security

- Added a forbidden target-specific string lint guard so the framework remains
  generic and downstream scenario data stays outside the public module.
