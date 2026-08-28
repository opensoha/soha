#!/bin/sh
set -eu

interval="${RELOAD_INTERVAL_SECONDS:-3}"
web_pid=""
lock_revision=""

stop_web() {
	if [ -n "$web_pid" ] && kill -0 "$web_pid" 2>/dev/null; then
		kill "$web_pid"
		wait "$web_pid" 2>/dev/null || true
	fi
	web_pid=""
}

start_web() {
	(cd /workspace/soha-web && exec npm run dev -- --host 0.0.0.0) &
	web_pid="$!"
}

current_lock_revision() {
	cksum /workspace/soha-web/package-lock.json 2>/dev/null | awk '{print $1 ":" $2}'
}

install_web() {
	(cd /workspace/soha-web && npm ci --registry=https://registry.npmmirror.com --no-audit --no-fund)
	lock_revision="$(current_lock_revision)"
}

trap 'stop_web; exit 0' INT TERM

until [ -f /workspace/soha-web/package-lock.json ]; do
	printf 'waiting for web sources\n'
	sleep "$interval"
done

install_web
start_web

while :; do
	current="$(current_lock_revision || true)"
	if [ -n "$current" ] && [ "$current" != "$lock_revision" ]; then
		printf 'web lockfile changed; reinstalling dependencies\n'
		stop_web
		install_web
		start_web
	elif [ -z "$web_pid" ] || ! kill -0 "$web_pid" 2>/dev/null; then
		start_web
		printf 'web process restarted\n'
	fi
	sleep "$interval"
done
