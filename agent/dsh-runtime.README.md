# Core → dsh runtime v1

Source: [dsh-runtime.schema.json](dsh-runtime.schema.json). Protocol version:
`opensoha.dev/dsh-runtime/v1`. This authenticated service protocol is separate
from the public user API and the Go Agent runner protocol. Core connects directly
to a trusted Node service. The service wraps the fixed dsh SDK; dsh owns the model
and tool loop. Core owns authorization, business tasks, approvals and Run facts.

## Messages and limits

Validate the complete message against the root strict `oneOf`, or the matching
`$defs.Start`, `Receipt`, `Status`, `Event`, `ToolCall`, `ToolResult`, `Control`,
`Health`. Each message requires schemaVersion and kind; all run-bound messages
require runId, attempt and fence. IDs are 1–128 ASCII letters/digits plus `._:-`,
starting with a letter/digit; attempt/fence are positive safe integers.

Start freezes ownerKey (trusted opaque owner/scope binding), sessionId, requestId,
inputHash (lowercase SHA256), contextRevision, profile (`chat`/`heya`), model,
prompt, toolCatalogRevision, deadline and maxOutputTokens. Optional context holds
only user/assistant messages. Optional tools each require name, description,
inputSchema and readOnly. Chat accepts no tools or only readOnly=true tools.
Heya remains subject to server-side catalog/schema/allowlist and current Core
authorization; readOnly=false is never a grant. Reject duplicate tool names,
unsupported models/catalog revisions and expired deadlines before acceptance.

Model is an identifier only. Model Gateway base URL, credentials and the Core
tool callback URL come exclusively from trusted service startup configuration;
no caller-supplied URL or native tool/plugin configuration is allowed. The
optional assembly contains only a profile ID, its immutable revision and
versioned asset references. A runtime that does not support assembly rejects
the request before admission. toolGrant
is optional, short-lived, opaque, HTTP-input-only and writeOnly. Never persist it
in receipt/status/event journal, expose it to the model, or include it in logs.

Status always returns submissionId, inputHash, sequence, status and usageKnown.
usageKnown=true requires usage; false forbids usage (unknown is not zero usage).
Receipt has the same submission identity/hash/status/sequence, without usage.
Status values: accepted/running/waiting_tool/completed/failed/canceled/unknown.
Sequence is a safe integer ≥0 for snapshots and ≥1 for Events. It is monotonic
per run/attempt and is independent of Core user-event cursors.

Event data only permits delta, assistantText, callId, toolName, status, stopReason,
errorCode, usageKnown and usage. message_delta requires delta. A delta is at most
16,384 characters and 16 KiB UTF-8; cumulative assistant output and terminal
assistantText are at most 131,072 characters and 128 KiB UTF-8 per run. Services
must enforce cumulative byte limits before journaling or responding. A single
message/event/page body is at most 1 MiB UTF-8; a Start is also limited to 1 MiB
including all context/tools. Reject oversized input with 413. Schema character
limits alone do not enforce aggregate byte limits.

Context has at most 128 messages; tools at most 64; each description at most
4,096 characters. JSON tool schemas, input and output have at most 64 properties
per object, 256 items per array, 128 characters per key and 131,072 per string.
The service additionally limits these JSON values to depth 16 and 128 KiB each.
Only locally supported tool schemas are allowed: validate as JSON Schema and
reject remote references or schema forms the runtime cannot enforce. Do not
fetch tool schema references from untrusted URLs. Grants, credentials and raw
provider payloads must never be copied into ToolResult, events or status. Known
output fields are not a substitute for runtime secret redaction.

ToolCall binds callId, toolCatalogRevision, toolName, frozen inputHash and input.
ToolResult echoes run/attempt/fence/callId/inputHash and reports
completed/denied/failed/unknown, optional bounded JSON output, errorCode and
receiptId. Core rechecks ownership, current authorization, approval and fence on
every call. A duplicated call reconciles its original business receipt; transport
timeout or unknown does not authorize automatic execution again. No arbitrary
native shell, filesystem, browser, package installation or MCP tool is enabled.

