#!/usr/bin/env bash
set -euo pipefail

readonly expected_helm_version='v4.2.4'
readonly cluster_name='curu-postgres-check'
readonly context_name="kind-${cluster_name}"
readonly namespace='cdp003-check'
readonly external_namespace='cdp003-external-check'
readonly release='check'
readonly postgres_image='postgres:17.6-bookworm@sha256:f3bd19c606e442c3d7bdfa8002e03fe260a1023351e0ea4598032022b68dd6e3'
readonly script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
readonly repo_root="$(cd "${script_dir}/../.." && pwd)"
readonly chart_dir="${repo_root}/deploy/helm/curu"

# shellcheck source=lib-kind.sh
source "${script_dir}/lib-kind.sh"

cleanup() {
  kind delete cluster --name "${cluster_name}" >/dev/null 2>&1 || true
}
trap cleanup EXIT

for command in docker helm kind kubectl sha256sum; do
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

kind create cluster --name "${cluster_name}" --image "${CURU_NODE_IMAGE}" --wait 120s
kubectl --context "${context_name}" create namespace "${namespace}"
kubectl --context "${context_name}" create namespace "${external_namespace}"
kubectl --context "${context_name}" apply -f - <<'YAML'
apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  name: cdp003-retained
provisioner: kubernetes.io/no-provisioner
volumeBindingMode: WaitForFirstConsumer
reclaimPolicy: Retain
---
apiVersion: v1
kind: PersistentVolume
metadata:
  name: cdp003-postgres-data
  labels:
    curu.local/storage: cdp003
spec:
  capacity:
    storage: 1Gi
  accessModes: [ReadWriteOnce]
  persistentVolumeReclaimPolicy: Retain
  storageClassName: cdp003-retained
  hostPath:
    path: /var/local-path-provisioner/cdp003-postgres-data
    type: DirectoryOrCreate
YAML

readonly -a helm_values=(
  --set edge.enabled=false
  --set runtime.enabled=false
  --set-string image.repository=postgres
  --set-string image.tag=17.6-bookworm
  --set-string image.pullPolicy=IfNotPresent
  --set-string runtime.publicOrigin=https://curu.test
  --set-string trust.issuer=https://identity.curu.test/realms/curu
  --set-string trust.audience=curu
  --set-string trust.jwksURL=https://identity.curu.test/realms/curu/protocol/openid-connect/certs
  --set-string owner.issuer=https://identity.curu.test/realms/curu
  --set-string owner.subject=owner-check
  --set-string owner.id=00000000-0000-4000-8000-000000000001
  --set identity.bundled.enabled=false
  --set identity.external.enabled=true
  --set-string identity.external.client.id=database-only-check
  --set-string identity.external.client.secret.name=unused-database-only-client
  --set-string database.bundled.storage.storageClassName=cdp003-retained
  --set-string database.bundled.storage.size=1Gi
  --set-string 'database.bundled.storage.selectorLabels.curu\.local/storage=cdp003'
)

helm install "${release}" "${chart_dir}" --kube-context "${context_name}" --namespace "${namespace}" "${helm_values[@]}" --timeout 5m
kubectl --context "${context_name}" --namespace "${namespace}" rollout status "statefulset/${release}-curu-postgres" --timeout=180s

secret_name="${release}-curu-postgres-credentials"
admin_password="$(kubectl --context "${context_name}" --namespace "${namespace}" get secret "${secret_name}" -o jsonpath='{.data.admin-password}' | base64 -d)"
runtime_password="$(kubectl --context "${context_name}" --namespace "${namespace}" get secret "${secret_name}" -o jsonpath='{.data.runtime-password}' | base64 -d)"
keycloak_password="$(kubectl --context "${context_name}" --namespace "${namespace}" get secret "${secret_name}" -o jsonpath='{.data.keycloak-password}' | base64 -d)"
postgres_pod="${release}-curu-postgres-0"

admin_psql() {
  kubectl --context "${context_name}" --namespace "${namespace}" exec "${postgres_pod}" -- env PGPASSWORD="${admin_password}" psql -U postgres -v ON_ERROR_STOP=1 -Atqc "$1" "$2"
}

[[ "$(admin_psql "SELECT count(*) FROM pg_roles WHERE rolname IN ('curu_migration','curu_initializer','curu_runtime','keycloak') AND NOT rolsuper AND NOT rolcreatedb AND NOT rolcreaterole AND NOT rolreplication AND NOT rolbypassrls;" postgres)" == 4 ]]
[[ "$(admin_psql "SELECT count(*) FROM pg_auth_members m JOIN pg_roles r ON r.oid=m.member WHERE r.rolname IN ('curu_migration','curu_initializer','curu_runtime','keycloak');" postgres)" == 0 ]]
[[ "$(admin_psql "SELECT has_database_privilege('curu_runtime','curu','TEMP')::int, has_database_privilege('curu_runtime','curu','CREATE')::int, has_database_privilege('curu_runtime','keycloak','CONNECT')::int;" postgres)" == '0|0|0' ]]
[[ "$(admin_psql "SELECT has_database_privilege('keycloak','keycloak','CONNECT')::int, has_database_privilege('keycloak','curu','CONNECT')::int;" postgres)" == '1|0' ]]
[[ "$(admin_psql "SELECT has_database_privilege('curu_migration','curu','CREATE')::int, has_database_privilege('curu_initializer','curu','CREATE')::int;" postgres)" == '1|0' ]]
[[ "$(admin_psql "SELECT count(*) FROM pg_namespace n CROSS JOIN LATERAL aclexplode(COALESCE(n.nspacl, acldefault('n', n.nspowner))) a WHERE n.nspname='public' AND a.grantee=0 AND a.privilege_type='CREATE';" curu)" == 0 ]]
[[ "$(admin_psql "SELECT has_schema_privilege('curu_runtime','public','CREATE')::int;" curu)" == 0 ]]

