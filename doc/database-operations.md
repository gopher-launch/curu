# Database operations

Curu uses PostgreSQL as its authoritative store. The platform provisions the database, network/TLS policy, backups, and credentials. Curu owns the `curu` schema, ordered migrations, and compatibility checks. These interfaces contain no provider-specific behavior.

## Roles and connections

Provision distinct PostgreSQL login roles:

- The migration owner owns the `curu` schema and its objects. Its URL is supplied only to the migration command as `CURU_MIGRATION_DATABASE_URL`.
- A one-shot initialization operator receives only the data privileges listed below. Its URL is supplied only to the initialization command as `CURU_INITIALIZATION_DATABASE_URL`.
- The restricted runtime role is named by `CURU_RUNTIME_DATABASE_ROLE`. Migrations grant it schema usage, read-only migration and initialization metadata, non-destructive activity access, and progression access. It cannot create or alter schema objects, change migration history or initialization state, or permanently delete activities.
- The server receives the runtime role's URL as `CURU_DATABASE_URL`.

The platform must create the runtime login without `SUPERUSER`, `CREATEDB`, `CREATEROLE`, `REPLICATION`, or `BYPASSRLS`, and must not grant it membership in any other PostgreSQL role. The no-membership rule prevents a `NOINHERIT` login from gaining additional authority through `SET ROLE`. The platform must also remove database-wide temporary-object creation inherited from PostgreSQL's default `PUBLIC` grant and ensure neither `PUBLIC` nor the runtime role can create objects in another schema. Run the database-wide revocation as the database owner, substituting the selected database name:

```sql
REVOKE TEMPORARY ON DATABASE curu FROM PUBLIC;
REVOKE CREATE ON DATABASE curu FROM PUBLIC;
REVOKE CREATE ON SCHEMA public FROM PUBLIC;
```

If another application role legitimately needs temporary objects or schema creation, grant that privilege directly to that other role rather than to `PUBLIC`. Do not make Curu runtime a member of it. Revoking a privilege only from the named runtime role is insufficient when it remains available through `PUBLIC`, and `NOINHERIT` alone is insufficient because the login can use `SET ROLE`.

The migration owner must be able to create the `curu` schema and grant object privileges to the already-created runtime role. The migration command checks the runtime role's effective privileges and rolls back all migration work if it has any role membership, can create temporary objects, create a schema, create objects in any existing schema, or has an elevated role attribute. It does not change database-wide or unrelated-schema grants because those are platform/database-owner responsibilities. URLs use `postgres://` or `postgresql://`, identify a database explicitly, and should select the deployment's required TLS mode. Supply real secrets through the deployment's secret facility; do not commit or log them.

## Apply migrations

Run migrations separately from server startup:

```sh
CURU_MIGRATION_DATABASE_URL='postgres://migration-owner:...@db.example/curu?sslmode=require' \
CURU_RUNTIME_DATABASE_ROLE='curu_runtime' \
go run ./cmd/curu-migrate
```

The application image also includes `/curu-migrate`; a deployment can run that binary as a separate one-shot migration process by overriding the image entrypoint and supplying the same two settings. Do not give the ordinary `/curu` server process the migration-owner URL.

The command takes a PostgreSQL advisory lock, verifies the checksum and order of existing migration history, applies pending migrations transactionally, reconciles the documented runtime grants, and validates the effective runtime role boundary. Repeating it at the current version leaves schema and content unchanged while rechecking grants and restrictions. This build supports schema version 2.

The initial schema contains owner-scoped activities, their ordered progressions, dataset-initialization metadata, archive timestamps, and positive revision tokens. Migration never seeds or replaces activities.

## Initialize the dataset

Provision a login role for the one-shot initializer without elevated attributes, role memberships, database or schema creation, or temporary-table access. After migration, the schema owner grants it only the implemented initialization surface (substitute the actual quoted role name):

```sql
GRANT USAGE ON SCHEMA curu TO curu_initializer;
GRANT SELECT ON curu.schema_migrations TO curu_initializer;
GRANT SELECT, INSERT ON curu.dataset_initializations TO curu_initializer;
GRANT SELECT, INSERT ON curu.activities TO curu_initializer;
GRANT INSERT ON curu.activity_progressions TO curu_initializer;
```

The initializer does not need schema ownership, update/delete privileges, or the runtime credential. Supply its secret only to this one-shot command. Seed mode requires the stable internal owner UUID selected for the deployment:

```sh
CURU_INITIALIZATION_DATABASE_URL='postgres://curu_initializer:...@db.example/curu?sslmode=require' \
CURU_OWNER_ID='00000000-0000-4000-8000-000000000001' \
go run ./cmd/curu-initialize --mode=seed
```

To record a deliberately empty installation instead:

```sh
CURU_INITIALIZATION_DATABASE_URL='postgres://curu_initializer:...@db.example/curu?sslmode=require' \
go run ./cmd/curu-initialize --mode=empty
```

The application image includes `/curu-initialize` with the same flags and settings. The command first performs the read-only schema compatibility check, then takes a transaction-scoped PostgreSQL advisory lock. A seed initialization inserts all 44 activities and 110 progressions and the `curu-coaching-library` version `1` completion record in one transaction. Empty mode records the empty choice without inserting content. An exact completed retry is a no-op; a different recorded choice or any activity without initialization metadata is refused. The completion record is authoritative: retries do not compare, repair, replace, or synchronize edited content, and startup never invokes this command.

## Start the server

After migration, start the server with only the runtime URL:

```sh
CURU_DATABASE_URL='postgres://curu_runtime:...@db.example/curu?sslmode=require' \
go run ./cmd/curu
```

Startup opens the runtime connection and reads migration history. It refuses a missing, older, newer, non-contiguous, or checksum-mismatched schema. It does not create a schema, apply a migration, seed data, or call GORM `AutoMigrate`.

## Disposable PostgreSQL verification

The mandatory local suite skips the database integration case unless an explicitly disposable database URL is supplied. Against a disposable PostgreSQL database whose user can create and drop roles and schemas, run:

```sh
CURU_TEST_DATABASE_URL='postgres://postgres:...@127.0.0.1:5432/curu_test?sslmode=disable' \
go test ./internal/database -run 'TestPostgres(MigrationAndRuntimeRestrictions|DatasetInitialization)' -count=1
```

The tests change database-wide `PUBLIC` temporary privileges, drop and recreate the `curu` schema, and create disposable roles named with `curu_runtime_test` or `curu_initializer_test`; never point them at retained data. They prove the migration and runtime boundary plus the narrow initializer grants, fresh seed and empty initialization, exact retries, concurrent serialization, rollback after a mid-seed failure, refusal of unrecorded content, and preservation of a coach edit through retry and startup compatibility. Cleanup restores the usual `PUBLIC` temporary privilege.

## Reset a named disposable target

Reset is not a web route or an initialization mode. It is a destructive operator action only for an explicitly named isolated database. For the disposable database named `curu_test`, first verify the selected URL resolves to that exact database, then drop only Curu's schema and rerun migration and initialization:

```sh
test "$(psql "$CURU_DISPOSABLE_DATABASE_URL" -Atqc 'select current_database()')" = 'curu_test' && \
  psql "$CURU_DISPOSABLE_DATABASE_URL" -v ON_ERROR_STOP=1 -c 'DROP SCHEMA IF EXISTS curu CASCADE'
```

Do not set `CURU_DISPOSABLE_DATABASE_URL` to a retained, shared, staging, or production database. Curu does not provision or destroy the database service itself.
