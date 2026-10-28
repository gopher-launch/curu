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
- Deferred capability: JWT authentication, application authorization, browser protections, and every activity/session/template behavior remain assigned to their roadmap tasks.

## PostgreSQL migration and compatibility boundary

- Delivery: CURU-019
- Explicit schema: embedded, checksummed forward migrations create schema version 2 with owner-scoped activities, ordered progressions, dataset-initialization metadata, archive state, and revision tokens. No session/template/recovery schema or seed content is introduced.
- Separate authority: `cmd/curu-migrate` accepts only the migration-owner connection and an existing runtime role name. It grants that role non-destructive activity/progression access and read-only migration/initialization metadata. Migration fails closed if the role has any PostgreSQL role membership, elevated attributes, or effective temporary, database-creation, or schema-creation privileges, including privileges inherited through `PUBLIC`; database-wide revocation remains an explicit platform prerequisite.
- Safe startup: the server uses only `CURU_DATABASE_URL` and performs a read-only exact-version/history checksum compatibility check. Startup never calls `AutoMigrate`, applies migrations, initializes content, or resets data.
- Operator interface: [`doc/database-operations.md`](doc/database-operations.md) documents provider-independent connections, roles, migration/start commands, supported schema version, and the destructive scope of the opt-in disposable PostgreSQL verification.
- Verification: focused tests cover embedded migration continuity/checksums and, when `CURU_TEST_DATABASE_URL` selects a disposable PostgreSQL target, fail-closed effective-privilege validation, fresh and forward migration, retained fixture content, repeat application, read-only startup compatibility, permitted runtime data writes, and denied persistent/temporary DDL, migration/initialization mutation, and permanent activity deletion.
- Initialization boundary: CURU-004 uses this schema through a separate operator command without changing migration history or runtime grants. Product library behavior and owner authorization remain assigned to later tasks.

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
- Safe repeat behavior: only an exact completion record produces an `already_initialized` no-op. A different recorded choice or unrecorded existing activity is refused. Retries and ordinary server startup never synchronize or replace edited content.
- Operator safety: [`doc/database-operations.md`](doc/database-operations.md) documents narrow initialization privileges, exact commands, disposable PostgreSQL verification, and a separate reset procedure guarded by the exact isolated database name. No web route or infrastructure lifecycle behavior is added.
- Verification: local tests cover completion matching; opt-in disposable PostgreSQL checks cover fresh seed and empty initialization, retries, concurrent invocation, induced partial failure rollback, non-empty refusal, exact 44/110 counts, edit preservation, and read-only startup compatibility.
- Deferred capability: JWT authentication, application authorization, product library routes, backup/restore evidence, and later activity/session behavior remain assigned to their roadmap tasks.
