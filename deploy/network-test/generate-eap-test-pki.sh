#!/bin/sh
set -eu

fail() {
	echo "EAP test PKI error: $1" >&2
	exit 1
}

[ "$#" -eq 2 ] || fail "usage: generate-eap-test-pki.sh OUTPUT_DIR SUBJECT_ID"
output_dir="$1"
subject_id="$2"
endpoint_runtime_id="${SOHA_EAP_TEST_ENDPOINT_RUNTIME_ID:-endpoint-eap-test}"
nas_runtime_id="${SOHA_RADIUS_RUNTIME_ID:-radius-lab}"

case "$output_dir" in ""|/|.|..) fail "output directory is unsafe" ;; esac
[ ! -e "$output_dir" ] && [ ! -L "$output_dir" ] || fail "output directory already exists"
[ "${#subject_id}" -le 128 ] && printf '%s\n' "$subject_id" | grep -Eq '^[A-Za-z0-9][A-Za-z0-9._@:-]*$' || fail "subject ID is invalid"
for runtime_id in "$endpoint_runtime_id" "$nas_runtime_id"; do
	[ "${#runtime_id}" -le 128 ] && printf '%s\n' "$runtime_id" | grep -Eq '^[A-Za-z0-9][A-Za-z0-9._:-]*$' || fail "runtime ID is invalid"
done
command -v openssl >/dev/null 2>&1 || fail "openssl is required"

script_dir="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
parent_dir="$(dirname -- "$output_dir")"
mkdir -p "$parent_dir"
work_dir="$(mktemp -d "$parent_dir/.soha-eap-pki.XXXXXX")"
trap 'rm -r "$work_dir"' EXIT HUP INT TERM
umask 077

mkdir -p "$work_dir/issuer" "$work_dir/network-control" "$work_dir/freeradius-control" \
	"$work_dir/freeradius-eap" "$work_dir/eap-test-client"

openssl genpkey -algorithm EC -pkeyopt ec_paramgen_curve:P-256 -out "$work_dir/issuer/ca.key" >/dev/null 2>&1
openssl req -x509 -new -sha256 -days 7 \
	-key "$work_dir/issuer/ca.key" \
	-subj '/CN=Soha EAP Test CA' \
	-addext 'basicConstraints=critical,CA:TRUE,pathlen:0' \
	-addext 'keyUsage=critical,keyCertSign,cRLSign' \
	-addext 'subjectKeyIdentifier=hash' \
	-out "$work_dir/issuer/ca.crt" >/dev/null 2>&1

printf '%s\n' \
	'basicConstraints=critical,CA:FALSE' \
	'keyUsage=critical,digitalSignature,keyAgreement' \
	'extendedKeyUsage=serverAuth' \
	'subjectAltName=DNS:network-control,DNS:localhost,IP:127.0.0.1' \
	'subjectKeyIdentifier=hash' \
	'authorityKeyIdentifier=keyid,issuer' > "$work_dir/network-control.ext"
openssl genpkey -algorithm EC -pkeyopt ec_paramgen_curve:P-256 -out "$work_dir/network-control/tls.key" >/dev/null 2>&1
openssl req -new -sha256 -key "$work_dir/network-control/tls.key" -subj '/CN=network-control' -out "$work_dir/network-control.csr" >/dev/null 2>&1
openssl x509 -req -sha256 -days 7 \
	-in "$work_dir/network-control.csr" \
	-CA "$work_dir/issuer/ca.crt" \
	-CAkey "$work_dir/issuer/ca.key" \
	-CAcreateserial \
	-extfile "$work_dir/network-control.ext" \
	-out "$work_dir/network-control/tls.crt" >/dev/null 2>&1

printf '%s\n' \
	'basicConstraints=critical,CA:FALSE' \
	'keyUsage=critical,digitalSignature' \
	'extendedKeyUsage=clientAuth' \
	"subjectAltName=URI:spiffe://opensoha.local/network-control/nas/$nas_runtime_id" \
	'subjectKeyIdentifier=hash' \
	'authorityKeyIdentifier=keyid,issuer' > "$work_dir/freeradius-control.ext"
