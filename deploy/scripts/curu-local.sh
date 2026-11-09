#!/usr/bin/env bash
set -euo pipefail

readonly expected_helm_version='v4.2.4'
readonly script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
readonly repo_root="$(cd -- "${script_dir}/../.." && pwd)"
readonly chart_dir="${repo_root}/deploy/helm/curu"

usage() {
  cat <<'EOF'
Usage: deploy/scripts/curu-local.sh COMMAND \
  --context CONTEXT --namespace NAMESPACE --release RELEASE \
  --record-dir ABSOLUTE_PATH [--values ABSOLUTE_FILE] [COMMAND OPTIONS]

Commands:
  install       Explicitly migrate, initialize, then launch runtime.
                Requires --values and --initialize seed|empty.
  migrate       Run only migration with runtime and edge disabled.
  initialize    Run only initialization. Requires --initialize seed|empty.
  upgrade       Apply an operation-free chart upgrade. Requires --values.
  backout       Reapply reviewed prior values without a migration rollback.
                Requires --values and --confirm-schema-compatible.
  status        Show release and workload state.
  logs          Show recent logs for all release workloads.
  stop          Scale release Deployments and StatefulSets to zero.
  start         Restore the bundled database and Deployments to one replica.
  remove        Remove the release; retained PVCs and Secrets are not erased.

All operations require an explicit context, namespace, release, and deployment
record directory. Mutating Helm commands use bounded waits and write a
credential-free deployment record. This command never deletes retained data.
EOF
}

if (( $# == 0 )); then usage >&2; exit 2; fi
if [[ "$1" == --help || "$1" == -h ]]; then usage; exit 0; fi
command_name=$1
shift
context=''
namespace=''
release=''
record_dir=''
values_file=''
initialize_mode=''
schema_compatible=false
timeout='10m'

while (( $# > 0 )); do
  case "$1" in
    --context) context=${2:-}; shift 2 ;;
    --namespace) namespace=${2:-}; shift 2 ;;
    --release) release=${2:-}; shift 2 ;;
    --record-dir) record_dir=${2:-}; shift 2 ;;
    --values) values_file=${2:-}; shift 2 ;;
    --initialize) initialize_mode=${2:-}; shift 2 ;;
    --timeout) timeout=${2:-}; shift 2 ;;
    --confirm-schema-compatible) schema_compatible=true; shift ;;
    --help|-h) usage; exit 0 ;;
    *) printf 'unknown argument: %s\n' "$1" >&2; usage >&2; exit 2 ;;
  esac
done

for required in context namespace release record_dir; do
  if [[ -z "${!required}" ]]; then
    printf 'missing required --%s\n' "${required//_/-}" >&2
    exit 2
  fi
done
if [[ ! "${context}" =~ ^[A-Za-z0-9._-]+$ || ! "${namespace}" =~ ^[a-z0-9]([-a-z0-9]*[a-z0-9])?$ ||
      ! "${release}" =~ ^[a-z0-9]([-a-z0-9]*[a-z0-9])?$ ]]; then
  echo 'context, namespace, or release contains unsupported characters' >&2
  exit 2