if kubectl --context "${context_name}" --namespace "${namespace}" exec "${postgres_pod}" -- env PGPASSWORD="${runtime_password}" psql -h "${release}-curu-postgres" -U curu_runtime -d keycloak -c 'select 1' >/dev/null 2>&1; then
  echo 'runtime role unexpectedly connected to the Keycloak database' >&2
  exit 1
fi
if kubectl --context "${context_name}" --namespace "${namespace}" exec "${postgres_pod}" -- env PGPASSWORD="${keycloak_password}" psql -h "${release}-curu-postgres" -U keycloak -d curu -c 'select 1' >/dev/null 2>&1; then
  echo 'Keycloak role unexpectedly connected to the Curu database' >&2
  exit 1
fi

admin_psql 'CREATE TABLE IF NOT EXISTS public.cdp003_persistence_check(value integer PRIMARY KEY); INSERT INTO public.cdp003_persistence_check VALUES (1) ON CONFLICT DO NOTHING;' postgres >/dev/null
kubectl --context "${context_name}" --namespace "${namespace}" delete pod "${postgres_pod}" --wait=true
kubectl --context "${context_name}" --namespace "${namespace}" rollout status "statefulset/${release}-curu-postgres" --timeout=180s
[[ "$(admin_psql 'SELECT count(*) FROM public.cdp003_persistence_check;' postgres)" == 1 ]]

secret_hash_before="$(kubectl --context "${context_name}" --namespace "${namespace}" get secret "${secret_name}" -o jsonpath='{.data}' | sha256sum | cut -d' ' -f1)"
helm upgrade "${release}" "${chart_dir}" --kube-context "${context_name}" --namespace "${namespace}" "${helm_values[@]}" --timeout 5m
secret_hash_after="$(kubectl --context "${context_name}" --namespace "${namespace}" get secret "${secret_name}" -o jsonpath='{.data}' | sha256sum | cut -d' ' -f1)"
[[ "${secret_hash_before}" == "${secret_hash_after}" ]]

for scoped_key in admin-url migration-url initializer-url runtime-url keycloak-url; do
  scoped_url="$(kubectl --context "${context_name}" --namespace "${namespace}" get secret "${secret_name}" \
    -o "go-template={{index .data \"${scoped_key}\"}}" | base64 -d)"
  scoped_url="${scoped_url/@${release}-curu-postgres:/@${release}-curu-postgres.${namespace}.svc:}"
  scoped_name="${scoped_key%-url}"
  [[ "${scoped_name}" == admin ]] && scoped_name=administration
  kubectl --context "${context_name}" --namespace "${external_namespace}" create secret generic "external-${scoped_name}" --from-literal=database-url="${scoped_url}" >/dev/null
done

helm template external "${chart_dir}" \
  -f "${chart_dir}/tests/fixtures/external-values.yaml" \
  --set-string database.external.host="${release}-curu-postgres.${namespace}.svc" \
  --set-string database.external.tls.mode=disable \
  --set-string database.external.administrationSecret.name=external-administration \
  --set-string database.external.migrationSecret.name=external-migration \
  --set-string database.external.initializerSecret.name=external-initializer \
  --set-string database.external.runtimeSecret.name=external-runtime \
  --set-string database.external.keycloakSecret.name=external-keycloak \
  --set-string database.external.tls.caSecret.name= \
  | kubectl --context "${context_name}" --namespace "${external_namespace}" apply -f - >/dev/null

kubectl --context "${context_name}" --namespace "${external_namespace}" apply -f - <<YAML
apiVersion: v1
kind: Pod
metadata:
  name: external-database-check
spec:
  restartPolicy: Never
  automountServiceAccountToken: false
  containers:
    - name: check
      image: ${postgres_image}
      command: ["sh", "-ec", "psql \"\${DATABASE_URL}\" -Atc 'select current_user || chr(58) || current_database()'"]
      env:
        - name: DATABASE_URL
          valueFrom:
            secretKeyRef:
              name: external-runtime
              key: database-url
YAML
kubectl --context "${context_name}" --namespace "${external_namespace}" wait --for=jsonpath='{.status.phase}'=Succeeded pod/external-database-check --timeout=180s
[[ "$(kubectl --context "${context_name}" --namespace "${external_namespace}" logs external-database-check)" == 'curu_runtime:curu' ]]

echo 'disposable PostgreSQL verification passed'
