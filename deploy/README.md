# Curu reference deployment

## Status and scope

This directory records the accepted deployment MVP design and its [implementation roadmap](roadmap.md). The owned chart lives at [`helm/curu/`](helm/curu/), and the local cluster interface lives at [`kind/`](kind/) and [`scripts/`](scripts/). The chart contains the Curu Deployment/internal Service, persistent PostgreSQL with separated database authority, and explicit ordered migration and initialization Jobs. The kind interface establishes the host/cluster, image, storage, name, and trust inputs used by later component Tasks. No identity provider, browser login, or network-isolation result is claimed. The next core deployment Task is CDP-005.

The first target is Chris's HP Ubuntu laptop running a single-node kind cluster. The objective is a reproducible, authenticated installation of the current activity library, with every runtime component deployed by an owned Helm chart. Paid services, hosted identity, public internet hosting, and cloud infrastructure are not prerequisites. Local HTTPS and the application's existing security checks remain part of ordinary development.

This deployment lives beside Curu so application/image/configuration changes can be reviewed with their deployment effects. The root [roadmap](../roadmap.md) owns application Tasks; this directory owns deployment Tasks. The root [contributor guidance](../AGENTS.md) applies to both. Application authoring, sessions, and templates are not prerequisites for deploying the delivered library.

## Accepted component choices

| Component | Local deployment | External integration boundary |
| --- | --- | --- |
| Cluster | Single-node kind on Ubuntu | A conforming hosted Kubernetes cluster is a future target |
| Packaging | Owned Helm chart at `deploy/helm/curu/` | Release values describe environment inputs |
| Edge | Plain NGINX Deployment, TLS termination and explicit proxy configuration | Cluster exposure and certificate delivery can change by environment |
| Browser authentication | OAuth2 Proxy, OIDC Authorization Code flow with PKCE | Configured issuer, client credentials, callbacks, and session policy |
| Identity provider | Single-replica Keycloak with persisted state | Bundled identity can be disabled for a compatible external issuer |
| Application | Existing non-root Curu OCI image | Explicit image version, runtime configuration, and Secret references |
| Database | One Postgres instance with separate Curu and Keycloak databases and credentials | Bundled Postgres can be disabled for explicitly supplied external connections and trust |
| Persistence | PVCs backed by explicitly selected laptop storage | Storage class and volume configuration are environment inputs |
| Recovery | Optional manual export and isolated restore | Scheduled retention and off-host backup services are deferred |

NGINX is an ordinary chart-owned reverse proxy, not the community ingress-nginx controller. Envoy Gateway, Gateway API controllers, service meshes, and database/identity operators are outside this initial design.

## Chart ownership and configuration policy

- Define every bundled component and operator Job directly in this repository's templates. No vendor or other third-party subcharts, transitive chart dependencies, or bootstrap downloads of component charts are allowed. Upstream container images are allowed.
- Pin tool, image, and chart-release inputs to reviewed versions; do not use floating `latest` references. Record the Curu source revision and selected image identity with deployment evidence. The owning Tasks select concrete compatible versions and publish exact executable commands.
- Keep kind configuration in `deploy/kind/` and operator automation in `deploy/scripts/`. Bootstrap creates/configures the cluster and host prerequisites; Helm deploys the runtime components.
- Define a bounded values schema, with explicit configuration and existing Secret references for external Postgres or identity. Validate inconsistent bundled/external selections before a deployment can expose Curu. Keep cloud-provider configuration outside application/domain code.
- Generate development secrets once or accept operator-supplied Secrets. Reuse them on ordinary upgrades; never regenerate database passwords, OAuth client credentials, session keys, or owner bindings implicitly. Keep secrets out of committed values, rendered examples, routine logs, and command output. A Kubernetes Secret is not itself an encryption-at-rest guarantee.
- Do not mount a service-account API token into workloads that do not need Kubernetes API access. Use narrowly scoped RBAC only where an operator workflow needs it, and apply non-root/restricted container settings compatible with the selected images.

## Identity, routing, and local HTTPS

