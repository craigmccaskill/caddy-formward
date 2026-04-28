# caddy-formward

A Caddy v2 module that receives form submissions and delivers them via pluggable transports — modern, API-first successor to [SchumacherFM/mailout](https://github.com/SchumacherFM/mailout).

> **Status:** pre-v1.0. The specification is locked; implementation is in progress. See [`spec/`](./spec/) for the full v1.0 brief, PRD, and architecture documents.

## Why

Most cloud hosts (DigitalOcean, AWS Lightsail, etc.) block outbound SMTP on port 587 and recommend an HTTP API service like [Postmark](https://postmarkapp.com) instead. The Caddy v1 mailout plugin closed this gap but was never ported to v2. Surviving alternatives are dead ([Formgate](https://github.com/jung-kurt/caddy-formgate), last release 2019), SMTP-only ([Mailform](https://github.com/Feuerhamster/mailform), broken on those same hosts), or paid SaaS (Formspree, Getform, Netlify Forms).

caddy-formward is built API-first to work where SMTP can't. v1.0 ships with a Postmark transport; SMTP, Resend, and webhook transports are planned for v1.1+.

## Planned Caddyfile syntax (v1.0)

```caddyfile
formward /api/contact {
    to craig@example.com
    from "Contact Form <noreply@example.com>"

    transport postmark {
        api_key {env.POSTMARK_API_KEY}
    }

    rate_limit 5 1m
    trusted_proxies cloudflare
    honeypot _gotcha
    allowed_origins https://example.com
    max_body_size 32KB

    required name email message
    subject "Contact from {{.name}}"
    body templates/contact.txt

    redirect_success /thank-you
    redirect_error /contact?error=true
}
```

Multiple `formward` directives per site are supported, each with independent configuration.

## Installation (planned, post-v1.0.0)

```bash
xcaddy build --with github.com/craigmccaskill/caddy-formward
```

A Docker image will be published to GitHub Container Registry once v1.0.0 is tagged.

## Roadmap

| Version | Scope |
|---|---|
| **v1.0** | Postmark transport, full spam protection stack, rate limiting, templating, JSON+redirect responses |
| **v1.1** | SMTP transport, Resend transport, CSRF + time-based token spam protection, dry run, health check |
| **v2** | SQLite submission log, retry queue, multiple outputs per endpoint, file attachments, webhook transport |
| **v3** | Admin UI, proof-of-work spam challenge, PGP encryption, Prometheus metrics |

The full feature breakdown lives in [`spec/01-project-brief.md`](./spec/01-project-brief.md) §"Post-MVP Vision".

## Contributing

The v1.0 specification is locked. Implementation follows the epic-and-story breakdown in [`spec/02-prd.md`](./spec/02-prd.md). Contributions outside the locked v1.0 scope should wait for v1.1 planning.

For implementation questions, the architecture document at [`spec/03-architecture.md`](./spec/03-architecture.md) is the source of truth.

## License

Apache-2.0. See [LICENSE](./LICENSE).
