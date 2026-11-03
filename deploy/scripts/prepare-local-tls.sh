#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
# shellcheck source=deploy/scripts/lib-kind.sh
source "${script_dir}/lib-kind.sh"

usage() {
  cat <<'EOF'
Usage: deploy/scripts/prepare-local-tls.sh --pki-dir ABSOLUTE_PATH

Creates or reuses a local CA and edge certificate for curu.test and
identity.curu.test. The CA private key remains only in the host PKI directory.
The namespace receives the leaf key/certificate Secret and public CA ConfigMap.
EOF
}

pki_dir=''
while (( $# > 0 )); do
  case "$1" in
    --pki-dir) pki_dir=${2:-}; shift 2 ;;
    --help|-h) usage; exit 0 ;;
    *) printf 'unknown argument: %s\n' "$1" >&2; usage >&2; exit 2 ;;
  esac
done

if [[ -z "${pki_dir}" ]]; then
  usage >&2
  exit 2
fi
curu_require_absolute_dir "${pki_dir}" 'PKI directory'
curu_require_command kind
curu_require_command kubectl
curu_require_command openssl
if ! kind get clusters | grep -Fxq "${CURU_CLUSTER_NAME}"; then
  printf 'cluster %s does not exist; run bootstrap-kind.sh first\n' "${CURU_CLUSTER_NAME}" >&2
  exit 1
fi

install -d -m 0700 -- "${pki_dir}"
ancestor=${pki_dir}
while [[ "${ancestor}" != '/' ]]; do
  if [[ -f "${ancestor}/.curu-kind-storage" ]]; then
    printf 'PKI directory must not be inside the kind workload-storage mount: %s\n' "${ancestor}" >&2
    exit 1
  fi
  ancestor=$(dirname -- "${ancestor}")
done
ca_key="${pki_dir}/ca.key"
ca_cert="${pki_dir}/ca.crt"
tls_key="${pki_dir}/tls.key"
tls_csr="${pki_dir}/tls.csr"
tls_cert="${pki_dir}/tls.crt"
serial_file="${pki_dir}/ca.srl"

present=0
for path in "${ca_key}" "${ca_cert}" "${tls_key}" "${tls_cert}"; do
  [[ -e "${path}" ]] && (( present += 1 ))
done
if (( present != 0 && present != 4 )); then
  printf 'partial PKI found in %s; restore all four files or choose an empty directory\n' "${pki_dir}" >&2
  exit 1
fi

if (( present == 0 )); then
  openssl genpkey -algorithm RSA -pkeyopt rsa_keygen_bits:3072 -out "${ca_key}" >/dev/null 2>&1
  chmod 0600 "${ca_key}"
  openssl req -x509 -new -sha256 -days 3650 \
    -key "${ca_key}" -out "${ca_cert}" \
    -subj '/CN=Curu Local Development CA' \
    -addext 'basicConstraints=critical,CA:TRUE,pathlen:0' \
    -addext 'keyUsage=critical,keyCertSign,cRLSign' >/dev/null 2>&1
  openssl genpkey -algorithm RSA -pkeyopt rsa_keygen_bits:2048 -out "${tls_key}" >/dev/null 2>&1
  chmod 0600 "${tls_key}"
  openssl req -new -sha256 -key "${tls_key}" -out "${tls_csr}" \
    -subj '/CN=curu.test' \
    -addext "subjectAltName=DNS:${CURU_APP_HOST},DNS:${CURU_IDENTITY_HOST}" >/dev/null 2>&1
  extension_file=$(mktemp)
  trap 'rm -f -- "${extension_file}"' EXIT
  cat >"${extension_file}" <<EOF
basicConstraints=critical,CA:FALSE
keyUsage=critical,digitalSignature,keyEncipherment
extendedKeyUsage=serverAuth
subjectAltName=DNS:${CURU_APP_HOST},DNS:${CURU_IDENTITY_HOST}
EOF
  openssl x509 -req -sha256 -days 397 \
    -in "${tls_csr}" -CA "${ca_cert}" -CAkey "${ca_key}" -CAcreateserial \
    -extfile "${extension_file}" -out "${tls_cert}" >/dev/null 2>&1
  rm -f -- "${tls_csr}" "${serial_file}"
fi
chmod 0600 "${ca_key}" "${tls_key}"

openssl verify -CAfile "${ca_cert}" -verify_hostname "${CURU_APP_HOST}" "${tls_cert}" >/dev/null
openssl verify -CAfile "${ca_cert}" -verify_hostname "${CURU_IDENTITY_HOST}" "${tls_cert}" >/dev/null

curu_kubectl --namespace "${CURU_NAMESPACE}" create secret tls "${CURU_TLS_SECRET}" \
  --cert "${tls_cert}" --key "${tls_key}" --dry-run=client -o yaml | \
  curu_kubectl --namespace "${CURU_NAMESPACE}" apply -f - >/dev/null
curu_kubectl --namespace "${CURU_NAMESPACE}" create configmap "${CURU_CA_CONFIG_MAP}" \
  --from-file=ca.crt="${ca_cert}" --dry-run=client -o yaml | \
  curu_kubectl --namespace "${CURU_NAMESPACE}" apply -f - >/dev/null

ca_fingerprint=$(openssl x509 -in "${ca_cert}" -noout -fingerprint -sha256 | cut -d= -f2)
printf 'CA certificate: %s\nCA SHA-256: %s\nTLS Secret: %s/%s\nCA ConfigMap: %s/%s\nCA private key retained only at: %s\n' \
  "${ca_cert}" "${ca_fingerprint}" "${CURU_NAMESPACE}" "${CURU_TLS_SECRET}" \
  "${CURU_NAMESPACE}" "${CURU_CA_CONFIG_MAP}" "${ca_key}"
