#!/usr/bin/env sh
set -eu

ENV_FILE="${SOHA_ENV_FILE:-.dev/soha.env}"
SECRET_DIR="${SOHA_SECRET_DIR:-$(dirname "$ENV_FILE")}"
ENV_FILE_MODE="${SOHA_ENV_FILE_MODE:-600}"
SECRET_FILE_MODE="${SOHA_SECRET_FILE_MODE:-600}"

random_secret() {
	dd if=/dev/urandom bs=32 count=1 2>/dev/null | base64 | tr '+/' '-_' | tr -d '=\n'
}

env_value() {
	key="$1"
	if [ ! -f "$ENV_FILE" ]; then
		return 0
	fi
	line="$(grep "^${key}=" "$ENV_FILE" | tail -n 1 || true)"
	if [ -z "$line" ]; then
		return 0
	fi
	value="${line#*=}"
	case "$value" in
		\'*\')
			value="${value#\'}"
			value="${value%\'}"
			printf "%s" "$value" | sed "s/'\\\\''/'/g"
			;;
		*)
			printf "%s" "$value"
			;;
	esac
}

shell_quote() {
	printf "'%s'" "$(printf "%s" "$1" | sed "s/'/'\\\\''/g")"
}

yaml_double_quote() {
	printf '"%s"' "$(printf "%s" "$1" | sed 's/\\/\\\\/g; s/"/\\"/g')"
}

set_env_value() {
	key="$1"
	value="$2"
	tmp="${ENV_FILE}.tmp"
	if [ -f "$ENV_FILE" ]; then
		grep -v "^${key}=" "$ENV_FILE" >"$tmp" || true
	else
		: >"$tmp"
	fi
	printf '%s=%s\n' "$key" "$(shell_quote "$value")" >>"$tmp"
	mv "$tmp" "$ENV_FILE"
	chmod "$ENV_FILE_MODE" "$ENV_FILE"
}

ensure_value() {
	key="$1"
	file="$2"
	eval "value=\${$key:-}"
	if [ -z "$value" ]; then
		value="$(env_value "$key")"
	fi
	if [ -z "$value" ]; then
		value="soha-$(random_secret)"
	fi
	set_env_value "$key" "$value"
	printf '%s' "$value" >"${SECRET_DIR}/${file}"
	chmod "$SECRET_FILE_MODE" "${SECRET_DIR}/${file}"
}

ensure_all() {
	mkdir -p "$(dirname "$ENV_FILE")" "$SECRET_DIR"
	touch "$ENV_FILE"
	chmod "$ENV_FILE_MODE" "$ENV_FILE"

	ensure_value SOHA_DATABASE_PASSWORD database-password
	ensure_value SOHA_AUTH_DEV_PRINCIPAL_PASSWORD auth-dev-principal-password
	ensure_value SOHA_AUTH_JWT_SECRET auth-jwt-secret

	if [ -n "${SOHA_EXECUTION_RUNNER_TOKEN:-}" ] && [ -z "${SOHA_RUNTIME_EXECUTION_RUNNER_TOKEN:-}" ]; then
		SOHA_RUNTIME_EXECUTION_RUNNER_TOKEN="$SOHA_EXECUTION_RUNNER_TOKEN"
		export SOHA_RUNTIME_EXECUTION_RUNNER_TOKEN
	fi
	ensure_value SOHA_RUNTIME_EXECUTION_RUNNER_TOKEN runtime-execution-runner-token
	runner_token="$(env_value SOHA_RUNTIME_EXECUTION_RUNNER_TOKEN)"
	SOHA_EXECUTION_RUNNER_TOKEN="$runner_token"
	export SOHA_EXECUTION_RUNNER_TOKEN
	set_env_value SOHA_EXECUTION_RUNNER_TOKEN "$runner_token"
	printf '%s' "$runner_token" >"${SECRET_DIR}/execution-runner-token"
	chmod "$SECRET_FILE_MODE" "${SECRET_DIR}/execution-runner-token"

	ensure_value SOHA_MONITORING_WEBHOOK_TOKEN monitoring-webhook-token
	ensure_value SOHA_SECURITY_CREDENTIAL_ENCRYPTION_KEY security-credential-encryption-key
}

