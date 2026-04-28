# CLAUDE.md — caddy-formward

This file is auto-loaded by Claude Code at session start. It captures the durable project context, current status, and development guardrails for this repository.

## Project context

caddy-formward is a Caddy v2 HTTP handler module that receives form submissions and delivers them via pluggable transports. v1.0 ships with a single Postmark HTTP API transport, designed for hosts where outbound SMTP is blocked (DigitalOcean, AWS, etc.).

The full project history, including why it was renamed from "caddy-mailout" and why v1.0 is Postmark-only, is in [`spec/01-project-brief.md`](./spec/01-project-brief.md) §"Status log" and §"Problem Statement". Don't re-derive — read the spec.

## Status (as of 2026-04-27)

**Phase:** pre-v1.0 implementation. Spec is locked, code has not started.

**Repo state:** Only `LICENSE`, `README.md`, `CLAUDE.md`, and `spec/` (locked specification). No Go code yet.

**Next task:** Epic 1, Story 1.1 from [`spec/02-prd.md`](./spec/02-prd.md).

## Read the spec before touching code

The v1.0 specification is locked across three documents in [`spec/`](./spec/):

1. **[`spec/01-project-brief.md`](./spec/01-project-brief.md)** — problem, users, scope, success metrics, threat model, risks, constraints
2. **[`spec/02-prd.md`](./spec/02-prd.md)** — 22 functional requirements, 16 non-functional requirements, 7-epic breakdown with 22 stories and acceptance criteria
3. **[`spec/03-architecture.md`](./spec/03-architecture.md)** — file layout, Caddy lifecycle, request flow, component design, threat-to-defense mapping, dependencies, ADRs

Read all three before writing the first line of code. The PRD has the canonical FR/NFR list with "must"-level requirements; the architecture doc has the implementation blueprint.

## Current story: Story 1.1

> Initialize Go module at `github.com/craigmccaskill/caddy-formward`. Create `go.mod` with Go 1.23 and Caddy 2.9 dependencies. Implement the minimal `caddy.Module` interface (`CaddyModule()` returning `caddy.ModuleInfo` with ID `http.handlers.formward`).

**Acceptance:**
- `go build ./...` succeeds
- `xcaddy build --with github.com/craigmccaskill/caddy-formward=.` produces a binary
- That binary, run with `./caddy list-modules`, includes `http.handlers.formward`

**Files this story creates:** `go.mod`, `formward.go` (minimal stub — full ServeHTTP comes in Story 1.2), and possibly `.gitignore` for `*.test`, the built `caddy` binary, and `coverage.out`.

After Story 1.1 ships, update this CLAUDE.md to point to Story 1.2 (Caddyfile unmarshaler + minimal handler).

## Hard guardrails

These derive from the locked spec. Do not violate without an explicit conversation that updates the spec first.

1. **Scope is v1.0 only.** Do not implement SMTP transport, CSRF tokens, file attachments, webhook transport, SQLite storage, admin UI, or any feature listed in the brief's §"Out of scope". Even if implementation goes faster than the 15-hour budget, additional time goes to v1.0 polish (better errors, more validator coverage), never to v1.1+ features.

2. **Budget tripwires.**
   - 15-hour total implementation budget for v1.0
   - 90-day calendar tripwire from project start (2026-04-27) to v1.0.0 tag
   - If 15h hits with no end in sight: cut polish features, keep core. The transport is non-cuttable — it's the whole product.

3. **Header injection prevention is mandatory (NFR1, NFR2).** Submitter-controlled fields **must never** be interpolated as raw strings into email headers. The Postmark transport must use Postmark's structured JSON API fields. The test suite must include explicit injection-payload coverage (CRLF in name/email/subject, `\r\nBcc:`, header smuggling). This is non-negotiable — see Risk R4.

4. **API keys must never be logged (NFR3).** Set them as HTTP headers during request construction, never log them in error or debug output. Tests must verify by triggering transport failures and asserting the key string does not appear in captured log output.