NGINX routes identity login traffic to Keycloak and browser login/callback/logout traffic to OAuth2 Proxy. For product requests it consults OAuth2 Proxy and sends authenticated traffic directly to Curu. NGINX is Curu's immediate trusted proxy; do not configure an invented forwarding chain.

The edge strips browser-supplied Authorization and forwarding-identity/address headers, then supplies exactly one `Authorization: Bearer <access-token>` header and one trusted client address. Access tokens must not be returned to the browser in response headers or routine logs. OAuth2 Proxy's ID-token forwarding option does not satisfy this contract. Propagate renewed session cookies, including split cookies if present, and preserve Curu's response/security headers.

The bundled realm/client configuration must establish RFC 9068 tokens: `at+jwt`, RS256, a dedicated Curu resource audience, the required claims including `client_id` and `jti`, and an access-token lifetime no greater than 15 minutes. A Keycloak header-type switch alone is not compatibility evidence; configure the required mappers and test a real issued token against the Curu validator. Login uses the authorization-code browser flow, not a password grant used as a substitute for browser acceptance.

Bind the exact owner issuer/subject to one stable Curu owner UUID, also supplied to seeded initialization. Preserve the Keycloak user identity and signing state across normal restarts/upgrades. A valid token for another user must remain unauthorized. Bundle controlled development-user bootstrap without committing passwords or resetting existing users on each upgrade; keep identity administration separate from ordinary application login.

Use stable HTTPS application and identity hostnames with local CA trust. Host-browser resolution and in-cluster resolution must both work; a pod's `localhost` is not the laptop browser. Curu and OAuth2 Proxy must trust the local issuer certificate without disabling certificate validation. Certificate issuance, installation of host/browser trust, renewal, and Secret references are owned by CDP-002 and the integration Tasks. Keep local CA private keys out of application pods. Actual phone access additionally requires device name resolution and certificate trust, and is recorded only when performed.

Expiry at either the application or gateway must retain the current HTMX DOM, return an authentication-required signal, and offer deliberate sign-in. Do not replace an editor with a login page, redirect/replay an unsafe request, or interpret an authentication failure as a successful save. Logout must clear the application gateway session, with provider-session behavior made explicit.

## Exposure and the local isolation qualification

Expose only the intended NGINX edge through kind's configured host ports. Keep Curu, Postgres, OAuth2 Proxy, and platform readiness internal; identity routes use the edge, with administrative access constrained by the documented local operator workflow. Initial acceptance is on the laptop browser. LAN/device access requires a deliberate exposure configuration; public hosting is not part of this milestone.

NetworkPolicy enforcement and verification may be deferred if unavailable in the selected kind setup. An additional CNI and successful denied-connection tests are not local MVP prerequisites. This is Chris's explicit local-development qualification of the platform isolation requirements in the [gateway](../doc/gateway.md) and [integration](../doc/integration-contract.md) contracts. It does not relax JWT validation, authorization, HTTPS trust, header handling, or database privilege boundaries.

Document the laptop administrator and local cluster as trusted boundaries, including the selected private-hop transport and any unverified isolation. Internal ClusterIP Services restrict exposure but do not establish pod-to-pod isolation. Report network enforcement as unverified until exercised. CDP-010 tracks that follow-up separately; hosted deployment must explicitly review and verify its exposure/private-hop controls instead of inheriting this qualification silently.

## Database and data lifecycle

Use the [database operations contract](../doc/database-operations.md) verbatim for the application binaries, environment settings, role restrictions, and grants. The platform provisions database administration, migration-owner, narrow initializer, and runtime credentials separately. Keycloak receives only its own database credentials. Account for effective privileges inherited through `PUBLIC`, not just direct runtime grants.

Deployment automation sequences database readiness/preparation, explicit migration, explicit first-install initialization, and runtime startup with only runtime credentials. Jobs use the same selected Curu image as the server. Preserve distinct operations even when a wrapper sequences them; failure stops progression. A hook cannot assume its dependency already exists on the first install. Startup and ordinary upgrades must not choose initialization mode or synchronize seed content implicitly.

