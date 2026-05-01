# Security Policy

## Supported Versions

`uiauto-framework` is pre-1.0 software. Security fixes are applied to the
default branch until the first stable release. After `v1.0.0`, supported
release lines will be listed here.

## Reporting a Vulnerability

Please do not report security vulnerabilities through public issues.

Report privately through GitHub's private vulnerability reporting feature when
available. If that is not available, contact the project maintainer listed on
the GitHub repository profile.

Include:

- Affected version or commit SHA.
- Minimal reproduction steps.
- Whether the issue requires a browser, an LLM endpoint, or an OmniParser
  endpoint.
- Any relevant logs with secrets removed.

## Secret Handling

Never commit credentials, browser profiles, tokens, session cookies, screenshots
containing sensitive data, or `.env` files. The repository ignores local
environment files by default, but contributors are responsible for reviewing
artifacts before sharing them.

## Browser Automation Risk

This project can control a real browser through CDP. Treat scenarios as code:

- Run untrusted scenarios only in a disposable browser profile.
- Avoid connecting the agent to authenticated sessions unless you trust the
  scenario source.
- Review `evaluate` actions carefully because they execute JavaScript in the
  page context.
- Store demo screenshots outside the repository unless they are intentionally
  sanitized fixtures.

## Disclosure Expectations

We aim to acknowledge valid reports within 5 business days and provide a
remediation plan or status update within 14 business days.
