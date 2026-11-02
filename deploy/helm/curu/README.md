# Curu Helm chart

This is the repository-owned Helm chart for Curu. CDP-001 supplies only the
Curu Deployment and an internal `ClusterIP` Service. It does not yet provision
Postgres, Keycloak, NGINX, OAuth2 Proxy, migration/initialization Jobs, local
TLS, or a runnable end-to-end installation.

## Required inputs

The chart intentionally has no deployable defaults. Supply all required values
in a private environment values file; never commit credentials to a values
file. The database URL is read from an existing Kubernetes Secret and is not
copied into a ConfigMap or rendered manifest.

| Values | Purpose |
| --- | --- |
| `image.repository`, `image.tag`, `image.pullPolicy`, `image.pullSecrets` | Explicit Curu image identity and registry access. `latest` is rejected. |
| `service.port` | Internal Curu listener and `ClusterIP` Service port. |
| `runtime.*` | Exact public HTTPS origin, optional trusted immediate-proxy CIDRs and gateway navigation URLs, and server timeouts. |
| `trust.*` | Required RFC 9068 issuer, audience, HTTPS JWKS URL, allowed algorithms, and bounded validation timings. |
| `owner.*` | Exact external owner issuer/subject and stable internal owner UUID. The owner issuer must equal the trusted issuer. |
| `database.bundled`, `database.external` | Exactly one database mode. The selected mode must reference an existing Secret whose selected key contains the restricted runtime PostgreSQL URL. CDP-003 will provision the bundled service. |
| `identity.bundled`, `identity.external` | Exactly one identity mode. Trust inputs remain explicit in either mode. CDP-005 will provision bundled identity and its private configuration. |
| `resources` | Curu CPU and memory requests/limits. |
| `podLabels`, `podAnnotations` | Optional pod metadata. `podLabels` cannot override the chart-owned `app.kubernetes.io/name` or `app.kubernetes.io/instance` selector labels. |

The selected runtime database Secret is the only Secret consumed by this chart
foundation. It must already exist in the release namespace. Migration,
initialization, database-administration, and identity-administration
credentials are deliberately not accepted by the Curu Deployment.

The fixture image repository uses the reserved `.invalid` domain and is a
render-only input. It cannot be pulled. A real installation must bind an image
built from the recorded source revision; lifecycle automation is owned by later
tasks.

## Validation

Validation is pinned to Helm `v4.2.4`. Run the complete CDP-001 check from the
repository root:

```sh
deploy/helm/curu/tests/validate.sh
```

The script runs strict lint and render checks for bundled and external modes,
confirms the chart has no dependencies, and proves rejection of floating image,
contradictory or absent mode, absent Secret-reference, missing trust,
mismatched owner-issuer, and unknown values inputs.
It also rejects either chart-owned selector label under `podLabels` while
confirming that non-reserved custom labels render once without changing the
Deployment selector.
The exact underlying lint and render commands are:

```sh
helm lint deploy/helm/curu --strict -f deploy/helm/curu/tests/fixtures/bundled-values.yaml
helm lint deploy/helm/curu --strict -f deploy/helm/curu/tests/fixtures/external-values.yaml
helm template curu-bundled deploy/helm/curu -f deploy/helm/curu/tests/fixtures/bundled-values.yaml
helm template curu-external deploy/helm/curu -f deploy/helm/curu/tests/fixtures/external-values.yaml
```

These checks validate manifests and configuration boundaries only. They do not
claim an installed release, a reachable database or issuer, browser login,
laptop acceptance, or enforced pod-network isolation.
