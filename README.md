# Curu

Curu is a greenfield Go application for maintaining activities and planning practical coaching sessions. The current runtime is deliberately only an application shell: it exposes coarse health checks and no product data or mutation routes.

## Requirements

- Go 1.27 or later
- Docker or another OCI builder/runtime only when building the container

No database or identity provider is needed for the current shell.

## Build, test, and start

Build the server:

```sh
go build ./cmd/curu
```

Run the mandatory local test suite:

```sh
go test ./...
```

Start the server on its safe loopback default:

```sh
go run ./cmd/curu
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
docker run --rm -e CURU_LISTEN_ADDRESS=0.0.0.0:8080 -p 127.0.0.1:8080:8080 curu:local
```

This task does not implement JWT authentication, authorization, product pages, PostgreSQL, migrations, or bootstrap commands. Their exact interfaces remain with their owning roadmap tasks.
