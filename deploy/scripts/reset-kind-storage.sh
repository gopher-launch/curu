#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
# shellcheck source=deploy/scripts/lib-kind.sh
source "${script_dir}/lib-kind.sh"

usage() {
  cat <<'EOF'
Usage: deploy/scripts/reset-kind-storage.sh \
  --storage-dir ABSOLUTE_PATH --confirm-curu-local-data-loss

Deletes the contents of the marked Curu kind storage directory. The curu-local
cluster must already be absent. This is never part of ordinary teardown.
EOF
}

storage_dir=''
confirmed=false
while (( $# > 0 )); do
  case "$1" in
    --storage-dir) storage_dir=${2:-}; shift 2 ;;
    --confirm-curu-local-data-loss) confirmed=true; shift ;;
    --help|-h) usage; exit 0 ;;
    *) printf 'unknown argument: %s\n' "$1" >&2; usage >&2; exit 2 ;;
  esac
done

[[ "${confirmed}" == true && -n "${storage_dir}" ]] || { usage >&2; exit 2; }
curu_require_command kind
curu_require_command docker
curu_require_absolute_dir "${storage_dir}" 'storage directory'
if kind get clusters | grep -Fxq "${CURU_CLUSTER_NAME}"; then
  printf 'delete cluster %s before resetting its storage\n' "${CURU_CLUSTER_NAME}" >&2
  exit 1
fi
if [[ ! -f "${storage_dir}/.curu-kind-storage" ]] || \
   [[ "$(cat -- "${storage_dir}/.curu-kind-storage")" != "${CURU_CLUSTER_NAME}" ]]; then
  printf 'refusing to reset an unmarked directory: %s\n' "${storage_dir}" >&2
  exit 1
fi

docker image inspect "${CURU_NODE_IMAGE}" >/dev/null || {
  printf 'pinned kind node image is absent; run bootstrap successfully before reset\n' >&2
  exit 1
}
docker run --rm \
  --mount "type=bind,src=${storage_dir},dst=/curu-storage" \
  --entrypoint /bin/sh \
  "${CURU_NODE_IMAGE}" \
  -ceu "find /curu-storage -mindepth 1 -maxdepth 1 ! -name '.curu-kind-storage' -exec rm -rf -- {} +"
printf 'deleted retained workload data below %s; the storage marker remains\n' "${storage_dir}"
