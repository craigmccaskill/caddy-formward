# Posthorn

A self-hosted email gateway for cloud platforms that block outbound SMTP. Accepts mail through pluggable ingress modes and delivers it via pluggable HTTP API transports. v1.0 ships with HTTP form ingress and a Postmark transport.

> **Status:** pre-v1.0. The specification is locked; implementation is in progress. See [`spec/`](./spec/) for the full v1.0 brief, PRD, and architecture documents.
>
> *Not related to [PostHog](https://posthog.com). Different category — Posthorn is an email gateway; PostHog is product analytics.*

## Why

DigitalOcean, AWS Lightsail, Linode, and most cloud hosts block outbound SMTP on ports 25, 465, and 587. The block is policy, not configurable: providers explicitly recommend using an HTTP API service like Postmark, Resend, or Mailgun instead.

This breaks two common patterns simultaneously: web forms that send email (contact forms, signups) and self-hosted apps that emit SMTP for transactional mail (Ghost, Gitea, Mastodon, etc., for admin emails and magic links).

The current options are bad: pay for SaaS form services, rewrite app configs to use API SDKs (rarely supported), run Postfix as a relay with custom HTTP glue, or move to a host that doesn't block SMTP. There is no actively maintained, self-hosted, HTTP-API-first email gateway in 2026.

Posthorn is the bridge.

## Planned deployment shapes (v1.0)

### Standalone (primary)

Run as a Docker container next to your apps:

```yaml
# docker-compose.yml
services:
  posthorn:
    image: ghcr.io/craigmccaskill/posthorn:latest
    volumes:
      - ./posthorn.toml:/etc/posthorn/config.toml:ro
    environment:
      POSTMARK_API_KEY: ${POSTMARK_API_KEY}
    ports:
      - "8080:8080"   # form ingress; reverse-proxy from your front door
```

```toml
# posthorn.toml
[[endpoints]]
path = "/api/contact"
to = ["craig@example.com"]
from = "Contact Form <noreply@example.com>"
honeypot = "_gotcha"
allowed_origins = ["https://example.com"]
required = ["name", "email", "message"]
subject = "Contact from {{.name}}"
body = """
From: {{.name}} <{{.email}}>

{{.message}}
"""
redirect_success = "/thank-you"

[endpoints.transport]
type = "postmark"

[endpoints.transport.settings]
api_key = "${env.POSTMARK_API_KEY}"

[endpoints.rate_limit]
count = 5
interval = "1m"
```

### Caddy adapter (optional)

For operators already running Caddy:

```bash
xcaddy build --with github.com/craigmccaskill/posthorn/caddy
```

```caddyfile
example.com {
    posthorn /api/contact {
        to craig@example.com
        from "Contact Form <noreply@example.com>"
        transport postmark {
            api_key {env.POSTMARK_API_KEY}
        }
        rate_limit 5 1m
        honeypot _gotcha
        allowed_origins https://example.com
        required name email message
        subject "Contact from {{.name}}"
        body templates/contact.txt
        redirect_success /thank-you
    }
}
```

Both deployment shapes use the same internal pipeline — they accept identical inputs and produce identical outbound mail.

## Roadmap

| Version | Scope |
|---|---|
| **v1.0** | HTTP form ingress, Postmark transport, full spam protection stack, rate limiting, templating, JSON+redirect responses, standalone Docker + Caddy adapter |
| **v1.1** | Resend, Mailgun, AWS SES, outbound-SMTP transports; CSRF + time-based token spam protection; dry run; health check; Prometheus metrics |
| **v1.2** | **SMTP ingress** — TCP listener accepting SMTP from internal apps (Ghost, Gitea, Mastodon) and forwarding via the configured HTTP API transport |
| **v2** | SQLite submission log, retry queue across restarts, file attachments, webhook transport |
| **v3** | Admin UI, proof-of-work spam challenge, PGP encryption |

The full feature breakdown lives in [`spec/01-project-brief.md`](./spec/01-project-brief.md) §"Post-MVP Vision".

## Contributing

The v1.0 specification is locked. Implementation follows the epic-and-story breakdown in [`spec/02-prd.md`](./spec/02-prd.md). Contributions outside the locked v1.0 scope should wait for v1.1 planning.

For implementation questions, the architecture document at [`spec/03-architecture.md`](./spec/03-architecture.md) is the source of truth.

## License

Apache-2.0. See [LICENSE](./LICENSE).
