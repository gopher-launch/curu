# Delivered capabilities

## Documentation baseline

- Delivery: CURU-001
- Product contract: [`doc/product-baseline.md`](doc/product-baseline.md) records the accepted greenfield product model, editing behavior, on-field principles, and platform responsibility boundary.
- Architecture contract: [`doc/implementation-design.md`](doc/implementation-design.md) records the intended Go/GORM/PostgreSQL and server-rendered architecture, data/trust boundaries, verification approach, and future build, test, start, migrate, and bootstrap interface classes.
- Runtime capability: none. CURU-001 adds documentation only; executable commands and application behavior remain assigned to their dependent tasks.

## Integration and data-service contract

- Delivery: CURU-014
- Integration contract: [`doc/integration-contract.md`](doc/integration-contract.md) defines gateway/JWT transport, deployment-selected access-token profiles, validation defaults, stable single-owner mapping, deny-by-default application authorization, browser/CSRF/proxy behavior, and safe authentication-expiry outcomes. It prefers the standard OAuth JWT access-token profile, permits suitable existing tokens without custom Curu typing or gateway reissuance, and requires explicit wrong-kind rejection rules before provider-specific profiles are supported.
- Data-service contract: the same document assigns PostgreSQL provisioning, backup, and restore execution to the platform while assigning migrations, initialization, compatibility, restricted runtime access, and post-restore semantic verification to Curu.
- Runtime capability: none. CURU-014 is a design delivery; authentication, authorization, browser protections, migrations, and recovery checks remain assigned to the implementation tasks identified in the roadmap.

## Minimal application runtime

- Delivery: CURU-002
- Runtime shell: the Go module and `cmd/curu` provide a validated, foreground HTTP server with structured logging, graceful `SIGINT`/`SIGTERM` shutdown, and coarse liveness/readiness endpoints.
- Safe surface: the default listener is loopback-only. No product-data route exists; unknown safe routes use the shared escaped HTML layout and unsafe or unimplemented routes remain unavailable.
- Contributor interface: [`README.md`](README.md) defines the executable `go build ./cmd/curu`, `go test ./...`, and `go run ./cmd/curu` commands. [`doc/configuration.md`](doc/configuration.md) defines the implemented runtime settings.
- Container interface: [`Dockerfile`](Dockerfile) builds the server and separate migration and initialization executables into a minimal image with TLS root certificates; all run as numeric non-root user `65532:65532`, and non-loopback binding must be selected explicitly by the deployment.
- Subsequent delivery: CURU-015 and CURU-020 add authentication and single-owner authorization around this runtime shell. Browser protections and every activity/session/template behavior remain assigned to their roadmap tasks.

## PostgreSQL migration and compatibility boundary

- Delivery: CURU-019
- Explicit schema: embedded, checksummed forward migrations create schema version 2 with owner-scoped activities, ordered progressions, dataset-initialization metadata, archive state, and revision tokens. No session/template/recovery schema or seed content is introduced.
- Separate authority: `cmd/curu-migrate` accepts only the migration-owner connection and an existing runtime role name. It grants that role non-destructive activity/progression access and read-only migration/initialization metadata. Migration fails closed if the role has any PostgreSQL role membership, elevated attributes, or effective temporary, database-creation, or schema-creation privileges, including privileges inherited through `PUBLIC`; database-wide revocation remains an explicit platform prerequisite.
- Safe startup: the server uses only `CURU_DATABASE_URL` and performs a read-only exact-version/history checksum compatibility check. Startup never calls `AutoMigrate`, applies migrations, initializes content, or resets data.
- Operator interface: [`doc/database-operations.md`](doc/database-operations.md) documents provider-independent connections, roles, migration/start commands, supported schema version, and the destructive scope of the opt-in disposable PostgreSQL verification.
- Verification: focused tests cover embedded migration continuity/checksums and, when `CURU_TEST_DATABASE_URL` selects a disposable PostgreSQL target, fail-closed effective-privilege validation, fresh and forward migration, retained fixture content, repeat application, read-only startup compatibility, permitted runtime data writes, and denied persistent/temporary DDL, migration/initialization mutation, and permanent activity deletion.
- Initialization boundary: CURU-004 uses this schema through a separate operator command without changing migration history or runtime grants. CURU-020 uses the same stable owner UUID in its runtime binding; product library behavior remains assigned to later tasks.

