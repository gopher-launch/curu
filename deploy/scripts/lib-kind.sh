#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

CURU_KIND_VERSION='v0.30.0'
CURU_KUBECTL_VERSION='v1.34.1'
CURU_NODE_IMAGE='kindest/node:v1.34.0@sha256:7416a61b42b1662ca6ca89f02028ac133a309a2a30ba309614e8ec94d976dc5a'
CURU_CLUSTER_NAME='curu-local'
CURU_CONTEXT='kind-curu-local'
CURU_NAMESPACE='curu-system'
CURU_APP_HOST='curu.test'
CURU_IDENTITY_HOST='identity.curu.test'
CURU_EDGE_SERVICE='curu-edge'
CURU_EDGE_NODE_PORT='30443'
CURU_TLS_SECRET='curu-local-edge-tls'
CURU_CA_CONFIG_MAP='curu-local-ca'

curu_repo_root() {
  local script_dir
  script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
  cd -- "${script_dir}/../.." && pwd
}

curu_require_command() {
  command -v "$1" >/dev/null 2>&1 || {
    printf 'required command not found: %s\n' "$1" >&2
    exit 1
  }
}

curu_require_versions() {
  local actual_kind actual_kubectl
  actual_kind=$(kind version | awk '{print $2}')
  actual_kubectl=$(kubectl version --client -o json | sed -n 's/.*"gitVersion":[[:space:]]*"\([^"]*\)".*/\1/p' | head -n 1)
  if [[ "${actual_kind}" != "${CURU_KIND_VERSION}" ]]; then
    printf 'kind %s is required; found %s\n' "${CURU_KIND_VERSION}" "${actual_kind:-unknown}" >&2
    exit 1
  fi
  if [[ "${actual_kubectl}" != "${CURU_KUBECTL_VERSION}" ]]; then
    printf 'kubectl %s is required; found %s\n' "${CURU_KUBECTL_VERSION}" "${actual_kubectl:-unknown}" >&2
    exit 1
  fi
}

curu_require_absolute_dir() {
  local path=$1 label=$2
  if [[ "${path}" != /* || "${path}" == '/' || ! "${path}" =~ ^/[A-Za-z0-9._/-]+$ ]]; then
    printf '%s must be an absolute directory below / using only letters, digits, dot, underscore, dash, and slash; got: %s\n' "${label}" "${path}" >&2
    exit 1
  fi
}

curu_validate_https_port() {
  local port=$1
  if [[ ! "${port}" =~ ^[0-9]+$ ]] || (( port < 1024 || port > 65535 )); then
    printf 'HTTPS port must be an integer from 1024 through 65535\n' >&2
    exit 1
  fi
}

curu_validate_cpus() {
  local cpus=$1
  if [[ ! "${cpus}" =~ ^[1-9][0-9]*([.][0-9]+)?$ ]]; then
    printf 'CPU limit must be a positive number accepted by docker update --cpus\n' >&2
    exit 1
  fi
}

curu_validate_memory() {
  local memory=$1
  if [[ ! "${memory}" =~ ^[1-9][0-9]*([.][0-9]+)?[kKmMgG]$ ]]; then
    printf 'memory limit must include a k, m, or g suffix, for example 6g\n' >&2
    exit 1
  fi
}

curu_validate_capacity() {
  local capacity=$1
  if [[ ! "${capacity}" =~ ^[1-9][0-9]*(Ki|Mi|Gi|Ti)$ ]]; then
    printf 'storage capacity must be a positive Kubernetes quantity, for example 20Gi\n' >&2
    exit 1
  fi
}

curu_prepare_storage_dir() {
  local storage_dir=$1 marker
  marker="${storage_dir}/.curu-kind-storage"

  if [[ -L "${storage_dir}" ]]; then
    printf 'storage directory must not be a symbolic link: %s\n' "${storage_dir}" >&2
    return 1
  fi
  if [[ -e "${storage_dir}" && ! -d "${storage_dir}" ]]; then
    printf 'storage path is not a directory: %s\n' "${storage_dir}" >&2
    return 1
  fi
  if [[ ! -e "${storage_dir}" ]]; then
    install -d -m 0750 -- "${storage_dir}"
  fi

  if [[ -e "${marker}" || -L "${marker}" ]]; then
    if [[ ! -f "${marker}" || -L "${marker}" ]] || \
       [[ "$(cat -- "${marker}")" != "${CURU_CLUSTER_NAME}" ]]; then
      printf 'storage marker does not belong to %s: %s\n' "${CURU_CLUSTER_NAME}" "${storage_dir}" >&2
      return 1
    fi
    return 0
  fi

  if find "${storage_dir}" -mindepth 1 -maxdepth 1 -print -quit | grep -q .; then
    printf 'refusing to claim non-empty unmarked storage directory: %s\n' "${storage_dir}" >&2
    return 1
  fi

  chmod 0750 "${storage_dir}"
  (set -o noclobber; umask 0027; printf '%s\n' "${CURU_CLUSTER_NAME}" >"${marker}")
}

curu_escape_sed_replacement() {
  printf '%s' "$1" | sed 's/[&|\\]/\\&/g'
}

curu_kubectl() {
  kubectl --context "${CURU_CONTEXT}" "$@"
}
