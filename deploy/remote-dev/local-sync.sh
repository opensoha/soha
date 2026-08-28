#!/bin/sh
set -eu

script_dir="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
repo_dir="$(CDPATH= cd -- "$script_dir/../.." && pwd)"
workspace_dir="$(CDPATH= cd -- "$repo_dir/.." && pwd)"
context="${KUBE_CONTEXT:-kubernetes-admin@kubernetes}"
namespace="opensoha"
base_url="${BASE_URL:-https://ops.popicorns.com}"
state_dir=""
devspace_pid=""
remote_prepared=false

require_file() {
	[ -f "$1" ] || { printf 'missing local source file: %s\n' "$1" >&2; exit 1; }
}

run_and_record() {
	state_file="$1"
	shift
	child_pid=""
	trap '
		trap - INT TERM
		[ -z "$child_pid" ] || kill "$child_pid" 2>/dev/null || true
		[ -z "$child_pid" ] || wait "$child_pid" 2>/dev/null || true
		exit 143
	' INT TERM
	status=0
	"$@" &
	child_pid=$!
	wait "$child_pid" || status=$?
	printf '%s\n' "$status" > "$state_file"
}

cleanup() {
	status=$?
	trap - EXIT INT TERM
	[ -z "$devspace_pid" ] || kill "$devspace_pid" 2>/dev/null || true
	[ -z "$devspace_pid" ] || wait "$devspace_pid" 2>/dev/null || true
	if [ "$remote_prepared" = true ]; then
		KUBE_CONTEXT="$context" BASE_URL="$base_url" sh "$script_dir/manage.sh" down \
			|| printf 'failed to restore the stable remote workload\n' >&2
	fi
	rm -f "$state_dir/devspace"
	rmdir "$state_dir" 2>/dev/null || true
	exit "$status"
}

command -v devspace >/dev/null || { printf 'devspace is required for local source sync\n' >&2; exit 1; }
require_file "$repo_dir/go.mod"
require_file "$workspace_dir/soha-web/package-lock.json"
require_file "$workspace_dir/soha-contracts/go.mod"

cd "$repo_dir"
devspace print --skip-info >/dev/null

state_dir="$(mktemp -d "${TMPDIR:-/tmp}/soha-remote-dev-sync.XXXXXX")"
trap 'exit 130' INT TERM
trap cleanup EXIT

remote_prepared=true
pod="$(KUBE_CONTEXT="$context" BASE_URL="$base_url" sh "$script_dir/manage.sh" prepare-local)"
[ -n "$pod" ] || { printf 'remote development pod was not created\n' >&2; exit 1; }

run_and_record "$state_dir/devspace" \
	devspace dev \
		--skip-deploy \
		--kube-context "$context" \
		--namespace "$namespace" \
		--no-colors &
devspace_pid=$!

attempts=400
while [ "$attempts" -gt 0 ]; do
	if [ -f "$state_dir/devspace" ]; then
		status="$(sed -n '1p' "$state_dir/devspace")"
		printf 'DevSpace stopped before the remote workload became ready (status %s)\n' "$status" >&2
		[ "$status" -ne 0 ] && exit "$status"
		exit 1
	fi
	deployment_status="$(kubectl --context "$context" --namespace "$namespace" get deployment soha-dev \
		-o jsonpath='{.status.observedGeneration} {.metadata.generation} {.status.readyReplicas} {.spec.replicas}' 2>/dev/null || true)"
	set -- $deployment_status
	if [ "${1:-}" = "${2:-x}" ] && [ "${3:-0}" = "${4:-x}" ] && [ "${4:-0}" -gt 0 ]; then
		break
	fi
	attempts=$((attempts - 1))
	sleep 3
done

[ "$attempts" -gt 0 ] || { printf 'remote workload did not become ready within 20 minutes\n' >&2; exit 1; }
KUBE_CONTEXT="$context" BASE_URL="$base_url" sh "$script_dir/manage.sh" activate-local

printf 'local source sync is active at %s; press Ctrl-C to restore the stable workload\n' "$base_url"

while [ ! -f "$state_dir/devspace" ]; do
	sleep 1
done

status="$(sed -n '1p' "$state_dir/devspace")"
printf 'DevSpace sync stopped with status %s\n' "$status" >&2
[ "$status" -ne 0 ] && exit "$status"
exit 1
