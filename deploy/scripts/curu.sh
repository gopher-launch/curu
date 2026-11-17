#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage: deploy/scripts/curu.sh COMMAND

Top-level control script for managing the system.

Commands:
  start      Start or restart the system.
  stop       Stop the system.
  delete     Delete all data. This is destructive.
EOF
}

readonly script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
readonly repo_root="$(cd -- "${script_dir}/../.." && pwd)"

case "$1" in
  start)
    site_values=${HOME}/.config/curu/curu-local-values.yaml
    record_dir=${HOME}/.local/state/curu/deployment
    install -d -m 0750 "$(dirname "$site_values")" "$record_dir"
    cp deploy/helm/curu/examples/curu-local-values.yaml "$site_values"
    source_revision=$(git rev-parse HEAD)
    sed -i "s/REPLACE_WITH_FULL_GIT_COMMIT/$source_revision/" "$site_values"

    ${script_dir}/bootstrap-kind.sh   --storage-dir /tmp/curu-kind --storage-capacity 20Gi --cpus 4 --memory 6g --https-port 8443
    ${script_dir}/prepare-local-tls.sh --pki-dir "${HOME}/.local/share/curu/pki"
    ${script_dir}/load-curu-image.sh --image "curu:$source_revision"
    ${script_dir}/curu-local.sh install --context kind-curu-local --namespace curu-system --release curu --record-dir "$record_dir" --values "$site_values" --initialize seed --timeout 5m
    ;;
  stop)
    ${script_dir}/delete-kind-cluster.sh
    ;;
  delete)
    ${script_dir}/reset-kind-storage.sh --storage-dir /tmp/curu-kind --confirm-curu-local-data-loss
    ;;
  *) printf 'unknown command: %s\n' "${command_name}" >&2; usage >&2; exit 2 ;;
esac
