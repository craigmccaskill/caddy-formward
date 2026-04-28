---
title: "Architecture: caddy-formward v1.0"
status: locked
created: 2026-04-27
synced_from_obsidian: 2026-04-27
---

# Architecture: caddy-formward v1.0

This document describes the structure, lifecycle, and component design for the v1.0 implementation. Every architectural decision derives from a requirement in [the PRD](./02-prd.md), which in turn derives from a commitment in [the project brief](./01-project-brief.md). Nothing here introduces new behavior.

This is the implementation blueprint. Open it before writing code; reference it during code review.

## Module overview

caddy-formward is a single Go package that registers as `http.handlers.formward` with Caddy. It implements the `caddyhttp.MiddlewareHandler` interface, intercepting POST requests on configured paths and turning them into outbound emails.

The package is intentionally flat — one Go module, no sub-packages — because Caddy expects modules to register from a single import path and the v1.0 scope doesn't justify package boundaries. Sub-packages may emerge in v2 (e.g., `internal/storage` for SQLite).

```
                        ┌──────────────────────────────────┐
                        │ Caddy v2 (xcaddy build)          │
                        │  ┌────────────────────────────┐  │
HTTP request ──────────▶│  │ caddy-formward handler     │  │
                        │  │                            │  │
                        │  │  body cap → method →       │  │
                        │  │  content-type → origin →   │  │
                        │  │  rate limit → form parse → │  │
                        │  │  honeypot → validation →   │  │
                        │  │  template render →         │  │
                        │  │  Transport.Send()          │  │
                        │  └─────────────┬──────────────┘  │
                        │                │                 │
                        └────────────────┼─────────────────┘
                                         │
                                         ▼
                                  ┌──────────────┐
                                  │ Postmark API │
                                  └──────────────┘
```

## File layout

All files live at the top level of the repository. Co-located `_test.go` files hold tests for the file they test.

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
├── go.mod
├── go.sum
├── Dockerfile             # xcaddy build + slim runtime image
├── LICENSE                # Apache-2.0
├── README.md
├── spec/                  # Locked specification (synced from author's Obsidian vault)
│   ├── 01-project-brief.md
│   ├── 02-prd.md
│   └── 03-architecture.md
└── .github/
    └── workflows/
        └── ci.yml         # go test ./... + go vet on push/PR
