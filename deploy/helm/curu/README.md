# Curu Helm chart

This repository-owned chart currently supplies the Curu Deployment and
internal Service plus CDP-003's PostgreSQL data-service boundary. Bundled mode
owns one pinned PostgreSQL 17.6 StatefulSet, internal Service, retained claim,
stable credential Secret, and idempotent role-preparation Job. External mode
provisions no database resource and accepts explicit endpoint, TLS/CA, and
scoped Secret references.

Keycloak, NGINX, OAuth2 Proxy, migration/initialization Jobs, and a runnable
end-to-end installation remain with later tasks. PostgreSQL readiness does not
mean the Curu schema exists or the dataset is initialized.

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
| `migration-password`, `migration-url` | Migration owner and later migration Job |
| `initializer-password`, `initializer-url` | Narrow initializer and later initialization Job |
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
and grants the initializer/runtime roles only `CONNECT`. CDP-004 intentionally
owns initializer object grants after migrations create the schema. Keycloak
receives connect/temporary plus creation rights only in its own database. The
Curu roles cannot connect to Keycloak's database, and Keycloak cannot connect
to Curu's database.

External mode requires explicit `host`, `port`, `curuDatabase`,
`keycloakDatabase`, TLS mode, and five existing Secret references:
administration, migration, initializer, runtime, and Keycloak. `verify-ca` and
`verify-full` also require a CA Secret; its chosen key is mounted read-only at
`/etc/curu/database/ca.crt` for Curu. Each URL remains authoritative and must
agree with the declared endpoint, database, TLS mode, and CA path. The chart
does not prepare, rotate, or acquire provider-administrator access to an
external service. `disable` exists for a deliberately trusted disposable/local
fixture; use a verified mode for a remote service.

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
checks the owned resource and role-preparation boundaries, and rejects invalid
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
application migration/initialization or claim browser acceptance.
