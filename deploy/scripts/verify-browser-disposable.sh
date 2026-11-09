#!/usr/bin/env bash
set -euo pipefail

readonly expected_helm_version='v4.2.4'
readonly cluster_name='curu-browser-check'
readonly context_name="kind-${cluster_name}"
readonly namespace='cdp006-check'
readonly release='check'
readonly public_port='18443'
readonly app_host='curu.test'
readonly identity_host='identity.curu.test'
readonly public_origin="https://${app_host}:${public_port}"
readonly identity_origin="https://${identity_host}:${public_port}"
readonly image='curu:cdp006-check'
readonly script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
readonly repo_root="$(cd "${script_dir}/../.." && pwd)"
readonly chart_dir="${repo_root}/deploy/helm/curu"
readonly work_dir="$(mktemp -d)"

# shellcheck source=lib-kind.sh
source "${script_dir}/lib-kind.sh"

cleanup() {
  local status=$?
  if (( status != 0 )) && kind get clusters 2>/dev/null | grep -Fxq "${cluster_name}"; then
    kubectl --context "${context_name}" --namespace "${namespace}" get pods,jobs,services >&2 || true
    kubectl --context "${context_name}" --namespace "${namespace}" logs deployment/curu-edge --tail=80 >&2 || true
    kubectl --context "${context_name}" --namespace "${namespace}" logs deployment/${release}-curu-oauth2-proxy --tail=80 >&2 || true
    kubectl --context "${context_name}" --namespace "${namespace}" logs deployment/${release}-curu --tail=80 >&2 || true
  fi
  kind delete cluster --name "${cluster_name}" >/dev/null 2>&1 || true
  if [[ "${work_dir}" == /tmp/* && -d "${work_dir}" ]]; then rm -rf -- "${work_dir}" >/dev/null 2>&1 || true; fi
  return "${status}"
}
trap cleanup EXIT

for command in curl docker helm kind kubectl openssl python3 sha256sum; do curu_require_command "${command}"; done
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
sed -e "s/__CLUSTER_NAME__/${cluster_name}/g" \
  -e "s/__HTTPS_PORT__/${public_port}/g" \
  -e "s|__STORAGE_DIR__|${work_dir}/storage|g" \
  "${repo_root}/deploy/kind/cluster-v1alpha4.yaml.tmpl" >"${work_dir}/kind.yaml"
mkdir -p "${work_dir}/storage"
kind create cluster --name "${cluster_name}" --image "${CURU_NODE_IMAGE}" --config "${work_dir}/kind.yaml" --wait 120s
kind load docker-image --name "${cluster_name}" "${image}"
kubectl --context "${context_name}" create namespace "${namespace}"

# Resolve both public names through the chart-owned edge from every pod.
kubectl --context "${context_name}" --namespace kube-system get configmap coredns -o jsonpath='{.data.Corefile}' >"${work_dir}/Corefile"
awk -v app_host="${app_host}" -v identity_host="${identity_host}" -v target="curu-edge.${namespace}.svc.cluster.local" '
  /^[[:space:]]*kubernetes[[:space:]]/ && !inserted {
    print "    rewrite stop name exact " app_host " " target " answer auto"
    print "    rewrite stop name exact " identity_host " " target " answer auto"
    inserted=1
  }
  { print }
  END { if (!inserted) exit 42 }
' "${work_dir}/Corefile" >"${work_dir}/Corefile.rendered"
kubectl --context "${context_name}" --namespace kube-system create configmap coredns \
  --from-file=Corefile="${work_dir}/Corefile.rendered" --dry-run=client -o yaml | \
  kubectl --context "${context_name}" --namespace kube-system apply -f - >/dev/null
kubectl --context "${context_name}" --namespace kube-system rollout restart deployment/coredns >/dev/null
kubectl --context "${context_name}" --namespace kube-system rollout status deployment/coredns --timeout=120s >/dev/null

openssl req -x509 -newkey rsa:2048 -nodes -days 1 -subj '/CN=Curu disposable browser CA' \
  -keyout "${work_dir}/ca.key" -out "${work_dir}/ca.crt" >/dev/null 2>&1
openssl req -newkey rsa:2048 -nodes -subj "/CN=${app_host}" \
  -addext "subjectAltName=DNS:${app_host},DNS:${identity_host}" \
  -keyout "${work_dir}/tls.key" -out "${work_dir}/tls.csr" >/dev/null 2>&1
openssl x509 -req -days 1 -sha256 -in "${work_dir}/tls.csr" \
  -CA "${work_dir}/ca.crt" -CAkey "${work_dir}/ca.key" -CAcreateserial \
  -extfile <(printf 'subjectAltName=DNS:%s,DNS:%s\n' "${app_host}" "${identity_host}") \
  -out "${work_dir}/tls.crt" >/dev/null 2>&1
openssl verify -CAfile "${work_dir}/ca.crt" -verify_hostname "${app_host}" "${work_dir}/tls.crt" >/dev/null
openssl verify -CAfile "${work_dir}/ca.crt" -verify_hostname "${identity_host}" "${work_dir}/tls.crt" >/dev/null

kubectl --context "${context_name}" --namespace "${namespace}" create configmap curu-local-ca \
  --from-file=ca.crt="${work_dir}/ca.crt" >/dev/null
kubectl --context "${context_name}" --namespace "${namespace}" create secret tls curu-local-edge-tls \
  --cert="${work_dir}/tls.crt" --key="${work_dir}/tls.key" >/dev/null
kubectl --context "${context_name}" apply -f - <<'YAML'
apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata: {name: cdp006-retained}
provisioner: kubernetes.io/no-provisioner
volumeBindingMode: WaitForFirstConsumer
reclaimPolicy: Delete
---
apiVersion: v1
kind: PersistentVolume
metadata:
  name: cdp006-postgres-data
  labels: {curu.local/storage: cdp006}
spec:
  capacity: {storage: 1Gi}
  accessModes: [ReadWriteOnce]
  persistentVolumeReclaimPolicy: Delete
  storageClassName: cdp006-retained
  hostPath: {path: /var/local-path-provisioner/cdp006-postgres-data, type: DirectoryOrCreate}
YAML

readonly -a base_values=(
  --set-string image.repository=curu
  --set-string image.tag=cdp006-check
  --set-string image.pullPolicy=IfNotPresent
  --set-string edge.publicOrigin="${public_origin}"
  --set edge.publicPort="${public_port}"
  --set edge.service.port="${public_port}"
  --set-string runtime.publicOrigin="${public_origin}"
  --set-string "runtime.loginURL=${public_origin}/oauth2/start?rd=https%3A%2F%2F${app_host}%3A${public_port}%2Factivities"
  --set-string "runtime.logoutURL=${public_origin}/oauth2/sign_out?rd=https%3A%2F%2F${app_host}%3A${public_port}%2F"
  --set-string trust.issuer="${identity_origin}/realms/curu"
  --set-string trust.audience=curu
  --set-string trust.jwksURL="${identity_origin}/realms/curu/protocol/openid-connect/certs"
  --set-string owner.issuer="${identity_origin}/realms/curu"
  --set-string owner.subject=00000000-0000-4000-8000-000000000001
  --set-string owner.id=00000000-0000-4000-8000-000000000001
  --set-string identity.bundled.hostname="${identity_origin}"
  --set-string 'identity.bundled.client.redirectURIs[0]'="${public_origin}/oauth2/callback"
  --set-string 'identity.bundled.client.webOrigins[0]'="${public_origin}"
  --set identity.bundled.accessTokenLifespanSeconds=60
  --set-string browserAuth.cookie.refresh=20s
  --set-string database.bundled.storage.storageClassName=cdp006-retained
  --set-string database.bundled.storage.size=1Gi
  --set-string 'database.bundled.storage.selectorLabels.curu\.local/storage=cdp006'
  --set dataLifecycle.activeDeadlineSeconds=600
)

helm upgrade --install "${release}" "${chart_dir}" \
  --kube-context "${context_name}" --namespace "${namespace}" "${base_values[@]}" \
  --set edge.enabled=false --set runtime.enabled=false --set dataLifecycle.migrate=true \
  --set-string dataLifecycle.initialize=seed --wait --wait-for-jobs --timeout 12m
helm upgrade "${release}" "${chart_dir}" \
  --kube-context "${context_name}" --namespace "${namespace}" "${base_values[@]}" \
  --set edge.enabled=true --set runtime.enabled=true --set dataLifecycle.migrate=false \
  --set-string dataLifecycle.initialize=none --wait --wait-for-jobs --timeout 12m

session_secret_before="$(kubectl --context "${context_name}" --namespace "${namespace}" get secret \
  "${release}-curu-oauth2-proxy-session" -o jsonpath='{.data.cookie-secret}')"
helm upgrade "${release}" "${chart_dir}" \
  --kube-context "${context_name}" --namespace "${namespace}" "${base_values[@]}" \
  --set edge.enabled=true --set runtime.enabled=true --set dataLifecycle.migrate=false \
  --set-string dataLifecycle.initialize=none --wait --wait-for-jobs --timeout 12m
session_secret_after="$(kubectl --context "${context_name}" --namespace "${namespace}" get secret \
  "${release}-curu-oauth2-proxy-session" -o jsonpath='{.data.cookie-secret}')"
[[ -n "${session_secret_before}" && "${session_secret_before}" == "${session_secret_after}" ]]

for deployment in "${release}-curu" "${release}-curu-keycloak" curu-edge "${release}-curu-oauth2-proxy"; do
  kubectl --context "${context_name}" --namespace "${namespace}" wait --for=condition=available \
    "deployment/${deployment}" --timeout=240s
done

readonly -a curl_common=(
  --silent --show-error --cacert "${work_dir}/ca.crt" --noproxy '*'
  --resolve "${app_host}:${public_port}:127.0.0.1"
  --resolve "${identity_host}:${public_port}:127.0.0.1"
)

for _ in {1..60}; do
  if [[ "$(curl "${curl_common[@]}" --output /dev/null --write-out '%{http_code}' "${public_origin}/activities")" == 401 ]]; then break; fi
  sleep 1
done
[[ "$(curl "${curl_common[@]}" --output /dev/null --write-out '%{http_code}' "${public_origin}/activities")" == 401 ]]
[[ "$(curl "${curl_common[@]}" --output /dev/null --write-out '%{http_code}' "${public_origin}/health/ready")" == 404 ]]
[[ "$(curl "${curl_common[@]}" --output /dev/null --write-out '%{http_code}' "${identity_origin}/admin/")" == 404 ]]
[[ "$(kubectl --context "${context_name}" --namespace "${namespace}" get service -o jsonpath='{range .items[?(@.spec.type=="NodePort")]}{.metadata.name}{"\n"}{end}')" == curu-edge ]]

identity_secret="${release}-curu-keycloak-credentials"
owner_password="$(kubectl --context "${context_name}" --namespace "${namespace}" get secret "${identity_secret}" -o jsonpath='{.data.owner-password}' | base64 -d)"
other_password="$(kubectl --context "${context_name}" --namespace "${namespace}" get secret "${identity_secret}" -o jsonpath='{.data.other-password}' | base64 -d)"

browser_login() {
  local username="$1" password="$2" jar="$3" output="$4" action
  local login_page="${work_dir}/${username}-login.html"
  curl "${curl_common[@]}" --location --cookie-jar "${jar}" --cookie "${jar}" \
    "${public_origin}/oauth2/start?rd=https%3A%2F%2F${app_host}%3A${public_port}%2Factivities" >"${login_page}"
  action="$(python3 - "${login_page}" <<'PY'
import html, re, sys
document = open(sys.argv[1], encoding="utf-8").read()
match = re.search(r'<form[^>]+action="([^"]+)"[^>]*method="post"', document, re.I)
if not match:
    match = re.search(r'<form[^>]+method="post"[^>]+action="([^"]+)"', document, re.I)
assert match, "Keycloak login form not found"
print(html.unescape(match.group(1)))
PY
)"
  curl "${curl_common[@]}" --location --cookie-jar "${jar}" --cookie "${jar}" \
    --data-urlencode "username=${username}" --data-urlencode "password=${password}" \
    --data-urlencode credentialId= "${action}" >"${output}"
}

owner_jar="${work_dir}/owner.cookies"
other_jar="${work_dir}/other.cookies"
browser_login owner "${owner_password}" "${owner_jar}" "${work_dir}/owner-activities.html"
grep -F '<h1>Activities</h1>' "${work_dir}/owner-activities.html" >/dev/null
[[ "$(grep -o 'href="/activities/[^"]*"' "${work_dir}/owner-activities.html" | sort -u | wc -l)" == 44 ]]

detail_path="$(grep -o 'href="/activities/[^"]*"' "${work_dir}/owner-activities.html" | head -n 1 | cut -d'"' -f2)"
curl "${curl_common[@]}" --cookie "${owner_jar}" "${public_origin}${detail_path}" >"${work_dir}/detail.html"
grep -F 'id="progressions-heading"' "${work_dir}/detail.html" >/dev/null
curl "${curl_common[@]}" --cookie "${owner_jar}" --get --data-urlencode q=balance "${public_origin}/activities" >"${work_dir}/search.html"
grep -F 'Results for <strong>balance</strong>' "${work_dir}/search.html" >/dev/null

# Browser credentials and forwarding claims are overwritten; no access-token
# authentication response header is exposed back to the browser.
curl "${curl_common[@]}" --cookie "${owner_jar}" \
  --header 'Authorization: Bearer browser-spoof' --header 'X-Forwarded-For: 203.0.113.7' \
  --header 'X-Forwarded-Port: 444' --header 'X-Auth-Request-User: attacker' \
  --dump-header "${work_dir}/spoof.headers" \
  --output /dev/null "${public_origin}/activities"
grep -F 'HTTP/1.1 200' "${work_dir}/spoof.headers" >/dev/null
if grep -Eiq '^(X-Auth-Request-Access-Token|X-Forwarded-Access-Token|Authorization):' "${work_dir}/spoof.headers"; then
  echo 'edge exposed an authentication response header' >&2
  exit 1
fi

# Observe effective requests at a temporary internal upstream. Each product,
# browser-authentication, and bundled-identity Service is repointed only inside
# this disposable cluster and immediately restored. Arbitrary forwarding and
# identity headers must be absent; only the product hop receives NGINX's Bearer
# token, and each configured hop supplies no browser-controlled client address.
kubectl --context "${context_name}" --namespace "${namespace}" apply -f - <<'YAML' >/dev/null
apiVersion: v1
kind: ConfigMap
metadata: {name: cdp006-header-probe}
data:
  nginx.conf: |
    worker_processes 1;
    pid /tmp/nginx.pid;
    events { worker_connections 64; }
    http {
      map $http_authorization $probe_authorization {
        default unexpected;
        "" absent;
        ~^Bearer\ .+$ trusted-bearer;
      }
      map $http_x_forwarded_for $probe_forwarded_for {
        default trusted-client;
        "" absent;
        "203.0.113.7" spoofed;
      }
      map $http_x_forwarded_user $probe_forwarded_user { "" absent; default present; }
      map $http_x_forwarded_arbitrary $probe_forwarded_arbitrary { "" absent; default present; }
      map $http_x_auth_request_access_token $probe_auth_request_token { "" absent; default present; }
      map $http_forwarded $probe_forwarded { "" absent; default present; }
      server {
        listen 8080;
        location / {
          add_header X-Probe-Authorization $probe_authorization always;
          add_header X-Probe-X-Forwarded-For $probe_forwarded_for always;
          add_header X-Probe-X-Forwarded-User $probe_forwarded_user always;
          add_header X-Probe-X-Forwarded-Arbitrary $probe_forwarded_arbitrary always;
          add_header X-Probe-X-Auth-Request-Access-Token $probe_auth_request_token always;
          add_header X-Probe-Forwarded $probe_forwarded always;
          return 204;
        }
      }
    }
---
apiVersion: apps/v1
kind: Deployment
metadata: {name: cdp006-header-probe}
spec:
  replicas: 1
  selector: {matchLabels: {curu.gopher-launch.dev/header-probe: "true"}}
  template:
    metadata: {labels: {curu.gopher-launch.dev/header-probe: "true"}}
    spec:
      automountServiceAccountToken: false
      securityContext:
        runAsNonRoot: true
        runAsUser: 101
        runAsGroup: 101
        seccompProfile: {type: RuntimeDefault}
      containers:
        - name: probe
          image: nginx:1.29.2-alpine@sha256:b03ccb7431a2e3172f5cbae96d82bd792935f33ecb88fbf2940559e475745c4e
          imagePullPolicy: IfNotPresent
          securityContext:
            allowPrivilegeEscalation: false
            readOnlyRootFilesystem: true
            capabilities: {drop: ["ALL"]}
          ports: [{name: http, containerPort: 8080}]
          volumeMounts:
            - {name: config, mountPath: /etc/nginx/nginx.conf, subPath: nginx.conf, readOnly: true}
            - {name: tmp, mountPath: /tmp}
            - {name: cache, mountPath: /var/cache/nginx}
      volumes:
        - {name: config, configMap: {name: cdp006-header-probe}}
        - {name: tmp, emptyDir: {}}
        - {name: cache, emptyDir: {}}
YAML
kubectl --context "${context_name}" --namespace "${namespace}" wait --for=condition=available \
  deployment/cdp006-header-probe --timeout=120s >/dev/null
runtime_service="${release}-curu"
original_runtime_selector="$(kubectl --context "${context_name}" --namespace "${namespace}" \
  get service "${runtime_service}" -o json | python3 -c \
  'import json,sys; print(json.dumps(json.load(sys.stdin)["spec"]["selector"], separators=(",", ":")))')"
kubectl --context "${context_name}" --namespace "${namespace}" patch service "${runtime_service}" \
  --type=json -p='[{"op":"replace","path":"/spec/selector","value":{"curu.gopher-launch.dev/header-probe":"true"}}]' >/dev/null
for _ in {1..30}; do
  curl "${curl_common[@]}" --cookie "${owner_jar}" \
    --header 'Authorization: Bearer browser-spoof' \
    --header 'Forwarded: for=203.0.113.7;proto=http' \
    --header 'X-Forwarded-For: 203.0.113.7' \
    --header 'X-Forwarded-User: attacker' \
    --header 'X-Forwarded-Arbitrary: attacker' \
    --header 'X-Auth-Request-Access-Token: attacker-token' \
    --dump-header "${work_dir}/probe.headers" --output /dev/null "${public_origin}/activities"
  if grep -Eiq '^X-Probe-Authorization: trusted-bearer' "${work_dir}/probe.headers"; then break; fi
  sleep 1
done
grep -F 'HTTP/1.1 204' "${work_dir}/probe.headers" >/dev/null
grep -Eiq '^X-Probe-Authorization: trusted-bearer' "${work_dir}/probe.headers"
grep -Eiq '^X-Probe-X-Forwarded-For: trusted-client' "${work_dir}/probe.headers"
grep -Eiq '^X-Probe-X-Forwarded-User: absent' "${work_dir}/probe.headers"
grep -Eiq '^X-Probe-X-Forwarded-Arbitrary: absent' "${work_dir}/probe.headers"
grep -Eiq '^X-Probe-X-Auth-Request-Access-Token: absent' "${work_dir}/probe.headers"
grep -Eiq '^X-Probe-Forwarded: absent' "${work_dir}/probe.headers"
kubectl --context "${context_name}" --namespace "${namespace}" patch service "${runtime_service}" \
  --type=json -p="[{\"op\":\"replace\",\"path\":\"/spec/selector\",\"value\":${original_runtime_selector}}]" >/dev/null
for _ in {1..30}; do
  if [[ "$(curl "${curl_common[@]}" --cookie "${owner_jar}" --output /dev/null \
    --write-out '%{http_code}' "${public_origin}/activities")" == 200 ]]; then break; fi
  sleep 1
done
[[ "$(curl "${curl_common[@]}" --cookie "${owner_jar}" --output /dev/null \
  --write-out '%{http_code}' "${public_origin}/activities")" == 200 ]]

oauth_service="${release}-curu-oauth2-proxy"
original_oauth_selector="$(kubectl --context "${context_name}" --namespace "${namespace}" \
  get service "${oauth_service}" -o json | python3 -c \
  'import json,sys; print(json.dumps(json.load(sys.stdin)["spec"]["selector"], separators=(",", ":")))')"
kubectl --context "${context_name}" --namespace "${namespace}" patch service "${oauth_service}" \
  --type=json -p='[{"op":"replace","path":"/spec/selector","value":{"curu.gopher-launch.dev/header-probe":"true"}}]' >/dev/null
for _ in {1..30}; do
  curl "${curl_common[@]}" --header 'Authorization: Bearer browser-spoof' \
    --header 'Forwarded: for=203.0.113.7;proto=http' \
    --header 'X-Forwarded-For: 203.0.113.7' --header 'X-Forwarded-User: attacker' \
    --header 'X-Forwarded-Arbitrary: attacker' --header 'X-Auth-Request-Access-Token: attacker-token' \
    --dump-header "${work_dir}/oauth-probe.headers" --output /dev/null \
    "${public_origin}/oauth2/start?rd=https%3A%2F%2F${app_host}%3A${public_port}%2Factivities"
  if grep -Eiq '^X-Probe-Authorization: absent' "${work_dir}/oauth-probe.headers"; then break; fi
  sleep 1
done
grep -F 'HTTP/1.1 204' "${work_dir}/oauth-probe.headers" >/dev/null
grep -Eiq '^X-Probe-Authorization: absent' "${work_dir}/oauth-probe.headers"
grep -Eiq '^X-Probe-X-Forwarded-For: absent' "${work_dir}/oauth-probe.headers"
grep -Eiq '^X-Probe-X-Forwarded-User: absent' "${work_dir}/oauth-probe.headers"
grep -Eiq '^X-Probe-X-Forwarded-Arbitrary: absent' "${work_dir}/oauth-probe.headers"
grep -Eiq '^X-Probe-X-Auth-Request-Access-Token: absent' "${work_dir}/oauth-probe.headers"
grep -Eiq '^X-Probe-Forwarded: absent' "${work_dir}/oauth-probe.headers"
kubectl --context "${context_name}" --namespace "${namespace}" patch service "${oauth_service}" \
  --type=json -p="[{\"op\":\"replace\",\"path\":\"/spec/selector\",\"value\":${original_oauth_selector}}]" >/dev/null
for _ in {1..30}; do
  if [[ "$(curl "${curl_common[@]}" --output /dev/null --write-out '%{http_code}' \
    "${public_origin}/oauth2/start?rd=https%3A%2F%2F${app_host}%3A${public_port}%2Factivities")" == 302 ]]; then break; fi
  sleep 1
done

identity_service="${release}-curu-keycloak"
original_identity_selector="$(kubectl --context "${context_name}" --namespace "${namespace}" \
  get service "${identity_service}" -o json | python3 -c \
  'import json,sys; print(json.dumps(json.load(sys.stdin)["spec"]["selector"], separators=(",", ":")))')"
kubectl --context "${context_name}" --namespace "${namespace}" patch service "${identity_service}" \
  --type=json -p='[{"op":"replace","path":"/spec/selector","value":{"curu.gopher-launch.dev/header-probe":"true"}}]' >/dev/null
for _ in {1..30}; do
  curl "${curl_common[@]}" --header 'Authorization: Bearer browser-spoof' \
    --header 'Forwarded: for=203.0.113.7;proto=http' \
    --header 'X-Forwarded-For: 203.0.113.7' --header 'X-Forwarded-User: attacker' \
    --header 'X-Forwarded-Arbitrary: attacker' --header 'X-Auth-Request-Access-Token: attacker-token' \
    --dump-header "${work_dir}/identity-probe.headers" --output /dev/null "${identity_origin}/realms/curu"
  if grep -Eiq '^X-Probe-Authorization: absent' "${work_dir}/identity-probe.headers"; then break; fi
  sleep 1
done
grep -F 'HTTP/1.1 204' "${work_dir}/identity-probe.headers" >/dev/null
grep -Eiq '^X-Probe-Authorization: absent' "${work_dir}/identity-probe.headers"
grep -Eiq '^X-Probe-X-Forwarded-For: trusted-client' "${work_dir}/identity-probe.headers"
grep -Eiq '^X-Probe-X-Forwarded-User: absent' "${work_dir}/identity-probe.headers"
grep -Eiq '^X-Probe-X-Forwarded-Arbitrary: absent' "${work_dir}/identity-probe.headers"
grep -Eiq '^X-Probe-X-Auth-Request-Access-Token: absent' "${work_dir}/identity-probe.headers"
grep -Eiq '^X-Probe-Forwarded: absent' "${work_dir}/identity-probe.headers"
kubectl --context "${context_name}" --namespace "${namespace}" patch service "${identity_service}" \
  --type=json -p="[{\"op\":\"replace\",\"path\":\"/spec/selector\",\"value\":${original_identity_selector}}]" >/dev/null
for _ in {1..30}; do
  if [[ "$(curl "${curl_common[@]}" --output /dev/null --write-out '%{http_code}' \
    "${identity_origin}/realms/curu/.well-known/openid-configuration")" == 200 ]]; then break; fi
  sleep 1
done

browser_login other-user "${other_password}" "${other_jar}" "${work_dir}/other-result.html"
[[ "$(curl "${curl_common[@]}" --cookie "${other_jar}" --output /dev/null --write-out '%{http_code}' "${public_origin}/activities")" == 403 ]]

# Trigger the declared proactive refresh, then prove the refreshed browser
# session remains usable after the original 60-second access token has expired.
sleep 25
curl "${curl_common[@]}" --cookie-jar "${owner_jar}" --cookie "${owner_jar}" \
  --dump-header "${work_dir}/refresh.headers" --output /dev/null "${public_origin}/activities"
grep -Eiq '^Set-Cookie: __Host-curu_session(_[01])?=' "${work_dir}/refresh.headers"
sleep 45
[[ "$(curl "${curl_common[@]}" --cookie "${owner_jar}" --output /dev/null --write-out '%{http_code}' "${public_origin}/activities")" == 200 ]]

# Gateway expiry preserves HTMX content through the same signal as Curu's
# application-side boundary fixture. Logout clears only the gateway session.
curl "${curl_common[@]}" --cookie-jar "${owner_jar}" --cookie "${owner_jar}" \
  --output /dev/null "${public_origin}/oauth2/sign_out?rd=https%3A%2F%2F${app_host}%3A${public_port}%2F"
curl "${curl_common[@]}" --cookie "${owner_jar}" --header 'HX-Request: true' \
  --dump-header "${work_dir}/expired.headers" --output "${work_dir}/expired.body" "${public_origin}/activities"
grep -F 'HTTP/1.1 401' "${work_dir}/expired.headers" >/dev/null
grep -Eiq '^HX-Reswap: none' "${work_dir}/expired.headers"
grep -Eiq '^HX-Trigger: curu:authentication-required' "${work_dir}/expired.headers"

echo 'disposable browser authentication verification passed'
