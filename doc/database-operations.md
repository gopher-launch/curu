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

## Backup and restore recovery

The external data service owns backup scheduling, retention, protected storage, and restore execution. Curu supports recovery when the restored database has the exact migration history required by the application build. This build requires schema version 2; its application dataset consists of migration history, dataset-initialization metadata, activities (including owner, revision, archive, timestamps, and all coaching content), and ordered activity progressions. Later schema-owning tasks must extend the recovery manifest and disposable fixture for their tables.

Before a backup exercise, capture an application manifest using a read-only runtime connection. The command uses one read-only repeatable-read transaction, so its own counts and digest represent a consistent state. Application writes must still be quiesced between that capture and the backup, or both must come from the same fixed provider snapshot clone. The command emits counts and a SHA-256 digest, not content, owner identifiers, or credentials:

```sh
CURU_RECOVERY_DATABASE_URL='postgres://curu_runtime:...@db.example/curu?sslmode=require' \
go run ./cmd/curu-recovery-check > curu-recovery-manifest.json
```

Treat the manifest as operational recovery evidence. It is not a backup and should be retained with the corresponding backup identifier. Do not compare a live manifest with a backup taken after intervening saves.

Restore only into a newly created, isolated empty database. Never use these example operations to overwrite a retained database. A provider's equivalent snapshot/restore operation may replace `pg_dump` and `pg_restore`; Curu does not provision the target:

```sh
pg_dump --format=custom --no-owner --no-privileges \
  --file=curu.dump "$CURU_BACKUP_SOURCE_DATABASE_URL"
pg_restore --exit-on-error --no-owner --no-privileges \
  --dbname="$CURU_ISOLATED_RESTORE_DATABASE_URL" curu.dump
```

Use the target's migration-owner credential to run the documented migration command against the restored target. At the current schema version this verifies migration checksums and reconciles restricted runtime grants without changing application content. Then use a newly supplied target runtime credential to compare the restore with the source manifest:

```sh
CURU_RECOVERY_DATABASE_URL='postgres://curu_runtime:...@isolated-db.example/curu_restore?sslmode=require' \
go run ./cmd/curu-recovery-check --expect=curu-recovery-manifest.json
```

A successful comparison proves exact preservation of every currently delivered application field, the initialization record, edited and archived content, ordered progressions, and migration history. It also proves that the restored runtime credential can read the supported schema. A mismatch or compatibility failure is a failed recovery exercise; do not promote that target.

Database backups intentionally do not contain JWT signing trust, gateway sessions, database passwords, or deployment secrets. Supply those anew. Configure the restored deployment's owner binding with the stable internal owner UUID represented by the restored content; issuer/subject rebinding remains deployment configuration under the integration contract. Validate the gateway/JWKS and readiness separately before any promotion. Recovery does not rerun initialization or synchronize the seed over edited content.

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

The application image includes `/curu-initialize` with the same flags and settings. The command first performs the read-only schema compatibility check, then takes a transaction-scoped PostgreSQL advisory lock. A seed initialization inserts all 44 activities and 110 progressions and the `curu-coaching-library` version `1` completion record in one transaction. Empty mode records the empty choice without inserting content. An exact completed retry is a no-op. For seed mode, exact means the recorded seed identity/version match and every deterministic seeded activity still establishes the supplied owner UUID; a different owner or missing ownership evidence is refused without rewriting content. A different recorded choice or any activity without initialization metadata is also refused. Retries never compare, repair, replace, or synchronize editable coaching content, and startup never invokes this command.

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
go test ./internal/database -run 'TestPostgres(MigrationAndRuntimeRestrictions|DatasetInitialization|BackupRestoreRecovery)' -count=1
```

The tests change database-wide `PUBLIC` temporary privileges, drop and recreate the `curu` schema, create disposable roles, and create and drop two isolated databases whose names start with `curu_recovery_`; never point them at retained data. The selected user therefore needs disposable role, schema, and database authority, and the recovery case requires `pg_dump` and `pg_restore` on `PATH`. The checks prove the migration/runtime boundary, narrow initializer grants, initialization safety, and a custom-format backup/restore that retains the exact edited and archived seeded dataset and initialization metadata. The restore is validated through the restricted runtime role. Cleanup restores the usual `PUBLIC` temporary privilege used by the schema/initialization fixtures; recovery databases remain isolated and are dropped.

## Reset a named disposable target

Reset is not a web route or an initialization mode. It is a destructive operator action only for an explicitly named isolated database. For the disposable database named `curu_test`, first verify the selected URL resolves to that exact database, then drop only Curu's schema and rerun migration and initialization:

```sh
test "$(psql "$CURU_DISPOSABLE_DATABASE_URL" -Atqc 'select current_database()')" = 'curu_test' && \
  psql "$CURU_DISPOSABLE_DATABASE_URL" -v ON_ERROR_STOP=1 -c 'DROP SCHEMA IF EXISTS curu CASCADE'
```

Do not set `CURU_DISPOSABLE_DATABASE_URL` to a retained, shared, staging, or production database. Curu does not provision or destroy the database service itself.