For the library demonstration, deliberately select seed mode and the stable owner UUID: the current asset has 44 activities and 110 progressions. Exact initialization retries are no-ops; conflicting choices are refused. The root application documentation is authoritative if implemented schema or seed versions later change.

External-database mode supports explicit endpoints, database names, scoped Secret references, TLS mode and CA trust, and separately supplied administration/migration/initializer/runtime/Keycloak connections. It provisions nothing externally and never gives provider-administrator access to Curu. Bundled mode uses plaintext PostgreSQL transport only on the trusted local pod network; this qualified local policy is not a hosted-environment default or evidence of pod isolation.

Persist database files outside an ephemeral kind node container using the bootstrap's named laptop-storage input. Document pod restart, release removal/reinstall, cluster recreation, and explicit data deletion separately, including any manual volume reattachment required. Recreating a cluster or upgrading a chart must not authorize wiping that storage. Persistence is not backup or host-failure recovery.

## Local kind boundary

### Prerequisites and explicit inputs

The implemented interface targets an x86-64 Ubuntu laptop with Docker Engine.
It requires Docker with a running daemon, kind `v0.30.0`, kubectl `v1.34.1`,
OpenSSL, curl, and standard Ubuntu `sudo`/`update-ca-certificates` commands for
the optional host-trust change. Helm `v4.2.4` remains the chart-validation
version. The disposable acceptance run used Docker Engine `29.2.0` and OpenSSL
`3.5.5`; those versions record evidence rather than impose an unneeded exact
runtime check. Bootstrap pins the kind node to Kubernetes `v1.34.0` by digest
and refuses different kind/kubectl client versions.

Choose these inputs deliberately before bootstrap:

- a dedicated absolute host directory on storage intended to survive cluster
  deletion; it must be absent, empty and unmarked, or already carry the valid
  Curu ownership marker from an earlier bootstrap;
- the capacity to declare for the retained local PV (this is scheduling
  metadata and does not manufacture or reserve laptop disk space);
- Docker CPU and memory limits that fit the laptop; and
- the loopback HTTPS port, `8443` by default.

For example, from the repository root:

```sh
deploy/scripts/bootstrap-kind.sh \
  --storage-dir /srv/curu-kind \
  --storage-capacity 20Gi \
  --cpus 4 \
  --memory 6g \
  --https-port 8443
```

The fixed cluster is `curu-local`, its explicit context is `kind-curu-local`,
and its default namespace is `curu-system`. Bootstrap makes that namespace the
context default, but every repository command still supplies the context and
namespace. The Kubernetes API uses kind's random loopback port. The only fixed
workload mapping is `127.0.0.1:8443` to the future edge NodePort `30443`; no
backend or administration port is mapped to the LAN.

Bootstrap refuses to claim a symbolic link, a non-directory path, or a
non-empty unmarked directory. It never adds its reset-authorizing marker beside
pre-existing unowned files. A directory with the valid marker remains reusable
for cluster recreation and may contain retained workload data.

Build the current checkout and load a source-bound, non-floating image into the
node:

```sh
deploy/scripts/load-curu-image.sh
```

The default image is `curu:<full-git-commit>`. `--image` selects another tagged,
non-`latest` local name, and `--no-build` reloads an already inspected local
image. The loader refuses dirty application build inputs and requires the
image's source-revision label to equal the checkout commit. A later chart
values file must use the selected name with an appropriate local pull policy;
loading an image does not install Curu.

### Local names and HTTPS trust

The stable browser/application name is `curu.test`; the stable identity name is
`identity.curu.test`. Bootstrap configures in-cluster DNS to resolve both names
to the future `curu-edge.curu-system.svc.cluster.local` Service. CDP-005 and
CDP-006 still own component-specific issuer and edge routing.

Keep local PKI outside the repository and retained workload directory:

```sh
deploy/scripts/prepare-local-tls.sh \
  --pki-dir /home/chris/.local/share/curu/pki
```