## Bounded coaching-library seed

- Delivery: CURU-003
- Committed asset: `internal/seed/library.json` identifies `curu-coaching-library` version `1`, records the SHA-256 provenance of the three supplied references, and contains all 44 activity and 110 progression inputs. `internal/seed.Load` decodes and validates the embedded asset without database side effects.
- Schema mapping: reference activity slugs remain source identities; deterministic UUIDs satisfy schema version 2; lower recommended durations are integer seconds while complete ranges, themes, phases, tags, coaching fields, and extra sections remain labeled Markdown; progression order is zero-based for the target schema.
- Verification: local focused tests validate deterministic decoding, counts, identity uniqueness, ordering, seconds-based duration examples, and representative extra sections. The opt-in disposable PostgreSQL test inserts the complete asset against the migrated schema and verifies its counts.
- Operator boundary: [`doc/seed-library.md`](doc/seed-library.md) records provenance and mapping decisions. CURU-003 adds no bootstrap command, blanket deletion, startup seeding, or synchronization over a working library; CURU-004 owns safe explicit dataset initialization.

## Safe explicit dataset initialization

- Delivery: CURU-004
- Operator interface: `cmd/curu-initialize` requires an explicit `--mode=seed` or `--mode=empty` and a separately supplied initialization connection. Seed mode also requires the deployment's stable internal owner UUID. The container includes the same `/curu-initialize` binary.
- Atomic behavior: a transaction-scoped PostgreSQL advisory lock serializes initializers. Seed content and its `curu-coaching-library` version `1` record commit together; failures roll back all rows. Empty mode records an intentional empty installation.
- Safe repeat behavior: only an exact completion produces an `already_initialized` no-op. Seed retries must match the recorded seed identity/version and the owner established by all deterministic seeded activity rows. A different owner, a different recorded choice, missing seeded ownership evidence, or unrecorded existing activity is refused. Retries and ordinary server startup never synchronize or replace edited content.
- Operator safety: [`doc/database-operations.md`](doc/database-operations.md) documents narrow initialization privileges, exact commands, disposable PostgreSQL verification, and a separate reset procedure guarded by the exact isolated database name. No web route or infrastructure lifecycle behavior is added.
- Verification: local tests cover completion-record matching; opt-in disposable PostgreSQL checks cover fresh seed and empty initialization, same-owner no-op and different-owner refusal, concurrent invocation, induced partial failure rollback, non-empty refusal, exact 44/110 counts, edit preservation, and read-only startup compatibility.
- Subsequent delivery: CURU-020 adds application authorization without changing initialization. Product library routes, backup/restore evidence, and later activity/session behavior remain assigned to their roadmap tasks.

## Trusted JWT authentication

- Delivery: CURU-015
- Explicit profile: runtime configuration selects only the standard RFC 9068 OAuth JWT access-token profile. The adapter accepts the two access-token media types case-insensitively and validates their required claims without a generic-JWT or provider-specific fallback.
- Trust and validation: compact JWTs require canonical unpadded base64url protected-header, payload, and signature segments, valid UTF-8 JSON objects with well-formed Unicode strings and duplicate-free member names, understood critical headers, one non-empty key ID, an allowed `RS256` or `ES256` algorithm, an eligible unique JWKS key, a valid signature, exact issuer, the configured Curu resource audience, non-empty subject, numeric temporal claims, and bounded lifetime/skew. Unsigned, unencoded-payload, wrong-kind, wrong-purpose, malformed, expired, substituted-key, and untrusted tokens fail closed.
- Principal boundary: successful validation yields only the exact issuer/subject external identity. Tokens, provider claims, emails, display names, roles, and groups do not enter application or domain behavior.
- JWKS behavior: Curu fetches only the configured HTTPS endpoint with bounded time and response size, applies HTTP freshness precedence and overflow-safe current-age calculation with a five-minute default and 15-minute cap, supports planned overlap, rate-limits unknown-key refresh, and never extends an expired cache after failure. Ambiguous or invalid explicit freshness is treated as stale, while numeric age overflow saturates rather than becoming zero. A fresh cached known key remains usable during a refresh failure; otherwise the key dependency is unavailable.
- Runtime integration: authentication trust configuration is required before startup. Liveness remains process-only, while readiness requires a non-expired usable key set. Local generated signing/JWKS fixtures cover both media types, RSA and EC validation, required/wrong claims, invalid UTF-8 and malformed Unicode escapes in protected headers and claims, valid escaped Unicode pairs, unencoded payload, critical-header and noncanonical-segment rejection, trust failures, rotation, HTTP freshness/current age including numeric overflow, cache failure, refresh limiting, duplicate credentials, and unsupported-profile rejection.
- Subsequent delivery: CURU-016 adds browser/HTMX authentication outcomes, CSRF, origin, and proxy protections. No product routes, login, issuer, exchange, production signer, or provider adapter are added by CURU-015.

