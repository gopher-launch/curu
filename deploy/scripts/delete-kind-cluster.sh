#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
# shellcheck source=deploy/scripts/lib-kind.sh
source "${script_dir}/lib-kind.sh"

curu_require_command kind
kind delete cluster --name "${CURU_CLUSTER_NAME}"
printf 'deleted cluster %s; host storage, PKI, and host trust were retained\n' "${CURU_CLUSTER_NAME}"