## HTTP binding

All endpoints, including health, require `Authorization: Bearer <service token>`.
The authenticated service identity must be authorized for the frozen request
identity; path runId/attempt must equal the request message. Use TLS or an
otherwise isolated trusted transport. Query identity does not replace auth.

| Method and path | Body / response |
| --- | --- |
| GET /v1/health | Health, 200; ready=false means unavailable |
| POST /v1/runs | Start → Receipt; 202 after durable acceptance; 200 for same-identity/same-hash duplicate |
| GET /v1/runs/:id/attempts/:attempt | Status, 200 |
| GET /v1/runs/:id/attempts/:attempt/events?after=N | `$defs.EventPage`, 200 |
| POST /v1/runs/:id/attempts/:attempt/cancel | Control → Status, 200 |

Events use a nonnegative safe integer after cursor (default 0), exclusive and
ascending. Page items all belong to the requested run/attempt/fence. The service
returns at most 256 events within 1 MiB. nextSequence equals the last returned
sequence or the supplied cursor for an empty page; hasMore means more durable
events currently exist. A client polls from nextSequence and deduplicates by
identity/sequence. Future cursors are rejected with 400. EventPage is a response
container, not an additional kind in the root message union.

ToolCall is POSTed to the fixed Core callback URL using authenticated service
transport and the run-bound toolGrant as a separate authorization header (never
in a journaled ToolCall); the response is ToolResult. The callback revalidates
all identity and hashes. No URL from tool input controls callback routing.

### Run assembly and native calls

An assembly-capable runtime advertises `runtime-assembly-v1`. It starts the
owned child for discovery and SDK initialization. It must obtain a positive
Core decision before sending the first prompt, invoking the model or dispatching
tools. It uses the same service token and Run grant as the tool callback.

| POST path relative to the Core callback parent | Body and result |
| --- | --- |
| `assembly` | `AssemblyReport` → `AssemblyDecision`. A loaded asset is bound to the exact selected kind, ID and version. A required failure is rejected. An optional failure is degraded. A duplicate identical report returns the same decision while still authorized. |
| `plugin-tools/authorize` | Frozen `ToolCall`. 204 authorizes one dispatch after the Core persists a reservation. 200 with a matching completed `ToolResult` is reuse of an existing observation and never authorizes another call. |
| `plugin-tools/settle` | Matching `ToolResult` → same result after persistence. A repeated identical settlement may reuse the receipt. A new result for the same call identity conflicts. |

An unconfirmed or rejected assembly does not authorize model or tool callbacks.
Native MCP and Skill tool definitions are fixed by the confirmed report, and
business tools continue through the existing Core bridge. Provider credentials
are absent from reports, decisions and journals. Never replay after a lost
dispatch permission or an uncertain invocation. The optional `hook_observed`
event reports only a call ID, tool name and fixed outcome. It records
observation and carries no execution authority.

Common failures: 400 invalid input/version/cursor, 401 absent/invalid service
authentication, 403 unauthorized binding/tool action, 404 unknown run/attempt,
409 identity/hash/fence conflict, 413 oversized body, 429 exhausted concurrent
execution slots, 503 not ready. Unknown attempts are never synthesized by status,
cancel or events lookups. Higher or lower fences cannot overwrite an accepted
attempt. A new attempt is a new explicit Core decision, never an HTTP retry.

## Core model and tool callbacks (B3)

Production uses Core-only model mode. The fixed callback is
`POST /api/v1/copilot/dsh/model/v1/chat/completions`; configure the model base URL
as `/api/v1/copilot/dsh/model/v1`. Direct Gateway access with a startup-configured
PAT is an explicitly selected diagnostic mode, never a production fallback.
There are no model retries or provider fallbacks after an uncertain model call.
Start fields and the ToolCall/ToolResult envelopes remain unchanged.

