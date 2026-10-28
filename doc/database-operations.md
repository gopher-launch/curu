# Database operations

Curu uses PostgreSQL as its authoritative store. The platform provisions the database, network/TLS policy, backups, and credentials. Curu owns the `curu` schema, ordered migrations, and compatibility checks. These interfaces contain no provider-specific behavior.

## Roles and connections

Provision distinct PostgreSQL login roles:

- The migration owner owns the `curu` schema and its objects. Its URL is supplied only to the migration command as `CURU_MIGRATION_DATABASE_URL`.
- The restricted runtime role is named by `CURU_RUNTIME_DATABASE_ROLE`. Migrations grant it schema usage, read-only migration and initialization metadata, non-destructive activity access, and progression access. It cannot create or alter schema objects, change migration history or initialization state, or permanently delete activities.
- The server receives the runtime role's URL as `CURU_DATABASE_URL`.

The migration owner must be able to create the `curu` schema and grant object privileges to the already-created runtime role. URLs use `postgres://` or `postgresql://`, identify a database explicitly, and should select the deployment's required TLS mode. Supply real secrets through the deployment's secret facility; do not commit or log them.

## Apply migrations

Run migrations separately from server startup:

```sh
CURU_MIGRATION_DATABASE_URL='postgres://migration-owner:...@db.example/curu?sslmode=require' \
CURU_RUNTIME_DATABASE_ROLE='curu_runtime' \
go run ./cmd/curu-migrate
```

The application image also includes `/curu-migrate`; a deployment can run that binary as a separate one-shot migration process by overriding the image entrypoint and supplying the same two settings. Do not give the ordinary `/curu` server process the migration-owner URL.

The command takes a PostgreSQL advisory lock, verifies the checksum and order of existing migration history, and applies pending migrations transactionally. Repeating it at the current version is a no-op apart from reconciling the documented runtime grants. This build supports schema version 2.

The initial schema contains owner-scoped activities, their ordered progressions, dataset-initialization metadata, archive timestamps, and positive revision tokens. Dataset initialization remains a separate future command; migration never seeds or replaces activities.

## Start the server

After migration, start the server with only the runtime URL:

```sh
CURU_DATABASE_URL='postgres://curu-runtime:...@db.example/curu?sslmode=require' \
go run ./cmd/curu
```

Startup opens the runtime connection and reads migration history. It refuses a missing, older, newer, non-contiguous, or checksum-mismatched schema. It does not create a schema, apply a migration, seed data, or call GORM `AutoMigrate`.

## Disposable PostgreSQL verification

The mandatory local suite skips the database integration case unless an explicitly disposable database URL is supplied. Against a disposable PostgreSQL database whose user can create and drop roles and schemas, run:

```sh
CURU_TEST_DATABASE_URL='postgres://postgres:...@127.0.0.1:5432/curu_test?sslmode=disable' \
go test ./internal/database -run TestPostgresMigrationAndRuntimeRestrictions -count=1
```

The test drops and recreates the `curu` schema and a role named `curu_runtime_test`; never point it at retained data. It proves startup compatibility is read-only, applies migration 1, inserts an existing fixture, applies migration 2, verifies retained content and repeatability, and confirms the runtime role can edit activity data but cannot create tables, alter migration/initialization state, or permanently delete activities.
