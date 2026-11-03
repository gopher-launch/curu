#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
# shellcheck source=deploy/scripts/lib-kind.sh
source "${script_dir}/lib-kind.sh"

usage() {
  cat <<'EOF'
Usage: deploy/scripts/load-curu-image.sh [--image curu:TAG] [--no-build]

Builds the repository Dockerfile with the current commit recorded as an OCI
label, then loads that exact local image into curu-local. --no-build requires
the named image to exist locally.
EOF
}

repo_root=$(curu_repo_root)
source_revision=$(git -C "${repo_root}" rev-parse --verify HEAD)
image="curu:${source_revision}"
build=true

while (( $# > 0 )); do
  case "$1" in
    --image) image=${2:-}; shift 2 ;;
    --no-build) build=false; shift ;;
    --help|-h) usage; exit 0 ;;
    *) printf 'unknown argument: %s\n' "$1" >&2; usage >&2; exit 2 ;;
  esac
done

if [[ -z "${image}" || "${image}" == *@* || "${image##*/}" != *:* || "${image}" == *:latest ]]; then
  printf 'image must be a tagged, non-latest local reference\n' >&2
  exit 2
fi

curu_require_command docker
curu_require_command kind
if ! kind get clusters | grep -Fxq "${CURU_CLUSTER_NAME}"; then
  printf 'cluster %s does not exist; run bootstrap-kind.sh first\n' "${CURU_CLUSTER_NAME}" >&2
  exit 1
fi
if [[ -n "$(git -C "${repo_root}" status --porcelain -- Dockerfile go.mod go.sum cmd internal)" ]]; then
  printf 'application image inputs are dirty; commit or remove those changes before building/loading\n' >&2
  exit 1
fi

if [[ "${build}" == true ]]; then
  docker build \
    --label "org.opencontainers.image.revision=${source_revision}" \
    --tag "${image}" \
    "${repo_root}"
else
  docker image inspect "${image}" >/dev/null
fi

recorded_revision=$(docker image inspect --format '{{ index .Config.Labels "org.opencontainers.image.revision" }}' "${image}")
if [[ "${recorded_revision}" != "${source_revision}" ]]; then
  printf 'image revision label %s does not match checkout %s\n' "${recorded_revision:-missing}" "${source_revision}" >&2
  exit 1
fi

kind load docker-image --name "${CURU_CLUSTER_NAME}" "${image}"
docker exec "${CURU_CLUSTER_NAME}-control-plane" crictl inspecti "${image}" >/dev/null
printf 'loaded %s from source revision %s into %s\n' "${image}" "${source_revision}" "${CURU_CLUSTER_NAME}"
