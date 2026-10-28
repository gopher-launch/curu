# Database operations

Curu uses PostgreSQL as its authoritative store. The platform provisions the database, network/TLS policy, backups, and credentials. Curu owns the `curu` schema, ordered migrations, and compatibility checks. These interfaces contain no provider-specific behavior.

## Roles and connections

Provision distinct PostgreSQL login roles:

- The migration owner owns the `curu` schema and its objects. Its URL is supplied only to the migration command as `CURU_MIGRATION_DATABASE_URL`.
- The restricted runtime role is named by `CURU_RUNTIME_DATABASE_ROLE`. Migrations grant it schema usage, read-only migration and initialization metadata, non-destructive activity access, and progression access. It cannot create or alter schema objects, change migration history or initialization state, or permanently delete activities.
- The server receives the runtime role's URL as `CURU_DATABASE_URL`.

The platform must create the runtime login without `SUPERUSER`, `CREATEDB`, `CREATEROLE`, `REPLICATION`, or `BYPASSRLS`. It must also remove database-wide temporary-object creation inherited from PostgreSQL's default `PUBLIC` grant and ensure neither `PUBLIC`, the runtime role, nor any role it inherits can create objects in another schema. Run the database-wide revocation as the database owner, substituting the selected database name:

```sql
REVOKE TEMPORARY ON DATABASE curu FROM PUBLIC;
REVOKE CREATE ON DATABASE curu FROM PUBLIC;
REVOKE CREATE ON SCHEMA public FROM PUBLIC;
```

If another application role legitimately needs temporary objects or schema creation, grant that privilege directly to that other role rather than to `PUBLIC` or a role inherited by Curu runtime. Revoking a privilege only from the named runtime role is insufficient when it remains available through `PUBLIC` or role membership.

The migration owner must be able to create the `curu` schema and grant object privileges to the already-created runtime role. The migration command checks the runtime role's effective privileges and rolls back all migration work if it can create temporary objects, create a schema, create objects in any existing schema, or has an elevated role attribute. It does not change database-wide or unrelated-schema grants because those are platform/database-owner responsibilities. URLs use `postgres://` or `postgresql://`, identify a database explicitly, and should select the deployment's required TLS mode. Supply real secrets through the deployment's secret facility; do not commit or log them.

## Apply migrations

Run migrations separately from server startup:

```sh
CURU_MIGRATION_DATABASE_URL='postgres://migration-owner:...@db.example/curu?sslmode=require' \
CURU_RUNTIME_DATABASE_ROLE='curu_runtime' \
go run ./cmd/curu-migrate
```

The application image also includes `/curu-migrate`; a deployment can run that binary as a separate one-shot migration process by overriding the image entrypoint and supplying the same two settings. Do not give the ordinary `/curu` server process the migration-owner URL.

The command takes a PostgreSQL advisory lock, verifies the checksum and order of existing migration history, applies pending migrations transactionally, reconciles the documented runtime grants, and validates the effective runtime role boundary. Repeating it at the current version leaves schema and content unchanged while rechecking grants and restrictions. This build supports schema version 2.

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

The test changes database-wide `PUBLIC` temporary privileges, drops and recreates the `curu` schema, and creates a role named `curu_runtime_test`; never point it at retained data. It first proves migration fails closed while inherited temporary or unrelated-schema creation is available. It then proves startup compatibility is read-only, applies migration 1, inserts an existing fixture, applies migration 2, verifies retained content and repeatability, and confirms the runtime role can edit activity data but cannot create persistent or temporary tables, alter migration/initialization state, or permanently delete activities. Cleanup restores the usual `PUBLIC` temporary privilege.
