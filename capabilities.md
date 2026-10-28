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
- Container interface: [`Dockerfile`](Dockerfile) builds the server and separate migration executable into a minimal image with TLS root certificates; both run as numeric non-root user `65532:65532`, and non-loopback binding must be selected explicitly by the deployment.
- Deferred capability: JWT authentication, application authorization, browser protections, initialization, and every activity/session/template behavior remain assigned to their roadmap tasks.

## PostgreSQL migration and compatibility boundary

- Delivery: CURU-019
- Explicit schema: embedded, checksummed forward migrations create schema version 2 with owner-scoped activities, ordered progressions, dataset-initialization metadata, archive state, and revision tokens. No session/template/recovery schema or seed content is introduced.
- Separate authority: `cmd/curu-migrate` accepts only the migration-owner connection and an existing runtime role name. It grants that role non-destructive activity/progression access and read-only migration/initialization metadata while denying schema creation, migration-history mutation, initialization-state mutation, and permanent activity deletion.
- Safe startup: the server uses only `CURU_DATABASE_URL` and performs a read-only exact-version/history checksum compatibility check. Startup never calls `AutoMigrate`, applies migrations, initializes content, or resets data.
- Operator interface: [`doc/database-operations.md`](doc/database-operations.md) documents provider-independent connections, roles, migration/start commands, supported schema version, and the destructive scope of the opt-in disposable PostgreSQL verification.
- Verification: focused tests cover embedded migration continuity/checksums and, when `CURU_TEST_DATABASE_URL` selects a disposable PostgreSQL target, fresh and forward migration, retained fixture content, repeat application, read-only startup compatibility, permitted runtime data writes, and denied runtime DDL, migration/initialization mutation, and permanent activity deletion.
- Deferred capability: CURU-003 owns the bounded seed asset; CURU-004 owns explicit dataset initialization. Product library behavior and owner authorization remain assigned to later tasks.
