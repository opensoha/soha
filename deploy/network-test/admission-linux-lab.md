# Isolated Linux admission lab (Task122 B3 L02 A02)

This is a Linux software NAS fixture, not a physical AP/switch certification.
Nothing runs until an operator selects an explicit config and invokes `apply`.
`plan` reads config only; `prepare` writes a private owner marker and empty journal.
`apply` creates owned namespaces, moves two explicitly preallocated **empty**
mac80211_hwsim phys, creates AP/STA interfaces and independent wired client links,
then runs FreeRADIUS, hostapd and wpa_supplicant under a foreground supervisor.
No module load/unload, host bridge change, host route or management link is used.

## Explicit inputs

Example JSON (replace every path and both preallocated radio identities):

```json
{
  "owner": "task122-b3-l02-a01",
  "project": "task122-admission",
  "state": "/var/lib/soha-task122-admission",
  "nas_id": "nas-task122-admission",
  "ap_phy": "phy91",
  "sta_phy": "phy92",
  "lease_seconds": 10,
  "radius_config": "/srv/task122/radius",
  "secret_file": "/srv/task122/shared-secret",
  "supplicant_config": "/srv/task122/wpa.conf",
  "event_file": "/srv/task122-radius-events/events.jsonl",
  "event_uid": 101,
  "session_grants": "/var/lib/soha-task122-admission/session-grants.json"
}
```

Run each stage explicitly:

```sh
sudo python3 admission-linux-lab.py plan --config /srv/task122/lab.json
sudo python3 admission-linux-lab.py prepare --config /srv/task122/lab.json
sudo python3 admission-linux-lab.py apply --config /srv/task122/lab.json
# Stop apply with SIGTERM/Ctrl-C, then inspect and clean with the same config:
sudo python3 admission-linux-lab.py status --config /srv/task122/lab.json
sudo python3 admission-linux-lab.py cleanup --config /srv/task122/lab.json
```

Install the lab JSON, shared-secret and supplicant config as root-owned regular
0600 files. Use a new state directory. It must be root-owned with mode 0700, and
the directory path and all its ancestors must be root-owned, free of symlinks,
and unwritable by group or other users. The same ancestor rules apply to the
lab JSON, secret, supplicant config, RADIUS config directory and grant output.
The operator is responsible for trusted FreeRADIUS configuration and includes.
Never elevate an old user-created state. Existing state with the previous
journal format needs manual resource reconciliation before using a fresh target.

The owner marker binds the complete config. Cleanup checks the original boot
and netns, recorded namespace inodes, PID start times and process netns, plus
each recorded hwsim interface's wiphy and ifindex. The tool requires an
unambiguous resource inventory before modifying it. Stop any operator-started
service in the lab namespaces before cleanup. Inspect partial or changed
identities before manual recovery. State, configs and logs remain private
after cleanup and can contain credentials. Apply, status and cleanup share a
non-truncating lock. Use namespace-bound read-only commands for live observation.

Prerequisites are Linux, root, iproute2 with bridge locked ports, nft bridge
concatenated sets with kernel timeouts, iw, hostapd, hostapd_cli, wpa_supplicant,
and FreeRADIUS. The environment owner allocates two empty hwsim phys in the
runner netns and records their actual identities; this script neither allocates
nor unloads shared radios. Cleanup deletes only the interfaces it created and
returns these phys to the original netns (checked by inode).

The RADIUS config is an operator-supplied private config directory, reusing
`deploy/freeradius` and its matching runtime inputs. It must listen on the AAA
interface `10.254.121.1`, accept NAS source `10.254.121.2` with the supplied secret,
and reach its Core/control/ingest backends through a separately prepared AAA
path. The script deliberately creates no host-management path or Docker network
attachment. The environment owner must provide that backend reachability inside
the AAA namespace. A missing backend is a failed authentication, never a grant.

The shared supplicant config must match SSID `Soha-Isolated-Lab` for Wi-Fi and
support wired EAP. Select EAP-TLS or TTLS/PAP using the existing PKI/EAP fixtures;
server CA and expected server name must be verified. This script does not
provision OS trust or certificates. Both wired clients have separate veth pairs
and distinct NAS ingress; a second client forging a first client MAC cannot
match the first ingress authorization.

