#!/bin/sh
set -eu

if [ "${1:-}" = "/usr/local/bin/radius-adapter" ]; then
	exec "$@"
fi

fail() {
	echo "FreeRADIUS configuration error: $1" >&2
	exit 1
}

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
export SOHA_RADIUS_SHARED_SECRET

mkdir -p /run/freeradius
chown freerad:freerad /run/freeradius
chmod 0700 /run/freeradius
: > /run/freeradius/soha-lab-users
chown freerad:freerad /run/freeradius/soha-lab-users
chmod 0600 /run/freeradius/soha-lab-users

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

exec /docker-entrypoint.sh "$@"