The command creates the CA once and reuses complete existing material. It
refuses a partial PKI instead of rotating implicitly. The host retains
`ca.key`; workloads receive only the leaf key/certificate in Secret
`curu-system/curu-local-edge-tls` and public `ca.crt` in ConfigMap
`curu-system/curu-local-ca`. Both names are certificate SANs and are verified
before those inputs are applied. CA renewal/rotation is an explicit operator
operation and must be coordinated with host and workload trust.

Review the generated CA fingerprint, then explicitly install the managed host
names and CA in Ubuntu's system trust store:

```sh
deploy/scripts/configure-host-trust.sh install \
  --ca-cert /home/chris/.local/share/curu/pki/ca.crt
```

This is the only bootstrap operation that invokes `sudo`; it reports that it
changes `/etc/hosts` and `/usr/local/share/ca-certificates` before the operator
runs it. Browsers with a private trust store require a separate explicit CA
import. After CDP-006 supplies the edge on the default port, verify host
resolution and trust with:

```sh
deploy/scripts/configure-host-trust.sh check \
  --ca-cert /home/chris/.local/share/curu/pki/ca.crt \
  --https-port 8443
```

Use `configure-host-trust.sh remove` to remove only the managed hosts line and
Ubuntu CA file; remove any browser-private import separately. No command uses a
TLS verification skip.

### Storage and teardown lifecycle

The host directory is mounted at `/var/local-path-provisioner` in the kind
node. Bootstrap creates the `curu-local-retained` no-provisioner StorageClass
and the `curu-local-data` PV at stable node path
`/var/local-path-provisioner/curu-data`, with reclaim policy `Retain`. CDP-003's
database claim binds to that PV.

| Event | Retention and reattachment |
| --- | --- |
| Pod restart or replacement | The same bound claim and host directory remain. |
| Helm release removal | The retained PV/host data are not an instruction to delete data; claim ownership and keep policy are completed by the owning component Task. |
| `delete-kind-cluster.sh` | Kubernetes objects disappear with the node, but the marked host directory and PKI remain. Rerunning bootstrap with the same host directory recreates the PV; the later component claim must bind the same stable PV/path. |
| Host failure or disk loss | Not covered. Local persistence is not a backup. |

Ordinary teardown is deliberately non-destructive:

```sh
deploy/scripts/delete-kind-cluster.sh
```

Only after cluster deletion, erase retained workload data with the separately
named and confirmed operation:

```sh
deploy/scripts/reset-kind-storage.sh \
  --storage-dir /srv/curu-kind \
  --confirm-curu-local-data-loss
```

The reset refuses a symbolic link, an unmarked directory, or a running
`curu-local` cluster. The implemented bootstrap creates its marker only in a
new or empty dedicated directory. Reset uses the already-pinned kind node image
so it can remove database files owned by container UIDs, preserves the
directory marker, and irreversibly deletes retained workload data below that
exact directory. It does not remove PKI or host trust.

### Local security qualification

The laptop administrator, Docker daemon, kind node, and pod network are trusted
for this local topology. The future NGINX edge is the only host-facing workload
and will be Curu's immediate trusted proxy. ClusterIP Services and loopback
mapping limit incidental exposure but do not prove pod isolation or protect an
administrator-controlled node. kind's default CNI is retained; NetworkPolicy
enforcement was not exercised and remains explicitly unverified. This local
qualification neither relaxes Curu JWT/owner checks and HTTPS verification nor
establishes a hosted-production boundary. CDP-010 owns actual enforcement
evidence.

## Operator interfaces and verification

The chart, PostgreSQL, and kind validation commands below are implemented. Each later owning Task must add exact working commands to this README as it delivers an interface; planned workflow names remain non-executable until then.

From the repository root, with Helm `v4.2.4`, run:

```sh
deploy/helm/curu/tests/validate.sh
```

The check uses pinned, non-runnable `.invalid` fixture image inputs to lint and render the bundled and external configuration modes without credential content, assert the dependency-free chart boundary, and reject contradictory or incomplete security-sensitive selections. Exact values and database contracts are documented in the [chart README](helm/curu/README.md).

Exercise the database boundary in its own disposable kind cluster:

```sh
deploy/scripts/verify-postgres-disposable.sh
```

