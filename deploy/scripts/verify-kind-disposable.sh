#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
# shellcheck source=deploy/scripts/lib-kind.sh
source "${script_dir}/lib-kind.sh"

usage() {
  cat <<'EOF'
Usage: deploy/scripts/verify-kind-disposable.sh \
  --storage-dir EMPTY_ABSOLUTE_PATH --pki-dir EMPTY_ABSOLUTE_PATH \
  --storage-capacity 2Gi --cpus 2 --memory 3g [--https-port 18443]

Creates curu-local twice, loads the Curu image, and verifies the loopback edge,
retained marker, local TLS, and pod name/trust path. It deletes only the cluster;
the supplied disposable storage and PKI directories remain as evidence.
EOF
}

storage_dir=''
pki_dir=''
storage_capacity=''
node_cpus=''
node_memory=''
https_port='18443'

while (( $# > 0 )); do
  case "$1" in
    --storage-dir) storage_dir=${2:-}; shift 2 ;;
    --pki-dir) pki_dir=${2:-}; shift 2 ;;
    --storage-capacity) storage_capacity=${2:-}; shift 2 ;;
    --cpus) node_cpus=${2:-}; shift 2 ;;
    --memory) node_memory=${2:-}; shift 2 ;;
    --https-port) https_port=${2:-}; shift 2 ;;
    --help|-h) usage; exit 0 ;;
    *) printf 'unknown argument: %s\n' "$1" >&2; usage >&2; exit 2 ;;
  esac
done

for required in storage_dir pki_dir storage_capacity node_cpus node_memory; do
  [[ -n "${!required}" ]] || { printf 'missing required option for %s\n' "${required}" >&2; usage >&2; exit 2; }
done
curu_require_absolute_dir "${storage_dir}" 'storage directory'
curu_require_absolute_dir "${pki_dir}" 'PKI directory'
curu_validate_capacity "${storage_capacity}"
curu_validate_cpus "${node_cpus}"
curu_validate_memory "${node_memory}"
curu_validate_https_port "${https_port}"
for command_name in curl docker kind kubectl openssl; do curu_require_command "${command_name}"; done

if kind get clusters | grep -Fxq "${CURU_CLUSTER_NAME}"; then
  printf 'refusing to use existing cluster %s\n' "${CURU_CLUSTER_NAME}" >&2
  exit 1
fi
if [[ -d "${storage_dir}" ]] && find "${storage_dir}" -mindepth 1 -print -quit | grep -q .; then
  printf 'disposable storage directory must be empty: %s\n' "${storage_dir}" >&2
  exit 1
fi
if [[ -d "${pki_dir}" ]] && find "${pki_dir}" -mindepth 1 -print -quit | grep -q .; then
  printf 'disposable PKI directory must be empty: %s\n' "${pki_dir}" >&2
  exit 1
fi

cluster_created=false
cleanup_cluster() {
  if [[ "${cluster_created}" == true ]] && kind get clusters | grep -Fxq "${CURU_CLUSTER_NAME}"; then
    kind delete cluster --name "${CURU_CLUSTER_NAME}" >/dev/null
  fi
}
trap cleanup_cluster EXIT

bootstrap() {
  "${script_dir}/bootstrap-kind.sh" \
    --storage-dir "${storage_dir}" \
    --storage-capacity "${storage_capacity}" \
    --cpus "${node_cpus}" \
    --memory "${node_memory}" \
    --https-port "${https_port}"
  cluster_created=true
}

apply_marker_resources() {
  curu_kubectl apply -f - <<'EOF'
apiVersion: v1
kind: PersistentVolume
metadata:
  name: cdp002-marker
spec:
  capacity:
    storage: 16Mi
  accessModes: [ReadWriteOnce]
  persistentVolumeReclaimPolicy: Retain
  storageClassName: cdp002-marker
  hostPath:
    path: /var/local-path-provisioner/cdp002-verification
    type: DirectoryOrCreate
---
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: cdp002-marker
  namespace: curu-system
spec:
  accessModes: [ReadWriteOnce]
  storageClassName: cdp002-marker
  volumeName: cdp002-marker
  resources:
    requests:
      storage: 16Mi
EOF
}

run_marker_pod() {
  local mode=$1
  curu_kubectl --namespace "${CURU_NAMESPACE}" delete pod cdp002-marker --ignore-not-found --wait=true >/dev/null
  curu_kubectl apply -f - <<EOF
apiVersion: v1
kind: Pod
metadata:
  name: cdp002-marker
  namespace: ${CURU_NAMESPACE}
spec:
  restartPolicy: Never
  automountServiceAccountToken: false
  securityContext:
    runAsUser: 0
  containers:
    - name: marker
      image: curlimages/curl:8.16.0@sha256:3af101f2b2c560bb7ca1be412a8087c9d382f01915625431b7d84cf166536a3a
      imagePullPolicy: IfNotPresent
      command: ['/bin/sh', '-ceu']
      args:
        - |
          if [ '${mode}' = write ]; then
            printf 'CDP-002 retained marker\n' >/data/marker
            chmod 0644 /data/marker
          else
            grep -Fx 'CDP-002 retained marker' /data/marker
          fi
      volumeMounts:
        - name: data
          mountPath: /data
  volumes:
    - name: data
      persistentVolumeClaim:
        claimName: cdp002-marker
EOF
  curu_kubectl --namespace "${CURU_NAMESPACE}" wait --for=jsonpath='{.status.phase}'=Succeeded pod/cdp002-marker --timeout=120s >/dev/null
}

