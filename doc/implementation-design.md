# Curu implementation design baseline

This is the initial architecture and delivery contract for the greenfield Curu application. It records intended boundaries, not implemented runtime behavior. [`product-baseline.md`](product-baseline.md) defines the product model, while [`../roadmap.md`](../roadmap.md) remains authoritative for task scope, dependencies, verification, and all CURU IDs.

## System shape

Curu will be one Go modular monolith and one externally operated PostgreSQL database. HTTP is built on `net/http` with a small router, server-rendered escaped templates, HTMX where partial-page interactions help, and minimal JavaScript. GORM is the persistence library, but it does not own schema evolution and runtime startup never invokes `AutoMigrate` or resets data.

The repository should keep dependencies pointing inward:

1. HTTP handlers translate authenticated requests and render responses.
2. Application use cases enforce authorization, validation, revision rules, and transaction boundaries.
3. Domain types express activities, sessions, blocks, placements, templates, and recovery data without JWT, GORM, gateway, or provider concepts.
4. Adapters implement PostgreSQL persistence, JWT verification, Markdown rendering, and external configuration.

Task-specific packages should be introduced only with the behavior that needs them. This baseline does not pre-create a speculative package tree or future schema.

## Data ownership and consistency

PostgreSQL is the authoritative application store. Curu supplies ordered, explicit, forward migrations and a compatibility check; a schema-owning migration credential is distinct from the restricted runtime credential. The external data service operates storage, backups, retention, and restore execution. Curu documents compatible application/schema versions and verifies application data after an isolated restore.

CURU-019 established the initial owner-scoped activity schema. CURU-017 advanced it to version 3 with optional classification, CURU-022 advanced it to version 4 with bounded owner-scoped activity recovery drafts, CURU-006 advanced it to version 5 with owner-scoped sessions and ordered named freeform blocks, CURU-007 advanced it to version 6 with plan-owned activity placements and copied progressions, CURU-023 advanced it to version 7 with plan-local placement notes and nullable integer-second duration storage, and CURU-008 advanced it to version 8 with optional session and block allocations. CURU-025 advances it to version 9 with bounded owner-scoped session recovery copies. Later template tasks extend that schema. GORM maps and queries the explicit schema; it does not infer or mutate production structure at application startup.

Activity creation and editing use transactions and compare-and-update revision checks at the activity aggregate boundary. Progression replacement participates in the same transaction; a stale write leaves authoritative data unchanged while the submitted browser form remains visible. The owner can explicitly persist that complete validated submission as a separate recovery draft before navigating away, reopen it, update the saved copy, or explicitly discard it. One active draft is retained per owner/source activity; saving another replaces that draft, no automatic expiry or merge applies, and no draft is ever applied to the authoritative activity. Archive and restore use the same owner-scoped revision boundary, advance revision atomically with archive state, and retain the activity and its progressions; archived records appear only in the protected archive view. Activity content copied into a placement is an owned snapshot, with source identity retained only as provenance.

Session creation and editing use a separate owner-scoped aggregate. Title, optional date, group/context, intent, optional allocation, and the complete ordered block set save in one transaction. Each block has a stable identity, name, intent, notes, observation lens, optional allocation, and zero or more ordered activity snapshots with optional plan-local durations. Retained blocks are reconciled in place after the session revision compare succeeds, preserving their placements; explicitly removed blocks cascade their snapshots. Revision or lifecycle changes, including a concurrent archive, return a conflict and retain the complete rejected submission with a link to current or archived truth; genuinely absent and other-owner identities remain not found. The rendered editor retains its copied-content snapshot outside ordinary mutation serialization and transfers it into every returned editor that retains submitted work, including validation and conflict responses; only the conflict editor names it for recovery submission. This keeps large valid plans outside the ordinary 1 MiB request boundary without losing recovery through a validation round trip. The owner can deliberately save one decoded-payload-capped, versioned recovery copy per source session, including submitted fields/order/local properties, copied snapshots and attempted structural action, before reloading current truth; oversize or failed recovery saves leave that editor available and the copy never merges or overwrites the session. Session archive/restore advances the same revision without deleting blocks or placements. No stage enum or recursive grouping is introduced.

Adding a placement locks and copies one coherent current owner-scoped activity plus its ordered progressions inside the same transaction that advances the session aggregate revision. Snapshot rows retain source UUID, revision, and optional seed identity as provenance but do not retain a source foreign key. Session reads and rendering therefore need no current library lookup, and later source edits, archive, or removal cannot affect the plan. The session editor lists the current owner library without leaving the open aggregate and applies add, replacement, cross-block move, removal, or adjacent block/placement reordering together with the submitted session/block fields, allocations, placement notes, and plan-local durations in one revision-checked transaction. Replacement rewrites only source-derived snapshot fields and copied progressions, retaining plan-local notes and duration with an explicit review warning; cross-block moves retain the whole placement while block data stays with its block; adjacent moves swap deterministic positions only inside the owning session and current block; removal never mutates the library. Boundary actions reject and roll back without consuming the revision, while a successful action consumes it so a duplicate submission is stale. Timing counts each block once, preferring its allocation and otherwise deriving a complete or partial placement total; unknown or inconsistent time produces non-blocking accessible warnings.

CURU-003 delivered the bounded `curu-coaching-library` version `1` asset described in [`seed-library.md`](seed-library.md). It maps the supplied 44 activities and 110 progressions to schema version 2 with retained source identities, deterministic UUIDs, integer-second minimum durations, and labeled Markdown that preserves range and metadata detail. It is committed application data, not an importer or an initialization side effect.

