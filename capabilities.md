# Delivered capabilities

## Documentation baseline

- Delivery: CURU-001
- Product contract: [`doc/product-baseline.md`](doc/product-baseline.md) records the accepted greenfield product model, editing behavior, on-field principles, and platform responsibility boundary.
- Architecture contract: [`doc/implementation-design.md`](doc/implementation-design.md) records the intended Go/GORM/PostgreSQL and server-rendered architecture, data/trust boundaries, verification approach, and future build, test, start, migrate, and bootstrap interface classes.
- Runtime capability: none. CURU-001 adds documentation only; executable commands and application behavior remain assigned to their dependent tasks.

## Integration and data-service contract

- Delivery: CURU-014
- Integration contract: [`doc/integration-contract.md`](doc/integration-contract.md) defines the gateway/JWT transport and validation defaults, stable single-owner mapping, deny-by-default application authorization, browser/CSRF/proxy behavior, and safe authentication-expiry outcomes.
- Data-service contract: the same document assigns PostgreSQL provisioning, backup, and restore execution to the platform while assigning migrations, initialization, compatibility, restricted runtime access, and post-restore semantic verification to Curu.
- Runtime capability: none. CURU-014 is a design delivery; authentication, authorization, browser protections, migrations, and recovery checks remain assigned to the implementation tasks identified in the roadmap.

## Minimal application runtime

- Delivery: CURU-002
- Runtime shell: the Go module and `cmd/curu` provide a validated, foreground HTTP server with structured logging, graceful `SIGINT`/`SIGTERM` shutdown, and coarse liveness/readiness endpoints.
- Safe surface: the default listener is loopback-only. No product-data route exists; unknown safe routes use the shared escaped HTML layout and unsafe or unimplemented routes remain unavailable.
- Contributor interface: [`README.md`](README.md) defines the executable `go build ./cmd/curu`, `go test ./...`, and `go run ./cmd/curu` commands. [`doc/configuration.md`](doc/configuration.md) defines the implemented runtime settings.
- Container interface: [`Dockerfile`](Dockerfile) builds the server into a minimal image that runs as numeric non-root user `65532:65532`; non-loopback binding must be selected explicitly by the deployment.
- Deferred capability: JWT authentication, application authorization, browser protections, PostgreSQL, migrations, initialization, and every activity/session/template behavior remain assigned to their roadmap tasks.