```

## Caddy module lifecycle

The module implements the following Caddy interfaces:

| Interface | Purpose | When called |
|---|---|---|
| `caddy.Module` | Module registration | Once at startup, returns `caddy.ModuleInfo{ID: "http.handlers.formward"}` |
| `caddyfile.Unmarshaler` | Caddyfile → struct | When operator uses Caddyfile config |
| `caddy.Provisioner` | One-time setup | After config loaded, before serving |
| `caddy.Validator` | Config sanity checks | After `Provision`, before serving |
| `caddyhttp.MiddlewareHandler` | Per-request handler | On every matched request |

### Provision

Called once when the configuration is loaded (or reloaded). Responsibilities:

1. Resolve `{env.VAR}` placeholders in transport config (Postmark API key)
2. Parse subject and body templates; cache compiled `*template.Template`
3. Construct the configured transport instance (Postmark in v1.0)
4. Construct the rate limiter with configured capacity and interval
5. Set up the structured logger via the Caddy context (`ctx.Logger(m)`)
6. Default unset values: `log_failed_submissions=true`, `max_body_size=64KB`, `email_field="email"`

Provision errors are fatal at config-load time, surfaced to the operator via `caddy validate` or `caddy run`.

### Validate

Lightweight semantic checks beyond what Provision needs:

- `to` is non-empty and contains valid email format
- `from` is non-empty
- If `allowed_origins` is present, the list is non-empty (NFR4 — explicit-empty rejection)
- If `required` is non-empty, the listed field names are syntactically valid
- Templates parse successfully (delegated to Provision; just confirm cached templates exist)

### ServeHTTP

The per-request handler. Detailed flow in [Request flow](#request-flow) below.

### CleanupUpper (deferred)

v1.0 has no background goroutines (the rate limiter is lazy — refills on `Allow()`), so the `CleanupUpper` interface is **not** implemented in v1.0. Add it in v2 when SQLite or persistent retry queues introduce background work.

## Request flow

The handler processes each request through an ordered pipeline. Order is intentional — cheaper checks first, header-only checks before body-parsing checks, security checks before processing.

```
┌──────────────────────────────────────────────────────────────┐
│ 1. body size cap        → http.MaxBytesReader wraps r.Body   │  413
│ 2. method check         → POST only                          │  405
│ 3. content-type check   → form-encoded only                  │  400
│ 4. origin/referer check → fail-closed if allowed_origins set │  403
│ 5. rate limit check     → token bucket, proxy-aware IP       │  429
│ 6. parse form           → r.ParseForm() reads body           │  413/400
│ 7. honeypot check       → silent 200 if field non-empty      │  200 (silent)
│ 8. required fields      → all listed fields present + non-empty │ 422
│ 9. email format         → submitter email field syntactic    │  422
│ 10. generate submission ID (UUIDv4), log "submission_received"│
│ 11. render subject template                                  │
│ 12. render body template + custom-fields passthrough         │
│ 13. transport.Send() with retry policy (FR18-21)             │
│ 14. log outcome, write response (JSON or redirect)           │  200/502
└──────────────────────────────────────────────────────────────┘
```

### Ordering rationale

- **Body size first.** Wrapping the reader before any read prevents large bodies from being buffered. The actual rejection fires when something attempts to read past the cap (typically `r.ParseForm` at step 6).
- **Header checks before form parse.** Origin/Referer and rate limit don't need the body parsed; rejecting at step 4-5 saves CPU and prevents form-parse-based DoS.
- **Rate limit before form parse.** Token bucket check is O(1) and uses connection IP — no need to wait for body.
- **Honeypot before validation.** Honeypot must respond `200` silently. If validation ran first and returned `422` for a bot's missing required fields, the bot would learn the field structure. Honeypot first means bots get an indistinguishable success response.
- **Validation before template render.** Templates can reference any field; a missing required field would render as empty in the email. Reject at validation so the operator's inbox shows complete submissions.
- **Submission ID at step 10.** After all rejection paths but before the send. Bots and rate-limited requests don't get IDs (no point — they have no recoverable state).

## Component design

### Configuration model

The single `Module` struct holds both serialized config (via `json:` tags) and runtime state (unexported). Caddy's reflection-based JSON marshaling handles the serialized portion; runtime state is populated in `Provision`.

```go
type Module struct {
    // Serialized config
    Path                  string             `json:"path,omitempty"`
    To                    []string           `json:"to,omitempty"`
    From                  string             `json:"from,omitempty"`
    Transport             TransportConfig    `json:"transport,omitempty"`
    RateLimit             *RateLimitConfig   `json:"rate_limit,omitempty"`
    TrustedProxies        []string           `json:"trusted_proxies,omitempty"`
    Honeypot              string             `json:"honeypot,omitempty"`
    AllowedOrigins        []string           `json:"allowed_origins,omitempty"`
    MaxBodySize           int64              `json:"max_body_size,omitempty"`
    Required              []string           `json:"required,omitempty"`
    EmailField            string             `json:"email_field,omitempty"`
    Subject               string             `json:"subject,omitempty"`
    Body                  string             `json:"body,omitempty"`
    LogFailedSubmissions  *bool              `json:"log_failed_submissions,omitempty"`
    RedirectSuccess       string             `json:"redirect_success,omitempty"`
    RedirectError         string             `json:"redirect_error,omitempty"`

    // Runtime (populated in Provision, not serialized)
    transport  Transport
    limiter    *rateLimiter
    subjectTpl *template.Template
    bodyTpl    *template.Template
    logger     *zap.Logger
}
```

`*bool` for `LogFailedSubmissions` allows distinguishing unset (default true) from explicitly false.

### Transport interface

The transport layer is one interface with one implementation in v1.0. The interface is intentionally narrow so future transports (SMTP, Resend, webhook) can implement it without changes.

```go
// Message is the canonical form of an email passed to a Transport.
// All fields are structured data — no transport may interpolate
// these as raw strings into protocol headers (NFR1).
type Message struct {
    From       string
    To         []string
    ReplyTo    string
    Subject    string
    BodyText   string
    // BodyHTML is reserved for v2 markdown body support.
}

// Transport sends a Message. Implementations must return
// a *TransportError so the handler can classify retries.
type Transport interface {
    Send(ctx context.Context, msg Message) error
}

// ErrorClass classifies transport errors for retry policy.
type ErrorClass int

const (
    ErrUnknown      ErrorClass = iota
    ErrTransient                // network/5xx, retry once after 1s
    ErrRateLimited              // 429, retry once after 5s
    ErrTerminal                 // 4xx (non-429), no retry
)

