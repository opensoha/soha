#!/usr/bin/env sh
set -eu

COMPOSE_BIN="${COMPOSE:-docker compose}"
COMPOSE_FILE="${SOHA_SMOKE_COMPOSE_FILE:-deploy/docker-compose.yaml}"
PROJECT_NAME="${SOHA_SMOKE_PROJECT:-soha-smoke}"
SOHA_SMOKE_HTTP_PORT="${SOHA_SMOKE_HTTP_PORT:-28080}"
BASE_URL="${SOHA_SMOKE_BASE_URL:-http://127.0.0.1:${SOHA_SMOKE_HTTP_PORT}}"
TIMEOUT_SECONDS="${SOHA_SMOKE_TIMEOUT_SECONDS:-180}"
KEEP_STACK="${SOHA_SMOKE_KEEP:-0}"
BUILD_IMAGE="${SOHA_SMOKE_BUILD:-1}"
export SOHA_IMAGE="${SOHA_SMOKE_IMAGE:-${SOHA_IMAGE:-yshanchui/soha:local}}"
export SOHA_HTTP_BIND="${SOHA_SMOKE_HTTP_BIND:-127.0.0.1}"
export SOHA_HTTP_PORT="$SOHA_SMOKE_HTTP_PORT"
export SOHA_POSTGRES_BIND="${SOHA_SMOKE_POSTGRES_BIND:-127.0.0.1}"
export SOHA_POSTGRES_PORT="${SOHA_SMOKE_POSTGRES_PORT:-28081}"
export SOHA_CONTAINER_NAME="${PROJECT_NAME}-soha"
export SOHA_SECRETS_CONTAINER_NAME="${PROJECT_NAME}-secrets"
export SOHA_POSTGRES_CONTAINER_NAME="${PROJECT_NAME}-postgres"
export SOHA_REDIS_CONTAINER_NAME="${PROJECT_NAME}-redis"
export SOHA_K3S_CONTAINER_NAME="${PROJECT_NAME}-k3s"
export SOHA_HERMES_CONTAINER_NAME="${PROJECT_NAME}-hermes-agent-runner"
export SOHA_POSTGRES_VOLUME="${PROJECT_NAME}-postgres-data"
export SOHA_SECRETS_VOLUME="${PROJECT_NAME}-secrets"
export SOHA_K3S_VOLUME="${PROJECT_NAME}-k3s-data"
export SOHA_HERMES_DATA_VOLUME="${PROJECT_NAME}-hermes-data"
export SOHA_HERMES_RUNTIME_VOLUME="${PROJECT_NAME}-hermes-runtime"
export SOHA_SMOKE_HTTP_PORT
export SOHA_ENV_FILE="${SOHA_SMOKE_ENV_FILE:-.dev/soha.env}"

compose() {
	# shellcheck disable=SC2086
	$COMPOSE_BIN -p "$PROJECT_NAME" -f "$COMPOSE_FILE" "$@"
}

cleanup() {
	if [ "$KEEP_STACK" = "1" ]; then
		return
	fi
	compose down -v --remove-orphans >/dev/null 2>&1 || true
}

on_exit() {
	status=$?
	if [ "$status" -ne 0 ]; then
		compose ps || true
		compose logs --no-color postgres soha || true
	fi
	cleanup
	exit "$status"
}

require_file() {
	if [ ! -f "$1" ]; then
		echo "Missing required file: $1" >&2
		exit 1
	fi
}

wait_for_ready() {
	deadline=$(( $(date +%s) + TIMEOUT_SECONDS ))
	while ! curl -fsS "$BASE_URL/readyz" >/tmp/soha-smoke-ready.json 2>/dev/null; do
		if [ "$(date +%s)" -ge "$deadline" ]; then
			echo "Timed out waiting for $BASE_URL/readyz" >&2
			return 1
		fi
		sleep 2
	done
}

extract_access_token() {
	sed -n 's/.*"accessToken"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$1" | head -n 1
}

