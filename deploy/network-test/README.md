# Network Runtime Compose Labs And Smoke Tests

Component smoke containers and disposable lab topologies live in `deploy/docker-compose.yaml` behind opt-in profiles and remain disabled by default.

Related environment fixtures in this directory:

- [admission-linux-lab.md](./admission-linux-lab.md): isolated Linux `hostapd` / `wpa_supplicant` / FreeRADIUS admission fixture
- [vpn-lab.md](./vpn-lab.md): macOS native `utun` WireGuard + DNS + ProtectedSet Docker lab

## Component Smoke (`network-test`)

`network-test` provides `mihomo`, `radclient`, a one-shot EAP-TLS `eapol_test` client, and a `NET_ADMIN`-scoped WireGuard/HTTP test client:

```bash
docker compose -f deploy/docker-compose.yaml --profile network-test up -d --build \
  mihomo-test network-test-client radius-test-client
docker compose -f deploy/docker-compose.yaml --profile network-test exec -T network-test-client \
  sh -c 'curl -fsS --proxy http://mihomo-test:7890 -H "Authorization: Bearer soha-network-test" http://mihomo-test:9090/version >/dev/null && ip link add wg-smoke type wireguard && wg show interfaces && ip link del wg-smoke'
docker compose -f deploy/docker-compose.yaml --profile network-test exec -T radius-test-client radclient -v
```

## Disposable EAP-TLS Handshake Lab

Generate a seven-day disposable lab CA and matching `network-control`, NAS, FreeRADIUS, and endpoint identities under the gitignored runtime directory. The endpoint certificate is shared with the EAP client so its serial and authority key ID match the credential created by endpoint enrollment:

```bash
sh deploy/network-test/generate-eap-test-pki.sh \
  deploy/network-runtime-tls/eap-lab <soha-subject-id>
```

Start `network-control` and FreeRADIUS with the generated directories, create an enrollment for runtime `endpoint-eap-test`, and consume it with `eap-test-client/tls.crt` and `tls.key` (do not insert a certificate binding directly). Then run the client in the FreeRADIUS network namespace so the loopback-only lab NAS remains closed to other containers:

```bash
SOHA_NETWORK_CONTROL_TLS_DIR=./network-runtime-tls/eap-lab/network-control \
SOHA_RADIUS_CONTROL_CLIENT_TLS_DIR=./network-runtime-tls/eap-lab/freeradius-control \
SOHA_RADIUS_EAP_TLS_DIR=./network-runtime-tls/eap-lab/freeradius-eap \
SOHA_RADIUS_EAP_TEST_TLS_DIR=./network-runtime-tls/eap-lab/eap-test-client \
docker compose -f deploy/docker-compose.yaml --profile network-access \
  --profile network-test run --rm --build radius-eap-test-client
```

Success requires `CTRL-EVENT-EAP-SUCCESS` and an `Access-Accept`; a TLS failure, unknown or revoked certificate binding, or denied policy must exit non-zero. `eapol_test` only accepts the shared secret via command-line arguments, so never use a production NAS secret for this test. Keep `issuer/ca.key` offline from every container and delete the lab directory after validation.

## Multi-Site Hub-And-Spoke Lab (`network-multisite-test`)

Run `network-access` together with `network-multisite-test` for an A/B/C WireGuard topology. The default `network-gateway` acts as hub A; the profile adds spoke B on UDP `51821` and spoke C on UDP `51822` with separate keys, enrollment identities, and data volumes. In the workbench, leave A's `hubGatewayId` empty, set B and C to A's gateway ID, and advertise only non-overlapping CIDRs owned by each site. B-to-C traffic transits A:

```bash
docker compose -f deploy/docker-compose.yaml --profile network-access \
  --profile network-multisite-test up -d --build \
  network-gateway network-gateway-b network-gateway-c
```
