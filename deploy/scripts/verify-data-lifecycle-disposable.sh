#!/usr/bin/env bash
set -euo pipefail

readonly expected_helm_version='v4.2.4'
readonly cluster_name='curu-data-lifecycle-check'
readonly context_name="kind-${cluster_name}"
readonly namespace='cdp004-check'
readonly release='check'
readonly owner_id='00000000-0000-4000-8000-000000000001'
readonly script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
readonly repo_root="$(cd "${script_dir}/../.." && pwd)"
readonly chart_dir="${repo_root}/deploy/helm/curu"
readonly image="curu:cdp004-check"

# shellcheck source=lib-kind.sh
source "${script_dir}/lib-kind.sh"

cleanup() {
  kind delete cluster --name "${cluster_name}" >/dev/null 2>&1 || true
}
trap cleanup EXIT

for command in docker helm kind kubectl grep; do
  curu_require_command "${command}"
done
curu_require_versions
if [[ "$(helm version --short | cut -d+ -f1)" != "${expected_helm_version}" ]]; then
  echo "helm ${expected_helm_version} is required" >&2
  exit 1
fi
if kind get clusters | grep -Fxq "${cluster_name}"; then
  echo "refusing existing disposable cluster ${cluster_name}" >&2
  exit 1
fi

docker build --tag "${image}" "${repo_root}"
kind create cluster --name "${cluster_name}" --image "${CURU_NODE_IMAGE}" --wait 120s
kind load docker-image --name "${cluster_name}" "${image}"
kubectl --context "${context_name}" create namespace "${namespace}"
kubectl --context "${context_name}" apply -f - <<'YAML'
apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  name: cdp004-retained
provisioner: kubernetes.io/no-provisioner
volumeBindingMode: WaitForFirstConsumer
reclaimPolicy: Delete
---
apiVersion: v1
kind: PersistentVolume
metadata:
  name: cdp004-postgres-data
  labels:
    curu.local/storage: cdp004
spec:
  capacity:
    storage: 1Gi
  accessModes: [ReadWriteOnce]
  persistentVolumeReclaimPolicy: Delete
  storageClassName: cdp004-retained
  hostPath:
    path: /var/local-path-provisioner/cdp004-postgres-data
    type: DirectoryOrCreate
YAML

readonly -a base_values=(
  --set edge.enabled=false
  --set-string image.repository=curu
  --set-string image.tag=cdp004-check
  --set-string image.pullPolicy=IfNotPresent
  --set-string runtime.publicOrigin=https://curu.test
  --set-string trust.issuer=https://identity.curu.test/realms/curu
  --set-string trust.audience=curu
  --set-string trust.jwksURL=https://identity.curu.test/realms/curu/protocol/openid-connect/certs
  --set-string owner.issuer=https://identity.curu.test/realms/curu
  --set-string owner.subject=owner-check
  --set-string owner.id="${owner_id}"
  --set-string database.bundled.storage.storageClassName=cdp004-retained
  --set-string database.bundled.storage.size=1Gi
  --set-string 'database.bundled.storage.selectorLabels.curu\.local/storage=cdp004'
)
readonly -a lifecycle_values=(
  --set runtime.enabled=false
  --set dataLifecycle.migrate=true
  --set-string dataLifecycle.initialize=seed
  --set dataLifecycle.activeDeadlineSeconds=180
  --set dataLifecycle.backoffLimit=0
)

helm upgrade --install "${release}" "${chart_dir}" \
  --kube-context "${context_name}" --namespace "${namespace}" \
  "${base_values[@]}" "${lifecycle_values[@]}" \
  --wait --wait-for-jobs --timeout 8m

for job in postgres-prepare migrate initializer-grant initialize; do
  kubectl --context "${context_name}" --namespace "${namespace}" \
    wait --for=condition=complete "job/${release}-curu-${job}" --timeout=5s
done
kubectl --context "${context_name}" --namespace "${namespace}" \
  logs "job/${release}-curu-migrate" | grep -F '"schema_version":2' >/dev/null
kubectl --context "${context_name}" --namespace "${namespace}" \
  logs "job/${release}-curu-initialize" | grep -F '"outcome":"initialized"' >/dev/null

secret_name="${release}-curu-postgres-credentials"
postgres_pod="${release}-curu-postgres-0"
admin_password="$(kubectl --context "${context_name}" --namespace "${namespace}" get secret "${secret_name}" -o jsonpath='{.data.admin-password}' | base64 -d)"
initializer_password="$(kubectl --context "${context_name}" --namespace "${namespace}" get secret "${secret_name}" -o jsonpath='{.data.initializer-password}' | base64 -d)"

admin_psql() {
  kubectl --context "${context_name}" --namespace "${namespace}" exec "${postgres_pod}" -- \
    env PGPASSWORD="${admin_password}" psql -U postgres -v ON_ERROR_STOP=1 -Atqc "$1" "$2"
}

[[ "$(admin_psql 'SELECT count(*) FROM curu.activities;' curu)" == 44 ]]
[[ "$(admin_psql 'SELECT count(*) FROM curu.activity_progressions;' curu)" == 110 ]]
[[ "$(admin_psql 'SELECT count(*) FROM curu.schema_migrations;' curu)" == 2 ]]
[[ "$(admin_psql "SELECT kind || ':' || seed_identity || ':' || seed_version FROM curu.dataset_initializations;" curu)" == 'seed:curu-coaching-library:1' ]]
[[ "$(admin_psql "SELECT has_schema_privilege('curu_initializer','curu','USAGE')::int, has_schema_privilege('curu_initializer','curu','CREATE')::int, has_table_privilege('curu_initializer','curu.activities','INSERT')::int, has_table_privilege('curu_initializer','curu.activities','UPDATE')::int, has_table_privilege('curu_initializer','curu.activities','DELETE')::int;" curu)" == '1|0|1|0|0' ]]

