#!/usr/bin/env sh
set -eu

SOHA_ENV_FILE="${SOHA_ENV_FILE:-/var/lib/soha/soha.env}"
export SOHA_ENV_FILE

needs_env=0
if [ -f "$SOHA_ENV_FILE" ]; then
	needs_env=1
elif [ -f "${SOHA_CONFIG_FILE:-}" ] && grep -q '\${SOHA_' "$SOHA_CONFIG_FILE"; then
	needs_env=1
fi

if [ "$needs_env" = "1" ] && [ ! -f "$SOHA_ENV_FILE" ]; then
	SOHA_SECRET_DIR="${SOHA_SECRET_DIR:-$(dirname "$SOHA_ENV_FILE")}"
	export SOHA_SECRET_DIR
	/app/scripts/soha-env.sh ensure
fi

if [ -f "$SOHA_ENV_FILE" ]; then
	set -a
	. "$SOHA_ENV_FILE"
	set +a
fi

exec "$@"
