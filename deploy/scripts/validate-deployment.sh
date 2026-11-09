#!/usr/bin/env bash
set -euo pipefail

readonly script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
readonly repo_root="$(cd -- "${script_dir}/../.." && pwd)"

usage() {
  cat <<'EOF'
Usage: deploy/scripts/validate-deployment.sh [--static-only]

Runs the owned chart/configuration checks and, by default, every focused
disposable kind integration check. Each live check owns a fixed disposable
cluster, refuses an existing cluster with that name, and deletes it on exit.
Use --static-only for the non-cluster preflight; it is not release acceptance.
EOF
}

static_only=false
while (( $# > 0 )); do
  case "$1" in
    --static-only) static_only=true; shift ;;
    --help|-h) usage; exit 0 ;;
    *) printf 'unknown argument: %s\n' "$1" >&2; usage >&2; exit 2 ;;
  esac
done

find "${script_dir}" "${repo_root}/deploy/helm/curu/tests" -type f -name '*.sh' -print0 |
  sort -z | xargs -0 -n1 bash -n
"${repo_root}/deploy/helm/curu/tests/validate.sh"
"${script_dir}/validate-kind.sh"

if [[ "${static_only}" == true ]]; then
  echo 'static deployment validation passed; disposable integration checks were not run'
  exit 0
fi

"${script_dir}/verify-postgres-disposable.sh"
"${script_dir}/verify-data-lifecycle-disposable.sh"
"${script_dir}/verify-identity-disposable.sh"
"${script_dir}/verify-browser-disposable.sh"

echo 'complete deployment validation passed'