## Authorization boundary and remaining integration

Before admission, only EAPOL reaches the authenticator. NAS has no business IP,
management NIC or forwarding route. Kernel bridge filtering defaults to drop
for both directions, with no conntrack established bypass. Wired ports are locked
with learning disabled; authorized static FDB entries alone cannot bypass nft.

`AUTHORIZED` from hostapd is **insufficient**. `admission-session-grants.py`
correlates successful Core authorization and actual NAS Accounting Start with
current hostapd ingress, normalized MAC and `acct_session_id`. The lab starts this
producer automatically when `event_file` is configured, tracks its process, and
closes admission if it exits. Standalone commands use the **same** lab config:

```sh
sudo python3 admission-session-grants.py run --config /srv/task122/lab.json
sudo python3 admission-session-grants.py status --config /srv/task122/lab.json
```

Do not run a second producer alongside the automatically started one. A separate
producer lock prevents competing output writers. `status` validates and reports
source records without probing hostapd or granting admission. Before starting the
lab, the environment owner must prepare an empty private JSONL file, owned by root
or the exact configured `event_uid`, mode 0600, one link and no symlink components.
Its parent must allow the intended FreeRADIUS writer to append. The grant output
uses the same protected directory and private file rules as the lab state. The actual
FreeRADIUS UID is an environment input; `101` above is only an example.

FreeRADIUS writes this dedicated file **only after** the Core post-auth verifier
accepted authorization or the Core Accounting callback accepted the NAS event.
The source must exclude radclient/eapol_test synthetic lifecycle routes. All
fields are nonsecret; passwords, shared secrets and key material must never be
logged. Exact schema v1:

```json
{"schemaVersion":"soha-admission-event/v1","event":"authorize","nasId":"nas-task122-admission","nasPortId":"w1","stationId":"02-00-00-00-00-01","coreSessionId":"core-session-1","observedAt":1800000000,"sessionTimeout":30,"filterId":"lab-tcp8000-only","vlanId":null}
{"schemaVersion":"soha-admission-event/v1","event":"start","nasId":"nas-task122-admission","nasPortId":"w1","stationId":"02:00:00:00:00:01","coreSessionId":"core-session-1","observedAt":1800000001,"accountingSessionId":"hostapd-acct-session-1","sessionTimeSeconds":0}
```