## Single-owner authorization

- Delivery: CURU-020
- Owner binding: runtime configuration binds one exact, case-sensitive issuer/subject identity to one canonical internal owner UUID. The owner issuer must match the configured JWT trust issuer; aliases, email, domains, wildcards, groups, and roles provide no fallback authority.
- Application policy: the inward `internal/authorization` policy maps only that external identity and provides explicit read, mutate, archive, and recovery authorization checks against an exact resource owner. Unknown identities, actions, malformed owner identifiers, and other-owner resources are denied by default.
- HTTP enforcement: every non-health route authenticates through the shared JWT adapter and then requires the owner mapping before dispatch. Missing/invalid credentials, valid unknown identities, and unavailable key evaluation produce detail-free `401`, `403`, and `503` responses respectively; each disables caching and every `401` carries the minimal `Bearer` challenge. Security denials emit one coarse reason plus a server-generated request correlation ID; the general access logger does not add method, path, status, or duration to that event.
- Safe exception: liveness and readiness retain their previously delivered coarse platform-probe behavior and expose no configuration, dependency detail, dataset, or identity data. There are still no product routes.
- Subsequent delivery: CURU-016 adds CSRF, origin, trusted-proxy, HTMX session-expiry, and browser editor-preservation mechanics. Later product use cases must invoke the application policy at their own boundaries; HTTP enforcement is not a substitute for owner-scoped data checks.

## Protected browser and HTMX boundary

- Delivery: CURU-016
- Browser mutations: every authorized unsafe request requires the exact configured public HTTPS origin, an exact constant-time CSRF cookie/token match, and same-origin fetch metadata when supplied. The secure host-only HTTP-only same-site CSRF cookie is stable across open editors; shared request bodies are capped at 1 MiB and receiving handlers retain responsibility for narrower content-type and field validation.
- Safe presentation: server templates remain contextually escaped. The bounded Markdown renderer omits raw HTML and unsafe link destinations before its output becomes trusted template HTML. No product-data route or database query is introduced, so product handlers still own field validation and parameterized owner-scoped persistence when delivered.
- HTMX expiry: every authentication `401` retains the minimal Bearer challenge. HTMX responses also suppress swaps and emit the stable `curu:authentication-required` event; the shared layout reveals a provider-neutral session-expired notice and optional gateway sign-in action without redirecting, replaying, or discarding the editor DOM.
- Gateway boundary: runtime configuration requires an explicit public origin, optionally trusts only named immediate-proxy CIDRs for a single overwritten client address, and optionally renders HTTPS login/logout links. [`doc/gateway.md`](doc/gateway.md) assigns public TLS, session cookies, conflicting-header stripping, access-token injection, direct-access prevention, and private-hop protection to the deployment.
- Verification: focused local handler tests cover normal and HTMX authentication failures, challenge/signal behavior, CSRF/origin/fetch-site rejection, secure cookie attributes, bounded and safely re-rendered invalid form input, escaped paths and Markdown, and trusted versus untrusted forwarding peers. No provider gateway or browser automation is claimed.
