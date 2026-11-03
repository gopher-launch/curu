#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
# shellcheck source=deploy/scripts/lib-kind.sh
source "${script_dir}/lib-kind.sh"

usage() {
  cat <<'EOF'
Usage:
  deploy/scripts/configure-host-trust.sh check --ca-cert ABSOLUTE_PATH [--https-port 8443]
  deploy/scripts/configure-host-trust.sh install --ca-cert ABSOLUTE_PATH
  deploy/scripts/configure-host-trust.sh remove

install explicitly changes /etc/hosts and Ubuntu's system CA store using sudo.
remove deletes only the managed Curu hosts line and CA file. Browser-specific
stores still require an explicit import/removal by the operator.
EOF
}

action=${1:-}
[[ -n "${action}" ]] && shift
if [[ "${action}" == '--help' || "${action}" == '-h' ]]; then
  usage
  exit 0
fi
ca_cert=''
https_port='8443'
while (( $# > 0 )); do
  case "$1" in
    --ca-cert) ca_cert=${2:-}; shift 2 ;;
    --https-port) https_port=${2:-}; shift 2 ;;
    --help|-h) usage; exit 0 ;;
    *) printf 'unknown argument: %s\n' "$1" >&2; usage >&2; exit 2 ;;
  esac
done

hosts_line="127.0.0.1 ${CURU_APP_HOST} ${CURU_IDENTITY_HOST} # curu-local"
trust_path='/usr/local/share/ca-certificates/curu-local-ca.crt'

case "${action}" in
  check)
    curu_require_command cmp
    curu_require_command curl
    curu_require_command openssl
    [[ -n "${ca_cert}" ]] || { usage >&2; exit 2; }
    [[ "${ca_cert}" == /* && -f "${ca_cert}" ]] || { printf 'CA certificate must be an absolute existing file\n' >&2; exit 2; }
    grep -Fxq "${hosts_line}" /etc/hosts || { printf 'managed hosts line is absent\n' >&2; exit 1; }
    [[ -f "${trust_path}" ]] && cmp --silent "${ca_cert}" "${trust_path}" || {
      printf 'Ubuntu CA store does not contain the selected Curu CA\n' >&2
      exit 1
    }
    openssl verify -CAfile "${ca_cert}" "${ca_cert}" >/dev/null
    curu_validate_https_port "${https_port}"
    curl --fail --silent --show-error \
      --resolve "${CURU_APP_HOST}:${https_port}:127.0.0.1" "https://${CURU_APP_HOST}:${https_port}/" >/dev/null
    printf 'host names and HTTPS trust are working\n'
    ;;
  install)
    curu_require_command cmp
    curu_require_command openssl
    curu_require_command sudo
    curu_require_command update-ca-certificates
    [[ -n "${ca_cert}" ]] || { usage >&2; exit 2; }
    [[ "${ca_cert}" == /* && -f "${ca_cert}" ]] || { printf 'CA certificate must be an absolute existing file\n' >&2; exit 2; }
    openssl x509 -in "${ca_cert}" -noout >/dev/null
    if { grep -Fq "${CURU_APP_HOST}" /etc/hosts || grep -Fq "${CURU_IDENTITY_HOST}" /etc/hosts; } && \
       ! grep -Fxq "${hosts_line}" /etc/hosts; then
      printf 'refusing to override an existing Curu hostname entry in /etc/hosts\n' >&2
      exit 1
    fi
    if [[ -e "${trust_path}" ]] && ! cmp --silent "${ca_cert}" "${trust_path}"; then
      printf 'refusing to overwrite a different managed CA at %s\n' "${trust_path}" >&2
      exit 1
    fi
    sudo -v
    if ! grep -Fxq "${hosts_line}" /etc/hosts; then
      printf '%s\n' "${hosts_line}" | sudo tee -a /etc/hosts >/dev/null
    fi
    sudo install -m 0644 -- "${ca_cert}" "${trust_path}"
    sudo update-ca-certificates
    printf 'installed Curu name and CA trust; import %s explicitly if the browser uses its own trust store\n' "${ca_cert}"
    ;;
  remove)
    curu_require_command sudo
    curu_require_command update-ca-certificates
    temp_hosts=$(mktemp)
    trap 'rm -f -- "${temp_hosts}"' EXIT
    grep -Fvx "${hosts_line}" /etc/hosts >"${temp_hosts}" || true
    sudo cp -- "${temp_hosts}" /etc/hosts
    if [[ -e "${trust_path}" ]]; then
      sudo rm -f -- "${trust_path}"
    fi
    sudo update-ca-certificates
    printf 'removed the managed Curu hosts line and Ubuntu CA file\n'
    ;;
  *) usage >&2; exit 2 ;;
esac
