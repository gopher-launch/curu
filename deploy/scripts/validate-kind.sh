#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

repo_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)

for script in "${repo_root}"/deploy/scripts/*.sh; do
  bash -n "${script}"
done

cluster_template="${repo_root}/deploy/kind/cluster-v1alpha4.yaml.tmpl"
storage_template="${repo_root}/deploy/kind/persistent-volume.yaml.tmpl"

grep -Fq 'apiServerAddress: "127.0.0.1"' "${cluster_template}"
grep -Fq 'listenAddress: "127.0.0.1"' "${cluster_template}"
grep -Fq 'containerPort: 30443' "${cluster_template}"
if grep -Eq 'containerPort: (22|80|443|5432|8080|8443)$' "${cluster_template}"; then
  printf 'unexpected host-facing container port in kind template\n' >&2
  exit 1
fi
grep -Fq 'persistentVolumeReclaimPolicy: Retain' "${storage_template}"
grep -Fq 'provisioner: kubernetes.io/no-provisioner' "${storage_template}"

if "${repo_root}/deploy/scripts/bootstrap-kind.sh" --storage-dir relative \
  --storage-capacity 20Gi --cpus 4 --memory 6g >/dev/null 2>&1; then
  printf 'bootstrap accepted a relative storage directory\n' >&2
  exit 1
fi
if "${repo_root}/deploy/scripts/bootstrap-kind.sh" --storage-dir /tmp/curu-check \
  --storage-capacity 0Gi --cpus 4 --memory 6g >/dev/null 2>&1; then
  printf 'bootstrap accepted an invalid storage capacity\n' >&2
  exit 1
fi
if "${repo_root}/deploy/scripts/bootstrap-kind.sh" --storage-dir /tmp/curu-check \
  --storage-capacity 20Gi --cpus 0 --memory 6g >/dev/null 2>&1; then
  printf 'bootstrap accepted an invalid CPU limit\n' >&2
  exit 1
fi
if "${repo_root}/deploy/scripts/bootstrap-kind.sh" --storage-dir /tmp/curu-check \
  --storage-capacity 20Gi --cpus 4 --memory 6 >/dev/null 2>&1; then
  printf 'bootstrap accepted an invalid memory limit\n' >&2
  exit 1
fi

printf 'kind bootstrap static validation passed\n'
