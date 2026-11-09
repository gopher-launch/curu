#!/usr/bin/env bash
set -euo pipefail

readonly expected_helm_version='v4.2.4'
readonly chart_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
readonly fixture_dir="${chart_dir}/tests/fixtures"

actual_helm_version="$(helm version --short | cut -d+ -f1)"
if [[ "${actual_helm_version}" != "${expected_helm_version}" ]]; then
  echo "helm ${expected_helm_version} is required; found ${actual_helm_version}" >&2
  exit 1
fi

helm lint "${chart_dir}" --strict -f "${fixture_dir}/bundled-values.yaml"
helm lint "${chart_dir}" --strict -f "${fixture_dir}/external-values.yaml"
bundled_render="$(helm template curu-bundled "${chart_dir}" -f "${fixture_dir}/bundled-values.yaml")"
external_render="$(helm template curu-external "${chart_dir}" -f "${fixture_dir}/external-values.yaml")"
lifecycle_render="$(helm template curu-lifecycle "${chart_dir}" -f "${fixture_dir}/bundled-values.yaml" \
  --set edge.enabled=false --set runtime.enabled=false --set dataLifecycle.migrate=true --set-string dataLifecycle.initialize=seed)"
external_lifecycle_render="$(helm template curu-external-lifecycle "${chart_dir}" -f "${fixture_dir}/external-values.yaml" \
  --set edge.enabled=false --set runtime.enabled=false --set dataLifecycle.migrate=true --set-string dataLifecycle.initialize=seed \
  --set-string database.external.initializerRole=external_curu_initializer \
  --set-string database.external.runtimeRole=external_curu_runtime)"

assert_contains() {
  local rendered="$1"
  local expected="$2"
  local description="$3"
  if ! grep -Fq -- "${expected}" <<<"${rendered}"; then
    echo "render assertion failed: ${description}" >&2
    exit 1
  fi
}

assert_contains "${bundled_render}" 'automountServiceAccountToken: false' 'service-account token mount is disabled'
assert_contains "${bundled_render}" 'runAsNonRoot: true' 'pod runs as non-root'
assert_contains "${bundled_render}" 'readOnlyRootFilesystem: true' 'container root filesystem is read-only'
assert_contains "${bundled_render}" 'type: ClusterIP' 'Curu Service remains internal'
assert_contains "${bundled_render}" 'type: NodePort' 'only the NGINX edge receives the kind host mapping'
assert_contains "${bundled_render}" 'nodePort: 30443' 'edge uses the bootstrap-declared NodePort'
assert_contains "${bundled_render}" 'nginx:1.29.2-alpine@sha256:b03ccb7431a2e3172f5cbae96d82bd792935f33ecb88fbf2940559e475745c4e' 'edge image is version and digest pinned'
assert_contains "${bundled_render}" 'oauth2-proxy:v7.15.5@sha256:8498b0d0ef0a7b29686414000a08aee467f02d0299c9ed1e006a8f33fc017916' 'browser authentication image is version and digest pinned'
assert_contains "${bundled_render}" '--code-challenge-method=S256' 'browser authorization code flow requires PKCE S256'
assert_contains "${bundled_render}" '--cookie-name=__Host-curu_session' 'browser session uses a secure host-prefix cookie'
assert_contains "${bundled_render}" '--cookie-refresh=5m' 'browser session refresh policy is explicit'
assert_contains "${bundled_render}" '--pass-access-token=true' 'OAuth2 Proxy returns the access token only to NGINX auth_request'
assert_contains "${bundled_render}" '--pass-authorization-header=false' 'OAuth2 Proxy does not substitute an ID token'
assert_contains "${bundled_render}" '--redeem-url=http://' 'bundled token redemption uses the trusted cluster-local identity hop'
assert_contains "${bundled_render}" '--skip-claims-from-profile-url=true' 'browser authentication does not send a Bearer token through the public identity hop'
assert_contains "${bundled_render}" 'proxy_pass_request_headers off' 'NGINX denies browser headers by default'
assert_contains "${bundled_render}" 'proxy_set_header Authorization "Bearer $curu_access_token"' 'NGINX injects exactly the access token for Curu'
assert_contains "${bundled_render}" 'proxy_set_header X-Forwarded-For $remote_addr' 'NGINX overwrites the trusted client address'
assert_contains "${bundled_render}" 'proxy_set_header X-CSRF-Token $http_x_csrf_token' 'NGINX allowlists the application CSRF header'
assert_contains "${bundled_render}" 'proxy_set_header HX-Request $http_hx_request' 'NGINX allowlists the HTMX request marker'
assert_contains "${bundled_render}" 'proxy_hide_header X-Auth-Request-Access-Token' 'access token response headers cannot reach the browser'
assert_contains "${bundled_render}" 'auth_request_set $curu_auth_cookie_upstream_1 $upstream_cookie___Host_curu_session_1' 'split refreshed session cookies are propagated'
assert_contains "${bundled_render}" 'proxy_buffer_size 32k' 'auth subrequest accepts bounded refreshed cookie headers'
assert_contains "${bundled_render}" 'add_header HX-Reswap $curu_hx_reswap always' 'gateway expiry preserves HTMX content'
assert_contains "${bundled_render}" 'location ^~ /health/ { return 404; }' 'application readiness is absent from the host route'
assert_contains "${bundled_render}" 'location ^~ /admin/ { return 404; }' 'identity administration is absent from the public identity route'
assert_contains "${bundled_render}" 'add_header Cache-Control "public, max-age=300" always' 'JWKS receives bounded explicit freshness for Curu readiness'
if [[ "$(grep -Fc 'include /etc/nginx/browser-request-headers.conf;' <<<"${bundled_render}")" -ne 6 ]] ||
   [[ "$(grep -Fc 'include /etc/nginx/browser-request-headers.conf;' <<<"${external_render}")" -ne 3 ]]; then
  echo 'every bundled and external proxy location must use the deny-by-default browser header allowlist' >&2
  exit 1