Initialization remains a separate, one-shot operator action. It applies the deterministic seed and its identity/version atomically, makes completed retries no-ops, rolls back partial failures, handles concurrent calls without duplication, and refuses unrecognized non-empty data. A recorded intentionally empty installation is valid. Ordinary startup, migrations, seed initialization, user saves, recovery drafts, backup/restore, and disposable reset are separate operations.

## Trust and request boundary

The deployment gateway terminates the public browser and owns third-party authentication and session lifecycle. Curu verifies a configured signed JWT intended for Curu, maps issuer/subject identity to an internal principal, and then authorizes application use cases under a deny-by-default single-owner policy. Provider claims do not enter domain behavior and authentication alone does not grant dataset access.

[`integration-contract.md`](integration-contract.md) defines the CURU-014 contract for token delivery, deployment-selected access-token profiles, trusted signing material and rotation, issuer/resource audience, allowed algorithms, lifetime checks, stable owner binding, error behavior, CSRF/origin protections, trusted proxy handling, token expiry, logout, and data-service responsibilities. Prefer the standard OAuth JWT access-token profile; a suitable existing token can be forwarded without a bespoke Curu type or gateway reissuance. Provider-specific validation requires explicit token-purpose rules and negative fixtures, not a generic signed-JWT fallback. Dependent implementation tasks implement the contract without moving provider claims into the domain or building a profile framework. No Curu password database, token issuer, public signup, arbitrary identity headers, token-controlled role system, or provider-specific cloud coupling is implied.

All data-backed pages and mutations use shared authentication, authorization, request, and rendering protections. GET remains read-only. Templates are escaped, Markdown is safely rendered, inputs are bounded and validated, queries are parameterized, and logs/errors exclude secrets. Authentication expiration must not be presented as a successful save or silently destroy open editor content.

## Runtime and operator interfaces

The following stable interface classes are required. Their exact executable commands become authoritative only when their owning implementation task supplies working code; contributor and operator documentation must then state commands verbatim and automated checks must exercise them. This baseline intentionally does not advertise commands against an absent application.

| Interface | Required contract | Owning task | Canonical documentation when implemented |
| --- | --- | --- | --- |
| Build | One reproducible repository command produces the Curu server binary; the container build creates a minimal non-root runtime image. | CURU-002 | Root `README.md` contributor quickstart and container metadata |
| Test | One mandatory repository command runs the default local suite; focused integration commands identify disposable PostgreSQL and browser prerequisites without paid or cloud services. | CURU-002, extended by later tasks | Root `README.md`, plus test-adjacent documentation when specialized setup is needed |
| Start | One foreground server command validates configuration, starts safely, reports coarse readiness, and shuts down gracefully; absent security configuration cannot expose private placeholder data. | CURU-002, CURU-015, constrained by CURU-014 | Root `README.md` and `doc/configuration.md` |
| Migrate | A separate explicit command applies ordered forward migrations using schema-owner credentials; ordinary server startup only checks compatibility. | CURU-019 | `doc/database-operations.md` |
| Bootstrap | A separate explicit command initializes either the versioned seed or an intentionally empty dataset and reports no-op/refusal outcomes; it is never server startup. | CURU-004 | `doc/database-operations.md` |
| Recovery validation | A read-only command captures application-owned evidence for a fixed backup state and compares it with an isolated restore; the data service still executes and retains backups. | CURU-013 | `doc/database-operations.md` |

Disposable reset is a separate, clearly named test/operator action restricted to an explicitly selected isolated target. Backup and restore are external-service operations paired with Curu compatibility and exact application-data manifest comparison. None of migrate, bootstrap, recovery validation, reset, backup, or restore is an incidental web request.

## Configuration and deployment boundary

Configuration supplies listen/shutdown settings, PostgreSQL connections, migration/runtime role separation, trusted JWT material, expected issuer/audience, owner binding, and gateway/proxy assumptions as those tasks are delivered. Secrets arrive through deployment facilities and are neither committed nor logged. Configuration validation fails closed for missing or contradictory security settings.

The application exposes only the minimal health behavior allowed by the accepted security contract. The platform owns TLS, routing, container scheduling, secrets delivery, identity-provider integration, database provisioning, monitoring collection, backup scheduling, and deployment promotion. Curu remains portable across those services and contains no AWS provisioning SDK merely because AWS may host the first deployment.

## Verification strategy

Pure domain rules use fast local tests. HTTP behavior uses handler/browser tests with local token fixtures. Database boundaries use disposable PostgreSQL fixtures for migrations, role restrictions, transactions, bootstrap concurrency, snapshot isolation, and restore checks. Security fixtures cover positive and negative JWT and authorization cases without contacting a real identity provider. Phone-width checks are automated where useful, while an actual on-field/device result is recorded only when an operator performs it.

Every delivered task adds focused verification and keeps the documented build/test interfaces executable. No cloud deployment, paid provider call, real retained-data reset, fabricated field trial, or old-prototype compatibility check is part of routine repository validation.

## Deferred reviewed decisions

- CURU-014's detailed JWT, authorization, browser-security, and data-service contract is recorded in [`integration-contract.md`](integration-contract.md). Later implementation tasks own the explicitly identified mechanics.
- CURU-026 classifies implemented session, block, and placement fields for template conversion. Date is already excluded, but group/context and note handling are not guessed in advance.
- Later tasks may refine package and schema details within this architecture, but must not broaden into microservices, a SPA, recursive planning structures, multi-tenancy, automatic conflict merging, or the roadmap's other explicit non-goals.