This live check never targets `curu-local`. It verifies readiness, restart persistence, repeatable role preparation, effective runtime/`PUBLIC` restrictions, Curu/Keycloak separation, generated-Secret stability through upgrade, and an external-mode connection fixture. It deletes its disposable cluster and does not migrate or initialize the Curu schema.

Exercise the complete explicit data lifecycle in a separate disposable cluster:

```sh
deploy/scripts/verify-data-lifecycle-disposable.sh
```

The check builds and loads the selected Curu image, holds runtime off while
Helm sequences preparation, migration, narrow grants, and seeded initialization,
then verifies schema version 2 and the 44-activity/110-progression dataset. It
also exercises the exact no-op retry, conflicting-choice refusal, missing and
incompatible schema rejection, initializer privilege scope, bounded hook
failure, and operation-free runtime launch. It never targets `curu-local`,
deletes its cluster, and does not claim identity or browser acceptance. Exact
first-install, independent-operation, and runtime-launch Helm commands are in
the [chart README](helm/curu/README.md).

Validate scripts and static exposure/storage invariants without creating a
cluster:

```sh
deploy/scripts/validate-kind.sh
```

Exercise two disposable cluster creations, image build/load, port and mount
configuration, a marker across pod and cluster recreation, and representative
host/pod HTTPS trust and name resolution:

```sh
check_root=$(mktemp -d /tmp/curu-kind-check.XXXXXX)
deploy/scripts/verify-kind-disposable.sh \
  --storage-dir "$check_root/storage" \
  --pki-dir "$check_root/pki" \
  --storage-capacity 2Gi \
  --cpus 2 \
  --memory 3g \
  --https-port 18443
```

The live check refuses an existing `curu-local` cluster or non-empty supplied
directories, uses pinned verification images, deletes its cluster, and retains
the disposable host directories for inspection/removal by the operator. Its
temporary TLS edge proves bootstrap inputs only; it is not the CDP-006 gateway
and makes no login or library claim.

| Planned interface | Owning Tasks | Required result |
| --- | --- | --- |
| Validate/render chart | CDP-001, extended by later chart Tasks | Lint, render, reject inconsistent inputs, and check bundled/external configurations |
| Prepare local cluster and trust | CDP-002 | Implemented versioned kind, port/storage configuration, certificates, trust steps, and disposable checks |
| Prepare data service | CDP-003 | Implemented ready Postgres, retained storage, stable scoped credentials, and separated database authority |
| Migrate and initialize | CDP-004 | Explicit, bounded Jobs with safe retry/refusal behavior |
| Configure identity | CDP-005 | Repeatable realm/client/owner setup and compatible tokens |
| Authenticate browser traffic | CDP-006 | HTTPS, owner access, refresh/expiry/logout, safe header handling |
| Install, inspect, restart, upgrade, remove | CDP-007 | Repeatable sequencing, bounded waits, useful diagnostics, and explicit data-retention behavior |
| Verify target acceptance | CDP-008 | Actual laptop-browser evidence, distinct from automated checks and optional phone results |
| Export and verify an isolated restore | CDP-009, optional | Application manifest comparison without overwriting a retained target |

Application build/test and data-operation commands already exist in the root [README](../README.md) and [database operations guide](../doc/database-operations.md). Their existence does not mean a Kubernetes deployment has been tested.

Chart changes require the lint/render/configuration checks introduced by their owning Tasks. Runtime claims require focused disposable-cluster evidence. Documentation-only changes require consistency checks; Go changes retain the root build/test requirements. Never run disposable database/reset fixtures against the retained local dataset. Keep run evidence and operator decisions outside product data and distinguish planned, skipped, passed, failed, and operator-pending checks honestly.

The first milestone requires repeatable local installation, owner login to the seeded library, search/detail access, rejection of another identity, safe authentication expiry, and persistence through restart and the documented upgrade path. An unavailable JWKS service fails according to Curu's fresh-cache/fail-closed rules. Scheduled backups, manual recovery delivery, NetworkPolicy enforcement, phone trials, and hosted-cluster deployment are not blockers for that milestone.