fi
assert_contains "${bundled_render}" 'name: "curu-postgres-credentials-fixture"' 'bundled mode selects its existing scoped Secret'
assert_contains "${external_render}" 'name: "external-curu-runtime-database-fixture"' 'external mode selects its existing Secret'
assert_contains "${bundled_render}" 'kind: StatefulSet' 'bundled mode owns PostgreSQL'
assert_contains "${bundled_render}" 'quay.io/keycloak/keycloak:26.8.2@sha256:4340d05ee7d15be34293868d495ac772d61f1a4d0e43d53bae2e75e2f05de185' 'bundled identity image is version and digest pinned'
assert_contains "${bundled_render}" 'app.kubernetes.io/component: identity' 'bundled mode owns internal Keycloak'
assert_contains "${bundled_render}" '- start' 'Keycloak uses the explicit production start mode'
assert_contains "${bundled_render}" '- --http-enabled=true' 'edge TLS termination uses the declared private HTTP hop'
assert_contains "${bundled_render}" '"access.token.header.type.rfc9068": "true"' 'client requests the RFC 9068 access-token header type'
assert_contains "${bundled_render}" '"clientAuthenticatorType": "client-secret"' 'browser gateway is an explicit confidential client'
assert_contains "${bundled_render}" '"protocolMapper": "oidc-audience-mapper"' 'client has a dedicated audience mapper'
assert_contains "${bundled_render}" '"claim.name": "client_id"' 'client has the required client_id claim mapper'
assert_contains "${bundled_render}" '"user.attribute": "curu_subject"' 'client maps the declared stable subject'
assert_contains "${bundled_render}" 'refusing silent rebinding' 'completed identity bootstrap refuses missing-user recreation'
assert_contains "${bundled_render}" 'name: SSL_CERT_FILE' 'Curu receives explicit issuer CA trust'
assert_contains "${bundled_render}" 'create partialImport' 'development users retain their declared stable subjects'
assert_contains "${bundled_render}" 'helm.sh/hook-weight: "-25"' 'identity reconciliation follows database preparation'
assert_contains "${bundled_render}" 'app.kubernetes.io/component: identity-database-bootstrap' 'Keycloak database authority is available before Helm waits for identity readiness'
assert_contains "${bundled_render}" 'curu.gopher-launch.dev/release-revision: "1"' 'identity database bootstrap Job is versioned for immutable recreation'
if grep -Fq -- '- start-dev' <<<"${bundled_render}"; then
  echo 'bundled Keycloak must not use the insecure development start mode' >&2
  exit 1