fi
if [[ "${record_dir}" != /* || "${record_dir}" == / ]]; then
  echo '--record-dir must be an absolute directory below /' >&2
  exit 2
fi
if [[ -n "${values_file}" && ( "${values_file}" != /* || ! -f "${values_file}" ) ]]; then
  echo '--values must name an existing absolute file' >&2
  exit 2
fi
if [[ ! "${timeout}" =~ ^[1-9][0-9]*[sm]$ ]]; then
  echo '--timeout must be a positive duration ending in s or m' >&2
  exit 2
fi

for required_command in helm kubectl git sha256sum python3; do
  command -v "${required_command}" >/dev/null || { echo "required command not found: ${required_command}" >&2; exit 1; }
done
if [[ "$(helm version --short | cut -d+ -f1)" != "${expected_helm_version}" ]]; then
  echo "helm ${expected_helm_version} is required" >&2
  exit 1
fi
kubectl config get-contexts "${context}" >/dev/null
if ! kubectl --context "${context}" get namespace "${namespace}" >/dev/null 2>&1; then
  printf 'namespace %s does not exist in context %s\n' "${namespace}" "${context}" >&2
  exit 1
fi
install -d -m 0750 -- "${record_dir}"

selector="app.kubernetes.io/instance=${release},app.kubernetes.io/name=curu"

diagnostics() {
  local status=${1:-1}
  printf '\noperation failed with exit status %s; bounded diagnostics follow\n' "${status}" >&2
  kubectl --context "${context}" --namespace "${namespace}" \
    get deployments,statefulsets,pods,jobs,services,persistentvolumeclaims -l "${selector}" -o wide >&2 || true
  kubectl --context "${context}" --namespace "${namespace}" get events \
    --field-selector type=Warning --sort-by=.lastTimestamp >&2 || true
  kubectl --context "${context}" --namespace "${namespace}" logs -l "${selector}" \
    --all-containers --prefix --tail=80 --ignore-errors >&2 || true
}

run_helm() {
  local status
  set +e
  helm "$@"
  status=$?
  set -e
  if (( status != 0 )); then
    diagnostics "${status}"
    return "${status}"
  fi
}

record_deployment() {
  local action=$1 record_file tmp values_json image_ref chart_version chart_digest source_revision
  record_file="${record_dir}/deployment.md"
  tmp=$(mktemp "${record_dir}/.deployment.XXXXXX")
  values_json=$(helm get values "${release}" --kube-context "${context}" --namespace "${namespace}" -o json)
  image_ref=$(python3 -c 'import json,sys; v=json.load(sys.stdin); i=v.get("image", {}); print(i.get("repository", "") + ":" + i.get("tag", ""))' <<<"${values_json}")
  chart_version=$(helm show chart "${chart_dir}" | sed -n 's/^version:[[:space:]]*//p')
  chart_digest=$(find "${chart_dir}" -type f -not -path '*/tests/*' -print0 | sort -z | xargs -0 sha256sum | sha256sum | cut -d' ' -f1)
  source_revision=$(git -C "${repo_root}" rev-parse HEAD)
  {
    printf '# Curu deployment record\n\n'
    printf -- '- Recorded at (UTC): `%s`\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
    printf -- '- Last action: `%s`\n' "${action}"
    printf -- '- Context / namespace / release: `%s` / `%s` / `%s`\n' "${context}" "${namespace}" "${release}"
    printf -- '- API server: `%s`\n' "$(kubectl --context "${context}" config view --minify -o jsonpath='{.clusters[0].cluster.server}')"
    printf -- '- Source revision: `%s`\n' "${source_revision}"
    printf -- '- Curu image: `%s`\n' "${image_ref}"
    printf -- '- Chart version / content SHA-256: `%s` / `%s`\n' "${chart_version}" "${chart_digest}"
    if [[ -n "${values_file}" ]]; then
      printf -- '- Site-values path / SHA-256: `%s` / `%s`\n' "${values_file}" "$(sha256sum "${values_file}" | cut -d' ' -f1)"
    else
      printf -- '- Site-values path / SHA-256: not supplied for this action\n'
    fi
    printf -- '- Helm release revision: `%s`\n' "$(helm status "${release}" --kube-context "${context}" --namespace "${namespace}" -o json | python3 -c 'import json,sys; print(json.load(sys.stdin)["version"])')"
    printf -- '- Tool versions: `%s`; `%s`\n' "$(helm version --short)" "$(kubectl version --client -o json | python3 -c 'import json,sys; print(json.load(sys.stdin)["clientVersion"]["gitVersion"])')"
    printf '\nThis record contains input identities and hashes only. It deliberately excludes values content, Secret data, tokens, and passwords.\n'
  } >"${tmp}"
  chmod 0640 "${tmp}"
  mv -f -- "${tmp}" "${record_file}"
  printf 'deployment record: %s\n' "${record_file}"
}

require_values() {
  [[ -n "${values_file}" ]] || { echo "${command_name} requires --values" >&2; exit 2; }
}
require_initialize() {
  [[ "${initialize_mode}" == seed || "${initialize_mode}" == empty ]] || {
    echo "${command_name} requires --initialize seed|empty" >&2
    exit 2
  }
}

helm_scope=(--kube-context "${context}" --namespace "${namespace}")
runtime_off=(--set edge.enabled=false --set runtime.enabled=false)
operations_off=(--set dataLifecycle.migrate=false --set-string dataLifecycle.initialize=none)

case "${command_name}" in
  install)
    require_values
    require_initialize
    run_helm upgrade --install "${release}" "${chart_dir}" "${helm_scope[@]}" --values "${values_file}" \
      "${runtime_off[@]}" --set dataLifecycle.migrate=true --set-string "dataLifecycle.initialize=${initialize_mode}" \
      --wait --wait-for-jobs --timeout "${timeout}"
    run_helm upgrade "${release}" "${chart_dir}" "${helm_scope[@]}" --values "${values_file}" \
      --set edge.enabled=true --set runtime.enabled=true "${operations_off[@]}" \
      --wait --wait-for-jobs --timeout "${timeout}"
    record_deployment install
    ;;
  migrate)
    require_values
    run_helm upgrade "${release}" "${chart_dir}" "${helm_scope[@]}" --values "${values_file}" \
      "${runtime_off[@]}" --set dataLifecycle.migrate=true --set-string dataLifecycle.initialize=none \
      --wait --wait-for-jobs --timeout "${timeout}"
    record_deployment migrate
    ;;
  initialize)
    require_values
    require_initialize
    run_helm upgrade "${release}" "${chart_dir}" "${helm_scope[@]}" --values "${values_file}" \
      "${runtime_off[@]}" --set dataLifecycle.migrate=false --set-string "dataLifecycle.initialize=${initialize_mode}" \
      --wait --wait-for-jobs --timeout "${timeout}"
    record_deployment "initialize-${initialize_mode}"
    ;;
  upgrade)
    require_values
    run_helm upgrade "${release}" "${chart_dir}" "${helm_scope[@]}" --values "${values_file}" \
      --set edge.enabled=true --set runtime.enabled=true "${operations_off[@]}" \
      --wait --wait-for-jobs --timeout "${timeout}"
    record_deployment upgrade
    ;;
  backout)
    require_values
    if [[ "${schema_compatible}" != true ]]; then
      echo 'backout requires --confirm-schema-compatible after checking the prior image against the resulting schema' >&2
      exit 2
    fi
    run_helm upgrade "${release}" "${chart_dir}" "${helm_scope[@]}" --values "${values_file}" \
      --set edge.enabled=true --set runtime.enabled=true "${operations_off[@]}" \
      --wait --wait-for-jobs --timeout "${timeout}"
    record_deployment backout
    ;;
  status)
    helm status "${release}" "${helm_scope[@]}"
    kubectl --context "${context}" --namespace "${namespace}" \
      get deployments,statefulsets,pods,jobs,services,persistentvolumeclaims -l "${selector}" -o wide
    ;;
  logs)
    kubectl --context "${context}" --namespace "${namespace}" logs -l "${selector}" \
      --all-containers --prefix --tail=200 --ignore-errors
    ;;
  stop)
    kubectl --context "${context}" --namespace "${namespace}" scale deployment,statefulset \
      -l "${selector}" --replicas=0 --timeout="${timeout}"
    stopped=false
    for _ in {1..120}; do
      if [[ "$(kubectl --context "${context}" --namespace "${namespace}" get pod -l "${selector}" \
        --field-selector=status.phase=Running -o name | wc -l)" == 0 ]]; then
        stopped=true
        break
      fi
      sleep 1
    done
    [[ "${stopped}" == true ]] || { diagnostics 1; exit 1; }
    ;;
  start)
    kubectl --context "${context}" --namespace "${namespace}" scale statefulset \
      -l "${selector}" --replicas=1 --timeout="${timeout}"
    if kubectl --context "${context}" --namespace "${namespace}" get statefulset -l "${selector}" -o name | grep -q .; then
      kubectl --context "${context}" --namespace "${namespace}" rollout status statefulset \
        -l "${selector}" --timeout="${timeout}" || { diagnostics 1; exit 1; }
    fi
    kubectl --context "${context}" --namespace "${namespace}" scale deployment \
      -l "${selector}" --replicas=1 --timeout="${timeout}"
    kubectl --context "${context}" --namespace "${namespace}" wait deployment -l "${selector}" \
      --for=condition=Available --timeout="${timeout}" || { diagnostics 1; exit 1; }
    ;;
  remove)
    run_helm uninstall "${release}" "${helm_scope[@]}" --wait --timeout "${timeout}"
    ;;
  *) printf 'unknown command: %s\n' "${command_name}" >&2; usage >&2; exit 2 ;;
esac
