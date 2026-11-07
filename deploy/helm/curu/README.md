# Curu Helm chart

This repository-owned chart supplies the Curu Deployment and internal Service,
CDP-003's PostgreSQL data-service boundary, and CDP-004's explicit lifecycle
Jobs. Bundled mode
owns one pinned PostgreSQL 17.6 StatefulSet, internal Service, retained claim,
stable credential Secret, and idempotent role-preparation Job. External mode
provisions no database resource and accepts explicit endpoint, TLS/CA, and
scoped Secret references.

Keycloak, NGINX, OAuth2 Proxy, and a runnable authenticated end-to-end
installation remain with later tasks. PostgreSQL readiness does not mean the
Curu schema exists or the dataset is initialized.

## Explicit data lifecycle

`dataLifecycle.migrate` defaults to `false` and
`dataLifecycle.initialize` defaults to `none`; installation and upgrade never
choose seed or empty initialization implicitly. Selecting either operation
requires `runtime.enabled=false`, so a first installation cannot launch the
server before the operation sequence finishes. Helm runs retained,
independently inspectable hook Jobs in this order:

1. bundled database readiness and role/database preparation;
2. `/curu-migrate` from the selected Curu image with only the migration URL and
   declared runtime role;
3. the five narrow initializer object grants, using the migration owner only
   after migration creates those objects; and
4. `/curu-initialize --mode=seed|empty` from the same Curu image with only the
   initializer URL and, for seed mode, `owner.id`.

Every Job has the configured active deadline and retry limit. A failed hook
stops later hooks and Helm returns failure; completed Job logs remain for one
day or until that operation is deliberately invoked again. Do not use
`--atomic` or Helm rollback as a database rollback. A successful migration
remains applied even if a later initialization attempt is refused.

For D1, use the stable owner UUID in the site values and explicitly run the
first phase from the repository root:

```sh
helm upgrade --install curu deploy/helm/curu \
  --kube-context kind-curu-local --namespace curu-system \
  --values /path/to/curu-local-values.yaml \
  --set runtime.enabled=false \
  --set dataLifecycle.migrate=true \
  --set-string dataLifecycle.initialize=seed \
  --wait --wait-for-jobs --timeout 10m
```

Only after that command succeeds, launch runtime with the same site values and
no selected data operation:

```sh
helm upgrade curu deploy/helm/curu \
  --kube-context kind-curu-local --namespace curu-system \
  --values /path/to/curu-local-values.yaml \
  --set runtime.enabled=true \
  --set dataLifecycle.migrate=false \
  --set-string dataLifecycle.initialize=none \
  --timeout 10m
```

The server process starts against the compatible schema, but its readiness
remains unavailable until CDP-005 supplies trusted JWKS. After identity is
delivered, the integrated operator workflow can add a bounded readiness wait.
Repeating the first command is an exact migration recheck and initialization
no-op. Selecting `empty`, changing the seed owner, or encountering unrecorded
content fails without replacing content. To invoke migration or initialization
independently, select only that operation while runtime is disabled; an
initialization-only invocation still runs the narrow grant Job first and
requires an already compatible schema. External mode requires distinct,
explicit `initializerRole` and `runtimeRole` values; it prepares no external
database or role.

## Database modes

Exactly one of `database.bundled.enabled` and `database.external.enabled` is
true. The values schema rejects contradictory modes and unknown fields.

Bundled mode uses the chart-owned `ClusterIP` Service and `sslmode=disable` on
the private pod network. This is limited to the documented trusted local kind
boundary: it is not proof of pod isolation and must not be copied into a hosted
environment without a reviewed private-hop/TLS policy. The PostgreSQL image is
both tag- and digest-pinned. Its private service/server port is fixed at `5432`;
configure resources, claim size, storage class, and PV selector under
`database.bundled`.

By default, Helm creates `<release>-curu-postgres-credentials`. A `lookup`
preserves its generated values on upgrade, and `helm.sh/resource-policy: keep`
retains it and the PVC on release removal. Set
`database.bundled.credentialsSecret.existingName` to use an operator-created
Secret instead. That Secret must contain these keys:

| Key | Consumer |
| --- | --- |
| `admin-password` | PostgreSQL startup and database preparation only |
| `migration-password`, `migration-url` | Migration owner, migration Job, and narrow grant Job |
| `initializer-password`, `initializer-url` | Narrow initialization Job only |
| `runtime-password`, `runtime-url` | Curu server only |
| `keycloak-password`, `keycloak-url` | Keycloak database access only |