extract_refresh_token() {
	sed -n 's/.*"refreshToken"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$1" | head -n 1
}

assert_sql_nonzero() {
	label="$1"
	sql="$2"
	count="$(compose exec -T postgres psql -U pgsql -d soha -tAc "$sql" | tr -d '[:space:]')"
	case "$count" in
		""|0)
			echo "Database smoke failed for ${label}: count=${count:-empty}" >&2
			exit 1
			;;
	esac
}

trap on_exit EXIT INT TERM

require_file "$COMPOSE_FILE"
require_file "scripts/soha-env.sh"
require_file "internal/staticassets/web/dist/index.html"
require_file "migrations/postgres/0001_init.sql"

./scripts/soha-env.sh ensure
set -a
. "$SOHA_ENV_FILE"
set +a

compose down -v --remove-orphans >/dev/null 2>&1 || true
if [ "$BUILD_IMAGE" = "1" ]; then
	compose up -d --build postgres soha
else
	compose up -d --no-build --pull never postgres soha
fi

wait_for_ready
curl -fsS "$BASE_URL/healthz" >/tmp/soha-smoke-health.json

assert_sql_nonzero "schema migration baseline" "SELECT COUNT(*) FROM public.schema_migrations WHERE filename LIKE '%0001_init.sql'"
assert_sql_nonzero "seeded opensoha user" "SELECT COUNT(*) FROM public.users WHERE id = 'opensoha'"
assert_sql_nonzero "seeded opensoha password" "SELECT COUNT(*) FROM public.user_password_credentials WHERE user_id = 'opensoha'"
assert_sql_nonzero "seeded roles" "SELECT COUNT(*) FROM public.roles WHERE id IN ('admin', 'ops', 'auditor')"
assert_sql_nonzero "seeded menus" "SELECT COUNT(*) FROM public.menus"
assert_sql_nonzero "seeded policies" "SELECT COUNT(*) FROM public.policies"

index_file="$(mktemp)"
curl -fsS "$BASE_URL/" -o "$index_file"
if ! grep -Eq '<div id="root"|/assets/|type="module"' "$index_file"; then
	echo "Embedded web smoke did not find a Vite SPA marker in /" >&2
	exit 1
fi

login_file="$(mktemp)"
curl -fsS \
	-H "Content-Type: application/json" \
	-d "{\"login\":\"opensoha\",\"password\":\"$SOHA_AUTH_DEV_PRINCIPAL_PASSWORD\"}" \
	"$BASE_URL/api/v1/auth/login" \
	-o "$login_file"

access_token="$(extract_access_token "$login_file")"
refresh_token="$(extract_refresh_token "$login_file")"
if [ -z "$access_token" ]; then
	echo "Login smoke did not return data.tokens.accessToken" >&2
	cat "$login_file" >&2
	exit 1
fi
if [ -z "$refresh_token" ]; then
	echo "Login smoke did not return data.tokens.refreshToken" >&2
	cat "$login_file" >&2
	exit 1
fi

refresh_file="$(mktemp)"
curl -fsS \
	-H "Content-Type: application/json" \
	-d "{\"refreshToken\":\"$refresh_token\"}" \
	"$BASE_URL/api/v1/auth/refresh" \
	-o "$refresh_file"
refreshed_access_token="$(extract_access_token "$refresh_file")"
if [ -z "$refreshed_access_token" ]; then
	echo "Refresh smoke did not return data.tokens.accessToken" >&2
	cat "$refresh_file" >&2
	exit 1
fi

me_file="$(mktemp)"
curl -fsS \
	-H "Authorization: Bearer $refreshed_access_token" \
	"$BASE_URL/api/v1/auth/me" \
	-o "$me_file"
if ! grep -q '"userId"[[:space:]]*:[[:space:]]*"opensoha"' "$me_file"; then
	echo "Authenticated API smoke did not return the seeded opensoha principal" >&2
	cat "$me_file" >&2
	exit 1
fi

echo "compose smoke passed: migration, seed, readyz, healthz, embedded web, login, refresh, authenticated API"
