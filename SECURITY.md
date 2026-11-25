# Security policy

## Scope

This repository is a test harness. It runs disposable containers locally or
in CI and publishes the headers observed on the wire between them. It has no
production deployment surface, stores no user data, and needs no credentials
except an optional New Relic license key used solely so New Relic agents will
emit propagation headers (their agents refuse to propagate until they have
completed a backend handshake).

## Reporting a vulnerability

If you find a security issue in the harness itself (for example, a way for a
pull request from a fork to exfiltrate the `NEW_RELIC_LICENSE_KEY` secret, or a
supply-chain problem in a pinned dependency), please email
**security@signadot.com**, or use GitHub's private reporting form,
[Report a vulnerability](https://github.com/signadot/context-propagation-matrix/security/advisories/new)
under this repository's **Security** tab, rather than opening a public issue or
pull request. We aim to acknowledge reports within three working days.

Vulnerabilities in the *instrumentation libraries under test* (OpenTelemetry,
Datadog, Elastic APM, New Relic agents) should be reported to those projects
directly; this harness only observes their behaviour.

## Secrets handling

- `NEW_RELIC_LICENSE_KEY` is read from a gitignored `.env` locally and from a
  repository secret in CI. It is never written to results files; results only
  record the *names* of configuration variables and non-secret values.
- CI workflows that need the secret run on the default branch, on
  `workflow_dispatch`, and on pull requests opened from a branch of this
  repository (which takes write access). Pull requests from forks never
  receive it: their New Relic variants are skipped.
- If you believe a key has been committed, rotate it in the New Relic UI
  first, then open an issue so we can purge history.