openssl genpkey -algorithm EC -pkeyopt ec_paramgen_curve:P-256 -out "$work_dir/freeradius-control/tls.key" >/dev/null 2>&1
openssl req -new -sha256 -key "$work_dir/freeradius-control/tls.key" -subj "/CN=$nas_runtime_id" -out "$work_dir/freeradius-control.csr" >/dev/null 2>&1
openssl x509 -req -sha256 -days 7 \
	-in "$work_dir/freeradius-control.csr" \
	-CA "$work_dir/issuer/ca.crt" \
	-CAkey "$work_dir/issuer/ca.key" \
	-CAserial "$work_dir/issuer/ca.srl" \
	-extfile "$work_dir/freeradius-control.ext" \
	-out "$work_dir/freeradius-control/tls.crt" >/dev/null 2>&1

printf '%s\n' \
	'basicConstraints=critical,CA:FALSE' \
	'keyUsage=critical,digitalSignature,keyAgreement' \
	'extendedKeyUsage=serverAuth' \
	'subjectAltName=DNS:freeradius' \
	'subjectKeyIdentifier=hash' \
	'authorityKeyIdentifier=keyid,issuer' > "$work_dir/server.ext"
openssl genpkey -algorithm EC -pkeyopt ec_paramgen_curve:P-256 -out "$work_dir/freeradius-eap/tls.key" >/dev/null 2>&1
openssl req -new -sha256 -key "$work_dir/freeradius-eap/tls.key" -subj '/CN=freeradius' -out "$work_dir/server.csr" >/dev/null 2>&1
openssl x509 -req -sha256 -days 7 \
	-in "$work_dir/server.csr" \
	-CA "$work_dir/issuer/ca.crt" \
	-CAkey "$work_dir/issuer/ca.key" \
	-CAcreateserial \
	-extfile "$work_dir/server.ext" \
	-out "$work_dir/freeradius-eap/tls.crt" >/dev/null 2>&1

printf '%s\n' \
	'basicConstraints=critical,CA:FALSE' \
	'keyUsage=critical,digitalSignature' \
	'extendedKeyUsage=clientAuth' \
	"subjectAltName=URI:spiffe://opensoha.local/network-control/endpoint/$endpoint_runtime_id" \
	'subjectKeyIdentifier=hash' \
	'authorityKeyIdentifier=keyid,issuer' > "$work_dir/client.ext"
openssl genpkey -algorithm EC -pkeyopt ec_paramgen_curve:P-256 -out "$work_dir/eap-test-client/tls.key" >/dev/null 2>&1
openssl req -new -sha256 -key "$work_dir/eap-test-client/tls.key" -subj "/CN=$subject_id" -out "$work_dir/client.csr" >/dev/null 2>&1
openssl x509 -req -sha256 -days 7 \
	-in "$work_dir/client.csr" \
	-CA "$work_dir/issuer/ca.crt" \
	-CAkey "$work_dir/issuer/ca.key" \
	-CAserial "$work_dir/issuer/ca.srl" \
	-extfile "$work_dir/client.ext" \
	-out "$work_dir/eap-test-client/tls.crt" >/dev/null 2>&1

for target_dir in network-control freeradius-control freeradius-eap eap-test-client; do
	cp "$work_dir/issuer/ca.crt" "$work_dir/$target_dir/ca.crt"
done
sed "s|replace-with-soha-subject-id|$subject_id|" \
	"$script_dir/eapol-test.conf.example" > "$work_dir/eap-test-client/eapol-test.conf"
rm "$work_dir/network-control.csr" "$work_dir/network-control.ext" \
	"$work_dir/freeradius-control.csr" "$work_dir/freeradius-control.ext" \
	"$work_dir/server.csr" "$work_dir/server.ext" "$work_dir/client.csr" "$work_dir/client.ext" "$work_dir/issuer/ca.srl"
chmod 0600 "$work_dir/issuer/ca.key" "$work_dir/network-control/tls.key" "$work_dir/freeradius-control/tls.key" \
	"$work_dir/freeradius-eap/tls.key" "$work_dir/eap-test-client/tls.key"
chmod 0644 "$work_dir/issuer/ca.crt" \
	"$work_dir/network-control/ca.crt" "$work_dir/network-control/tls.crt" \
	"$work_dir/freeradius-control/ca.crt" "$work_dir/freeradius-control/tls.crt" \
	"$work_dir/freeradius-eap/ca.crt" "$work_dir/freeradius-eap/tls.crt" \
	"$work_dir/eap-test-client/ca.crt" "$work_dir/eap-test-client/tls.crt" "$work_dir/eap-test-client/eapol-test.conf"

mv "$work_dir" "$output_dir"
trap - EXIT HUP INT TERM
printf 'Created disposable EAP-TLS test PKI in %s\n' "$output_dir"
