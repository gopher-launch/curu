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
assert_contains "${bundled_render}" 'name: "curu-postgres-credentials-fixture"' 'bundled mode selects its existing scoped Secret'
assert_contains "${external_render}" 'name: "external-curu-runtime-database-fixture"' 'external mode selects its existing Secret'
assert_contains "${bundled_render}" 'kind: StatefulSet' 'bundled mode owns PostgreSQL'
assert_contains "${bundled_render}" 'kind: PersistentVolumeClaim' 'bundled mode owns persistent storage claim'
assert_contains "${bundled_render}" 'helm.sh/hook: post-install,post-upgrade' 'role preparation is repeatable on install and upgrade'
assert_contains "${bundled_render}" 'REVOKE ALL ON DATABASE %I FROM PUBLIC' 'role preparation removes PUBLIC database authority'
assert_contains "${bundled_render}" 'NOINHERIT' 'service roles cannot inherit membership authority'
assert_contains "${external_render}" 'secretName: "external-postgres-ca-fixture"' 'external CA trust is mounted explicitly'
assert_contains "${bundled_render}" 'curu.gopher-launch.dev/environment: fixture' 'non-reserved custom pod label is retained'

if [[ "$(grep -Fc 'curu.gopher-launch.dev/environment: fixture' <<<"${bundled_render}")" -ne 1 ]]; then
  echo 'non-reserved custom pod label must render exactly once' >&2
  exit 1
fi

if grep -Fq 'postgres://' <<<"${bundled_render}${external_render}"; then
  echo 'rendered fixture must not contain a database credential' >&2
  exit 1
fi

if [[ "$(grep -c '^kind: Deployment$' <<<"${bundled_render}")" -ne 1 ]] ||
   [[ "$(grep -c '^kind: StatefulSet$' <<<"${bundled_render}")" -ne 1 ]] ||
   [[ "$(grep -c '^kind: Service$' <<<"${bundled_render}")" -ne 2 ]]; then
  echo 'bundled mode must render one Curu Deployment, one Postgres StatefulSet, and two Services' >&2
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
expect_rejected 'invalid bundled role identifier' --set-string database.bundled.names.runtimeRole=curu-runtime
expect_rejected 'external mode without endpoint' --set database.bundled.enabled=false --set database.external.enabled=true
expect_rejected 'external verified TLS without CA' -f "${fixture_dir}/external-values.yaml" --set-string database.external.tls.caSecret.name=
expect_rejected 'owner issuer differing from trusted issuer' --set-string owner.issuer=https://other.test.invalid/realms/curu
expect_rejected 'missing required trust input' --set-string trust.jwksURL=
expect_rejected 'unknown values key' --set-string runtime.authenticationBypass=true
expect_rejected 'chart-owned pod name label override' --set-string 'podLabels.app\.kubernetes\.io/name=rogue'
expect_rejected 'chart-owned pod instance label override' --set-string 'podLabels.app\.kubernetes\.io/instance=rogue'

echo 'chart validation passed'
