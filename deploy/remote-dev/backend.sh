#!/bin/sh
set -eu

interval="${RELOAD_INTERVAL_SECONDS:-3}"
server_pid=""
last_fingerprint=""

stop_server() {
	if [ -n "$server_pid" ] && kill -0 "$server_pid" 2>/dev/null; then
		kill "$server_pid"
		wait "$server_pid" 2>/dev/null || true
	fi
	server_pid=""
}

start_server() {
	(cd /workspace/soha && exec /workspace/bin/soha) &
	server_pid="$!"
}

source_fingerprint() {
	[ -f /workspace/soha/go.mod ] || return 1
	[ -f /workspace/soha/cmd/server/main.go ] || return 1
	[ -f /workspace/soha-contracts/go.mod ] || return 1
	[ -f /workspace/soha-contracts/gen/go/sohaapi/client.go ] || return 1
	[ -f /workspace/soha-contracts/gen/go/sohaapi/types.go ] || return 1
	find /workspace/soha /workspace/soha-contracts \
		\( -type d \( \
			-name .git -o \
			-name node_modules -o \
			-name dist -o \
			-name generated -o \
			-name coverage -o \
			-name test-results -o \
			-name graphify-out -o \
			-name .cache -o \
			-name .next -o \
			-name bin -o \
			-name tmp -o \
			-name .tmp \
		\) -prune \) -o \
		\( -type f \( \
			-name '*.go' -o \
			-name '*.s' -o \
			-name '*.c' -o \
			-name '*.h' -o \
			-name '*.sql' -o \
			-name '*.json' -o \
			-name '*.yaml' -o \
			-name '*.yml' -o \
			-name '*.tmpl' -o \
			-name '*.html' -o \
			-name go.mod -o \
			-name go.sum \
		\) -print0 \) \
		| sort -z \
		| xargs -0 cksum \
		| cksum \
		| awk '{print $1 ":" $2}'
}

trap 'stop_server; exit 0' INT TERM

until fingerprint="$(source_fingerprint)"; do
	printf 'waiting for backend sources\n'
	sleep "$interval"
done

printf 'go 1.26.6\n\nuse (\n\t./soha\n\t./soha-contracts\n)\n\nreplace github.com/opensoha/soha-contracts v0.0.0 => ./soha-contracts\n' > /workspace/go.work
mkdir -p /workspace/bin

while :; do
	fingerprint="$(source_fingerprint 2>/dev/null || true)"
	if [ -n "$fingerprint" ] && [ "$fingerprint" != "$last_fingerprint" ]; then
		candidate="$fingerprint"
		sleep "$interval"
		fingerprint="$(source_fingerprint 2>/dev/null || true)"
		[ "$fingerprint" = "$candidate" ] || continue
		last_fingerprint="$fingerprint"
		printf 'building backend at source fingerprint %s\n' "$fingerprint"
		if (cd /workspace/soha && CGO_ENABLED=0 go build -o /workspace/bin/soha.next ./cmd/server); then
			stop_server
			mv /workspace/bin/soha.next /workspace/bin/soha
			start_server
			printf 'backend restarted at source fingerprint %s\n' "$fingerprint"
		else
			rm -f /workspace/bin/soha.next
			last_fingerprint=""
			printf 'backend build failed; keeping the previous process and retrying in 30 seconds\n' >&2
			sleep 30
		fi
	fi

	if [ -f /workspace/bin/soha ] && { [ -z "$server_pid" ] || ! kill -0 "$server_pid" 2>/dev/null; }; then
		start_server
		printf 'backend process restarted\n'
	fi
	sleep "$interval"
done
