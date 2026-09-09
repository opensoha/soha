#!/bin/sh
set -eu

fail() {
	echo "EAP-TLS test configuration error: $1" >&2
	exit 1
}

config_file="${SOHA_RADIUS_EAP_TEST_CONFIG_FILE:-/run/soha-radius/eap-test/eapol-test.conf}"
secret_file="${SOHA_RADIUS_SHARED_SECRET_FILE:-/run/soha-radius/secrets/shared-secret}"
nas_id="${SOHA_RADIUS_NAS_ID:-nas-lab}"
station_id="${SOHA_RADIUS_TEST_STATION_ID:-02:00:00:00:00:01}"

[ -r "$config_file" ] || fail "EAP configuration file is missing"
[ -r "$secret_file" ] || fail "RADIUS shared secret file is missing"
printf '%s\n' "$nas_id" | grep -Eq '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$' || fail "NAS identifier is invalid"
printf '%s\n' "$station_id" | grep -Eq '^([A-Fa-f0-9]{2}:){5}[A-Fa-f0-9]{2}$' || fail "station ID must be a MAC address"
[ "$(awk 'END { print NR }' "$secret_file")" -le 1 ] || fail "shared secret file must contain one line"

shared_secret="$(tr -d '\r\n' < "$secret_file")"
[ "${#shared_secret}" -ge 16 ] && [ "${#shared_secret}" -le 256 ] || fail "shared secret must be 16-256 characters"
printf '%s\n' "$shared_secret" | grep -Eq '^[A-Za-z0-9._~+/@%=-]+$' || fail "shared secret contains unsupported characters"

# eapol_test accepts the disposable lab RADIUS secret only as an argument.
exec eapol_test \
	-c "$config_file" \
	-a 127.0.0.1 \
	-p 1812 \
	-s "$shared_secret" \
	-M "$station_id" \
	-N "32:s:$nas_id"
