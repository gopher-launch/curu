# Curu gateway, authorization, and data-service contract

This document is the CURU-014 contract. It refines the boundaries in [`product-baseline.md`](product-baseline.md) and [`implementation-design.md`](implementation-design.md). Later tasks implement and test it; no runtime behavior is claimed here. Curu remains independent of any identity provider, gateway product, cloud, or managed PostgreSQL vendor.

## Request and identity boundary

The public browser authenticates to a gateway. The gateway owns third-party login, refresh, its browser cookie, session termination, and any identity-provider exchange. For an authenticated request, the gateway constructs an `Authorization: Bearer <JWT>` header for its private upstream request to Curu. It may forward an existing JWT access token that satisfies the configured Curu validation profile; issuing or exchanging a token is required only when the deployment needs it. It must not forward an ID token as an access token. The gateway removes any browser-supplied `Authorization`, forwarding-identity, and forwarding-address headers before adding its own values. Network policy must prevent public traffic from reaching Curu around the gateway, but Curu still validates every presented token.

Curu accepts a token only from the single `Authorization` header. Duplicate headers, a non-Bearer scheme, a malformed compact token, or token material in a query parameter or application cookie are rejected. Curu never logs the header or token. TLS on the public hop is mandatory; protection of the gateway-to-Curu hop and prevention of direct access are platform responsibilities documented by the deployment.

The initial trust configuration names exactly one issuer, one dedicated Curu resource audience, an allowlist containing `RS256` and/or `ES256`, one HTTPS JWKS endpoint, one access-token validation profile, and one owner binding. The audience must distinguish Curu resource access from browser/client identity tokens and tokens for other services. Trust configuration is deployment input, not token input: Curu never follows `jku`/`x5u`, discovers an issuer, or lets an unverified token choose its trust configuration, validation profile, or allowed algorithms. Symmetric JWT algorithms and `none` are not accepted. The configured JWKS endpoint may be on a different host from the issuer only as an explicit operator choice.

The deployment selects one supported access-token profile explicitly. Curu does not require a bespoke Curu token type or try successively weaker profiles when validation fails:

- Prefer the standard OAuth JWT access-token profile in [RFC 9068](https://www.rfc-editor.org/rfc/rfc9068.html). This profile checks `typ` against `at+jwt` or `application/at+jwt` using the case-insensitive media-type comparison rules in [RFC 7515 §4.1.9](https://www.rfc-editor.org/rfc/rfc7515.html#section-4.1.9). It also validates the standard's required claims, including `client_id` and `jti`, with the required claim types. These additional claims remain adapter validation inputs, not owner identity or application permissions.
- A provider-specific profile may be supported when an actual deployment requires it. Its documented type, required-claim, issuer/audience, or signing-key rules must distinguish access tokens from ID tokens and other token kinds issued by that trusted issuer. A generic `typ: JWT`, a missing type, or a valid signature alone does not establish token purpose. Such a profile must have explicit distinguishing rules and fixtures proving wrong-kind rejection before it is usable.
- Profile selection is a small validation boundary, not a plugin framework or token-controlled policy. CURU-015 initially implements the standard profile. Additional provider-specific support requires a concrete documented integration need and a bounded implementation scope; it is not a prerequisite for the first authentication delivery.

Validation completes before principal mapping and requires all of the following:

- satisfaction of the configured access-token profile, including its token-type, token-purpose, and required-claim rules;
- a signature made by a currently trusted JWKS key, an allowed algorithm that matches the key type, and a non-empty `kid` selecting exactly one key;
- exact string equality for `iss`, the configured audience present in `aud`, and non-empty string `sub`;
- numeric `iat` and `exp`, with `exp` later than `iat`, total lifetime no greater than a configured maximum (15 minutes by default), and current time before `exp`;
- `iat` not unreasonably in the future and `nbf`, when present, already reached, allowing at most 60 seconds of configured clock skew; and
- structurally valid UTF-8 JSON protected-header and claims objects with no duplicate member names.

Token-purpose rules and the dedicated resource audience must reject other JWT kinds as required by RFC 8725 [§3.11](https://www.rfc-editor.org/rfc/rfc8725.html#section-3.11) and [§3.12](https://www.rfc-editor.org/rfc/rfc8725.html#section-3.12). Changing deployment profiles does not relax the common signature, issuer, audience, algorithm, lifetime, or owner-binding checks. Issuer and subject matching remain case-sensitive; media-type comparison does not normalize identity claims.

JWKS responses are accepted only over HTTPS, with bounded response size and fetch time. A successful set follows its HTTP freshness, defaults to a five-minute cache lifetime when no freshness is supplied, and is always capped at 15 minutes. An unknown `kid` can trigger one rate-limited refresh. Refresh failure does not extend an expired cache and fails the request closed; a still-fresh cached key may continue to validate tokens. Key removal takes effect no later than cache expiry, so emergency revocation also requires the platform to block or shorten the affected sessions. Initial startup may be healthy at the process level while identity-key retrieval is unavailable, but protected requests fail closed and readiness reports the dependency unavailable unless a still-fresh usable key set is cached.

After validation, Curu reduces the token to an internal principal containing the exact `(issuer, subject)` external identity and the mapped internal principal ID. Email, display name, groups, roles, and other provider claims do not establish identity or enter domain behavior. Provider rebinding changes configuration or explicit application identity data, not ownership fields on activities and sessions.

## Single-owner authorization

The initial policy has one configured `(issuer, subject)` binding to one stable internal owner ID. A valid token proves an external identity; only an exact match to that binding authorizes the owner policy. Matching is case-sensitive and performs no email, alias, wildcard, domain, group, or role fallback.

Application use cases—not only HTTP routes—authorize actions. The owner may read, create, change, archive, restore, and, when those capabilities exist, manage recovery drafts for only the owner dataset. Unknown principals are denied by default, including guessed object URLs and HTMX endpoints. Background/operator commands use separate database/operator authority and do not manufacture a browser principal.

The only unauthenticated HTTP exception is a minimal liveness endpoint that reports process liveness without configuration, dependency, build, dataset, or user details. Readiness may be restricted to the platform network and reports only a coarse ready/not-ready result. All product pages and product-data endpoints require both authentication and owner authorization.

HTTP outcomes are stable:

| Condition | Result |
| --- | --- |
| Missing, malformed, untrusted, not-yet-valid, or expired token | `401 Unauthorized`, `WWW-Authenticate: Bearer`, `Cache-Control: no-store`, no token detail |
| Valid token for an identity other than the configured owner | `403 Forbidden`, `Cache-Control: no-store`, no object existence detail |
| Authenticated owner requesting an absent object | `404 Not Found` |
| Dependency prevents authentication from being evaluated safely | `503 Service Unavailable`, fail closed |

Every server-generated `401` carries the minimal `WWW-Authenticate: Bearer` challenge required by [RFC 9110 §15.5.2](https://www.rfc-editor.org/rfc/rfc9110.html#section-15.5.2). Curu does not add token error details to that challenge or the response body.

Security failures are logged only as a coarse reason code and request correlation ID. Issuer, subject, claims, keys, tokens, cookies, form content, and database credentials are excluded from routine logs and error pages.

## Browser, CSRF, and proxy contract

Gateway authentication is ambient to the browser even though the gateway injects an Authorization header upstream, so that header does not remove CSRF risk. Curu uses all of these controls:

- Every state-changing action uses a non-GET method. `GET`, `HEAD`, and `OPTIONS` do not mutate product or persistent server state; a safe response may issue the CSRF cookie described below.
- Curu issues a random `__Host-curu_csrf` cookie with `Secure`, `HttpOnly`, `SameSite=Lax`, and `Path=/`, with no `Domain`. Server-rendered forms carry the same value in a hidden field; HTMX sends it in `X-CSRF-Token`. Unsafe requests require an exact constant-time match. Curu does not routinely rotate a valid token underneath open editors. If the cookie is absent or replaced, an older form fails safely and retains its entered values while a fresh safe page establishes the new token.
- Unsafe requests require an `Origin` header exactly matching the one configured public HTTPS origin. Missing, opaque, malformed, or different origins are rejected. `Sec-Fetch-Site`, when present, must be `same-origin`; it is defense in depth rather than a substitute for the token or Origin check.
- CORS is disabled for product routes. Content types and input sizes are allowlisted and bounded by the receiving handler.

The public origin is explicit configuration and is not inferred from `Host`, `Forwarded`, or `X-Forwarded-*`. Forwarding headers affect scheme/client-address handling only when the immediate peer is in the configured trusted-proxy CIDRs; otherwise Curu ignores them. The gateway must overwrite, not append to, client-supplied forwarding headers. Deployment documentation must name the proxy hop count or CIDRs and the private-hop transport protection.

An authentication failure never masquerades as a saved form. Normal requests receive the status above; HTMX requests also receive a stable response signal that the shared browser layer can present as “session expired.” Curu keeps the current editor DOM and submitted values in place and offers a gateway sign-in action in a new navigation flow. It does not automatically redirect an unsafe request, replay a mutation after login, or claim the save succeeded. CURU-016 chooses the exact signal/header while preserving these semantics.

Logout and login URLs are optional configured HTTPS gateway URLs. Curu may render them but does not clear the gateway cookie, revoke provider tokens, or implement a logout endpoint. A deployment without configured URLs presents the authentication failure without inventing provider behavior.

## PostgreSQL and operational responsibility

The external data service and delivery platform provision PostgreSQL, storage, network controls, TLS policy, availability, monitoring, backup scheduling, retention, encryption facilities, and restore execution. They create/rotate credentials and provide an isolated restore target. Those responsibilities do not authorize the service to reinterpret, merge, seed, or reset Curu data.

Curu owns ordered forward migrations, schema and data meaning, bootstrap metadata, application/schema compatibility, explicit initialization, runtime transactions, and post-restore semantic checks. It publishes supported application/schema versions when migrations exist. No application code selects AWS or another provider, invokes a cloud backup API, or assumes a provider-specific identity mechanism.

At minimum, deployments use distinct PostgreSQL roles and secrets:

| Role | Required access | Forbidden access |
| --- | --- | --- |
| Migration owner | Connect to the selected database; own the Curu schema and migration objects; apply reviewed forward migrations | Provider administration, other databases/schemas, backup control |
| Runtime | Connect; use the Curu schema; read/write only the tables and sequences required by implemented use cases; read schema-version state | Create/alter/drop schema objects, grant roles, run migrations, access provider administration |
| Bootstrap operator | Invoke the explicit bootstrap interface using narrowly documented data privileges after migration | Schema changes, startup-side seeding, blanket replacement of a working dataset |
| Backup/restore service | Capture and restore the complete selected Curu database according to the service contract | Application login or ordinary use-case authority |

Migration, runtime, and bootstrap credentials are supplied independently and never logged. Ordinary server startup uses only the runtime role, checks schema compatibility, and fails without mutating schema or content. Migrations are explicit, ordered, transactional where PostgreSQL permits, and never run through GORM `AutoMigrate`. Initialization is a separate atomic, versioned operation and is not backup, restore, migration, startup, or ordinary UI editing.

Backups and restores cover the complete Curu schema and required database metadata as they exist at the chosen application version: implemented ownership identifiers, activities, sessions/templates when introduced, archive state, revisions, seed/bootstrap records, and recovery drafts when introduced. Recovery drafts are private owner content and receive the same backup, access, retention, and restore treatment as authoritative content unless their later design records an explicit shorter retention policy. JWT signing keys, the configured external owner binding, gateway sessions, application binaries, and deployment secrets are not application database content and must be restored or reconfigured by their owning platform.

A restore is first made into an isolated target. Before promotion, the operator:

1. identifies the backup's schema version and selects a compatible Curu build;
2. supplies fresh target-specific database and JWT/gateway configuration rather than restoring secrets;
3. verifies migration history, required constraints, bootstrap identity, configured owner binding, representative current/archived records, revision values, and row/count invariants supplied by implemented tasks;
4. runs read-only application compatibility and authorization checks, then any separately approved forward migrations; and
5. records the backup identity, target, application/schema versions, checks, and promotion decision outside the product dataset.

An incompatible, partial, or semantically invalid restore is not promoted. Curu never repairs it by reseeding or merging a seed over user edits. Point-in-time recovery objectives, retention periods, restore authorization, and final traffic cutover belong to the platform runbook; Curu supplies the compatibility and semantic verification commands once their owning implementation tasks exist.

## Review scenarios

These scenarios are normative review examples for later focused tests:

- A signed and unexpired access token satisfying the configured profile, issuer, dedicated Curu resource audience, and exact owner subject maps to the internal owner and may enter an authorized use case. A suitable existing access token forwarded by the gateway does not need a custom Curu type or reissuance.
- Under the standard profile, both access-token media-type forms and their case-insensitive equivalents are accepted only when the remaining checks pass. Missing or wrong `typ`, missing or malformed profile-required claims, and an ID token from the same trusted issuer are rejected. A rejected token is not retried against another profile.
- A missing token, a key absent from or ineligible under the current trusted JWKS, a disallowed algorithm, missing `exp`/`iat`, excessive lifetime, another issuer/audience, or expiry is `401`; the response includes `WWW-Authenticate: Bearer` and token claims never reach authorization.
- Before a provider-specific profile is enabled, fixtures must prove that its distinguishing rules accept the intended access token and reject same-issuer ID tokens and other wrong-purpose tokens, including any that share its generic type value. No generic-JWT fallback is available.
- A valid token for a different subject is authenticated but receives `403` for list pages, guessed object URLs, HTMX fragments, archive actions, and recovery drafts.
- A cross-origin form post carrying valid gateway authentication is rejected even if it guesses a CSRF value; a same-origin unsafe request still fails without the matching cookie/form token.
- When a token expires while an editor is open, the attempted save is not replayed or reported as successful and entered content remains visible for deliberate sign-in and retry.
- During a planned rotation overlap, both old and new eligible keys in a still-fresh trusted JWKS are accepted. An unknown key can be accepted after a bounded refresh makes it eligible; a key that remains unknown during a failed refresh never falls back to unchecked claims or an expired key cache.
- Server startup with the runtime role cannot create or alter tables. A migration uses its separate owner role, retains existing content, and advances explicit version state.
- Repeating completed initialization is a no-op; restoring a database retains bootstrap identity, edits, archive/revision state, owner mapping, and recovery content, while gateway secrets are supplied anew.

## Explicitly deferred implementation details

CURU-015 selects a maintained JWT/JWKS library, implements the standard access-token profile and its explicit configuration boundary, and turns the cache, timeout, skew, and failure rules into tested configuration. It does not implement a generic profile interpreter or speculative provider adapters. CURU-020 implements the internal principal and use-case policy. CURU-016 chooses the concrete HTMX expiration signal and CSRF-token issuance mechanics. CURU-019 and CURU-013 provide executable database privilege, migration, compatibility, and restore checks. These are implementation choices within this contract, not permission to weaken it.

No material product choice is silently deferred by this contract. Selecting an implemented profile or supplying issuer, resource audience, keys, and owner binding is deployment configuration within the documented rules. Introducing a provider-specific profile requires its distinguishing rules and negative fixtures to be reviewed explicitly. Changing injected Bearer transport, the owner-mapping model, common validation/lifetime requirements, the browser CSRF model, or database role split requires explicit product review and a contract revision before dependent implementation diverges.
