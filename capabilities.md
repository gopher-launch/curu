# Delivered capabilities

## Reproducible local kind and trust inputs

- Delivery: CDP-002
- Host/cluster boundary: the versioned single-node kind template and bounded scripts create fixed cluster `curu-local`, explicit context `kind-curu-local`, and namespace `curu-system` from required host storage, declared PV capacity, CPU, and memory inputs. The pinned Kubernetes node maps only future edge NodePort `30443` to a configurable host-loopback HTTPS port; kind administration remains loopback-only and backend ports are not published.
- Image and persistence: the image loader builds the current repository revision into a non-floating local Curu image and loads it into the named node. Bootstrap claims only a new or empty dedicated real directory and refuses to mark a non-empty unowned directory; a valid marked directory remains reusable with retained data. That host directory backs stable retained PV `curu-local-data`; ordinary cluster deletion retains host data, while a separate confirmed reset refuses a running cluster or unmarked directory. This is laptop persistence, not backup or host-failure recovery.
- Local HTTPS inputs: stable `curu.test` and `identity.curu.test` names resolve on the host through an explicit managed hosts change and in-cluster through the future `curu-edge` Service. PKI preparation creates/reuses a host-held local CA, verifies both certificate names, uploads only the leaf TLS key/certificate and public CA, and provides explicit Ubuntu trust install/check/removal steps without disabling verification.
- Verification and qualification: static checks reject invalid resource/storage inputs, prove a non-empty unowned directory cannot be marked while an owned retained directory can be reused, and assert loopback exposure/retention. The disposable live check exercises two cluster creations, source image build/load and reload, expected Docker port/mount configuration, marker retention across pod and cluster recreation, and CA-verified HTTPS from both the host and a pod. NetworkPolicy enforcement remains unverified; the laptop administrator, Docker/kind node, and pod network are the trusted local boundary. No database, issuer, gateway login, library access, LAN exposure, or target-laptop acceptance is claimed.

## Owned deployment chart foundation

- Delivery: CDP-001
- Chart boundary: [`deploy/helm/curu/`](deploy/helm/curu/) is a dependency-free, repository-owned Helm chart foundation. It renders one Curu Deployment and one internal `ClusterIP` Service; Postgres, identity, edge routing, operator Jobs, kind bootstrap, and end-to-end installation remain assigned to later deployment tasks.
- Explicit configuration: the bounded values schema requires a pinned Curu image input, public origin, RFC 9068 trust, exact owner binding, resource settings, one database mode, and one identity mode. The selected restricted runtime database URL comes only from an existing Secret; migration, initialization, database-administration, and identity-administration credentials are not accepted by the runtime Deployment.
- Restricted runtime: the pod uses the image's numeric non-root identity, a read-only root filesystem, dropped capabilities, the runtime-default seccomp profile, and no service-account token mount. Curu remains responsible for rejecting invalid runtime configuration and for schema/JWKS readiness; the chart does not introduce an authentication bypass or startup data operation.
- Verification: the pinned Helm validation interface strictly lints and renders bundled/external fixtures, confirms the absence of chart dependencies and credentials in rendered examples, asserts the internal/restricted workload boundary, preserves safe custom pod labels, and rejects floating image, contradictory mode, missing Secret-reference, mismatched owner-issuer, and chart-owned selector-label override inputs. This is render/configuration evidence only; it does not establish a live database, issuer, cluster, browser login, laptop result, or network-policy enforcement.

## Explicit deployment data lifecycle

