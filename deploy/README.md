# Curu reference deployment

## Status and scope

This directory records the accepted deployment MVP design and its [implementation roadmap](roadmap.md). The owned chart foundation now lives at [`helm/curu/`](helm/curu/), with only the Curu Deployment, internal Service, explicit configuration boundary, and render checks implemented. It does not yet contain kind configuration or deployment scripts. No Helm install command, running environment, browser trial, or network-isolation result is claimed. The next deployment Task is CDP-002.

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
- Keep kind configuration in `deploy/kind/` and operator automation in `deploy/scripts/`. These are intended locations, not currently implemented interfaces. Bootstrap creates/configures the cluster and host prerequisites; Helm deploys the runtime components.
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

External-database mode must support explicit endpoints, database names, Secret references, TLS mode and CA trust, and separately supplied operator/runtime connections. Support an already provisioned database/roles without requiring provider-superuser access from the application. Document the selected internal database transport policy too; do not copy a development-only connection policy into an external environment silently.

Persist database files outside an ephemeral kind node container using the bootstrap's named laptop-storage input. Document pod restart, release removal/reinstall, cluster recreation, and explicit data deletion separately, including any manual volume reattachment required. Recreating a cluster or upgrading a chart must not authorize wiping that storage. Persistence is not backup or host-failure recovery.

## Operator interfaces and verification

The chart validation command below is implemented. Each later owning Task must add exact working commands to this README as it delivers an interface; planned workflow names remain non-executable until then.

From the repository root, with Helm `v4.2.4`, run:

```sh
deploy/helm/curu/tests/validate.sh
```

The check uses pinned, non-runnable `.invalid` fixture image inputs to lint and render the bundled and external configuration modes, assert the dependency-free chart boundary, and reject contradictory or incomplete security-sensitive selections. Exact individual commands and the values contract are documented in the [chart README](helm/curu/README.md). This is manifest/configuration evidence only, not a live component or browser result.

| Planned interface | Owning Tasks | Required result |
| --- | --- | --- |
| Validate/render chart | CDP-001, extended by later chart Tasks | Lint, render, reject inconsistent inputs, and check bundled/external configurations |
| Prepare local cluster and trust | CDP-002 | Versioned kind, port/storage configuration, certificates, and documented trust steps |
| Prepare data service | CDP-003 | Ready Postgres and appropriately separated database authority |
| Migrate and initialize | CDP-004 | Explicit, bounded Jobs with safe retry/refusal behavior |
| Configure identity | CDP-005 | Repeatable realm/client/owner setup and compatible tokens |
| Authenticate browser traffic | CDP-006 | HTTPS, owner access, refresh/expiry/logout, safe header handling |
| Install, inspect, restart, upgrade, remove | CDP-007 | Repeatable sequencing, bounded waits, useful diagnostics, and explicit data-retention behavior |
| Verify target acceptance | CDP-008 | Actual laptop-browser evidence, distinct from automated checks and optional phone results |
| Export and verify an isolated restore | CDP-009, optional | Application manifest comparison without overwriting a retained target |

Application build/test and data-operation commands already exist in the root [README](../README.md) and [database operations guide](../doc/database-operations.md). Their existence does not mean a Kubernetes deployment has been tested.

Chart changes require the lint/render/configuration checks introduced by their owning Tasks. Runtime claims require focused disposable-cluster evidence. Documentation-only changes require consistency checks; Go changes retain the root build/test requirements. Never run disposable database/reset fixtures against the retained local dataset. Keep run evidence and operator decisions outside product data and distinguish planned, skipped, passed, failed, and operator-pending checks honestly.

The first milestone requires repeatable local installation, owner login to the seeded library, search/detail access, rejection of another identity, safe authentication expiry, and persistence through restart and the documented upgrade path. An unavailable JWKS service fails according to Curu's fresh-cache/fail-closed rules. Scheduled backups, manual recovery delivery, NetworkPolicy enforcement, phone trials, and hosted-cluster deployment are not blockers for that milestone.
