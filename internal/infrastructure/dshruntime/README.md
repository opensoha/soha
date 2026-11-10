# Trusted dsh service client

This infrastructure adapter follows
[`soha-contracts/agent/dsh-runtime.schema.json`](../../../../soha-contracts/agent/dsh-runtime.schema.json)
and its HTTP binding. It connects Core directly to the authenticated service;
it does not use the Go Agent claim/callback path. B2 does not register it in
production bootstrap or change application defaults.

```go
client, err := dshruntime.New(dshruntime.Config{
    BaseURL: "http://127.0.0.1:19091", // loopback for isolated integration only
    ServiceToken: token,
    Timeout: 5 * time.Second,
})
// Handle err; close idle connections when the consumer shuts down.
defer client.Close()
health, err := client.Health(ctx) // inspect health.Ready, including ready=false
receipt, err := client.Start(ctx, request) // StartRequest, no automatic retries
status, err := client.Status(ctx, receipt.Submission)
page, err := client.Events(ctx, receipt.Identity, 0)
status, err = client.Cancel(ctx, receipt.Submission, "user")
```

The consumer owns Run/attempt/fence, the frozen opaque Start input hash and
submission identity. StartRequest uses the wire field names with Go ID suffixes
(`RunID`, `SessionID`, `RequestID`); the client fills schemaVersion/kind. Deadline
is RFC3339 and must be in the future. No cross-language canonical JSON hashing
occurs here. The callback's **ToolCall** input hash is separately defined as the
hash of the exact transmitted raw input JSON; this package does not implement
the callback or reserialize tool input for hash verification.

Status and Cancel require the full expected Submission. Events requires the
expected Identity and exclusive safe-integer cursor. Events are strictly
ascending, with nextSequence equal to the last item or the supplied empty-page
cursor; resume from nextSequence. The client checks per-response and per-page
limits; the service owns the cumulative 128 KiB output bound across pages.
The consumer must stop using results when err is non-nil. Unknown usage is
`UsageKnown=false, Usage=nil`; unknown status is not completion. Terminal()
only recognizes completed/failed/canceled service facts, never SDK idle.

New requires HTTPS, except literal loopback IPs or localhost over HTTP for local
integration, a nonempty service token and timeout in (0, 5 minutes]. Trusted
startup may explicitly set AllowInsecureHTTP=true for an isolated private
service network with Bearer authentication; the default is false. Never derive
this setting or BaseURL from user/model input. HTTPS verifies system CAs. It owns a
private transport with no environment proxy and rejects redirects. Bodies are
bounded to 1 MiB, response JSON is strict (including duplicate keys, required
fields and nulls), and responses bind the expected identity. HTTPError exposes
only StatusCode; transport/protocol errors never include remote bodies or URLs.
ToolGrant is only serialized into Start, never response types or request logs.
No calls are automatically retried or resubmitted after timeout; reconcile
through lookup before an explicit consumer decision.

The service remains responsible for supported models/catalog/schema forms,
authorization, durable acceptance, per-Run isolation and cancellation proof.
This adapter validates basic input structure/bounds and forbids remote schema
references without fetching or interpreting the tool schema.

Focused self-check: `GOWORK=off opensoha-verify run -- go test -race ./internal/infrastructure/dshruntime`.
Real Node/Core integration and production wiring are separate batch checks.
