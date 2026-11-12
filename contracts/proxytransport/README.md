# Guarded proxy transports

TCP keeps the existing `New` / `Handler` behavior. UDP is opt-in plumbing; it
is not proof of Mihomo/upstream/platform UDP compatibility. Advertise
`guarded_udp_v1` only after installing and verifying the real guarded chain.
`ErrPacketUnsupported` from a verified dial callback maps to a static HTTP 501
with the UDP transport header. The client recognizes it only after verified TLS
and exact protocol-header checks, without reading/echoing the response body.
Other unsafe protocol responses are not converted into unsupported.
TCP-only upstreams must produce that static unsupported result, never a direct OS
UDP fallback or a downloaded replacement transport.

## Frozen UDP v1

The approved internal HTTPS endpoint accepts HTTP/1 CONNECT `host:port` with
exactly one `Soha-Transport: udp/1` and the existing HTTP Basic mapped username
(`EncodeMappedUsername`) and credential secret. The 200 response must echo
exactly one identical transport header. Missing/unknown/duplicate transport
values fail the UDP handshake. Header-free requests continue to the existing
TCP Handler. This private protocol is **not** RFC 9298 CONNECT-UDP.

After the headers, each datagram is a two-byte unsigned big-endian payload length
followed by exactly that many bytes. Zero is a valid empty datagram, not EOF.
The payload maximum is 65507 bytes. Oversize/truncated frames and insufficient
Read buffers close the association; data is never truncated. Each remote
association has one immutable destination; frames contain no alternative target,
credential or upstream/source metadata. TLS stream transport can cause head of
line blocking; this does not claim native UDP timing, QUIC performance or loss
semantics.

A SOCKS UDP datagram must additionally fit its **whole header plus payload**
within 65507 bytes. Header space is charged per address type; oversized replies
are rejected, not fragmented/truncated. RSV is zero and FRAG must be zero;
SOCKS fragmentation is unsupported. This rule does not promise that the OS or
an upstream network never performs IP fragmentation.

## Shared APIs

```go
type DialPacket func(context.Context, string /* user */, string /* target */) (net.Conn, error)
type PacketAuthorize func(context.Context, string /* user */, string /* target */) error

NewPacketConn(net.Conn, time.Time) (*PacketConn, error)
NewPacketHandler(map[string]string, DialPacket, PacketAuthorize, time.Time) (*PacketHandler, error)
DialPacketTunnel(context.Context, net.Conn /* verified *tls.Conn */, string /* target */, string /* user */, string /* secret */, time.Time) (*PacketConn, error)
NewSOCKS5Gate(map[string]string, Dial, DialPacket, PacketAuthorize, time.Time) (*SOCKS5Gate, error)
DialSOCKS5UDP(context.Context, string /* owned loopback backend */, string /* user */, string /* secret */, string /* pinned literal IP:port */, time.Time) (net.Conn, error)
```

`Read` / `Write` on packet connections each represent one datagram, including
empty datagrams. Never use `io.Copy` to bridge a packet connection.
`PacketConn.Refresh`, `PacketHandler.Refresh` and `SOCKS5Gate.Refresh` require
that the previous lease is still alive. The next expiry must be in the future
and at most five minutes from now. There is no arbitrary five-minute lifetime
limit on a continuously authorized flow. Callers cap renewal by the current
configuration/certificate/credential limits and cannot renew old authorization
past an in-flight check's expiry. Expired, closed or idle packet connections
cannot be revived; a new association needs fresh authorization.

`PacketAuthorize` is required before forwarding **each datagram in both
directions**. Callers recheck current node/credential/epoch/scope, ProtectedSet,
mandatory rules, approved destination resolution and their hard expiry. A
bounded current grant/cache may be used only until its expiry; online grants
must expire within five seconds. The package does not grant access based on
capabilities or a successful earlier handshake. Dial callbacks resolve, validate
all DNS answers and pin one authorized IP. Packet callbacks and dials have
bounded contexts; callers must honor cancellation.