fi
assert_contains "${bundled_render}" 'kind: PersistentVolumeClaim' 'bundled mode owns persistent storage claim'
assert_contains "${bundled_render}" 'helm.sh/hook: post-install,post-upgrade' 'role preparation is repeatable on install and upgrade'
assert_contains "${bundled_render}" 'REVOKE ALL ON DATABASE %I FROM PUBLIC' 'role preparation removes PUBLIC database authority'
assert_contains "${bundled_render}" 'NOINHERIT' 'service roles cannot inherit membership authority'
assert_contains "${bundled_render}" 'pg_isready -U postgres -d postgres --port=5432' 'probes check the fixed PostgreSQL port'
assert_contains "${external_render}" 'secretName: "external-postgres-ca-fixture"' 'external CA trust is mounted explicitly'
assert_contains "${bundled_render}" 'curu.gopher-launch.dev/environment: fixture' 'non-reserved custom pod label is retained'
assert_contains "${lifecycle_render}" 'command: ["/curu-migrate"]' 'migration uses the application image command'
assert_contains "${lifecycle_render}" 'command: ["/curu-initialize", "--mode=seed"]' 'initialization mode is explicit'
assert_contains "${lifecycle_render}" 'helm.sh/hook-weight: "-20"' 'migration follows database preparation'
assert_contains "${lifecycle_render}" 'helm.sh/hook-weight: "-10"' 'initializer grants follow migration'
assert_contains "${lifecycle_render}" 'helm.sh/hook-weight: "0"' 'initialization follows grants'
assert_contains "${lifecycle_render}" 'REVOKE ALL PRIVILEGES ON ALL TABLES IN SCHEMA curu' 'initializer grants are reconciled to the narrow surface'
assert_contains "${lifecycle_render}" 'activeDeadlineSeconds: 300' 'lifecycle jobs have a bounded deadline'
assert_contains "${lifecycle_render}" 'key: "migration-url"' 'migration receives its scoped credential'
assert_contains "${lifecycle_render}" 'key: "initializer-url"' 'initialization receives its scoped credential'
assert_contains "${external_lifecycle_render}" 'name: "external-curu-migration-fixture"' 'external lifecycle uses the migration Secret'
assert_contains "${external_lifecycle_render}" 'name: "external-curu-initializer-fixture"' 'external lifecycle uses the initializer Secret'
assert_contains "${external_lifecycle_render}" 'value: "external_curu_initializer"' 'external lifecycle declares the distinct initializer role'
assert_contains "${external_lifecycle_render}" 'value: "external_curu_runtime"' 'external lifecycle declares the distinct runtime role'
assert_contains "${external_lifecycle_render}" 'secretName: "external-postgres-ca-fixture"' 'external lifecycle mounts database CA trust'

if [[ "$(grep -c '^kind: Deployment$' <<<"${lifecycle_render}")" -ne 1 ]]; then
  echo 'only the bundled identity Deployment may be present during data lifecycle operations' >&2
  exit 1
fi
if grep -Fq 'CURU_DATABASE_URL' <<<"${lifecycle_render}"; then
  echo 'lifecycle jobs must not receive the runtime database credential' >&2
  exit 1
fi

if [[ "$(grep -Fc 'curu.gopher-launch.dev/environment: fixture' <<<"${bundled_render}")" -ne 1 ]]; then
  echo 'non-reserved custom pod label must render exactly once' >&2
  exit 1
fi

if [[ "$(grep -Fc 'pg_isready -U postgres -d postgres --port=5432' <<<"${bundled_render}")" -ne 3 ]]; then
  echo 'all three PostgreSQL probes must check the fixed server port' >&2
  exit 1
fi

if grep -Fq 'postgres://' <<<"${bundled_render}${external_render}"; then
  echo 'rendered fixture must not contain a database credential' >&2
  exit 1
fi

if [[ "$(grep -c '^kind: Deployment$' <<<"${bundled_render}")" -ne 4 ]] ||
   [[ "$(grep -c '^kind: StatefulSet$' <<<"${bundled_render}")" -ne 1 ]] ||
   [[ "$(grep -c '^kind: Service$' <<<"${bundled_render}")" -ne 5 ]]; then
  echo 'bundled mode must render Curu, Keycloak, NGINX, and OAuth2 Proxy Deployments, one Postgres StatefulSet, and five Services' >&2
  exit 1
fi
if [[ "$(grep -c 'app.kubernetes.io/component: runtime' <<<"${bundled_render}")" -ne 3 ]]; then
  echo 'runtime Deployment, pod, and Service selectors must share the runtime component label' >&2
  exit 1
fi

if grep -Eq '^kind: (StatefulSet|PersistentVolumeClaim|Job)$' <<<"${external_render}"; then
  echo 'external mode must not provision or administer the external database' >&2
  exit 1
fi

if grep -Fq 'kind: Secret' <<<"${bundled_render}${external_render}"; then
  echo 'rendered fixtures must use existing Secrets and contain no generated credential object' >&2
  exit 1
fi

if grep -Eq '^dependencies:' "${chart_dir}/Chart.yaml"; then
  echo 'Chart.yaml must not declare dependencies' >&2
  exit 1
