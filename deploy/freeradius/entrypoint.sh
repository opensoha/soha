#!/bin/sh
set -eu

case "${1:-}" in
	/usr/local/bin/radius-adapter|/usr/local/bin/radius-runtime) exec "$@" ;;
esac

fail() {
	echo "FreeRADIUS configuration error: $1" >&2
	exit 1
}

# Debug packet output includes tunneled PAP credentials.
for argument in "$@"; do
	case "$argument" in
		-*[xX]*|--debug*) fail "packet debug logging is disabled for credential safety" ;;
	esac
done
SOHA_RADIUS_ADMISSION_EVENTS_ENABLED="${SOHA_RADIUS_ADMISSION_EVENTS_ENABLED:-false}"
case "$SOHA_RADIUS_ADMISSION_EVENTS_ENABLED" in true|false) ;; *) fail "admission event switch is invalid" ;; esac
export SOHA_RADIUS_ADMISSION_EVENTS_ENABLED

SOHA_RADIUS_MANAGED_ENABLED="${SOHA_RADIUS_MANAGED_ENABLED:-false}"
SOHA_RADIUS_PROCESS_EPOCH="${SOHA_RADIUS_PROCESS_EPOCH:-unmanaged}"
SOHA_RADIUS_HOOK_SOCKET="${SOHA_RADIUS_HOOK_SOCKET:-/run/soha-radius/hook.sock}"
case "$SOHA_RADIUS_MANAGED_ENABLED" in true|false) ;; *) fail "managed runtime switch is invalid" ;; esac
SOHA_RADIUS_HOOK_POOL_START=0
if [ "$SOHA_RADIUS_MANAGED_ENABLED" = true ]; then
	echo "$SOHA_RADIUS_PROCESS_EPOCH" | grep -Eq '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$' || fail "process epoch is invalid"
	case "$SOHA_RADIUS_HOOK_SOCKET" in /*/hook.sock) ;; *) fail "managed hook socket path is invalid" ;; esac
	case "$SOHA_RADIUS_HOOK_SOCKET" in *'/../'*|*'/./'*|*'//'*) fail "managed hook socket path is invalid" ;; esac
	[ ! -L "$SOHA_RADIUS_HOOK_SOCKET" ] && [ -S "$SOHA_RADIUS_HOOK_SOCKET" ] || fail "managed hook socket is unavailable"
	SOHA_RADIUS_HOOK_POOL_START=1
fi
export SOHA_RADIUS_MANAGED_ENABLED SOHA_RADIUS_PROCESS_EPOCH SOHA_RADIUS_HOOK_SOCKET SOHA_RADIUS_HOOK_POOL_START

: "${SOHA_RADIUS_RUNTIME_ID:?SOHA_RADIUS_RUNTIME_ID is required}"
: "${SOHA_RADIUS_NAS_ID:?SOHA_RADIUS_NAS_ID is required}"
: "${SOHA_RADIUS_NAS_CIDR:?SOHA_RADIUS_NAS_CIDR is required}"
: "${SOHA_RADIUS_SHARED_SECRET_FILE:?SOHA_RADIUS_SHARED_SECRET_FILE is required}"
: "${SOHA_RADIUS_CONTROL_URL:?SOHA_RADIUS_CONTROL_URL is required}"
: "${SOHA_RADIUS_INGEST_URL:?SOHA_RADIUS_INGEST_URL is required}"
: "${SOHA_RADIUS_EAP_KEY_FILE:?SOHA_RADIUS_EAP_KEY_FILE is required}"
: "${SOHA_RADIUS_EAP_CERT_FILE:?SOHA_RADIUS_EAP_CERT_FILE is required}"
: "${SOHA_RADIUS_EAP_CA_FILE:?SOHA_RADIUS_EAP_CA_FILE is required}"
: "${SOHA_RADIUS_CONTROL_KEY_FILE:?SOHA_RADIUS_CONTROL_KEY_FILE is required}"
: "${SOHA_RADIUS_CONTROL_CERT_FILE:?SOHA_RADIUS_CONTROL_CERT_FILE is required}"
: "${SOHA_RADIUS_CONTROL_CA_FILE:?SOHA_RADIUS_CONTROL_CA_FILE is required}"
: "${SOHA_RADIUS_INGEST_KEY_FILE:?SOHA_RADIUS_INGEST_KEY_FILE is required}"
: "${SOHA_RADIUS_INGEST_CERT_FILE:?SOHA_RADIUS_INGEST_CERT_FILE is required}"
: "${SOHA_RADIUS_INGEST_CA_FILE:?SOHA_RADIUS_INGEST_CA_FILE is required}"

echo "$SOHA_RADIUS_RUNTIME_ID" | grep -Eq '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$' || fail "runtime ID is invalid"
echo "$SOHA_RADIUS_NAS_ID" | grep -Eq '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$' || fail "NAS ID is invalid"
echo "$SOHA_RADIUS_NAS_CIDR" | grep -Eq '^[0-9a-fA-F:.]+/[0-9]{1,3}$' || fail "NAS CIDR is invalid"
case "$SOHA_RADIUS_CONTROL_URL" in https://*) ;; *) fail "network control URL must use HTTPS" ;; esac
case "$SOHA_RADIUS_INGEST_URL" in https://*) ;; *) fail "ingest URL must use HTTPS" ;; esac

for required_file in \
	"$SOHA_RADIUS_SHARED_SECRET_FILE" \
	"$SOHA_RADIUS_EAP_KEY_FILE" "$SOHA_RADIUS_EAP_CERT_FILE" "$SOHA_RADIUS_EAP_CA_FILE" \
	"$SOHA_RADIUS_CONTROL_KEY_FILE" "$SOHA_RADIUS_CONTROL_CERT_FILE" "$SOHA_RADIUS_CONTROL_CA_FILE" \
	"$SOHA_RADIUS_INGEST_KEY_FILE" "$SOHA_RADIUS_INGEST_CERT_FILE" "$SOHA_RADIUS_INGEST_CA_FILE"
do
	[ -f "$required_file" ] || fail "required credential file is missing"
done

[ "$(awk 'END { print NR }' "$SOHA_RADIUS_SHARED_SECRET_FILE")" -le 1 ] || fail "shared secret file must contain one line"
SOHA_RADIUS_SHARED_SECRET="$(tr -d '\r\n' < "$SOHA_RADIUS_SHARED_SECRET_FILE")"
echo "$SOHA_RADIUS_SHARED_SECRET" | grep -Eq '^[A-Za-z0-9._~+/@%=-]{16,256}$' || fail "shared secret must be a single safe 16-256 character value"

mkdir -p /run/freeradius
chown freerad:freerad /run/freeradius
chmod 0700 /run/freeradius
: > /run/freeradius/soha-lab-users
chown freerad:freerad /run/freeradius/soha-lab-users
chmod 0600 /run/freeradius/soha-lab-users

SOHA_RADIUS_LAB_USERS_FILE=/run/freeradius/soha-lab-users
SOHA_RADIUS_SHARED_SECRET_CONFIG_FILE=/run/freeradius/soha-shared-secret.conf
[ ! -L /run/freeradius ] && [ ! -L "$SOHA_RADIUS_SHARED_SECRET_CONFIG_FILE" ] || fail "shared secret configuration path must not be a symlink"
if [ -e "$SOHA_RADIUS_SHARED_SECRET_CONFIG_FILE" ]; then
	[ -f "$SOHA_RADIUS_SHARED_SECRET_CONFIG_FILE" ] && [ "$(stat -c %h "$SOHA_RADIUS_SHARED_SECRET_CONFIG_FILE")" = 1 ] || fail "shared secret configuration identity is invalid"
	case "$(stat -c %u "$SOHA_RADIUS_SHARED_SECRET_CONFIG_FILE")" in 0|"$(id -u freerad)") ;; *) fail "shared secret configuration owner is invalid" ;; esac
fi
shared_config_tmp="$(mktemp /run/freeradius/.soha-secret.XXXXXXXX)" || fail "shared secret configuration creation failed"
trap 'rm -f "$shared_config_tmp"' EXIT HUP INT TERM
printf 'soha_nas_shared_secret = "%s"\n' "$SOHA_RADIUS_SHARED_SECRET" > "$shared_config_tmp"
chown freerad:freerad "$shared_config_tmp"
chmod 0600 "$shared_config_tmp"
mv -f "$shared_config_tmp" "$SOHA_RADIUS_SHARED_SECRET_CONFIG_FILE"
trap - EXIT HUP INT TERM
unset SOHA_RADIUS_SHARED_SECRET
export SOHA_RADIUS_LAB_USERS_FILE SOHA_RADIUS_SHARED_SECRET_CONFIG_FILE

if [ -n "${SOHA_RADIUS_LAB_USERNAME:-}" ] || [ -n "${SOHA_RADIUS_LAB_PASSWORD_FILE:-}" ]; then
	[ -n "${SOHA_RADIUS_LAB_USERNAME:-}" ] && [ -n "${SOHA_RADIUS_LAB_PASSWORD_FILE:-}" ] || fail "lab username and password file must be configured together"
	[ -f "$SOHA_RADIUS_LAB_PASSWORD_FILE" ] || fail "lab password file is missing"
	echo "$SOHA_RADIUS_LAB_USERNAME" | grep -Eq '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$' || fail "lab username is invalid"
	[ "$(awk 'END { print NR }' "$SOHA_RADIUS_LAB_PASSWORD_FILE")" -le 1 ] || fail "lab password file must contain one line"
	lab_password="$(tr -d '\r\n' < "$SOHA_RADIUS_LAB_PASSWORD_FILE")"
	echo "$lab_password" | grep -Eq '^[A-Za-z0-9._~+/@%=-]{12,128}$' || fail "lab password must be a single safe 12-128 character value"
	printf '%s Cleartext-Password := "%s"\n' "$SOHA_RADIUS_LAB_USERNAME" "$lab_password" > /run/freeradius/soha-lab-users
	unset lab_password
fi


if [ "$SOHA_RADIUS_ADMISSION_EVENTS_ENABLED" = true ]; then
	event_dir=/run/freeradius/admission-events
	event_file="$event_dir/events.jsonl"
	event_uid="$(id -u freerad)"
	[ ! -L /run/freeradius ] && [ ! -L "$event_dir" ] && [ ! -L "$event_file" ] || fail "admission event path must not be a symlink"
	[ ! -e "$event_dir" ] || [ -d "$event_dir" ] || fail "admission event directory is invalid"
	if [ -d "$event_dir" ]; then
		case "$(stat -c %u "$event_dir")" in 0|"$event_uid") ;; *) fail "admission event directory owner is invalid" ;; esac
	fi
	mkdir -p "$event_dir"
	chown freerad:freerad "$event_dir"
	chmod 0700 "$event_dir"
	if [ -e "$event_file" ]; then
		[ -f "$event_file" ] && [ "$(stat -c %h "$event_file")" = 1 ] || fail "admission event file identity is invalid"
		case "$(stat -c %u "$event_file")" in 0|"$event_uid") ;; *) fail "admission event file owner is invalid" ;; esac
	else
		(umask 077; set -C; : > "$event_file") || fail "admission event file creation failed"
	fi
	chown freerad:freerad "$event_file"
	chmod 0600 "$event_file"
fi

exec /docker-entrypoint.sh "$@"
