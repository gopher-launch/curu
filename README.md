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

## Initialize a dataset

After migration, explicitly choose either the bounded coaching library or an intentionally empty installation. Seed mode also requires the stable internal owner UUID that the later authorization configuration will use:

```sh
CURU_INITIALIZATION_DATABASE_URL='postgres://curu_initializer:...@db.example/curu?sslmode=require' \
CURU_OWNER_ID='00000000-0000-4000-8000-000000000001' \
go run ./cmd/curu-initialize --mode=seed
```

For an intentionally empty installation, omit `CURU_OWNER_ID` and use `--mode=empty`. Completed exact retries report `already_initialized`; a different initialization choice or unrecorded existing content is refused. Initialization is never performed by migration or server startup. Required privileges, disposable verification, and isolated reset are documented in [`doc/database-operations.md`](doc/database-operations.md).

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

This runtime implements explicit PostgreSQL migrations, read-only startup compatibility checks, and separate explicit dataset initialization. The bounded coaching-library asset is documented in [`doc/seed-library.md`](doc/seed-library.md). JWT authentication, authorization, and product pages remain with their owning roadmap tasks.