bootstrap
node_name="${CURU_CLUSTER_NAME}-control-plane"
edge_binding=$(docker inspect --format '{{(index (index .HostConfig.PortBindings "30443/tcp") 0).HostIp}}:{{(index (index .HostConfig.PortBindings "30443/tcp") 0).HostPort}}' "${node_name}")
[[ "${edge_binding}" == "127.0.0.1:${https_port}" ]]
docker inspect --format '{{range .Mounts}}{{println .Source .Destination}}{{end}}' "${node_name}" | \
  grep -Fq "${storage_dir} /var/local-path-provisioner"
"${script_dir}/load-curu-image.sh" --image curu:cdp002-verification

apply_marker_resources
run_marker_pod write
run_marker_pod read
"${script_dir}/delete-kind-cluster.sh"
cluster_created=false
grep -Fxq 'CDP-002 retained marker' "${storage_dir}/cdp002-verification/marker"

bootstrap
"${script_dir}/load-curu-image.sh" --image curu:cdp002-verification --no-build
apply_marker_resources
run_marker_pod read
"${script_dir}/prepare-local-tls.sh" --pki-dir "${pki_dir}"

curu_kubectl --namespace "${CURU_NAMESPACE}" apply -f - <<EOF
apiVersion: v1
kind: ConfigMap
metadata:
  name: cdp002-nginx
data:
  default.conf: |
    server {
      listen 8443 ssl;
      server_name ${CURU_APP_HOST} ${CURU_IDENTITY_HOST};
      ssl_certificate /tls/tls.crt;
      ssl_certificate_key /tls/tls.key;
      location / { return 200 "CDP-002 trusted HTTPS\n"; }
    }
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: cdp002-edge
spec:
  replicas: 1
  selector:
    matchLabels: {app: cdp002-edge}
  template:
    metadata:
      labels: {app: cdp002-edge}
    spec:
      automountServiceAccountToken: false
      containers:
        - name: nginx
          image: nginx:1.29.2-alpine@sha256:b03ccb7431a2e3172f5cbae96d82bd792935f33ecb88fbf2940559e475745c4e
          ports:
            - {name: https, containerPort: 8443}
          volumeMounts:
            - {name: tls, mountPath: /tls, readOnly: true}
            - {name: config, mountPath: /etc/nginx/conf.d, readOnly: true}
      volumes:
        - name: tls
          secret: {secretName: ${CURU_TLS_SECRET}}
        - name: config
          configMap: {name: cdp002-nginx}
---
apiVersion: v1
kind: Service
metadata:
  name: ${CURU_EDGE_SERVICE}
spec:
  type: NodePort
  selector: {app: cdp002-edge}
  ports:
    - name: https
      port: 443
      targetPort: https
      nodePort: ${CURU_EDGE_NODE_PORT}
EOF
curu_kubectl --namespace "${CURU_NAMESPACE}" rollout status deployment/cdp002-edge --timeout=120s >/dev/null

curl --fail --silent --show-error --retry 10 --retry-all-errors --retry-delay 1 \
  --cacert "${pki_dir}/ca.crt" \
  --resolve "${CURU_APP_HOST}:${https_port}:127.0.0.1" \
  "https://${CURU_APP_HOST}:${https_port}/" | grep -Fxq 'CDP-002 trusted HTTPS'

curu_kubectl --namespace "${CURU_NAMESPACE}" apply -f - <<EOF
apiVersion: v1
kind: Pod
metadata:
  name: cdp002-https-client
spec:
  restartPolicy: Never
  automountServiceAccountToken: false
  containers:
    - name: curl
      image: curlimages/curl:8.16.0@sha256:3af101f2b2c560bb7ca1be412a8087c9d382f01915625431b7d84cf166536a3a
      command: ['/bin/sh', '-ceu']
      args:
        - curl --fail --silent --show-error --cacert /trust/ca.crt https://${CURU_APP_HOST}/ | grep -Fx 'CDP-002 trusted HTTPS'
      volumeMounts:
        - {name: trust, mountPath: /trust, readOnly: true}
  volumes:
    - name: trust
      configMap: {name: ${CURU_CA_CONFIG_MAP}}
EOF
curu_kubectl --namespace "${CURU_NAMESPACE}" wait --for=jsonpath='{.status.phase}'=Succeeded pod/cdp002-https-client --timeout=120s >/dev/null

printf 'disposable kind verification passed; retained marker: %s; CA: %s\n' \
  "${storage_dir}/cdp002-verification/marker" "${pki_dir}/ca.crt"
