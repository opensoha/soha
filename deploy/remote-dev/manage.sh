#!/bin/sh
set -eu

script_dir="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
context="${KUBE_CONTEXT:-kubernetes-admin@kubernetes}"
namespace="opensoha"
base_url="${BASE_URL:-https://ops.popicorns.com}"
base_host="${base_url#*://}"
base_host="${base_host%%/*}"
base_host="${base_host%%:*}"
soha_ref="${SOHA_REF:-main}"
web_ref="${SOHA_WEB_REF:-main}"
contracts_ref="${SOHA_CONTRACTS_REF:-main}"
replicas_annotation="remote-dev.opensoha.io/stable-replicas"

case "$base_host" in
	''|*[!A-Za-z0-9.-]*) printf 'invalid remote development URL: %s\n' "$base_url" >&2; exit 2 ;;
esac

k() {
	kubectl --context "$context" --namespace "$namespace" "$@"
}

preflight() {
	command -v kubectl >/dev/null
	command -v curl >/dev/null
	kubectl --context "$context" version --request-timeout=10s >/dev/null
	for resource in deployment/soha service/soha secret/soha-config persistentvolumeclaim/soha-data; do
		k get "$resource" >/dev/null
	done
}

wait_for_service_endpoint() {
	mode="$1"
	attempts=90
	while [ "$attempts" -gt 0 ]; do
		names="$(k get endpointslices.discovery.k8s.io -l kubernetes.io/service-name=soha -o jsonpath='{range .items[*].endpoints[*]}{.targetRef.name}{"\n"}{end}' 2>/dev/null || true)"
		case "$mode" in
			dev)
				printf '%s\n' "$names" | grep -q '^soha-dev-' && return 0
				;;
			stable)
				if printf '%s\n' "$names" | grep -q '^soha-' && ! printf '%s\n' "$names" | grep -q '^soha-dev-'; then
					return 0
				fi
				;;
		esac
		attempts=$((attempts - 1))
		sleep 1
	done
	printf 'service endpoint did not switch to %s\n' "$mode" >&2
	return 1
}

switch_service() {
	mode="$1"
	case "$mode" in
		dev) name=soha-dev ;;
		stable) name=soha ;;
		*) printf 'unknown service mode: %s\n' "$mode" >&2; return 1 ;;
	esac
	k patch service soha --type=merge --patch "{\"spec\":{\"selector\":{\"app.kubernetes.io/name\":\"$name\"}}}" >/dev/null
	wait_for_service_endpoint "$mode"
}

verify_external() {
	attempts=30
	while [ "$attempts" -gt 0 ]; do
		if curl --fail --silent --show-error --max-time 5 "$base_url/healthz" >/dev/null \
			&& curl --fail --silent --show-error --max-time 5 "$base_url/readyz" >/dev/null \
			&& curl --fail --silent --show-error --max-time 10 "$base_url/" >/dev/null; then
			return 0
		fi
		attempts=$((attempts - 1))
		sleep 2
	done
	printf 'external verification failed for %s\n' "$base_url" >&2
	return 1
}

restore_stable() {
	replicas="$(k get deployment soha -o jsonpath="{.metadata.annotations['remote-dev\.opensoha\.io/stable-replicas']}" 2>/dev/null || true)"
	if [ -z "$replicas" ]; then
		replicas="$(k get deployment soha -o jsonpath='{.spec.replicas}')"
		case "$replicas" in
			''|*[!0-9]*|0) replicas=1 ;;
		esac
		k annotate deployment soha "$replicas_annotation=$replicas" --overwrite >/dev/null
	fi
	k scale deployment/soha --replicas="$replicas" >/dev/null
	k rollout status deployment/soha --timeout=10m
	switch_service stable
	verify_external
}

configure_web_host() {
	k set env deployment/soha-dev --containers=web \
		__VITE_ADDITIONAL_SERVER_ALLOWED_HOSTS="$base_host" >/dev/null
}

activate() {
	k rollout status deployment/soha-dev --timeout=20m

	switch_service dev
	if ! verify_external; then
		switch_service stable
		return 1
	fi
	if ! k scale deployment/soha --replicas=0 >/dev/null; then
		switch_service stable
		return 1
	fi
	if ! verify_external; then
		restore_stable
		return 1
	fi
	printf 'remote development is active at %s\n' "$base_url"
}

up() {
	preflight
	restore_stable
	kubectl --context "$context" --namespace "$namespace" apply -k "$script_dir"
	k set env deployment/soha-dev --containers=source-sync \
		SOHA_REF="$soha_ref" \
		SOHA_WEB_REF="$web_ref" \
		SOHA_CONTRACTS_REF="$contracts_ref" >/dev/null
	configure_web_host
	activate
}

down() {
	preflight
	restore_stable
	kubectl --context "$context" --namespace "$namespace" delete -k "$script_dir" --ignore-not-found=true
	k annotate deployment soha "$replicas_annotation-" >/dev/null 2>&1 || true
	printf 'Helm deployment is active again at %s\n' "$base_url"
}

status() {
	preflight
	k get deployments soha soha-dev --ignore-not-found
	k get pods -l app.kubernetes.io/instance=soha,app.kubernetes.io/component=app
	printf 'service selector: '
	k get service soha -o go-template='{{range $key, $value := .spec.selector}}{{$key}}={{$value}} {{end}}{{"\n"}}'
}

logs() {
	preflight
	k logs deployment/soha-dev --all-containers=true --prefix=true --follow
}

case "${1:-}" in
	up) up ;;
	down) down ;;
	status) status ;;
	logs) logs ;;
	*) printf 'usage: %s {up|down|status|logs}\n' "$0" >&2; exit 2 ;;
esac
