#!/usr/bin/env bash
set -euo pipefail

readonly expected_helm_version='v4.2.4'
readonly cluster_name='curu-identity-check'
readonly context_name="kind-${cluster_name}"
readonly namespace='cdp005-check'
readonly release='check'
readonly owner_subject='00000000-0000-4000-8000-000000000001'
readonly other_subject='00000000-0000-4000-8000-000000000002'
readonly owner_id='00000000-0000-4000-8000-000000000001'
readonly identity_host="identity-check.${namespace}.svc.cluster.local"
readonly identity_origin="https://${identity_host}:8443"
readonly nginx_image='nginx:1.28.0-alpine3.21@sha256:09ab424a8c788f8d0fe3a64429f6d19dfa526885c8609b748d0943a75dcb9f8c'
readonly script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
readonly repo_root="$(cd "${script_dir}/../.." && pwd)"
readonly chart_dir="${repo_root}/deploy/helm/curu"
readonly image='curu:cdp005-check'
readonly work_dir="$(mktemp -d)"

# shellcheck source=lib-kind.sh
source "${script_dir}/lib-kind.sh"

identity_forward_pid=''
curu_forward_pid=''
cleanup() {
  local status=$?
  [[ -z "${identity_forward_pid}" ]] || kill "${identity_forward_pid}" >/dev/null 2>&1 || true
  [[ -z "${curu_forward_pid}" ]] || kill "${curu_forward_pid}" >/dev/null 2>&1 || true
  if (( status != 0 )) && kind get clusters 2>/dev/null | grep -Fxq "${cluster_name}"; then
    kubectl --context "${context_name}" --namespace "${namespace}" get pods,jobs >&2 || true
    kubectl --context "${context_name}" --namespace "${namespace}" logs \
      "job/${release}-curu-keycloak-reconcile" --all-containers=true >&2 || true
    kubectl --context "${context_name}" --namespace "${namespace}" logs \
      "deployment/${release}-curu-keycloak" --tail=40 >&2 || true
  fi
  kind delete cluster --name "${cluster_name}" >/dev/null 2>&1 || true
  if [[ "${work_dir}" == /tmp/* && -d "${work_dir}" ]]; then
    rm -rf -- "${work_dir}"
  fi
  return "${status}"
}
trap cleanup EXIT

for command in curl docker helm kind kubectl openssl python3 sha256sum; do
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

openssl req -x509 -newkey rsa:2048 -nodes -days 1 -subj '/CN=Curu disposable identity CA' \
  -keyout "${work_dir}/ca.key" -out "${work_dir}/ca.crt" >/dev/null 2>&1
openssl req -newkey rsa:2048 -nodes -subj "/CN=${identity_host}" \
  -addext "subjectAltName=DNS:${identity_host}" \
  -keyout "${work_dir}/tls.key" -out "${work_dir}/tls.csr" >/dev/null 2>&1
openssl x509 -req -days 1 -sha256 -in "${work_dir}/tls.csr" \
  -CA "${work_dir}/ca.crt" -CAkey "${work_dir}/ca.key" -CAcreateserial \
  -extfile <(printf 'subjectAltName=DNS:%s\n' "${identity_host}") \
  -out "${work_dir}/tls.crt" >/dev/null 2>&1

kubectl --context "${context_name}" --namespace "${namespace}" create configmap curu-test-ca \
  --from-file=ca.crt="${work_dir}/ca.crt" >/dev/null
kubectl --context "${context_name}" --namespace "${namespace}" create secret tls identity-test-tls \
  --cert="${work_dir}/tls.crt" --key="${work_dir}/tls.key" >/dev/null
kubectl --context "${context_name}" apply -f - <<'YAML'
apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  name: cdp005-retained
provisioner: kubernetes.io/no-provisioner
volumeBindingMode: WaitForFirstConsumer
reclaimPolicy: Delete
---
apiVersion: v1
kind: PersistentVolume
metadata:
  name: cdp005-postgres-data
  labels:
    curu.local/storage: cdp005
spec:
  capacity:
    storage: 1Gi
  accessModes: [ReadWriteOnce]
  persistentVolumeReclaimPolicy: Delete
  storageClassName: cdp005-retained
  hostPath:
    path: /var/local-path-provisioner/cdp005-postgres-data
    type: DirectoryOrCreate
YAML

readonly -a base_values=(
  --set-string image.repository=curu
  --set-string image.tag=cdp005-check
  --set-string image.pullPolicy=IfNotPresent
  --set-string runtime.publicOrigin=https://curu.test
  --set-string trust.issuer="${identity_origin}/realms/curu"
  --set-string trust.audience=curu
  --set-string trust.jwksURL="${identity_origin}/realms/curu/protocol/openid-connect/certs"
  --set-string trust.clockSkew=0s
  --set-string owner.issuer="${identity_origin}/realms/curu"
  --set-string owner.subject="${owner_subject}"
  --set-string owner.id="${owner_id}"
  --set-string identity.bundled.hostname="${identity_origin}"
  --set identity.bundled.tokenFixture.enabled=true
  --set-string identity.caTrust.configMapName=curu-test-ca
  --set-string database.bundled.storage.storageClassName=cdp005-retained
  --set-string database.bundled.storage.size=1Gi
  --set-string 'database.bundled.storage.selectorLabels.curu\.local/storage=cdp005'
  --set dataLifecycle.activeDeadlineSeconds=600
  --set dataLifecycle.backoffLimit=0
)
readonly -a lifecycle_values=(
  --set runtime.enabled=false
  --set dataLifecycle.migrate=true
  --set-string dataLifecycle.initialize=seed
)

helm upgrade --install "${release}" "${chart_dir}" \
  --kube-context "${context_name}" --namespace "${namespace}" \
  "${base_values[@]}" "${lifecycle_values[@]}" \
  --wait --wait-for-jobs --timeout 12m
kubectl --context "${context_name}" --namespace "${namespace}" \
  wait --for=condition=available "deployment/${release}-curu-keycloak" --timeout=180s
kubectl --context "${context_name}" --namespace "${namespace}" \
  wait --for=condition=complete "job/${release}-curu-keycloak-reconcile" --timeout=10s

kubectl --context "${context_name}" --namespace "${namespace}" apply -f - <<YAML
apiVersion: v1
kind: ConfigMap
metadata:
  name: identity-test-proxy
data:
  nginx.conf: |
    events {}
    http {
      server {
        listen 8443 ssl;
        server_name ${identity_host};
        ssl_certificate /etc/nginx/tls/tls.crt;
        ssl_certificate_key /etc/nginx/tls/tls.key;
        location / {
          proxy_set_header Host ${identity_host}:8443;
          proxy_set_header X-Forwarded-Host ${identity_host};
          proxy_set_header X-Forwarded-Port 8443;
          proxy_set_header X-Forwarded-Proto https;
          proxy_hide_header Cache-Control;
          add_header Cache-Control "public, max-age=300" always;
          proxy_pass http://${release}-curu-keycloak:8080;
        }
      }
    }
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: identity-test-proxy
spec:
  replicas: 1
  selector:
    matchLabels: {app: identity-test-proxy}
  template:
    metadata:
      labels: {app: identity-test-proxy}
    spec:
      automountServiceAccountToken: false
      containers:
        - name: nginx
          image: ${nginx_image}
          ports: [{name: https, containerPort: 8443}]
          volumeMounts:
            - {name: config, mountPath: /etc/nginx/nginx.conf, subPath: nginx.conf, readOnly: true}
            - {name: tls, mountPath: /etc/nginx/tls, readOnly: true}
      volumes:
        - name: config
          configMap: {name: identity-test-proxy}
        - name: tls
          secret: {secretName: identity-test-tls}
---
apiVersion: v1
kind: Service
metadata:
  name: identity-check
spec:
  type: ClusterIP
  selector: {app: identity-test-proxy}
  ports: [{name: https, port: 8443, targetPort: https}]
YAML
kubectl --context "${context_name}" --namespace "${namespace}" \
  rollout status deployment/identity-test-proxy --timeout=180s

kubectl --context "${context_name}" --namespace "${namespace}" \
  port-forward service/identity-check 18443:8443 >"${work_dir}/identity-forward.log" 2>&1 &
identity_forward_pid=$!
for _ in {1..30}; do
  if curl --silent --fail --cacert "${work_dir}/ca.crt" \
    --resolve "${identity_host}:18443:127.0.0.1" \
    "https://${identity_host}:18443/realms/curu/.well-known/openid-configuration" \
    >"${work_dir}/discovery.json"; then
    break
  fi
  sleep 1
done
if ! kill -0 "${identity_forward_pid}" 2>/dev/null; then
  cat "${work_dir}/identity-forward.log" >&2
  exit 1
fi
if curl --silent --fail --resolve "${identity_host}:18443:127.0.0.1" \
  "https://${identity_host}:18443/realms/curu/.well-known/openid-configuration" >/dev/null 2>&1; then
  echo 'disposable issuer unexpectedly succeeded without its configured CA trust' >&2
  exit 1
fi
python3 -c 'import json,sys; document=json.load(open(sys.argv[1])); expected=sys.argv[2]; assert document["issuer"] == expected; assert document["jwks_uri"] == expected + "/protocol/openid-connect/certs"' \
  "${work_dir}/discovery.json" "${identity_origin}/realms/curu"

keycloak_pod="$(kubectl --context "${context_name}" --namespace "${namespace}" \
  get pod -l app.kubernetes.io/component=identity -o jsonpath='{.items[0].metadata.name}')"
kubectl --context "${context_name}" --namespace "${namespace}" exec "${keycloak_pod}" -- \
  /bin/bash -s <<'SCRIPT'
set -euo pipefail
kcadm=/opt/keycloak/bin/kcadm.sh
config=/tmp/verify-kcadm.config
"${kcadm}" config credentials --config "${config}" --server http://localhost:8080 --realm master \
  --user "${KC_BOOTSTRAP_ADMIN_USERNAME}" --password "${KC_BOOTSTRAP_ADMIN_PASSWORD}" >/dev/null
browser_id="$("${kcadm}" get clients --config "${config}" -r curu -q clientId=curu-browser -F id --format csv --noquotes | tail -n 1)"
SCRIPT

keycloak_user_id() {
  local username="$1"
  kubectl --context "${context_name}" --namespace "${namespace}" exec "${keycloak_pod}" -- \
    env CHECK_USERNAME="${username}" /bin/bash -ec '
      kcadm=/opt/keycloak/bin/kcadm.sh
      config=/tmp/inspect-kcadm.config
      "${kcadm}" config credentials --config "${config}" --server http://localhost:8080 --realm master --user "${KC_BOOTSTRAP_ADMIN_USERNAME}" --password "${KC_BOOTSTRAP_ADMIN_PASSWORD}" >/dev/null
      "${kcadm}" get users --config "${config}" -r curu -q "username=${CHECK_USERNAME}" -q exact=true -F id --format csv --noquotes
    ' | tail -n 1
}
owner_keycloak_id_before="$(keycloak_user_id owner)"
other_keycloak_id_before="$(keycloak_user_id other-user)"

identity_secret="${release}-curu-keycloak-credentials"
identity_hash_before="$(kubectl --context "${context_name}" --namespace "${namespace}" get secret "${identity_secret}" -o jsonpath='{.data}' | sha256sum | cut -d' ' -f1)"
owner_password="$(kubectl --context "${context_name}" --namespace "${namespace}" get secret "${identity_secret}" -o jsonpath='{.data.owner-password}' | base64 -d)"
other_password="$(kubectl --context "${context_name}" --namespace "${namespace}" get secret "${identity_secret}" -o jsonpath='{.data.other-password}' | base64 -d)"

token_request() {
  local client="$1" username="$2" password="$3"
  local response body status
  response="$(curl --silent --show-error --cacert "${work_dir}/ca.crt" \
    --resolve "${identity_host}:18443:127.0.0.1" \
    --data-urlencode grant_type=password --data-urlencode scope=openid \
    --data-urlencode "client_id=${client}" \
    --data-urlencode "username=${username}" --data-urlencode "password=${password}" \
    --write-out $'\n%{http_code}' \
    "https://${identity_host}:18443/realms/curu/protocol/openid-connect/token")"
  body="${response%$'\n'*}"
  status="${response##*$'\n'}"
  if [[ "${status}" != 200 ]]; then
    python3 -c 'import json,sys; value=json.load(sys.stdin); print("token fixture failed: " + value.get("error", "unknown") + ": " + value.get("error_description", "no description"), file=sys.stderr)' <<<"${body}"
    kubectl --context "${context_name}" --namespace "${namespace}" exec "${keycloak_pod}" -- \
      env CHECK_CLIENT="${client}" /bin/bash -ec '
        kcadm=/opt/keycloak/bin/kcadm.sh
        config=/tmp/token-diagnostic.config
        "${kcadm}" config credentials --config "${config}" --server http://localhost:8080 --realm master --user "${KC_BOOTSTRAP_ADMIN_USERNAME}" --password "${KC_BOOTSTRAP_ADMIN_PASSWORD}" >/dev/null
        "${kcadm}" get clients --config "${config}" -r curu -q "clientId=${CHECK_CLIENT}" -F clientId,enabled,publicClient,directAccessGrantsEnabled,clientAuthenticatorType
      ' >&2 || true
    return 1
  fi
  printf '%s' "${body}"
}
json_field() {
  python3 -c 'import json,sys; print(json.load(sys.stdin)[sys.argv[1]])' "$1"
}

owner_response="$(token_request curu-token-fixture owner "${owner_password}")"
other_response="$(token_request curu-token-fixture other-user "${other_password}")"
wrong_response="$(token_request curu-wrong-audience-fixture owner "${owner_password}")"
expired_response="$(token_request curu-expired-token-fixture owner "${owner_password}")"
owner_token="$(json_field access_token <<<"${owner_response}")"
owner_id_token="$(json_field id_token <<<"${owner_response}")"
other_token="$(json_field access_token <<<"${other_response}")"
wrong_token="$(json_field access_token <<<"${wrong_response}")"
expired_token="$(json_field access_token <<<"${expired_response}")"
TOKEN="${owner_token}" EXPECT_ISSUER="${identity_origin}/realms/curu" EXPECT_SUBJECT="${owner_subject}" python3 - <<'PY'
import base64, json, os
header_part, claims_part, _ = os.environ["TOKEN"].split(".")
decode = lambda value: json.loads(base64.urlsafe_b64decode(value + "=" * (-len(value) % 4)))
header, claims = decode(header_part), decode(claims_part)
assert header["alg"] == "RS256" and header["typ"].lower() == "at+jwt" and header["kid"]
assert claims["iss"] == os.environ["EXPECT_ISSUER"]
assert claims["sub"] == os.environ["EXPECT_SUBJECT"]
assert "curu" in ([claims["aud"]] if isinstance(claims["aud"], str) else claims["aud"])
assert claims["client_id"] == "curu-token-fixture" and isinstance(claims["jti"], str)
assert 0 < claims["exp"] - claims["iat"] <= 900
PY
jwks_before="$(curl --silent --fail --cacert "${work_dir}/ca.crt" --resolve "${identity_host}:18443:127.0.0.1" "https://${identity_host}:18443/realms/curu/protocol/openid-connect/certs")"
kid_before="$(python3 -c 'import json,sys; print(json.load(sys.stdin)["keys"][0]["kid"])' <<<"${jwks_before}")"

helm upgrade "${release}" "${chart_dir}" \
  --kube-context "${context_name}" --namespace "${namespace}" \
  "${base_values[@]}" --set runtime.enabled=true --set dataLifecycle.migrate=false \
  --set-string dataLifecycle.initialize=none --wait --wait-for-jobs --timeout 8m
identity_hash_after="$(kubectl --context "${context_name}" --namespace "${namespace}" get secret "${identity_secret}" -o jsonpath='{.data}' | sha256sum | cut -d' ' -f1)"
[[ "${identity_hash_before}" == "${identity_hash_after}" ]]
jwks_after="$(curl --silent --fail --cacert "${work_dir}/ca.crt" --resolve "${identity_host}:18443:127.0.0.1" "https://${identity_host}:18443/realms/curu/protocol/openid-connect/certs")"
kid_after="$(python3 -c 'import json,sys; print(json.load(sys.stdin)["keys"][0]["kid"])' <<<"${jwks_after}")"
[[ "${kid_before}" == "${kid_after}" ]]
kubectl --context "${context_name}" --namespace "${namespace}" exec "${keycloak_pod}" -- /bin/bash -s <<'SCRIPT'
set -euo pipefail
kcadm=/opt/keycloak/bin/kcadm.sh
config=/tmp/verify-kcadm.config
"${kcadm}" config credentials --config "${config}" --server http://localhost:8080 --realm master \
  --user "${KC_BOOTSTRAP_ADMIN_USERNAME}" --password "${KC_BOOTSTRAP_ADMIN_PASSWORD}" >/dev/null
browser_id="$("${kcadm}" get clients --config "${config}" -r curu -q clientId=curu-browser -F id --format csv --noquotes | tail -n 1)"
"${kcadm}" get "clients/${browser_id}" --config "${config}" -r curu -F 'attributes(*)' | grep -F 'curu.unmanaged.fixture' >/dev/null
SCRIPT
[[ "$(keycloak_user_id owner)" == "${owner_keycloak_id_before}" ]]
[[ "$(keycloak_user_id other-user)" == "${other_keycloak_id_before}" ]]

# Password credentials are verification-only token fixtures; CDP-006 still owns the real browser authorization-code flow.
kubectl --context "${context_name}" --namespace "${namespace}" \
  port-forward service/${release}-curu 18080:8080 >"${work_dir}/curu-forward.log" 2>&1 &
curu_forward_pid=$!
for _ in {1..60}; do
  if curl --silent --fail http://127.0.0.1:18080/health/ready >/dev/null; then
    break
  fi
  sleep 1
done
if ! curl --silent --fail http://127.0.0.1:18080/health/ready >/dev/null; then
  kubectl --context "${context_name}" --namespace "${namespace}" logs deployment/${release}-curu >&2
  exit 1
fi

[[ "$(curl --silent --output /dev/null --write-out '%{http_code}' -H "Authorization: Bearer ${owner_token}" http://127.0.0.1:18080/activities)" == 200 ]]
[[ "$(curl --silent --output /dev/null --write-out '%{http_code}' -H "Authorization: Bearer ${other_token}" http://127.0.0.1:18080/activities)" == 403 ]]
[[ "$(curl --silent --output /dev/null --write-out '%{http_code}' -H "Authorization: Bearer ${owner_id_token}" http://127.0.0.1:18080/activities)" == 401 ]]
[[ "$(curl --silent --output /dev/null --write-out '%{http_code}' -H "Authorization: Bearer ${wrong_token}" http://127.0.0.1:18080/activities)" == 401 ]]
sleep 3
[[ "$(curl --silent --output /dev/null --write-out '%{http_code}' -H "Authorization: Bearer ${expired_token}" http://127.0.0.1:18080/activities)" == 401 ]]

echo 'disposable identity verification passed'
