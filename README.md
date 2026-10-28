# Curu

Curu is a greenfield Go application for maintaining activities and planning practical coaching sessions. The current runtime exposes coarse health checks and no product data or mutation routes. It requires a compatible migrated PostgreSQL schema and never mutates that schema at startup.

## Requirements

- Go 1.27 or later
- PostgreSQL
- Docker or another OCI builder/runtime only when building the container

No identity provider is needed for the current shell. Database provisioning and migration are described in [`doc/database-operations.md`](doc/database-operations.md).

## Build, test, and start

Build the server:

```sh
go build ./cmd/curu
```

Run the mandatory local test suite:

```sh
go test ./...
```

After applying migrations, start the server with its restricted runtime connection on the safe loopback default:

```sh
CURU_DATABASE_URL='postgres://curu_runtime:...@db.example/curu?sslmode=require' go run ./cmd/curu
```

The process logs structured JSON to standard output. In another terminal, check its minimal endpoints:

```sh
curl --fail http://127.0.0.1:8080/health/live
curl --fail http://127.0.0.1:8080/health/ready
```

Stop it with `Ctrl-C`; `SIGINT` and `SIGTERM` both initiate graceful shutdown. See [`doc/configuration.md`](doc/configuration.md) for runtime settings.

## Container

Build a minimal image whose runtime process is non-root:

```sh
docker build -t curu:local .
```

The default application bind is loopback. A container deployment must explicitly select its internal listener and publish it only behind the intended gateway or platform network:

```sh
docker run --rm \
  -e CURU_LISTEN_ADDRESS=0.0.0.0:8080 \
  -e CURU_DATABASE_URL='postgres://curu_runtime:...@db.example/curu?sslmode=require' \
  -p 127.0.0.1:8080:8080 curu:local
```

This runtime implements explicit PostgreSQL migrations and read-only startup compatibility checks. The bounded coaching-library asset is documented in [`doc/seed-library.md`](doc/seed-library.md), but no bootstrap command applies it yet. JWT authentication, authorization, product pages, and initialization remain with their owning roadmap tasks.