type TransportError struct {
    Class   ErrorClass
    Status  int    // HTTP status if applicable, 0 otherwise
    Cause   error  // underlying error (network, decode, etc.)
    Message string
}

func (e *TransportError) Error() string { ... }
func (e *TransportError) Unwrap() error { return e.Cause }
```

### Postmark transport

`transport_postmark.go` implements `Transport` against `https://api.postmarkapp.com/email`.

Key design decisions:

- **No third-party Postmark SDK.** A bespoke HTTP client is ~80 lines and avoids pulling in a dependency we'd have to update.
- **Headers as JSON fields.** The Postmark request body is `encoding/json`-marshaled from a struct with named fields (`From`, `To`, `ReplyTo`, `Subject`, `TextBody`). At no point does the implementation construct headers via string concatenation — this is the architectural enforcement of NFR1.
- **API key in `X-Postmark-Server-Token` header.** Set on the request once during construction; never logged (NFR3).
- **Response classification.** Status code mapping:
  - `200`/`202` → success
  - `429` → `ErrRateLimited`
  - `5xx`, network errors, timeouts → `ErrTransient`
  - `4xx` (non-429) → `ErrTerminal`
- **HTTP client.** Reuses a package-level `*http.Client` with a 5s per-request timeout (caller's context handles overall deadline).

### Spam protection

`spam.go` exposes three independent check functions, each returning a typed result the handler can act on:

```go
type spamResult int
const (
    spamPass spamResult = iota
    spamSilentReject    // honeypot — return 200 OK silently
    spamHardReject      // origin/body — return appropriate status
)

func checkHoneypot(r *http.Request, fieldName string) spamResult
func checkOrigin(r *http.Request, allowed []string) (spamResult, string /* reason */)
// max-body-size is enforced via http.MaxBytesReader at the handler entry,
// not as a function call — included here for completeness only.
```

The handler runs these in sequence and translates results to HTTP responses.

### Rate limiter

`ratelimit.go` implements a token-bucket limiter keyed by client IP.

```go
type tokenBucket struct {
    tokens float64
    last   time.Time
}

type rateLimiter struct {
    capacity     float64       // burst size = `count` parameter
    refillPerSec float64       // capacity / interval seconds

    mu      sync.Mutex
    buckets *lru.Cache[string, *tokenBucket]  // hashicorp/golang-lru/v2
}

func (r *rateLimiter) Allow(clientIP string) bool {
    r.mu.Lock(); defer r.mu.Unlock()
    bucket, ok := r.buckets.Get(clientIP)
    if !ok {
        bucket = &tokenBucket{tokens: r.capacity, last: time.Now()}
        r.buckets.Add(clientIP, bucket)
    }
    // refill based on elapsed time
    elapsed := time.Since(bucket.last).Seconds()
    bucket.tokens = math.Min(r.capacity, bucket.tokens + elapsed * r.refillPerSec)
    bucket.last = time.Now()
    if bucket.tokens >= 1 {
        bucket.tokens--
        return true
    }
    return false
}
```

LRU cap of 10,000 IPs (NFR6) bounds memory at ~640KB per endpoint (rough — 64 bytes per entry). Eviction of an IP resets its bucket on next request, which is acceptable: an evicted IP that was being limited just gets a fresh budget. Worst case: an attacker rotating through 10,001+ IPs evicts honest IPs' state. That's a botnet attack, which the brief explicitly defers to v3.

### Client IP extraction

```go
func clientIP(r *http.Request, trustedProxies []netip.Prefix) string {
    // If the connection's RemoteAddr is in trustedProxies,
    // walk X-Forwarded-For from right to left, returning the
    // rightmost IP that is *not* in trustedProxies.
    // Otherwise return RemoteAddr's IP.
}
```

`trustedProxies` syntax: a list of CIDRs in the Caddyfile (e.g., `trusted_proxies 10.0.0.0/8 192.168.0.0/16`). Open question #1 in the PRD allows for named presets (`cloudflare`) if budget permits — implemented as a lookup that expands to known CIDR sets.

### Validation

`validate.go` exposes:

```go
func validateRequired(form url.Values, required []string) []string {
    // returns list of field names that are missing or empty
}

func validateEmail(value string) bool {
    // basic syntactic check via net/mail.ParseAddress
}
```

Both functions are pure — no logging, no response writing. The handler uses their returns to construct 422 responses.

### Templating

`template.go` exposes:

```go
type renderInput struct {
    Form     url.Values        // raw form values
    Named    map[string]string // single-value subset of fields named in config
}

func (m *Module) renderSubject(form url.Values) (string, error)
func (m *Module) renderBody(form url.Values, namedFields map[string]bool) (string, error)
```

`renderBody` adds the custom-fields passthrough block: any form field NOT in `namedFields` (the union of `required`, `email_field`, `honeypot`, and any field referenced in templates) is appended to the rendered body in a sorted block:

```
[rendered body template output]

Additional fields:
  company: Acme Corp
  source: HN

```

Detection of "fields referenced in templates" uses Go template's `Tree.Root.Nodes` walk to collect field references at parse time, cached in `Module`.

### Response handling

`response.go`:

```go
type errorResponse struct {
    Error  string            `json:"error"`
    Code   string            `json:"code"`
    Fields map[string]string `json:"fields,omitempty"`  // for 422
}

func writeJSON(w http.ResponseWriter, status int, body any) error
func writeRedirect(w http.ResponseWriter, r *http.Request, url string) error

// Negotiate decides JSON vs redirect based on Accept header
// and configured redirect URLs.
func (m *Module) negotiate(r *http.Request) responseMode
```

Content negotiation logic:
1. If neither `redirect_success` nor `redirect_error` is configured → JSON
2. Else if `Accept: application/json` is preferred over `text/html` → JSON
3. Else → redirect

### Logging

`logging.go` wraps the Caddy logger with structured-field helpers:

```go
type logCtx struct {
    submissionID uuid.UUID
    endpoint     string
    transport    string  // "postmark"
    startTime    time.Time
}

func (m *Module) logReceived(ctx logCtx, form url.Values)
func (m *Module) logSent(ctx logCtx)
func (m *Module) logRetry(ctx logCtx, err error)
func (m *Module) logFailed(ctx logCtx, err error, payload url.Values)
func (m *Module) logSpamBlocked(ctx logCtx, reason string)
func (m *Module) logRateLimited(ctx logCtx, clientIP string)
func (m *Module) logValidationFailed(ctx logCtx, fields []string)
```

Every log line includes: `submission_id`, `endpoint`, `transport`, `latency_ms` (calculated from `startTime`). API keys are never logged — the transport never receives the key as a separate logged value, only as an HTTP header set during request construction (NFR3).

`logFailed` includes the full payload only if `log_failed_submissions` is true; otherwise omits the form values and logs only field names.

## Threat → defense → code mapping

This is the load-bearing artifact for security review. Every in-scope threat from [the project brief](./01-project-brief.md) §"Threat Model" maps to a concrete defense in concrete code with concrete tests.

| Threat | Defense | Code | Tests |
|---|---|---|---|
| Drive-by scraper bots | Honeypot field | `spam.go::checkHoneypot` | `spam_test.go::TestHoneypot_*` |
| Direct-POST bots that skip the form page | Origin/Referer with fail-closed when configured | `spam.go::checkOrigin` | `spam_test.go::TestOrigin_AllowedDeniedMissingBoth` |
| Basic targeted abuse | Token bucket rate limit, proxy-aware | `ratelimit.go::Allow`, `clientIP` | `ratelimit_test.go::TestTokenBucket_*`, `TestClientIP_TrustedProxies` |
| Postmark quota burn | Rate limit + max body size | `ratelimit.go` + `http.MaxBytesReader` in handler | as above + `formward_test.go::TestBodySizeLimit` |
| Email header injection | All header fields passed as JSON struct fields, never string-concat | `transport_postmark.go::Send` (struct → `json.Marshal`) | `transport_postmark_test.go::TestNoHeaderInjection_*` (CRLF in From, To, Subject, ReplyTo) |

For threats explicitly out of scope, this table also documents the *non*-defense:

| Out-of-scope threat | Disposition |
|---|---|
| Botnet spam (many low-rate IPs) | No v1.0 defense; LRU eviction at 10K IPs gracefully degrades but does not protect. v3 captcha/PoW is the planned response. |
| DDoS / Layer 7 attacks | CDN's responsibility; module trusts upstream proxy/load balancer to absorb. Documented in README. |
| API key theft | Operator concern; mitigated by `{env.VAR}` config + NFR3 (key never logged). Documented in README. |

## Concurrency and state

### Per-request state

Each `ServeHTTP` invocation operates on:
- The request and response writer (request-scoped, no shared mutation)
- A locally-allocated `logCtx` with a fresh UUID
- Locally-rendered subject and body strings
- A local `Message` passed to `Transport.Send`

No request-scoped state is shared across requests.

### Shared state

The module holds three pieces of shared state, all populated in `Provision` and immutable thereafter:

| State | Concurrency strategy |
|---|---|
| `*template.Template` (subject and body) | Go's `template.Template` is safe for concurrent execution after parse; no extra locking needed. |
| `Transport` (Postmark client) | Wraps a single `*http.Client`. Standard library guarantees `*http.Client` is safe for concurrent use. The transport itself is stateless beyond the client and API key. |
| `*rateLimiter` | Mutex-guarded; one mutex per limiter, held only during the per-IP bucket update. Contention is bounded by request rate. |

No goroutines are spawned. No channels are used. The module is request-driven and stateless beyond the rate limiter cache.

## Dependencies

### Production

| Dependency | Purpose | Justification |
|---|---|---|
| `github.com/caddyserver/caddy/v2` | Module framework | Required by definition |
| `go.uber.org/zap` | Structured logging | Comes with Caddy; use it directly |
| `github.com/google/uuid` | Submission ID generation | Standard, single-purpose, ~200 LOC |
| `github.com/hashicorp/golang-lru/v2` | LRU cache for rate limiter | Battle-tested, type-parameterized in v2 |

### Test-only

| Dependency | Purpose |
|---|---|
| Standard library `testing` | Test framework |
| Standard library `net/http/httptest` | Mock Postmark API server |

### Explicitly NOT pulled in

- Postmark SDKs (e.g., `mrz1836/postmark`) — bespoke client is small enough
- Validation libraries (e.g., `go-playground/validator`) — v1 needs are simple (required + email format), stdlib `net/mail` suffices
- Rate-limiting libraries (e.g., `golang.org/x/time/rate`) — token bucket is ~30 lines and ours needs LRU eviction which `x/time/rate` lacks
- Templating engines beyond stdlib `text/template`

This list is conservative on purpose: every dependency is a v1.1+ liability. Adding one requires documented justification.

## Test architecture

Test files are co-located with the source they test (`spam.go` ↔ `spam_test.go`). Test helpers and fixtures live in `testhelpers_test.go` (a single `_test.go` file is per-package shared across all tests).

### Mock Postmark server

Transport tests use `httptest.NewServer` with handler stubs:

```go
func mockPostmark(t *testing.T, status int, body string) *httptest.Server {
    return httptest.NewServer(http.HandlerFunc(func(w, r) {
        w.WriteHeader(status)
        w.Write([]byte(body))
    }))
}
```

The transport accepts a `BaseURL` config value (default `https://api.postmarkapp.com`) so tests can point it at the mock. Production config never sets `BaseURL`.

### Header injection tests

NFR2 requires explicit injection-payload coverage. Test cases as a table:

```go
var injectionPayloads = []struct {
    name  string
    field string  // which field to inject into
    value string
}{
    {"crlf_bcc_in_from",     "from",     "attacker@evil.com\r\nBcc: target@victim.com"},
    {"crlf_bcc_in_subject",  "subject",  "Hello\r\nBcc: target@victim.com"},
    {"crlf_in_replyto",      "reply_to", "x@x.com\r\nBcc: target@victim.com"},
    {"unicode_crlf",         "subject",  "Hello
Bcc: target@victim.com"},
    {"smuggled_header",      "name",     "Jane\r\nX-Spoof: yes"},
}
```

For each payload, the test sends a submission, captures the outgoing JSON to the mock Postmark server, and asserts:
1. The injected CRLF sequence does not appear as a JSON-level header in the marshaled request
2. The literal string is preserved as data within the field it was injected into (no silent truncation or sanitization that could mask the issue elsewhere)

### CI matrix

Single Linux + Go 1.23 job. No matrix builds for v1.0 — Caddy's compatibility floor is well-defined and we don't need to test Windows/macOS for a Linux-targeted Docker deploy.

## Build and distribution

### xcaddy build

```
xcaddy build --with github.com/craigmccaskill/caddy-formward
```

This is the canonical install path documented in the README. It produces a `caddy` binary with the module compiled in.

### Dockerfile

Multi-stage:

```dockerfile
FROM caddy:2.9-builder AS builder
RUN xcaddy build \
    --with github.com/craigmccaskill/caddy-formward

FROM caddy:2.9-alpine
COPY --from=builder /usr/bin/caddy /usr/bin/caddy
```

Image is published to GitHub Container Registry on tagged releases.

### Release artifacts

For each tagged release (v1.0.0+):
- GitHub Release with hand-written notes summarizing changes
- Docker image at `ghcr.io/craigmccaskill/caddy-formward:v1.0.0` and `:latest`
- No pre-built binaries — `xcaddy` is the supported install path because it produces a Caddy build with whatever else the operator wants compiled in

### Modules-page submission

Within 7 days of v1.0.0 (R3 mitigation): file a PR against `github.com/caddyserver/website` adding caddy-formward to the modules listing. The submission requires a working module-info JSON entry; the entry will be drafted in `docs/modules-page-entry.json` for review before submission.

## Architectural decisions log

Worth recording explicitly, in case future-you wonders.

**ADR-1: No Postmark SDK.** A third-party SDK adds a dependency we'd have to track and update for every Postmark API change. The bespoke client is ~80 lines, has zero runtime overhead, and gives us complete control over error classification (which is the whole point of the transport interface). Reconsider in v1.1+ only if we add 3+ HTTP API transports and the duplication becomes painful.

**ADR-2: Flat package, not internal sub-packages.** The v1.0 surface is small enough that splitting `internal/spam`, `internal/transport`, etc., would be ceremony without benefit. A flat package is easier to navigate for contributors and matches the convention of most Caddy modules. Revisit in v2 when SQLite adds 1000+ LOC of storage code.

**ADR-3: Token bucket with LRU, not `golang.org/x/time/rate`.** `x/time/rate` is excellent but doesn't bound memory — every distinct key (IP) holds a `Limiter` forever. We need LRU eviction at 10K IPs (NFR6) which requires rolling our own. The implementation is ~30 lines and well-tested.

**ADR-4: `*bool` for `LogFailedSubmissions`.** A pointer lets us distinguish "operator omitted the field" (default true) from "operator explicitly set false." A plain `bool` defaults to false on omit, which would silently change behavior between versions if we ever changed the default. Pointer-bool is a common Go config pattern despite the awkwardness.

**ADR-5: Synchronous send, not async with queue.** v1.0 has no persistent storage, so an async queue would be lost on restart. The brief commits to "log on terminal failure" as the recovery mechanism, which works only if the failure is observed in the request log. v2 brings SQLite + async queue together, at which point this decision flips.

## Open architectural questions

These are implementation decisions deferred from the brief and PRD that affect code organization. Each has a recommended answer; final decision is made during the relevant story.

1. **`trusted_proxies` syntax — CIDR-only or with named presets (`cloudflare`)?**
   Recommendation: CIDR-only in v1.0; named presets in v1.1 if there's user demand. Decided during Story 3.2.

2. **Rate limit configuration syntax — single line or sub-block?**
   Recommendation: single line (`rate_limit 5 1m`). Sub-block is nicer if we add `burst`, `key_by_path`, etc., but those are post-v1. Decided during Story 3.2.

3. **Body template — file path vs inline detection.**
   Recommendation: heuristic — if the value contains `{{` it's inline; otherwise it's a file path. Reject ambiguity at validation time. Decided during Story 4.2.

4. **Response JSON schema versioning.**
   Recommendation: don't version v1.0 responses. Add a top-level `"version": "1"` field if we ever change the schema in a breaking way (post-v1). Decided during Story 5.1.

5. **Error message verbosity for terminal failures.**
   Recommendation: 502 response body says "Submission could not be delivered. Please try again later." with no detail. Detail is in the operator's logs. Avoiding leaking whether the failure was config (4xx from Postmark) vs runtime (network) to a potential attacker. Decided during Story 6.1.

## Appendix: Caddyfile grammar reference

For Story 1.2 implementation:

```
formward <path> {
    to <email> [<email>...]
    from <email>
    
    transport postmark {
        api_key <token>           # supports {env.VAR}
        # base_url <url>          # test-only, undocumented
    }
    
    rate_limit <count> <interval>
    trusted_proxies <cidr> [<cidr>...]
    honeypot <field_name>
    allowed_origins <url> [<url>...]
    max_body_size <size>          # supports KB/MB suffix
    
    required <field> [<field>...]
    email_field <field_name>      # default: "email"
    
    subject <template>            # inline string
    body <path>                   # file path (heuristic per open-Q 3)
    
    log_failed_submissions <bool>
    
    redirect_success <url>
    redirect_error <url>
}
```

The grammar is deliberately conservative: every directive is a single-line key-value pair except `transport` which is a sub-block (extensible — SMTP and Resend will use the same pattern).
