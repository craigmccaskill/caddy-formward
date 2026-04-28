# Contributing to caddy-formward

Thanks for your interest. caddy-formward is in pre-v1.0 implementation. This guide covers what you need to build, test, and contribute.

## Scope

The v1.0 specification is **locked**. The full requirements live in [`spec/`](./spec/) across three documents:

1. [`spec/01-project-brief.md`](./spec/01-project-brief.md) — problem, users, scope, threat model, risks
2. [`spec/02-prd.md`](./spec/02-prd.md) — functional and non-functional requirements, epic and story breakdown
3. [`spec/03-architecture.md`](./spec/03-architecture.md) — file layout, lifecycle, request flow, component design, ADRs

Contributions outside the locked v1.0 scope (SMTP transport, CSRF tokens, file attachments, webhook transport, SQLite, admin UI) should wait for v1.1+ planning. The canonical "out of scope" list is in [`spec/01-project-brief.md`](./spec/01-project-brief.md) §"MVP Scope > Out of scope". If you're unsure, open an issue before writing code.

The architecture doc's [Architectural decisions log](./spec/03-architecture.md#architectural-decisions-log) records five ADRs. To deviate from any of them, update the architecture doc with the new decision and rationale before changing code.

## Prerequisites

- Go 1.25+
- [`xcaddy`](https://github.com/caddyserver/xcaddy)
- A Postmark account for end-to-end testing (a sandbox token is sufficient)

## Build and test

```bash
# Standard Go workflow
go build ./...
go test ./...
go vet ./...

# Build a Caddy binary with this module
xcaddy build --with github.com/craigmccaskill/caddy-formward=.

# Verify module registration
./caddy list-modules | grep formward
```

CI runs `go test ./... && go vet ./...` on every push and pull request.

## Commit conventions

- Tag each commit with the story ID it implements, e.g. `feat: scaffold module skeleton (Story 1.1)`
- Prefixes: `feat:` for new functionality, `fix:` for bug fixes, `test:` for test-only changes, `docs:` for documentation, `chore:` for build/config/CI
- Reference the relevant FR or NFR in the commit body when it adds clarity (e.g., "Implements NFR1 — header injection prevention via structured JSON API")
- Don't squash stories into a single commit — each story should be at least one commit so the git history maps to the PRD

## Updating the spec

If implementation reveals something the spec missed, update the relevant doc in `spec/` and reference the change in the commit that exposes it. The spec is the source of truth for v1.0 work; pull requests that change behavior without a corresponding spec update will be sent back.

## Security

This module handles untrusted input from public form submissions and credentials for an outbound email API. Security-relevant changes — header construction, API key handling, rate limiting, input validation, fail-closed origin checks — require explicit test coverage per the security NFRs in [`spec/02-prd.md`](./spec/02-prd.md) (NFR1 through NFR4).

For vulnerability reporting, see `SECURITY.md` (added before v1.0.0).

## Questions

Open a GitHub issue or start a discussion. For implementation questions, [`spec/03-architecture.md`](./spec/03-architecture.md) is the source of truth; for scoping questions, [`spec/01-project-brief.md`](./spec/01-project-brief.md).