URLs for bundled mode select `sslmode=disable` and the chart Service. Passwords
must be URL-safe when an existing Secret supplies complete URLs. Rotation is
never implicit. For service roles, create a complete replacement Secret with
mutually consistent passwords/URLs, preserve the current `admin-password`,
switch `existingName`, and perform a reviewed Helm upgrade; the preparation Job
then reconciles those role passwords without granting memberships or elevated
attributes. Administrator rotation additionally requires a separately
authorized database-side password change coordinated with the Secret update;
changing only `admin-password` makes preparation unable to connect. Never print
or commit either the old or replacement Secret.

The Job creates separate `curu` and `keycloak` databases. Configured service
role names must be pairwise distinct, configured database names must be
distinct, and PostgreSQL system/reserved names are rejected before rendering.
The Job removes
database-wide `PUBLIC` connect/create/temporary authority, removes `PUBLIC`
schema creation, grants the migration owner database schema-creation authority,
and grants the initializer/runtime roles only `CONNECT`. The lifecycle grant
Job adds only the documented initializer object grants after migration. Keycloak
receives connect/temporary plus creation rights only in its own database. The
Curu roles cannot connect to Keycloak's database, and Keycloak cannot connect
to Curu's database.

External mode requires explicit `host`, `port`, `curuDatabase`,
`keycloakDatabase`, distinct initializer/runtime role names, TLS mode, and five existing Secret references:
administration, migration, initializer, runtime, and Keycloak. `verify-ca` and
`verify-full` also require a CA Secret; its chosen key is mounted read-only at
`/etc/curu/database/ca.crt` for Curu. Each URL remains authoritative and must
agree with the declared endpoint, database, TLS mode, and CA path; initializer
and runtime URLs must authenticate as their corresponding distinct declared
roles. The chart does not prepare, rotate, or acquire provider-administrator
access to an external service. `disable` exists for a deliberately trusted
disposable/local fixture; use a verified mode for a remote service.

## Storage lifecycle

The default claim selects the `curu-local-retained` StorageClass and
`curu.local/storage: retained` PV created by the kind bootstrap. Pod replacement
and chart upgrade retain the bound claim and database files. Release removal
keeps the claim and generated Secret; reinstall the same release/namespace to
reuse those owned objects. Cluster deletion removes the claim object but leaves
the host directory and retained PV data. After recreating the cluster/PV, clear
the old PV claim reference if Kubernetes reports it `Released`, then reinstall
the same release so the same named claim can bind. Inspect the PV and host path
before any such operator action.

Only the separately confirmed `reset-kind-storage.sh` interface authorizes
deleting the retained host data, and only after cluster deletion. Persistence
is not a backup or host-failure recovery mechanism.

## Other required inputs

The chart still has no deployable application defaults. Supply a source-bound,
non-`latest` Curu image; exact runtime origin/timeouts; RFC 9068 issuer,
audience, algorithms, and JWKS inputs; and the exact external owner binding.
The Curu Deployment receives only the runtime database URL. It never receives
the administration, migration-owner, initializer, or Keycloak credential.

## Validation

With Helm `v4.2.4`, run the complete render/configuration check:

```sh
deploy/helm/curu/tests/validate.sh
```

It lints and renders bundled/external fixtures without credential material,
checks the owned resource, lifecycle/credential boundary, and role-preparation boundaries, and rejects invalid
mode, identity, trust, role-name, role/database collision, reserved PostgreSQL
name, bundled-port, endpoint, Secret, and TLS/CA inputs.

With Docker, kind `v0.30.0`, kubectl `v1.34.1`, and Helm `v4.2.4`, run the live
disposable verification:

```sh
deploy/scripts/verify-postgres-disposable.sh
```

The script refuses its pre-existing `curu-postgres-check` cluster, creates and
deletes that cluster automatically, and never targets `curu-local`. It verifies
readiness, pod-restart persistence, idempotent upgrade preparation, generated
Secret stability, service-role attributes and membership, effective `PUBLIC`
and runtime restrictions, Curu/Keycloak database separation, and an external
mode connection using the same disposable PostgreSQL service. It does not run
application migration/initialization or claim browser acceptance. Use
`deploy/scripts/verify-data-lifecycle-disposable.sh` for CDP-004's live check.
