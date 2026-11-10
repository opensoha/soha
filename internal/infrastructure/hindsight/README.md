# Optional Hindsight memory backend

This adapter targets the fixed Hindsight 1.2.0 API qualified in
`../../../scripts/hindsight-qualification/versions.json`. Core enables it through
the optional Hindsight bootstrap configuration.

Apply migrations through `0145_memory_lifecycle.sql` before starting the updated
memory repository. CRUD commits revisions and outbox entries atomically, and the
lifecycle migration fences old writers and reconciles existing deletion receipts.

Composition root:

```go
client, err := hindsight.NewClient(hindsight.Config{
    BaseURL: serviceURL,
    Token: serviceToken, // required: inject from a Secret; no default token
    Timeout: 15 * time.Second,
}, nil)
// Handle err before injecting client.
backend, err := memory.NewBackendService(repo, repo, client, sourceValidator, instanceNamespace)
```

`repo` is `repository/memory.New(db)`; `sourceValidator` must implement
`ValidateSource(context.Context, memory.Record) error`, checking current source
existence/revision and caller access. All dependencies are required, including
rejection of typed nil. The caller resolves trusted owner/scope values before
`Recall`; never expose this internal method as a client-supplied bank selector.
The persisted opaque bank mapping rejects an instance namespace change until an
explicit migration. Owner/scope changes require a new record ID plus deletion of
the old record. Deleted IDs cannot be resurrected.

Bootstrap must own scheduling and shutdown. Call `SyncOne(ctx)` repeatedly with a
bounded idle delay; it processes one step, returning `false,nil` when no job is
ready. Each job has a 2-minute lease, the application step is bounded to 90 seconds,
and the HTTP timeout defaults to 15 seconds (maximum 30). Retry/unknown jobs become
eligible again after 10 seconds. Expiry queues raw document cleanup on worker polls.
Retain UUIDs, submission intent, revision document IDs, dependencies and deletion
state survive restart in PostgreSQL. A timeout/lost ACK triggers operation lookup;
if a previously attempted operation is missing, state remains `unknown` and lookup
continues. It does not blindly re-submit after the vendor's operation retention
window. Unknown submissions require operator reconciliation before cleanup can be
proved complete. Failed or cancelled operations release dependent deletes only
when their workers are confirmed settled. The protected gateway acknowledges the
purge of the settled operation and its task payload separately from document
deletion. Core requires the matching acknowledgement before completing cleanup.

Only asynchronous retain, operation lookup, raw world/experience recall and raw
document deletion are exposed. Recall projects the currently authorized Soha fact;
provider generated text, observations and missing provenance never become confirmed
user facts. Missing sources, revisions, policies, wrong owner/scope, tombstones and
expiry fail closed. Reflect, mental models and automatic consolidation are not
exposed in this batch; their provenance/deletion acceptance remains future work.
Deployment must disable those background capabilities and route all actual
extraction/embedding/rerank traffic through the controlled Gateway. This client
does not supply model keys or prove provider egress isolation. Requiring a token in this client only guarantees a
Bearer header is sent; it does not prove Hindsight authenticates that header.
The deployment owner must configure and verify upstream service authentication.

Focused checks:

```sh
opensoha-verify run -- env GOWORK=off go test -race -count=1 ./internal/application/memory ./internal/infrastructure/hindsight
# Only a private, disposable loopback PostgreSQL cluster may supply this variable.
# The DB name must be task123_memory_test; the test creates/drops its own random schema.
opensoha-verify run -- env GOWORK=off go test -race -count=1 -v ./internal/repository/memory -run TestSyncPostgresDurability
```

The PG test reads `TASK123_MEMORY_TEST_DSN` and skips when it is absent. A package
PASS with that skip is not persistence evidence. HTTP tests use a synthetic server;
they do not establish real model/Gateway/embedding quality or production bootstrap
acceptance.
