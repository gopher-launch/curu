#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
# shellcheck source=deploy/scripts/lib-kind.sh
source "${script_dir}/lib-kind.sh"

usage() {
  cat <<'EOF'
Usage: deploy/scripts/bootstrap-kind.sh \
  --storage-dir ABSOLUTE_PATH --storage-capacity 20Gi --cpus 4 --memory 6g \
  [--https-port 8443]

Creates the fixed curu-local kind cluster and curu-system namespace. The storage
directory, declared PV capacity, and Docker CPU/memory limits are mandatory
operator inputs. Only the edge NodePort is mapped, on host loopback.
EOF
}

storage_dir=''
storage_capacity=''
node_cpus=''
node_memory=''
https_port='8443'

while (( $# > 0 )); do
  case "$1" in
    --storage-dir) storage_dir=${2:-}; shift 2 ;;
    --storage-capacity) storage_capacity=${2:-}; shift 2 ;;
    --cpus) node_cpus=${2:-}; shift 2 ;;
    --memory) node_memory=${2:-}; shift 2 ;;
    --https-port) https_port=${2:-}; shift 2 ;;
    --help|-h) usage; exit 0 ;;
    *) printf 'unknown argument: %s\n' "$1" >&2; usage >&2; exit 2 ;;
  esac
done

for required in storage_dir storage_capacity node_cpus node_memory; do
  if [[ -z "${!required}" ]]; then
    printf 'missing required option for %s\n' "${required}" >&2
    usage >&2
    exit 2
  fi
done

curu_require_command docker
curu_require_command kind
curu_require_command kubectl
curu_require_absolute_dir "${storage_dir}" 'storage directory'
curu_validate_capacity "${storage_capacity}"
curu_validate_cpus "${node_cpus}"
curu_validate_memory "${node_memory}"
curu_validate_https_port "${https_port}"
curu_require_versions
docker info >/dev/null

if kind get clusters | grep -Fxq "${CURU_CLUSTER_NAME}"; then
  printf 'cluster %s already exists; refusing to replace it\n' "${CURU_CLUSTER_NAME}" >&2
  exit 1
fi

curu_prepare_storage_dir "${storage_dir}"

repo_root=$(curu_repo_root)
rendered_config=$(mktemp)
rendered_storage=$(mktemp)
cluster_created=false
cleanup() {
  local status=$?
  rm -f -- "${rendered_config}" "${rendered_storage}" "${corefile:-}" "${corefile_rendered:-}"
  if (( status != 0 )) && [[ "${cluster_created}" == true ]]; then
    kind delete cluster --name "${CURU_CLUSTER_NAME}" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT

storage_replacement=$(curu_escape_sed_replacement "${storage_dir}")
sed \
  -e "s|__CLUSTER_NAME__|${CURU_CLUSTER_NAME}|g" \
  -e "s|__HTTPS_PORT__|${https_port}|g" \
  -e "s|__STORAGE_DIR__|${storage_replacement}|g" \
  "${repo_root}/deploy/kind/cluster-v1alpha4.yaml.tmpl" >"${rendered_config}"

kind create cluster \
  --name "${CURU_CLUSTER_NAME}" \
  --image "${CURU_NODE_IMAGE}" \
  --config "${rendered_config}" \
  --wait 120s
cluster_created=true

node_name="${CURU_CLUSTER_NAME}-control-plane"
docker update --cpus "${node_cpus}" --memory "${node_memory}" --memory-swap "${node_memory}" "${node_name}" >/dev/null

curu_kubectl create namespace "${CURU_NAMESPACE}" --dry-run=client -o yaml | curu_kubectl apply -f - >/dev/null
curu_kubectl label namespace "${CURU_NAMESPACE}" app.kubernetes.io/part-of=curu --overwrite >/dev/null
kubectl config set-context "${CURU_CONTEXT}" --namespace "${CURU_NAMESPACE}" >/dev/null

sed -e "s|__STORAGE_CAPACITY__|${storage_capacity}|g" \
  "${repo_root}/deploy/kind/persistent-volume.yaml.tmpl" >"${rendered_storage}"
curu_kubectl apply -f "${rendered_storage}" >/dev/null

corefile=$(mktemp)
corefile_rendered=$(mktemp)
curu_kubectl --namespace kube-system get configmap coredns -o jsonpath='{.data.Corefile}' >"${corefile}"
if ! grep -Fq '# curu-local names' "${corefile}"; then
  awk -v app_host="${CURU_APP_HOST}" -v identity_host="${CURU_IDENTITY_HOST}" -v target="${CURU_EDGE_SERVICE}.${CURU_NAMESPACE}.svc.cluster.local" '
    /^[[:space:]]*kubernetes[[:space:]]/ && ! inserted {
      print "    # curu-local names"
      print "    rewrite stop name exact " app_host " " target " answer auto"
      print "    rewrite stop name exact " identity_host " " target " answer auto"
      inserted=1
    }
    { print }
    END { if (!inserted) exit 42 }
  ' "${corefile}" >"${corefile_rendered}"
  curu_kubectl --namespace kube-system create configmap coredns \
    --from-file=Corefile="${corefile_rendered}" --dry-run=client -o yaml | \
    curu_kubectl --namespace kube-system apply -f - >/dev/null
  curu_kubectl --namespace kube-system rollout restart deployment/coredns >/dev/null
  curu_kubectl --namespace kube-system rollout status deployment/coredns --timeout=120s >/dev/null
fi

printf 'cluster: %s\ncontext: %s\nnamespace: %s\nhttps: https://%s:%s\nidentity: https://%s:%s\nstorage: %s (%s declared)\nresources: %s CPUs, %s memory\n' \
  "${CURU_CLUSTER_NAME}" "${CURU_CONTEXT}" "${CURU_NAMESPACE}" \
  "${CURU_APP_HOST}" "${https_port}" "${CURU_IDENTITY_HOST}" "${https_port}" \
  "${storage_dir}" "${storage_capacity}" "${node_cpus}" "${node_memory}"