case "${1:-ensure}" in
	ensure)
		ensure_all
		;;
	export)
		ensure_all
		sed 's/^/export /' "$ENV_FILE"
		;;
	k8s-secret)
		ensure_all
		set -a
		. "$ENV_FILE"
		set +a
		postgres_password_yaml="$(yaml_double_quote "$SOHA_DATABASE_PASSWORD")"
		runner_token_yaml="$(yaml_double_quote "$SOHA_RUNTIME_EXECUTION_RUNNER_TOKEN")"
		dev_password_yaml="$(yaml_double_quote "$SOHA_AUTH_DEV_PRINCIPAL_PASSWORD")"
		jwt_secret_yaml="$(yaml_double_quote "$SOHA_AUTH_JWT_SECRET")"
		webhook_token_yaml="$(yaml_double_quote "$SOHA_MONITORING_WEBHOOK_TOKEN")"
		credential_key_yaml="$(yaml_double_quote "$SOHA_SECURITY_CREDENTIAL_ENCRYPTION_KEY")"
		cat <<EOF
apiVersion: v1
kind: Namespace
metadata:
  name: soha
---
apiVersion: v1
kind: Secret
metadata:
  name: soha-app-config
  namespace: soha
type: Opaque
stringData:
  postgres-password: $postgres_password_yaml
  config.yaml: |
    app:
      name: soha
    http:
      addr: :8080
      base_path: /api/v1
      read_timeout: 15s
      write_timeout: 15s
      cors_allowed_origins:
        - https://soha.example.com
    logger:
      level: info
      format: json
    runtime:
      workflow_workers: 4
      workflow_queue_size: 64
      workflow_node_parallelism: 4
      cluster_sync_parallelism: 4
      copilot_inspection_parallelism: 2
      alert_upsert_batch_size: 100
      execution_runner_token: $runner_token_yaml
    database:
      driver: postgres
      host: soha-postgres
      port: 5432
      name: soha
      user: pgsql
      password: $postgres_password_yaml
      sslmode: disable
      max_open_conns: 20
      max_idle_conns: 10
      conn_max_lifetime: 1h
      auto_migrate: true
      migration_file: /app/migrations/postgres/0001_init.sql
    auth:
      enable_dev_auth: false
      dev_principal:
        user_id: opensoha
        name: OpenSoha
        email: opensoha@soha.local
        password: $dev_password_yaml
        roles:
          - admin
          - ops
          - auditor
      jwt:
        issuer: soha
        secret: $jwt_secret_yaml
        access_ttl: 15m
        refresh_ttl: 168h
      oidc:
        enabled: false
        provider_name: default
        issuer: ""
        client_id: ""
        client_secret: ""
        redirect_url: https://soha.example.com/api/v1/auth/oidc/callback
        frontend_redirect_url: https://soha.example.com/login/callback
        scopes:
          - openid
          - profile
          - email
        default_roles:
          - readonly
    gitlab:
      enabled: false
      base_url: https://gitlab.com/api/v4
      token: ""
      group_id: ""
      per_page: 50
      timeout: 10s
    monitoring:
      enabled: true
      webhook_token: $webhook_token_yaml
      prometheus_url: ""
      prometheus_bearer_token: ""
      prometheus_default_range_minutes: 60
      prometheus_step_seconds: 60
      prometheus_cluster_label: cluster
      grafana_base_url: ""
    swagger:
      enabled: false
      path: /swagger/*any
    mcp:
      enabled: true
      default_timeout: 10s
    ai_gateway:
      rate_limit:
        backend: postgres
        redis:
          addr: ""
          username: ""
          password: ""
          db: 0
          tls: false
          key_prefix: soha:ai-gateway:rate-limit
          timeout: 500ms
    modules:
      delivery:
        enabled: true
      monitoring:
        enabled: true
      ai:
        enabled: true
      ai_gateway:
        enabled: true
      virtualization:
        enabled: true
      docker:
        enabled: true
      security:
        enabled: false
      cmdb:
        enabled: false
    security:
      credential_encryption_key: $credential_key_yaml
      secret_provider: ""
    bootstrap:
      seed_defaults: true
    kubernetes:
      clusters: []
EOF
		;;
	run)
		shift
		ensure_all
		set -a
		. "$ENV_FILE"
		set +a
		exec "$@"
		;;
	*)
		echo "usage: $0 [ensure|export|k8s-secret|run -- command...]" >&2
		exit 2
		;;
esac
