# Runtime configuration

The Curu server reads runtime settings from environment variables and rejects malformed values before listening. It exposes no product-data routes yet, but startup now requires a migrated compatible database.

| Variable | Default | Contract |
| --- | --- | --- |
| `CURU_DATABASE_URL` | required | PostgreSQL URL for the restricted runtime role. Must include user, host, and database. Startup reads migration history and never migrates or seeds. Select the deployment-required TLS mode in the URL. |
| `CURU_LISTEN_ADDRESS` | `127.0.0.1:8080` | TCP `host:port`; use an explicit non-loopback address only inside the intended deployment boundary. Port `0` is supported for disposable tests. |
| `CURU_SHUTDOWN_TIMEOUT` | `10s` | Positive Go duration allowed for graceful HTTP shutdown. |
| `CURU_READ_HEADER_TIMEOUT` | `5s` | Positive Go duration allowed to read request headers. |
| `CURU_IDLE_TIMEOUT` | `60s` | Positive Go duration for idle keep-alive connections. |
| `CURU_JWT_PROFILE` | required | Must be `rfc9068`; no generic or token-selected fallback profile exists. |
| `CURU_JWT_ISSUER` | required | Exact, case-sensitive HTTPS issuer identifier. |
| `CURU_JWT_AUDIENCE` | required | Dedicated, non-empty Curu resource audience. |
| `CURU_JWKS_URL` | required | Operator-trusted absolute HTTPS JWKS endpoint; it is never taken from a token. |
| `CURU_JWT_ALGORITHMS` | required | Comma-separated allowlist containing `RS256`, `ES256`, or both. Symmetric and unsigned tokens are unsupported. |
| `CURU_JWT_CLOCK_SKEW` | `60s` | Non-negative temporal tolerance, capped at `60s`. |
| `CURU_JWT_MAX_LIFETIME` | `15m` | Positive maximum `exp`–`iat` lifetime, capped at `15m`. |
| `CURU_JWKS_FETCH_TIMEOUT` | `5s` | Positive bound for each JWKS request. |
| `CURU_JWKS_DEFAULT_CACHE_TTL` | `5m` | Freshness used when the endpoint supplies none, capped at `15m`; HTTP freshness can shorten it. |
| `CURU_JWKS_UNKNOWN_KID_REFRESH_INTERVAL` | `1m` | Positive rate limit for refreshes triggered by an unknown key ID. |
| `CURU_OWNER_ISSUER` | required | Exact, case-sensitive issuer in the single-owner binding. It must match `CURU_JWT_ISSUER` for the one supported trust configuration. |
| `CURU_OWNER_SUBJECT` | required | Exact, case-sensitive non-empty subject in the single-owner binding. No alias, email, group, role, domain, or wildcard matching is performed. |
| `CURU_OWNER_ID` | required | Stable internal owner UUID used by the initialized dataset and application policy. Seeded deployments must use the same UUID supplied to `curu-initialize`. |

Migration-only settings and commands are documented in [`database-operations.md`](database-operations.md); migration credentials are never accepted by the server. JWT trust settings are mandatory even though product routes are not implemented yet. The server retrieves only the configured JWKS URL, bounds response size and fetch time, follows HTTP freshness precedence and overflow-safe current age up to a 15-minute cap, treats invalid or ambiguous explicit freshness as stale, rate-limits unknown-key refreshes, and fails closed when no fresh usable set is available. Liveness does not depend on the key service; readiness does. The deployment platform is responsible for restricting readiness to its platform network.

The exact configured owner issuer/subject is the only authenticated identity mapped to the internal owner UUID. Every non-health HTTP route passes through this owner boundary; missing or invalid credentials return `401`, a valid unknown identity returns `403`, and an unavailable signing-key dependency returns `503`, all without credential or identity detail. The coarse health endpoints remain product-data-free platform probes. Tokens and claims are not logged. Public origin, CSRF, HTMX expiry presentation, and trusted-proxy configuration remain absent until CURU-016 implements those boundaries.