Packet connections have a 30-second activity idle window. Renewals do not reset
activity. Per-packet operations are serialized per direction, with no unbounded
queue or multiplexing buffer. Each gate permits at most 4096 controls and at
most 4096 destination-specific UDP associations; each local SOCKS control has
at most 128 inner destinations. These are upper bounds, not capacity claims.
`ActiveCount` counts admitted **UDP destination associations**, not TCP controls,
and is not an overall TCP+UDP service count. Callers combine it with TCP counts
for drain/stop proof, or track the returned connections themselves.

## Local SOCKS boundary and backend

`SOCKS5Gate.Serve` accepts only a loopback listener and loopback clients. It
supports RFC 1929 password authentication, TCP CONNECT via the supplied existing
`Dial`, and UDP ASSOCIATE through `DialPacket`. Local usernames/passwords have a
255-byte RFC limit: use short random/node-local names, not the potentially
354-byte mapped HTTP username. Keep local credentials private.

Each TCP UDP control owns a dedicated loopback UDP socket. The source IP must
match the TCP peer. A requested nonzero port must match exactly; port zero pins
the first valid datagram's source port. A malicious local process capable of
racing/spoofing that source remains inside the local-host trust boundary; this
is not process identity isolation. The control's close/expiry closes its socket
and children. Multiple destinations on one SOCKS control get independent
single-destination inner associations; there is no remote wildcard association.

`DialSOCKS5UDP` connects only to an installer-owned **literal loopback** TCP
backend, performs password authentication and UDP ASSOCIATE, and requires a
literal loopback relay reply. It connects an OS UDP socket only to that relay;
the owned engine routes to the target. Replies must carry the pinned target,
RSV=0 and FRAG=0. Closing it closes both relay UDP and control TCP. The caller
verifies backend process/port ownership and UDP support for the selected node;
loopback address validation alone is not ownership proof.

## Lifecycle and integration

Use an explicit HTTP dispatcher: requests with `Soha-Transport` go to
`PacketHandler`; absent header goes to the legacy TCP handler. No existing TCP
code or authentication behavior is changed by installing these new types.

`NoNew` denies new associations and new local SOCKS destinations, retaining
already admitted destinations for bounded drain. `Close` terminates the gate
and all owned associations/controls. It cannot be reversed with Refresh.
Callers retain per-user/node connection registries and close only revoked node
connections immediately; the next per-packet check independently rejects stale
scope. Do not implement node revocation by closing a shared gate for unrelated B.

Secrets, payloads, original source URLs and upstream parameters must never be
logged or exported. Shared code emits static errors only. Real validation still
requires the pinned Mihomo native SOCKS UDP backend, distinct A/B upstreams,
ProtectedSet/rule refusal, revoked A with surviving B, grant expiry/late renewal,
and drain/stop confirmation; package socket tests are not that acceptance.

## Original SOCKS5 upstream relay boundary

`UpstreamConnect func(context.Context) (net.Conn, error)` is supplied by the
caller. Both `DialSOCKS5UpstreamTCP` and `DialSOCKS5UpstreamUDP` take
`(ctx, connect, approvedUpstreamIP netip.Addr, user, secret, pinnedTarget, deadline)`.
The caller owns source Scope resolution/pinning, verified TLS/SNI when required,
source credentials/authentication choice and original-node compatibility. The
factory returns the connected TCP/TLS stream before SOCKS negotiation. The helpers
first check that the connected TCP/TLS `RemoteAddr` equals the explicit approved
source IP, then perform SOCKS authentication and request negotiation. Empty user plus empty secret selects NoAuth;
otherwise upstream RFC 1929 credentials are bounded to 255 bytes each.

The UDP helper accepts only a literal same-IP BND relay; unspecified IPv4/IPv6
BND addresses become the approved IP. Other IPs, hostnames and invalid ports are
rejected before creating the UDP socket. The OS socket connects only to the
approved proxy IP and validated relay port, never directly to `pinnedTarget`.
Replies must retain the literal pinned target, FRAG=0 and the total datagram
budget. This helper supplements caller authorization/protection/lease checks;
it neither grants source/target access nor proves an upstream server's UDP
support. Only the original SOCKS5 node needs this controlled second loopback
bridge; it is not a general-purpose network sandbox.
