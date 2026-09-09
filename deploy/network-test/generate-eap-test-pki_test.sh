#!/bin/sh
set -eu

script_dir="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
test_dir="$(mktemp -d /tmp/soha-eap-pki.XXXXXX)"
trap 'rm -r "$test_dir"' EXIT HUP INT TERM

output_dir="$test_dir/eap-lab"
sh "$script_dir/generate-eap-test-pki.sh" "$output_dir" "subject-test"

for required_file in \
	"$output_dir/issuer/ca.crt" "$output_dir/issuer/ca.key" \
	"$output_dir/network-control/ca.crt" "$output_dir/network-control/tls.crt" "$output_dir/network-control/tls.key" \
	"$output_dir/freeradius-control/ca.crt" "$output_dir/freeradius-control/tls.crt" "$output_dir/freeradius-control/tls.key" \
	"$output_dir/freeradius-eap/ca.crt" "$output_dir/freeradius-eap/tls.crt" "$output_dir/freeradius-eap/tls.key" \
	"$output_dir/eap-test-client/ca.crt" "$output_dir/eap-test-client/tls.crt" "$output_dir/eap-test-client/tls.key" \
	"$output_dir/eap-test-client/eapol-test.conf"
do
	test -f "$required_file"
done

openssl verify -CAfile "$output_dir/issuer/ca.crt" \
	"$output_dir/network-control/tls.crt" "$output_dir/freeradius-control/tls.crt" \
	"$output_dir/freeradius-eap/tls.crt" "$output_dir/eap-test-client/tls.crt" >/dev/null
openssl x509 -in "$output_dir/network-control/tls.crt" -noout -text | grep -Fq 'DNS:network-control'
openssl x509 -in "$output_dir/network-control/tls.crt" -noout -text | grep -Fq 'IP Address:127.0.0.1'
openssl x509 -in "$output_dir/freeradius-control/tls.crt" -noout -text | grep -Fq 'URI:spiffe://opensoha.local/network-control/nas/radius-lab'
openssl x509 -in "$output_dir/freeradius-eap/tls.crt" -noout -text | grep -Fq 'DNS:freeradius'
openssl x509 -in "$output_dir/freeradius-eap/tls.crt" -noout -text | grep -Fq 'TLS Web Server Authentication'
openssl x509 -in "$output_dir/eap-test-client/tls.crt" -noout -subject -nameopt RFC2253 | grep -Fq 'CN=subject-test'
openssl x509 -in "$output_dir/eap-test-client/tls.crt" -noout -text | grep -Fq 'TLS Web Client Authentication'
openssl x509 -in "$output_dir/eap-test-client/tls.crt" -noout -text | grep -Fq 'X509v3 Authority Key Identifier'
openssl x509 -in "$output_dir/eap-test-client/tls.crt" -noout -text | grep -Fq 'URI:spiffe://opensoha.local/network-control/endpoint/endpoint-eap-test'
grep -Fq 'identity="subject-test"' "$output_dir/eap-test-client/eapol-test.conf"
grep -Fq 'domain_match="freeradius"' "$output_dir/eap-test-client/eapol-test.conf"
test ! -e "$output_dir/freeradius-eap/ca.key"
test ! -e "$output_dir/eap-test-client/ca.key"
test ! -e "$output_dir/network-control/ca.key"
test ! -e "$output_dir/freeradius-control/ca.key"

key_mode() {
	stat -f '%Lp' "$1" 2>/dev/null || stat -c '%a' "$1"
}

test "$(key_mode "$output_dir/issuer/ca.key")" = 600
test "$(key_mode "$output_dir/network-control/tls.key")" = 600
test "$(key_mode "$output_dir/freeradius-control/tls.key")" = 600
test "$(key_mode "$output_dir/freeradius-eap/tls.key")" = 600
test "$(key_mode "$output_dir/eap-test-client/tls.key")" = 600

if sh "$script_dir/generate-eap-test-pki.sh" "$output_dir" "subject-test" >/dev/null 2>&1; then
	echo "expected an existing output directory to fail" >&2
	exit 1
fi

if sh "$script_dir/generate-eap-test-pki.sh" "$test_dir/invalid" "invalid subject" >/dev/null 2>&1; then
	echo "expected an invalid subject ID to fail" >&2
	exit 1
fi

if SOHA_EAP_TEST_ENDPOINT_RUNTIME_ID='invalid/runtime' \
	sh "$script_dir/generate-eap-test-pki.sh" "$test_dir/invalid-runtime" "subject-test" >/dev/null 2>&1; then
	echo "expected an invalid runtime ID to fail" >&2
	exit 1
fi
