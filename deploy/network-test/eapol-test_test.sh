#!/bin/sh
set -eu

script_dir="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
test_dir="$(mktemp -d /tmp/soha-eapol-test.XXXXXX)"
trap 'rm -r "$test_dir"' EXIT HUP INT TERM

mkdir -p "$test_dir/bin"
config_file="$test_dir/eapol-test.conf"
secret_file="$test_dir/shared-secret"
capture_file="$test_dir/args"
expected_file="$test_dir/expected"

printf '%s\n' 'network={}' > "$config_file"
printf '%s\n' 'test-radius-secret-1234' > "$secret_file"
printf '%s\n' '#!/bin/sh' 'printf "%s\n" "$@" > "$SOHA_EAPOL_CAPTURE_FILE"' > "$test_dir/bin/eapol_test"
chmod 0755 "$test_dir/bin/eapol_test"

PATH="$test_dir/bin:$PATH" \
	SOHA_EAPOL_CAPTURE_FILE="$capture_file" \
	SOHA_RADIUS_EAP_TEST_CONFIG_FILE="$config_file" \
	SOHA_RADIUS_SHARED_SECRET_FILE="$secret_file" \
	SOHA_RADIUS_NAS_ID="nas-lab" \
	SOHA_RADIUS_TEST_STATION_ID="02:00:00:00:00:01" \
	sh "$script_dir/eapol-test.sh"

printf '%s\n' \
	-c "$config_file" \
	-a 127.0.0.1 \
	-p 1812 \
	-s test-radius-secret-1234 \
	-M 02:00:00:00:00:01 \
	-N 32:s:nas-lab > "$expected_file"
cmp "$expected_file" "$capture_file"

if PATH="$test_dir/bin:$PATH" \
	SOHA_EAPOL_CAPTURE_FILE="$capture_file" \
	SOHA_RADIUS_EAP_TEST_CONFIG_FILE="$test_dir/missing.conf" \
	SOHA_RADIUS_SHARED_SECRET_FILE="$secret_file" \
	SOHA_RADIUS_NAS_ID="nas-lab" \
	SOHA_RADIUS_TEST_STATION_ID="02:00:00:00:00:01" \
	sh "$script_dir/eapol-test.sh" >/dev/null 2>&1; then
	echo "expected a missing EAP configuration to fail" >&2
	exit 1
fi

if PATH="$test_dir/bin:$PATH" \
	SOHA_EAPOL_CAPTURE_FILE="$capture_file" \
	SOHA_RADIUS_EAP_TEST_CONFIG_FILE="$config_file" \
	SOHA_RADIUS_SHARED_SECRET_FILE="$secret_file" \
	SOHA_RADIUS_NAS_ID="invalid nas" \
	SOHA_RADIUS_TEST_STATION_ID="02:00:00:00:00:01" \
	sh "$script_dir/eapol-test.sh" >/dev/null 2>&1; then
	echo "expected an invalid NAS identifier to fail" >&2
	exit 1
fi