5. **Origin/Referer fail-closed (FR4, NFR4).** When `allowed_origins` is configured and both `Origin` and `Referer` headers are missing, reject the request. When `allowed_origins` is empty (explicitly `[]`, not unset), the parser must reject the configuration — no fail-open default for an empty allowlist.

6. **Every FR/NFR traces back to the brief.** If you find yourself writing something not traceable to a spec requirement, stop and check the spec rather than improvising.

7. **Follow the architecture doc's file layout exactly.** Flat package, file names match [`spec/03-architecture.md`](./spec/03-architecture.md) §"File layout". No sub-packages in v1.0.

## Build and test commands

Once Go is installed and `xcaddy` is on PATH:

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

CI will run `go test ./... && go vet ./...` on push and PR (Story 7.3).

## File layout (target)

Per [`spec/03-architecture.md`](./spec/03-architecture.md) §"File layout":

```
caddy-formward/
├── formward.go            # Module registration, ServeHTTP, lifecycle hooks
├── caddyfile.go           # Caddyfile unmarshaler
├── config.go              # Module struct, JSON tags, Provision, Validate
├── transport.go           # Transport interface, Message struct, error types
├── transport_postmark.go  # Postmark HTTP API implementation
├── spam.go                # Honeypot, Origin/Referer, max-body-size checks
├── ratelimit.go           # Token bucket rate limiter, IP extraction, LRU
├── validate.go            # Required-fields and email-format validation
├── template.go            # Subject/body rendering, custom-fields passthrough
├── response.go            # JSON response builder, content negotiation
├── logging.go             # Structured logging helpers, submission ID
├── *_test.go              # Co-located unit and integration tests
├── go.mod, go.sum
├── Dockerfile             # xcaddy build + slim runtime image
├── LICENSE                # Apache-2.0
├── README.md
├── CLAUDE.md              # this file
├── spec/                  # locked v1.0 specification
└── .github/workflows/ci.yml
```

## Commit conventions

- Tag each commit with the story ID it implements: `feat: scaffold module skeleton (Story 1.1)`
- Prefix conventions: `feat:` for new functionality, `fix:` for bug fixes, `test:` for test-only commits, `docs:` for documentation changes, `chore:` for build/config/CI
- Reference the relevant FR or NFR in the commit body when it adds clarity (e.g., "Implements NFR1 — header injection prevention via structured JSON API")
- Don't squash stories into a single commit — each story should be at least one commit so the git history maps to the PRD

## Spec sync note

The canonical specification lives in this repository at `spec/`. The author also maintains an Obsidian vault with the same documents plus the broader project dashboard, roadmap notes, and cross-project links. If implementation reveals something the spec missed:

1. Update the spec in this repo
2. Mirror the change to the Obsidian vault (the author's editing surface)
3. Reference the spec change in the commit that exposes it

The repo's `spec/` is the source of truth for code-side work. The Obsidian vault is for ongoing project planning beyond v1.0.

## Architectural decisions worth knowing

Five ADRs are recorded in [`spec/03-architecture.md`](./spec/03-architecture.md) §"Architectural decisions log". The most likely to come up during implementation:

- **ADR-1:** No third-party Postmark SDK. ~80 lines of bespoke HTTP client.
- **ADR-3:** Hand-rolled token-bucket rate limiter, not `golang.org/x/time/rate`. We need LRU eviction at 10K IPs (NFR6) which `x/time/rate` doesn't provide.
- **ADR-4:** `*bool` (pointer) for `LogFailedSubmissions` to distinguish "operator omitted" from "operator explicitly set false."
- **ADR-5:** Synchronous send with one in-request retry. v2 brings async + SQLite together.

If you find yourself wanting to deviate from any ADR, update the architecture doc with the new decision and rationale before changing code.

## When in doubt

1. Re-read the relevant spec section
2. If the spec is silent or ambiguous, the architecture doc's open questions section ([`spec/03-architecture.md`](./spec/03-architecture.md) §"Open architectural questions") may already have a recommended answer
3. If neither helps, ask the author before improvising

The cost of asking is small. The cost of building the wrong thing is the entire 15-hour budget.
