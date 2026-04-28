---
title: "Project Brief: caddy-formward v1"
status: locked
created: 2026-04-27
synced_from_obsidian: 2026-04-27
---

# Project Brief: caddy-formward v1

This document captures the locked decisions for caddy-formward v1.0. It is the upstream artifact for [the PRD](./02-prd.md) and [the architecture document](./03-architecture.md). Changes here require revisiting both downstream documents.

The project's Obsidian dashboard retains the broader project roadmap and v2/v3 scope context (not included in this repository). This brief is scoped strictly to v1.

## Executive Summary

caddy-formward is a Caddy v2 HTTP handler module that receives form submissions and delivers them via pluggable transports. v1.0 ships with a single Postmark HTTP API transport. The module is designed for self-hosted operators on cloud platforms where outbound SMTP is blocked or unavailable — a common case on DigitalOcean, AWS, and similar hosts that block port 587 by default.

It is the modern, API-first successor to [SchumacherFM/mailout](https://github.com/SchumacherFM/mailout), the Caddy v1 plugin that filled this niche but was never ported to v2.

## Problem Statement

### The gap

The Caddy v1 mailout plugin let operators add a contact form to a static or CMS-backed site without involving a third-party SaaS. It was never ported to Caddy v2, and the surviving alternatives are:

- **Dead:** Formgate (last release 2019)
- **SMTP-only:** Mailform — fails on hosts that block port 587
- **SaaS:** Formspree, Getform, Netlify Forms — recurring costs, third-party data handling, vendor lock-in

There is no actively maintained, self-hosted, Caddy-native form handler in 2026.

### The deployment reality

DigitalOcean, AWS Lightsail, and many other cloud hosts block outbound port 587 on new accounts as an anti-spam measure. The block is not negotiable: a 2026-04 support request to DigitalOcean confirmed they will not unblock the port range and explicitly recommended using an HTTP API service like Postmark.

This means a SMTP-first form handler is non-functional on the most common hosting platforms in this audience's stack. Any v1 that doesn't ship an HTTP API transport ships dead on arrival for a meaningful share of its addressable users.

### The personal use case

The author's blog (craigmccaskill.com) currently uses Formspree for its contact form. Formspree is a paid SaaS for any volume above its free tier, and the author already pays Postmark for the blog's transactional email. Replacing Formspree with caddy-formward eliminates one SaaS bill and removes a third-party data-handling dependency.

## Proposed Solution

A Caddy v2 module (`http.handlers.formward`) configured entirely via Caddyfile, exposing a pluggable transport interface implemented in v1.0 by Postmark's HTTP API. Form submissions flow through validation, spam protection, rate limiting, and templating before being handed to the transport.

The architecture is deliberately pluggable: the transport interface in v1.0 has one implementation, but is designed to accept SMTP, Resend, SendGrid, AWS SES, and webhook transports in subsequent releases without breaking changes to the Caddyfile syntax.

## Target Users

### Primary (v1.0)

**Indie developers running a personal site or blog on cloud infrastructure.** They run Caddy because it's simpler than nginx, host on cheap cloud (DigitalOcean, Hetzner, Lightsail), and want a contact form without paying Formspree or running a separate service. They will not migrate from Formspree unless setup takes ten minutes or less. The DigitalOcean port-587 block makes them the canonical Postmark-API audience.

This is the author. The Ghost blog migration is the v1 dogfooding case.

### Secondary (v1.0)

**Homelab and self-hosted operators running Caddy as the front door for multiple services.** They are comfortable with Caddyfile and want one Caddy build that handles forms across their stack, with structured logging that flows into their existing log pipeline. v1's multi-endpoint support and Caddy-native logging directly serve them.

### Future audiences (post-v1, named to constrain architecture)

- **Small agencies and freelancers deploying static-site clients.** Need template scalability, per-site config isolation, file attachments, PGP. Out of scope for v1.0 — but their needs do shape the templating system to grow into includes/snippets without a rewrite, and the transport interface to support attachments later.
- **Caddy v1 mailout migrants.** Want a config-compatible upgrade path. Out of scope for v1.0 (clean-slate Caddyfile syntax) but the syntax should not contradict v1's where it doesn't have to.

## Goals and Success Metrics

### Done — when is v1 ready to ship?

All three must pass:

1. The author's Ghost blog runs on caddy-formward for 30 days with zero dropped submissions, confirmed via Postmark logs
2. README has copy-pasteable Caddyfile examples for the Postmark transport, verified on a clean Caddy build via xcaddy
3. Tagged v1.0.0 release on GitHub with a published Docker image

### Worked — did v1 actually achieve anything?

One primary signal:

- **Listed on caddyserver.com modules page within 60 days of release.** Binary, verifiable, and the gate that determines whether random Caddy users discover the module.

GitHub stars, forum mentions, and external user reports are noise relative to this. The modules-page listing is the lever.

## MVP Scope (v1.0)

### In scope

**Transports**
- Postmark HTTP API transport (only transport in v1.0)
- Pluggable transport interface ready for v1.1 additions

**Spam protection**
- Honeypot field (configurable name)
- Origin/Referer check, fails closed when both headers missing if `allowed_origins` is configured
- Token bucket rate limiter per endpoint, with `trusted_proxies` config for X-Forwarded-For handling
- Maximum request body size limit (promoted from v1.1 to address quota-burn threat)

**Validation**
- Required fields list with per-field error responses (HTTP 422)
- Email format validation on the submitter's email field

**Email features**
- Go template rendering for subject and body, with form fields as template data
- Custom fields passthrough — fields not named in config appear in a structured block at the bottom of the email

**Response handling**
- JSON API responses with appropriate HTTP status codes (200, 422, 429, 400, 502)
- Content negotiation via `Accept` header (JSON for fetch, redirect for plain forms)
- `redirect_success` and `redirect_error` URLs

**Operational**
- Caddy structured logging for all events (submissions, sends, failures, spam blocks, rate limits)
- Configurable `log_failed_submissions` flag (default `true`) for terminal-failure recovery
- Multi-endpoint support — each `formward` directive independent

**Failure handling**
- Synchronous send with one retry on transient errors (network/5xx)
- 429 handling with longer backoff (5s)
- Fail fast on 4xx config errors
- Hard 10s request timeout including retries
- On terminal failure, log full submission payload at ERROR level (configurable) and return HTTP 502

**Security NFR**
- Submitter-controlled fields must never be interpolated into email headers as raw strings; must pass through transport library APIs as structured data
- Test coverage required against header-injection payloads (CRLF in email, name, subject; BCC injection attempts)

### Out of scope (v1.0)

- SMTP transport — v1.1
- Resend HTTP API transport — v1.1 candidate
- Webhook transport — post-v1.1
- CSRF token, time-based token spam protection — v1.1
- Captcha, proof-of-work — v3
- File attachments, PGP encryption, confirmation auto-replies — post-v1
- SQLite submission log, retry queue, admin UI — v2/v3
- Prometheus metrics, health check endpoint, dry run mode — v1.1+
- Markdown body support — post-v1
- Config-compatible port from v1 mailout plugin — never

## Post-MVP Vision

**v1.1** lands within ~4 weeks of v1.0:
- SMTP transport (the historical baseline, now the headline post-v1.0 feature)
- Resend HTTP API transport — proves multi-API support, ~2h work, gives "supports multiple API services" credibility before SMTP ships
- Time-based token + CSRF token spam protection
- Max length limits per field
- Dry run mode, health check endpoint
- IP stripping option for GDPR contexts

**v2** is the architectural shift — ~15–20 hours of work — that unlocks the rest of the roadmap:
- SQLite submission log
- Send queue with retry across restarts
- Multiple outputs per endpoint (email + webhook + log fan-out)

**v3** is speculative and depends on community traction:
- Admin UI (embedded web app, requires SQLite storage)
- Proof-of-work spam challenge
- PGP encryption

## Technical Constraints (locked)

| Constraint | Value |
|---|---|
| Language | Go 1.25+ |
| Caddy version | 2.9+ |
| License | Apache-2.0 (matches Caddy itself) |
| Caddyfile syntax stability | Stable within a major version after v1.0.0 |
| Go API stability | Not guaranteed; subject to refactor |
| Repo | github.com/craigmccaskill/caddy-formward |
| Caddy module ID | http.handlers.formward |
| Caddyfile directive | formward |
| Build tooling | xcaddy |
| Distribution | GitHub releases + Docker image with pre-built Caddy |

## Threat Model

### In scope for v1.0

| # | Threat | v1.0 defense |
|---|---|---|
| 1 | Drive-by scraper bots | Honeypot field |
| 2 | Direct-POST bots that skip the form page | Origin/Referer check, fails closed |
| 3 | Basic targeted abuse | Token bucket rate limit (proxy-aware via `trusted_proxies`) |
| 4 | Postmark quota burn | Rate limit + max request body size |
| 5 | Email header injection | Structured-data transport API + explicit injection-payload test coverage |

### Out of scope for v1.0

- Botnet spam from many low-rate IPs — v3 (captcha or proof-of-work)
- DDoS / Layer 7 attacks — CDN's responsibility, not the form handler's
- API key theft from misconfigured deployment — operator concern, addressed via documentation

## Constraints and Assumptions

**Constraints:**
- Single author, working part-time
- 15-hour total budget for v1.0 implementation
- 90-day calendar tripwire from project start to v1.0.0 tag
- All testing must be possible with infrastructure the author already has (Postmark account; no SMTP server)

**Assumptions:**
- Caddy 2.9 module API remains stable through v1.0 development
- Postmark API and free-tier availability remain unchanged in pricing structure
- The `caddy-formward` repo name and module ID remain unclaimed (verified 2026-04-27)

## Risks

| ID | Risk | Likelihood | Impact | Mitigation |
|----|------|------------|--------|------------|
| R1 | Solo maintainer abandonment | Medium | High | Time-bound commitment statement in README; 90-day shipping tripwire — if v1.0 isn't shipped within 90 days of project start, scope cuts further |
| R2 | Effort blowup beyond 15h budget | High | High | Hard tripwire at 15h; if hit, cut polish features (content negotiation refinements, validation depth) before cutting core. SMTP is already out of v1.0, so it cannot be cut as an escape valve. |
| R3 | Discoverability failure (no modules-page listing) | Low | High | Submit modules-page PR within 7 days of v1.0.0; participate in Caddy community forum; publish launch blog post |
| R4 | Header injection vulnerability ships in v1.0.0 | Medium without explicit testing; low with | Very high | Explicit injection-payload test cases as a PRD requirement; use Postmark library APIs that handle headers as structured JSON |
| R5 | Email deliverability rabbit hole on launch day | Medium | Medium | Pre-launch DNS verification checklist (SPF, DKIM, DMARC) on test domain; document DNS requirements in README |
| R6 | Postmark API or pricing change mid-development | Low | High | Acknowledged. SMTP transport in v1.1 is the natural backstop. |
| R7 | Caddy ecosystem loses momentum | Low | High | Acknowledged. Out of author's control. |

## Open Questions

None remaining at the brief level. Implementation-level questions belong in [the PRD](./02-prd.md).

## Appendices

### A. References

- Original v1 plugin: https://github.com/SchumacherFM/mailout
- Caddy v2 module docs: https://caddyserver.com/docs/extending-caddy
- Caddy v2 Caddyfile integration: https://caddyserver.com/docs/extending-caddy/caddyfile
- xcaddy: https://github.com/caddyserver/xcaddy
- Postmark API: https://postmarkapp.com/developer

### B. Related project documents

- Caddy Formward Dashboard (in author's Obsidian vault) — project roadmap and v2/v3 scope context
- Ghost Migration project notes (in author's Obsidian vault) — immediate consumer of this module