fi

expect_rejected() {
  local description="$1"
  shift
  if helm template invalid "${chart_dir}" -f "${fixture_dir}/bundled-values.yaml" "$@" >/dev/null 2>&1; then
    echo "expected rejection: ${description}" >&2
    exit 1
  fi
}

expect_rejected 'floating image tag' --set-string image.tag=latest
expect_rejected 'both database modes enabled' --set database.external.enabled=true
expect_rejected 'no database mode enabled' --set database.bundled.enabled=false
expect_rejected 'both identity modes enabled' --set identity.external.enabled=true
expect_rejected 'no identity mode enabled' --set identity.bundled.enabled=false
expect_rejected 'bundled identity issuer mismatch' --set-string trust.issuer=https://identity.curu.test:8443/realms/other --set-string owner.issuer=https://identity.curu.test:8443/realms/other
expect_rejected 'bundled identity JWKS mismatch' --set-string trust.jwksURL=https://identity.curu.test:8443/realms/other/protocol/openid-connect/certs
expect_rejected 'bundled identity audience mismatch' --set-string identity.bundled.audience=other
expect_rejected 'bundled identity owner subject mismatch' --set-string identity.bundled.users.owner.subject=00000000-0000-4000-8000-000000000099
expect_rejected 'excessive identity token lifetime' --set identity.bundled.accessTokenLifespanSeconds=901
expect_rejected 'invalid JWKS publication freshness' --set identity.bundled.jwksCacheMaxAgeSeconds=0
expect_rejected 'duplicate development-user subjects' --set-string identity.bundled.users.other.subject=00000000-0000-4000-8000-000000000001
expect_rejected 'external identity without browser client inputs' --set identity.bundled.enabled=false --set identity.external.enabled=true
expect_rejected 'invalid bundled role identifier' --set-string database.bundled.names.runtimeRole=curu-runtime
expect_rejected 'duplicate bundled service roles' --set-string database.bundled.names.runtimeRole=curu_migration
expect_rejected 'built-in administrator as service role' --set-string database.bundled.names.runtimeRole=postgres
expect_rejected 'predefined PostgreSQL role as service role' --set-string database.bundled.names.runtimeRole=pg_read_all_data
expect_rejected 'duplicate bundled databases' --set-string database.bundled.names.keycloakDatabase=curu
expect_rejected 'maintenance database as service database' --set-string database.bundled.names.curuDatabase=postgres
expect_rejected 'template database as service database' --set-string database.bundled.names.keycloakDatabase=template1
expect_rejected 'non-default bundled PostgreSQL port' --set database.bundled.service.port=55432
expect_rejected 'external mode without endpoint' --set database.bundled.enabled=false --set database.external.enabled=true
expect_rejected 'external verified TLS without CA' -f "${fixture_dir}/external-values.yaml" --set-string database.external.tls.caSecret.name=
expect_rejected 'duplicate external initializer and runtime roles' -f "${fixture_dir}/external-values.yaml" --set-string database.external.initializerRole=curu_runtime
expect_rejected 'owner issuer differing from trusted issuer' --set-string owner.issuer=https://other.test.invalid/realms/curu
expect_rejected 'missing required trust input' --set-string trust.jwksURL=
expect_rejected 'unknown values key' --set-string runtime.authenticationBypass=true
expect_rejected 'chart-owned pod name label override' --set-string 'podLabels.app\.kubernetes\.io/name=rogue'
expect_rejected 'chart-owned pod instance label override' --set-string 'podLabels.app\.kubernetes\.io/instance=rogue'
expect_rejected 'chart-owned pod component label override' --set-string 'podLabels.app\.kubernetes\.io/component=rogue'
expect_rejected 'migration concurrent with runtime' --set dataLifecycle.migrate=true
expect_rejected 'initialization concurrent with runtime' --set-string dataLifecycle.initialize=seed
expect_rejected 'edge exposed during migration' --set runtime.enabled=false --set dataLifecycle.migrate=true
expect_rejected 'public origin differing from edge' --set-string runtime.publicOrigin=https://different.test:8443
expect_rejected 'identical bundled application and identity hosts' --set-string edge.identityHost=curu.test
expect_rejected 'open-ended login URL' --set-string runtime.loginURL=https://curu.test:8443/oauth2/start
expect_rejected 'missing immediate proxy trust' --set-string runtime.trustedProxyCIDRs=
expect_rejected 'implicit initialization mode' --set runtime.enabled=false --set-string dataLifecycle.initialize=automatic

echo 'chart validation passed'