`event` is `authorize`, `start`, `interim-update` or `stop`; unknown/extra fields
fail closed. `observedAt` is the trusted server observation epoch (integer or
finite fractional seconds), not a client-controlled timestamp. Authorization has
exactly `sessionTimeout` (positive integer, at most 86400), `filterId`, and
`vlanId`, and no accounting fields. Accounting has exactly `accountingSessionId`
and `sessionTimeSeconds` (nonnegative integer), and cannot change authorization
attributes. Core session comes from the successful authorization Class and its
corresponding NAS Accounting Class. `stationId` accepts six MAC octets with a
consistent colon or hyphen separator and either case, then normalizes to lower
case with colons. `nasPortId` must be `ap0`, `w1`, or `w2`; the generated hostapd
config explicitly supplies RADIUS attribute 87 for both auth and accounting.
The native [hostapd configuration example](https://android.googlesource.com/platform/external/wpa_supplicant_8/+/master/hostapd/hostapd.conf)
documents the request-attribute syntax; selected Linux tool compatibility still
requires real environment acceptance.

A grant requires a matching authorization followed by Start (session time zero),
then matching current hostapd `AUTHORIZED` state and accounting session. Interim
observations and session times must advance; accounting cannot go backwards or
move across Core/NAS/port/MAC/session identities. Duplicate events are rejected.
A newer Start retires the older session; a correctly matched delayed old Stop
only terminates that old session and cannot end the new one. Stop is terminal.

The immutable hard deadline is `authorize.observedAt + sessionTimeout`. Interim,
producer restart and new grant output never extend it. Grant output keeps the
A01 list format, with v1 schema, producer owner/project, NAS/Core session identity,
`produced_at`, `hard_expires_at` and bounded `expires_at`. Consumer validation
rejects stale output, mismatched ownership/session or an expiry beyond the hard
bound. Before the first grant publication the supervisor denies admission while
waiting. A missing file after publication, malformed or untrusted output, or
producer death stops the supervisor and flushes grants. Abrupt termination still
leaves at most the configured kernel lease. Polls never renew the same session.

The source inode/device and complete observed prefix hash (including incomplete
trailing bytes) are pinned in `state/event-source.json` across restarts. Rotation,
replacement, truncation, prefix modification, untrusted permissions, malformed or
unknown events stop the producer and clear grants. A partial append waits for its
newline. Logs are capped at 16 MiB and event lines at 8192 bytes for this bounded
lab. Do not rotate the active file. Recovery requires stopping admission, retaining
the exact old source/pin as evidence, then preparing a new explicitly owned lab
source; the tool does not adopt or reset an unknown source.

Only `filterId=lab-tcp8000-only` and `vlanId=null` are supported. Configure that
exact Core SiteProfileBinding for this lab, with no VLAN assignment. Every
nonnull VLAN (including numeric 0; the logger maps an absent Core VLAN to null)
and other Filter-ID is denied. Dynamic VLAN application/readback remains
**unsupported**; no unobserved VLAN/filter result is presented as enforced.
The whitelist has the explicit fixed data-plane meaning below. CoA remains
unsupported. Software correlation tests do not prove real RADIUS module loading,
EAP, AP association or packet enforcement.

The only supported fixture profile allows TCP port 8000 to service
`10.254.122.2` / `fd42:122::2`, plus target-specific IPv4 ARP. Static IPv6 neighbors
avoid broad ND exceptions. Start the resource service explicitly in the service
namespace, bound to its business address, after inspecting `plan`; service startup
and packet probe recording belong to the environment acceptance owner. Other
resources/protocols remain denied even after authentication.

Each grant installs one bounded kernel timeout for both directions. Same-session
polls never renew it; expiry deauthenticates the station and requires a new
session. Reject, missing grant, malformed state, authenticator death or supervisor
error removes access; SIGKILL leaves at most the configured 2–30 second kernel
lease. Restart cannot silently resume a recorded lab. No CoA/Disconnect listener
is implemented; its capability is explicitly unsupported. An ACK is not evidence
of actual traffic blocking.

Hostapd Accounting is a NAS-origin lifecycle source. `radius-lab.py` radclient
Start/Interim/Stop remain **synthetic Accounting**, and eapol_test remains a
protocol-only test. Neither is evidence that a controlled ingress passed data.

## RADIUS lab target isolation

`radius-lab.py` retains action names but now requires all explicit target flags:

```sh
python3 radius-lab.py prepare \
  --project task122-radius --owner task122-b3-l02-a01 \
  --state /var/lib/soha-task122-radius \
  --core-image example/core:explicit-revision \
  --radius-image example/radius:explicit-revision \
  --eap-image example/eap:explicit-revision \
  --api http://127.0.0.1:29180/api/v1 \
  --api-port 29180 --control-port 29182 --ingest-port 29183 \
  --runtime-id radius-task122 --nas-id nas-task122 --device-id endpoint-task122
```

Use the same flags for subsequent actions. Container/network/volume owner labels
and the full state marker are checked before reuse or stop. `--legacy-task53`
is an explicit compatibility target selector, never a fallback. Old version-1
markers and unlabeled resources are refused; their adoption/migration is outside
this tool. No old lab is silently touched.

## Verification limits

Run the two scripts directly (unittest discovery skips hyphenated module names):

```sh
rtk opensoha-verify run -- python3 deploy/network-test/radius-lab_test.py
rtk opensoha-verify run -- python3 deploy/network-test/admission-linux-lab_test.py
rtk opensoha-verify run -- python3 deploy/network-test/admission-session-grants_test.py
```

The Linux/producer unit tests mock commands and time. They verify generated
topology, identity guards, default deny, event/session correlation, replay and
ordering rejection, Stop retirement, original hard deadlines, unsupported
attributes, file replacement/truncation/permission rejection and bounded
input/output grants. Real Linux kernel/iproute2 command compatibility, EAP-TLS/TTLS credentials,
AAA backend reachability, AP association, locked-port EAPOL delivery, packet
blocking/expiry, resource probes and cleanup recovery remain unverified. This
revision does not close V32/V33/V35. V34 synthetic protocol coverage remains
separate from NAS-origin and real data-plane evidence.