In Core mode, service-Bearer-authenticated Start comes only from trusted Core.
Start.tools is the frozen, authorized catalog for that run; toolCatalogRevision
is an opaque Core binding. Node compiles and validates these schemas separately
for each run and isolates the catalogs; it does not compare the revision with a
startup catalog revision. Core mode does not require TOOL_CATALOG_FILE (runtime
configuration SOHA_DSH_TOOL_CATALOG_FILE). Diagnostic Gateway mode still requires
its startup whitelist and matching catalog revision. Chat readOnly=true checks,
schema restrictions, and per-call current Core authorization remain mandatory.

Both callbacks require service `Authorization: Bearer <service token>` and
`X-Soha-Tool-Grant: <run-bound toolGrant>`. Neither credential may enter logs,
journals, model messages or user events. The model callback also requires:

| Header | Binding |
| --- | --- |
| X-Soha-Run-ID | Start.runId |
| X-Soha-Run-Attempt | Start.attempt, positive integer |
| X-Soha-Run-Fence | Start.fence, positive integer |
| X-Soha-Context-Revision | Start.contextRevision |
| X-Soha-Tool-Catalog-Revision | Start.toolCatalogRevision |
| X-Soha-Model-Call-ID | Unique identifier for this model call within the run/attempt |
| X-Soha-Input-Hash | Lowercase 64-hex SHA256 of the exact unchanged raw request body |

The body is raw OpenAI-compatible chat-completions JSON, at most 1 MiB UTF-8.
Forward its original bytes; never decode/re-encode before verifying Input-Hash.
Output is OpenAI-compatible streaming SSE, including usage when known. Core
checks the active run, current owner authorization, grant, lease/fence, context
and catalog revision and budget before dispatch. Unknown usage remains unknown.
A repeated model-call ID must not cause another model dispatch; reject/reconcile
uncertain delivery rather than retrying it. Authentication or binding errors fail
closed and never activate diagnostic Gateway mode.

The tool callback remains `POST /api/v1/copilot/dsh/tools`, with the same service
Bearer and X-Soha-Tool-Grant headers and unchanged ToolCall/ToolResult envelopes.
ToolCall.inputHash still covers only the exact raw input JSON value, as below;
the model callback Input-Hash covers its entire raw HTTP body.

## Hash binding

Start.inputHash is the frozen input digest owned by Core. The service binds and
echoes it; it is not a cross-language canonical-JSON checksum. Do not accept a
different semantic Start merely because its claimed hash matches. The service
computes its own private stable submission fingerprint over every Start field
except toolGrant, and compares both fingerprint and claimed hash for duplicates.
The fingerprint algorithm is local implementation detail, not another wire field.

ToolCall.inputHash is SHA256 of the exact UTF-8 JSON bytes of its input value as
transmitted in the HTTP body. Core preserves the raw input JSON slice (for example
Go json.RawMessage), verifies it before decoding, and never reserializes it for
hash verification. ToolResult echoes this same hash. Senders must hash the exact
input bytes they embed, not a separately reserialized object.

## Durability and stop

Persist metadata and the accepted event before returning 202. The service keeps
a receipt/event journal, not another business task queue. Reject at capacity with
429 before acceptance. Same run/attempt and same frozen hash returns the original
submission; changed frozen identity/hash/fence is 409. Lookup precedes any retry
after uncertain transport delivery. Do not automatically resubmit prompts.

Each active Run owns one isolated subprocess/home. Cancel is durable intent;
report canceled only after its subprocess has stopped and pending tool outcomes
are reconciled. Unknown side effects remain unknown. Terminal runs reject late
callbacks/events. On restart mark previously nonterminal accepted runs unknown,
retain their journal for lookup, and never replay them automatically. A stale
fence or revoked grant cannot resume execution. Runtime SDK idle alone is not a
causally bound completion receipt.

## Verification

`npm run validate:dsh-runtime` compiles the schema with Ajv 2020/formats, checks
valid/invalid fixtures, output length boundaries and EventPage semantics. Normal
`npm run validate` also includes this schema fixture directory. Consumer/runtime
conformance and the full repository/consumer gates are separate batch checks.