- Delivery: CDP-004
- Ordered first installation: chart-owned, bounded post-install/upgrade Jobs sequence bundled database readiness/preparation, `/curu-migrate`, migration-owner grants for the narrow initializer surface, and explicit `/curu-initialize --mode=seed|empty`. Both application Jobs use the selected Curu image and exact delivered environment interfaces; hook failure stops later operations.
- Deliberate operation boundary: migration defaults off and initialization defaults to `none`. Selecting either requires runtime to be disabled; an operation-free upgrade is the only phase that launches the server. Seed mode reuses the runtime's stable internal owner UUID. Migration and initialization can be invoked independently, completed Jobs remain diagnosable, and no workflow treats Helm rollback as database rollback.
- Credential scope: migration receives only its owner URL and the declared restricted runtime role; the grant Job uses that same schema-owner connection; initialization receives only its narrow URL and the seed owner when applicable; ordinary runtime receives only the runtime URL. External mode requires distinct initializer/runtime role names and accepts existing scoped Secrets without provisioning external resources.
- Verification: strict Helm checks cover ordering, deadlines, explicit-mode rejection, operation/runtime exclusion, credential references, and bundled/external rendering. The disposable kind exercise proves fresh schema-version-2 migration and 44-activity/110-progression initialization, exact retry as `already_initialized`, conflicting-choice refusal without replacement, missing/checksum-incompatible schema rejection, narrow initializer privileges, bounded failure propagation, and subsequent runtime launch. Identity, browser login, target-laptop acceptance, and network-policy enforcement remain unclaimed.

## Repeatable Keycloak identity and compatible access tokens

- Delivery: CDP-005
- Owned identity boundary: bundled mode deploys one pinned, production-mode Keycloak replica on an internal Service, backed by its separately scoped PostgreSQL database. Explicit hostname/proxy settings prepare HTTPS termination at the future edge; probes, restricted pod settings, retained generated credentials, and a constrained internal administration endpoint avoid adding a host-facing admin route. External mode deploys and reconciles no provider resources and accepts explicit issuer, JWKS, CA, and browser-client inputs.
- Stable reconciliation: a bounded Helm Job creates or reconciles the declared realm, confidential browser client, named mappers, and two controlled development users without importing committed credentials. Repeated upgrades retain Secret values, Keycloak user IDs/passwords, signing state, and client fields outside the named managed surface. A completed bootstrap refuses to recreate a missing declared user, preserving the exact owner-subject/internal-owner binding instead of silently rebinding data.
- Token contract: the browser client requires authorization code with S256 PKCE and issues RS256 RFC 9068 `at+jwt` access tokens with the dedicated Curu audience, `client_id`, `jti`, stable subject, and at most a 15-minute lifetime. Curu mounts explicit issuer CA trust; no validator relaxation, TLS skip, ID-token substitution, or ordinary password-grant login path is introduced.
- Verification: strict Helm checks cover bundled/external identity inputs, image pinning, private Services, production start mode, trust/owner consistency, Secret references, token lifetime, and reconciliation ordering. The disposable kind check uses temporary trusted HTTPS, issues real tokens, proves discovery/JWKS trust and claim shape, exercises Curu owner acceptance plus other-user `403`, and rejects ID, wrong-audience, and expired tokens. It also verifies Secret, user, signing-key, and unmanaged-client-field persistence across upgrade. CDP-006 still owns the actual browser authorization-code flow, gateway sessions, edge exposure, refresh/logout behavior, and browser acceptance.

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
- Safe surface: the default listener is loopback-only. CURU-002 introduced no product-data route; unknown safe routes use the shared escaped HTML layout and unsafe or unimplemented routes remain unavailable.
- Contributor interface: [`README.md`](README.md) defines the executable `go build ./cmd/curu`, `go test ./...`, and `go run ./cmd/curu` commands. [`doc/configuration.md`](doc/configuration.md) defines the implemented runtime settings.
- Container interface: [`Dockerfile`](Dockerfile) builds the server and separate migration, initialization, and recovery-validation executables into a minimal image with TLS root certificates; all run as numeric non-root user `65532:65532`, and non-loopback binding must be selected explicitly by the deployment.
- Subsequent delivery: CURU-015, CURU-020, and CURU-016 added authentication, single-owner authorization, and browser protections around this runtime shell. CURU-005 adds read-only activity library pages; mutation, session, and template behavior remains assigned to later roadmap tasks.

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
- Subsequent delivery: CURU-020 adds application authorization without changing initialization. CURU-013 adds backup/restore evidence and CURU-005 adds read-only library routes; later activity mutation and session behavior remain assigned to their roadmap tasks.

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
- Safe exception: liveness and readiness retain their previously delivered coarse platform-probe behavior and expose no configuration, dependency detail, dataset, or identity data. CURU-005 subsequently adds product read routes behind this boundary.
- Subsequent delivery: CURU-016 adds CSRF, origin, trusted-proxy, HTMX session-expiry, and browser editor-preservation mechanics. Later product use cases must invoke the application policy at their own boundaries; HTTP enforcement is not a substitute for owner-scoped data checks.