# Exact retry reruns both commands and must preserve content.
helm upgrade "${release}" "${chart_dir}" \
  --kube-context "${context_name}" --namespace "${namespace}" \
  "${base_values[@]}" "${lifecycle_values[@]}" \
  --wait --wait-for-jobs --timeout 8m
kubectl --context "${context_name}" --namespace "${namespace}" \
  logs "job/${release}-curu-initialize" | grep -F '"outcome":"already_initialized"' >/dev/null
[[ "$(admin_psql 'SELECT count(*) FROM curu.activities;' curu)" == 44 ]]

# A conflicting explicit choice fails and leaves the completed seed untouched.
if helm upgrade "${release}" "${chart_dir}" \
  --kube-context "${context_name}" --namespace "${namespace}" \
  "${base_values[@]}" --set runtime.enabled=false --set dataLifecycle.migrate=false \
  --set-string dataLifecycle.initialize=empty --set dataLifecycle.activeDeadlineSeconds=60 \
  --set dataLifecycle.backoffLimit=0 --wait --wait-for-jobs --timeout 2m; then
  echo 'conflicting initialization choice unexpectedly succeeded' >&2
  exit 1
fi
kubectl --context "${context_name}" --namespace "${namespace}" \
  logs "job/${release}-curu-initialize" | grep -F 'different initialization record' >/dev/null
[[ "$(admin_psql 'SELECT count(*) FROM curu.activities;' curu)" == 44 ]]

# The selected initializer executable rejects missing and incompatible schemas.
admin_psql 'CREATE DATABASE curu_missing OWNER postgres TEMPLATE template0;' postgres >/dev/null
admin_psql 'REVOKE ALL ON DATABASE curu_missing FROM PUBLIC; GRANT CONNECT ON DATABASE curu_missing TO curu_initializer;' postgres >/dev/null
admin_psql 'CREATE DATABASE curu_incompatible OWNER postgres TEMPLATE template0;' postgres >/dev/null
admin_psql 'REVOKE ALL ON DATABASE curu_incompatible FROM PUBLIC; GRANT CONNECT ON DATABASE curu_incompatible TO curu_initializer;' postgres >/dev/null
admin_psql "CREATE SCHEMA curu; CREATE TABLE curu.schema_migrations(version integer PRIMARY KEY, checksum text NOT NULL); INSERT INTO curu.schema_migrations VALUES (1, 'wrong'); GRANT USAGE ON SCHEMA curu TO curu_initializer; GRANT SELECT ON curu.schema_migrations TO curu_initializer;" curu_incompatible >/dev/null
kubectl --context "${context_name}" --namespace "${namespace}" create secret generic incompatible-schema-urls \
  --from-literal="missing=postgres://curu_initializer:${initializer_password}@${release}-curu-postgres:5432/curu_missing?sslmode=disable" \
  --from-literal="incompatible=postgres://curu_initializer:${initializer_password}@${release}-curu-postgres:5432/curu_incompatible?sslmode=disable" >/dev/null

for schema_case in missing incompatible; do
  kubectl --context "${context_name}" --namespace "${namespace}" apply -f - <<YAML
apiVersion: batch/v1
kind: Job
metadata:
  name: schema-${schema_case}
spec:
  backoffLimit: 0
  activeDeadlineSeconds: 60
  template:
    spec:
      restartPolicy: Never
      automountServiceAccountToken: false
      containers:
        - name: initialize
          image: ${image}
          imagePullPolicy: IfNotPresent
          command: ["/curu-initialize", "--mode=seed"]
          env:
            - name: CURU_INITIALIZATION_DATABASE_URL
              valueFrom:
                secretKeyRef: {name: incompatible-schema-urls, key: ${schema_case}}
            - name: CURU_OWNER_ID
              value: ${owner_id}
YAML
  kubectl --context "${context_name}" --namespace "${namespace}" \
    wait --for=jsonpath='{.status.failed}'=1 "job/schema-${schema_case}" --timeout=90s
  kubectl --context "${context_name}" --namespace "${namespace}" \
    logs "job/schema-${schema_case}" | grep -E 'incompatible|compatibility state|checksum' >/dev/null
done

# An operation-free upgrade is the only phase that launches the runtime pod.
helm upgrade "${release}" "${chart_dir}" \
  --kube-context "${context_name}" --namespace "${namespace}" \
  "${base_values[@]}" --set runtime.enabled=true --set dataLifecycle.migrate=false \
  --set-string dataLifecycle.initialize=none --timeout 3m
kubectl --context "${context_name}" --namespace "${namespace}" \
  wait --for=jsonpath='{.status.phase}'=Running "pod" -l app.kubernetes.io/component=runtime --timeout=120s
runtime_pod="$(kubectl --context "${context_name}" --namespace "${namespace}" get pod -l app.kubernetes.io/component=runtime -o jsonpath='{.items[0].metadata.name}')"
runtime_started=false
for _ in {1..30}; do
  if kubectl --context "${context_name}" --namespace "${namespace}" logs "${runtime_pod}" 2>/dev/null | grep -F 'server listening' >/dev/null; then
    runtime_started=true
    break
  fi
  sleep 1
done
if [[ "${runtime_started}" != true ]]; then
  echo 'runtime did not report a compatible-schema startup within 30 seconds' >&2
  exit 1
fi
[[ "$(kubectl --context "${context_name}" --namespace "${namespace}" get deployment "${release}-curu" -o jsonpath='{.spec.template.spec.containers[0].env[0].valueFrom.secretKeyRef.key}')" == 'runtime-url' ]]

echo 'disposable data lifecycle verification passed'