## Protected browser and HTMX boundary

- Delivery: CURU-016
- Browser mutations: every authorized unsafe request requires the exact configured public HTTPS origin, an exact constant-time CSRF cookie/token match, and same-origin fetch metadata when supplied. The secure host-only HTTP-only same-site CSRF cookie is stable across open editors; authorized product request bodies, including safe-method bodies, are capped at 1 MiB and receiving handlers retain responsibility for narrower content-type and field validation.
- Safe presentation: server templates remain contextually escaped. The bounded Markdown renderer omits raw HTML and unsafe link destinations before its output becomes trusted template HTML. CURU-016 introduced no product-data route or database query; CURU-005 subsequently applies these protections to owner-scoped library reads.
- HTMX expiry: every authentication `401` retains the minimal Bearer challenge. HTMX responses also suppress swaps and emit the stable `curu:authentication-required` event; the shared layout reveals a provider-neutral session-expired notice and optional gateway sign-in action without redirecting, replaying, or discarding the editor DOM.
- Gateway boundary: runtime configuration requires an explicit public origin, optionally trusts only named immediate-proxy CIDRs for a single overwritten client address, and optionally renders HTTPS login/logout links. [`doc/gateway.md`](doc/gateway.md) assigns public TLS, session cookies, conflicting-header stripping, access-token injection, direct-access prevention, and private-hop protection to the deployment.
- Verification: focused local handler tests cover normal and HTMX authentication failures, challenge/signal behavior, CSRF/origin/fetch-site rejection, secure cookie attributes, bounded and safely re-rendered invalid form input, escaped paths and Markdown, and trusted versus untrusted forwarding peers. No provider gateway or browser automation is claimed.

## Application data recovery evidence

- Delivery: CURU-013
- Recovery manifest: `cmd/curu-recovery-check` performs the application read-only compatibility check and emits counts plus a SHA-256 digest over every delivered schema-version-2 application field. It can compare an isolated restored database with a manifest captured from the fixed source state without exposing activity content, owner identifiers, or credentials.
- Included data: recovery evidence covers migration history, seeded or intentionally empty initialization metadata, owner-scoped activities including edited content/revisions/archive state/timestamps, and ordered progressions. Later schema tasks must extend the manifest and restore fixture for their own tables.
- External boundary: [`doc/database-operations.md`](doc/database-operations.md) assigns scheduling, retention, protected backup storage, target provisioning, and restore execution to the data service. Database credentials, gateway sessions, JWT/JWKS trust, and deployment secrets are supplied anew; the stable internal owner binding must remain consistent with restored content.
- Verification: the opt-in disposable PostgreSQL exercise creates distinct source and restore databases, captures an edited and archived 44-activity/110-progression seeded fixture, uses `pg_dump`/`pg_restore`, reconciles restricted runtime grants, and compares the restored manifest through the runtime role. It never targets retained data or provisions cloud infrastructure.

## Protected activity library discovery

- Delivery: CURU-005
- Read-only library: authenticated owners can browse current, non-archived activities at `/activities` and open stable UUID detail links. The root route leads to the library; no create, edit, archive, advanced-filter, session, or template behavior is implied.
- Basic discovery: optional `q` search is trimmed, bounded, case-insensitive, and treats `%`, `_`, and backslash literally. It matches titles, summaries, activity Markdown, and progression titles/content without requiring classification or interpreting seed grouping as a session stage.
- Complete details: detail pages include summary and recommended duration when present, the full safely rendered activity Markdown, and ordered progressions with their titles, content, and optional durations. Empty-library and zero-match states are distinct.
- Boundaries: the application use case rechecks read authorization, and PostgreSQL queries scope current rows to the exact internal owner. Missing, archived, malformed, guessed, and other-owner IDs do not disclose an activity. Shared JWT, owner, CSRF/browser, no-store, escaped-template, and safe-Markdown protections remain in force.
- Verification: focused use-case and handler tests cover stable IDs, known content, search input, empty states, complete safe detail rendering, and denied access. The disposable PostgreSQL check covers title/text/progression search, literal wildcard handling, current-row loading, ordered progressions, owner isolation, and runtime-role restrictions.
