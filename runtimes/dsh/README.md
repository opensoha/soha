# dsh qualification (Task123 B1-U1a)

This directory is an isolated qualification fixture, not a production runtime
service or a change to the Go Agent route. Core will call the independent runtime
service directly. Nothing here enables a default Chat/Heya route.

## Fixed input and reproduction

- Node >=24.9.0; qualification host: Node 24.14.0, macOS arm64.
- npm `@deepseek-ai/dsh` and `@deepseek-ai/dsh-sdk-client`: **0.2.2-rc.1**.
  `package-lock.json` fixes the dependency closure and registry integrity hashes.
- Both direct upstream packages declare MIT. The lock records dependency licenses;
  this is not a full legal or vulnerability audit.
- Research source: [fcc0ad462172522aaf5c93a7b2e01f6b9f0b0159](https://github.com/deepseek-ai/deepseek-harness/tree/fcc0ad462172522aaf5c93a7b2e01f6b9f0b0159).
  A source snapshot is not proof that an npm tarball was built from that commit.
  Installed package versions and tarball integrity are the runnable input.

From the soha-agent repository:

```sh
opensoha-verify run -- npm --prefix runtimes/dsh ci --ignore-scripts --no-audit --no-fund
opensoha-verify run -- npm --prefix runtimes/dsh run qualify
```

No model keys are needed. The fixture launches real dsh through the official SDK
against a deterministic local Messages/SSE peer bound to an ephemeral loopback
port. Its temporary root is under `OPENSOHA_VERIFY_DIR` (or an exact `mkdtemp`
directory). Each owner gets a new home, workspace and subprocess; child env is
replaced rather than inherited. It never uses user credentials/configuration.
It closes children and sockets and removes its exact temporary root on exit.
Output is a JSON assertion summary; copy it into task evidence if retention is
needed. Failures exit nonzero. No shared service, database or fixed port is used.

## What this fixture establishes

`qualification.mjs` checks the installed SDK/runtime match, stdio identity,
literal low-level prompts, durable prompt-message receipt correlation,
committed output, two Session contexts, separate owner homes/processes,
history/cwd restoration without new model input, absent cancel RPC, timeout not
cancelling model work, close reaping the process, and interrupted-history resume
followed by a new explicit prompt. The peer asserts zero advertised tools and
the exact model/output cap on every model request.

`chat-restricted.patch.yml` disables shell/PTY, filesystem and cwd tools, MCP
resources, automatic compaction, image offload, raw session-log upload and package
inventory upload in `sdk-minimal`. The stock
profile is **not** a Chat permission boundary: it has danger-full-access shell,
read_image and working_directory. It omits skills, plugin_manager, settings,
jobs UI and subagents but still has runtime storage/local services. The patch
does not provide an OS sandbox. Production must use a trusted immutable profile,
fresh owner-isolated home and workspace, no client-controlled patches/plugins,
least-privilege OS/container access and network egress enforcement. A restored
Session must be revalidated against current policy/context revision.

Use `HarnessClient.prompt`, not `DeepSeekHarness.run`/`session/query`, for product
chat text. The latter route native permission/goal/plan commands. Low-level prompt
bypasses native command routing but skill recognition still exists if a future
profile adds skills. Keep the composition closed and test its tool roster after
every version change. The fixture's native tool roster is intentionally empty;
Soha read-only tools and the Heya authorized tool bridge are later work.

## Protocol decisions for the service adapter

- Subscribe **before** prompt. Save the enqueue `messageId`, bind it to Soha
  message/run/attempt, and observe its durable `agent/inbox/spliced` receipt.
  A queued receipt is not a model reply. A high-level `finalResponse` covers an
  activity interval and does not prove exclusive prompt causality. Enforce one
  active Run/writer per Session and add Soha durable event sequence/replay.
- Runtime notifications include all Sessions; SDK filtering is client-side.
  Session IDs are not authorization. Never expose this raw transport to users.
- There is no wire cancel/session-close method or version negotiation. The
  handshake reports wire `serverInfo.version=0.0.1`; it is not the package version.
  The installed runtime rejects `session/cancel` with `-32603` and an unknown
  method message (not the transport's generic `-32601`). Exact package pins and
  qualification are needed on upgrade.
- Each active Run must own its process. Stop revokes the Soha tool grant/fence,
  closes the owning harness and confirms exit. A request timeout is merely a
  stopped waiter. Killing a process shared with other active Runs is unsuitable.
  External tool side effects need their own cancellation/unknown-result receipts.
- Resume restores persisted history/cwd and repairs an interrupted tail; it does
  not restore shell/browser processes or durable approvals. Never automatically
  replay a timed-out prompt or restore an old approval. Soha must reconcile
  accepted/unknown outcomes and recheck provenance/permissions before resuming.
  The interrupted user block can remain in the history sent with the next
  explicit prompt; process cancellation does not erase that input.
- The stock minimal DeepSeek adapter uses **Messages**, not OpenAI Chat
  Completions: configurable `baseURL`, `POST /v1/messages`, `x-api-key`, SSE.
  Default public endpoint is `https://api.deepseek.com/anthropic`. The fixture
  overrides it with loopback and synthetic credentials. A production Gateway
  must support that protocol or the profile must use a compatible official
  adapter. Merely changing an OpenAI URL is insufficient. Deny direct public
  provider egress; do not depend on an environment URL alone as enforcement.
  The coordinator initially located Core's
  `POST /api/v1/ai-gateway/llm/anthropic/v1/messages` route, but the local profile
  refresh returned 401. The first follow-up reached HTTP 403 with a login JWT;
  the successful A03 follow-up below uses a scoped relay PAT and the official
  OpenAI adapter. The local stub fixture never reads credentials.
- Native `approval/request` offers sessionId/toolName/optional callId/reason,
  not Soha's frozen action/argument digest, fence, expiry and policy revision.
  It cannot replace the Soha authorized tool bridge or approval ledger.

## Limits

Local stub qualification cannot establish real Soha Gateway/model/usage
compatibility, model quality/cost, Heya bridge, business idempotency/fencing,
durable SSE replay, revocation/provenance, OS/network confinement, or deployment.
Those remain outside this local fixture; see the separate real Gateway result
below. B1 as a whole is not qualified by the local fixture.

Official references: [client](https://github.com/deepseek-ai/deepseek-harness/blob/fcc0ad462172522aaf5c93a7b2e01f6b9f0b0159/packages/sdk/client/README.md),
[wire](https://github.com/deepseek-ai/deepseek-harness/blob/fcc0ad462172522aaf5c93a7b2e01f6b9f0b0159/packages/sdk/protocol/README.md),
[minimal composition](https://github.com/deepseek-ai/deepseek-harness/blob/fcc0ad462172522aaf5c93a7b2e01f6b9f0b0159/packages/bundle/sdk-minimal/cordis.patch.yml).

## Optional real Gateway qualification

This separate command performs **one native OpenAI preflight and one dsh
inference**. Run when these short inferences and a temporary PAT lifecycle are
authorized, with an authenticated local CLI profile:

```sh
opensoha-verify run -- npm --prefix runtimes/dsh run qualify:gateway
```

`gateway-qualification.mjs` first runs the normal read-only CLI command
`soha token list --profile local`, discarding its output and reusing its supported
session refresh/save flow. It then reads the local JWT only in memory and creates
a normal PAT through the public API: **15-minute expiry**, only
`ai.gateway.relay.invoke`, purpose/scope `llm-relay`, model `gpt-6-luna`, provider
kind `openai`. Login JWTs cannot invoke relay. The separate `governed gateway_only`
binding is for governed tools/tasks and its current endpoint allowlist does not
include model relay; no false binding or policy relaxation is used here. Any API
rejection fails closed. The PAT is revoked in `finally`, and a normal token-list
read must confirm its `revokedAt`. An uncertain creation is reconciled by its
unique fixture name rather than replayed. Credentials never enter source,
configuration files, output or the dsh child; no upstream API key is read.

The official, already-installed `@deepseek-ai/dsh-llm-pi-ai@0.2.2-rc.1` is mounted
in a temporary startup patch. Its `soha-gateway` route uses
`api: openai-completions` and the exact public model `gpt-6-luna`, with
`supportsUsageInStreaming: true` and `maxTokensField: max_completion_tokens`.
The preflight caps output at 256; the dsh request at 512. Both reach
`/api/v1/ai-gateway/llm/openai/v1/chat/completions`. A loopback recording proxy
replaces a synthetic Bearer credential with the temporary PAT and forwards body
bytes unchanged (including model), recording matching SHA256 values. It does not
translate SSE, change models or follow redirects. There are no automatic retries
or fallback. The public model name does not prove the provider's underlying model
identity. The same restricted tool profile and fresh isolated home remain in use.

2026-11-07 actual evidence:

| Check | Result |
| --- | --- |
| Initial Messages/JWT attempt | HTTP403, terminal error, no usage; request `task123-dsh-4066f3d6-975d-49aa-af8d-8757eab0b702`; not an inference success |
| A03 first PAT request before refresh integration | HTTP401 unauthorized; no PAT and no inference created |
| Normal CLI refresh + temporary PAT creation | PASS; HTTP201; ID `6bf2ae1a-4c84-4077-a44f-46b8e77af43f`, expiry `2026-11-07T15:53:19.938Z` |
| Native OpenAI preflight | HTTP200, assistant text present, finish `stop`; usage input10/output5/total15; request `task123-dsh-preflight-ea7be7de-7fbd-4ef7-8c5f-c8c522fb66fc` |
| Real SDK → dsh → Gateway → model | HTTP200/SSE, exact assistant `OK`, durable prompt receipt bound, runtime terminal `completed`, stream `stop` and `[DONE]`; public/returned model `gpt-6-luna`; request `task123-dsh-sdk-bca8947e-f059-44e2-ad87-f3e37207053c` |
| Usage agreement | Gateway97 input +17 output =114 total; dsh `inputTokens=97`, `outputTokens=17`, `totalTokens=114` |
| Body unchanged | Request and forwarded SHA256 both `8cc4fd47df4bdb036aa9b06094845362e47a7ca0085e9c0cbd9aa8f04f08a2a0` |
| PAT cleanup | Revoke HTTP200/status ok; GET confirmed `revokedAt=2026-11-07T15:38:34.411354Z`; child, proxy and temporary home removed |

The successful inference run's aggregate script exit was **1**: after the SDK
read the complete `[DONE]` stream and committed its response, its normal downstream
close caused the proxy to abort the already-completed upstream reader, incorrectly
setting `proxyFailure=AbortError`. The narrow fix aborts on downstream close only
before `[DONE]` and stops reading at the terminal marker. Existing stop, usage,
receipt and completed assertions remain required. **No second PAT or repeated
model call was used to validate that fix.** The revised source was validated only
with this minimum local stream check (also parses/loads the script):

```sh
opensoha-verify run -- node runtimes/dsh/gateway-qualification.mjs --check-completed-eof
```

PASS/exit0: closing the completed local stream does not abort; closing an
incomplete stream does abort. The post-fix script has **not** been rerun against
the real model. The seven stub scenarios were not repeated. This evidence does
not cover Heya tools, business fencing/approval/idempotency, budget enforcement,
production egress confinement, model quality or all B1 acceptance requirements.

## B2 standalone runtime service

`npm ci --ignore-scripts && npm start` launches `service.mjs`, independent of the
Go Agent. Node >=24.9 and the locked 0.2.2-rc.1 SDK/runtime are required.
All HTTP routes (including `/v1/health`) require the service Bearer token.
Core connects directly using the frozen `dsh-runtime` schema in contracts.

Trusted startup environment (never accepted from Start or model tool arguments):

| Variable | Meaning / default |
| --- | --- |
| `SOHA_DSH_HOST` / `SOHA_DSH_PORT` | `127.0.0.1` / `8097` |
| `SOHA_DSH_SERVICE_TOKEN_FILE` | Required service token secret file, >=32 characters |
| `SOHA_DSH_MODEL_AUTH_MODE` | `core` by default; explicit `gateway` for diagnostics only |
| `SOHA_DSH_GATEWAY_PAT_FILE` | Required only in `gateway` mode; not read in `core` mode |
| `SOHA_DSH_GATEWAY_URL` | Required model base URL; core mode uses `/api/v1/copilot/dsh/model/v1`; service appends `/chat/completions` |
| `SOHA_DSH_CORE_CALLBACK_URL` | Required complete Core ToolCall POST URL |
| `SOHA_DSH_MODELS` | Required comma-separated exact model allowlist |
| `SOHA_DSH_TOOL_CATALOG_FILE` | Required only in `gateway` diagnostics: immutable JSON `{ "revision": "...", "tools": [...] }`; not read in `core` mode |
| `SOHA_DSH_DATA_DIR` | Private receipt/journal/home volume, `/var/lib/soha-dsh` |
| `SOHA_DSH_SCHEMA_PATH` | Bundled frozen wire schema path; development defaults to sibling contracts source |
| `SOHA_DSH_MAX_CONCURRENCY` | Active processes, `4`; excess requests receive 429 |
| `SOHA_DSH_MAX_RUN_MS` | Run time limit, `120000` (max 3600000) |
| `SOHA_DSH_MAX_OUTPUT_TOKENS` | Maximum accepted Run output budget, `4096` |

Catalog tools use the exact wire Tool shape (`name`, `description`, `inputSchema`,
`readOnly`). The complete source is validated and enforced by Ajv strict with
formats. Unknown validation keywords and remote references fail closed with
explicit fixed error codes; local fragment references are compiled locally,
without dynamic schema downloads. If the SDK supports the original schema it
registers that schema; otherwise only its registration uses the explicit
`{type:"object",additionalProperties:true}` superset. The parent restores complete
Start parameters and description before each model request is hashed and sent.
The SDK projection never replaces parent or Core input validation.
In production `core` mode, the authenticated Core freezes each Run catalog in
Start.tools and binds its opaque Start.toolCatalogRevision. Node independently
compiles that Run schema and validates each tool callback against that Run only;
there is no shared startup catalog or revision equality check. Core revalidates
ownership, authorization, directory and approvals on every callback. In explicit
`gateway` diagnostics, Start must still select exact startup catalog entries and
match the startup revision. Chat additionally requires trusted
`readOnly=true`; Heya never obtains authority merely by setting readOnly=false.
The parent sends the service Bearer and the separate `X-Soha-Tool-Grant` only to
the fixed callback. Stable aliases `soha_tool_N` map back to original public names.
Tool callbacks may wait for Core approval/task completion for the remaining Run
budget: the earlier of Start.deadline and the acceptance time plus
SOHA_DSH_MAX_RUN_MS. They have no fixed 15-second response deadline. Cancel and
terminal state abort the callback. Deadline exhaustion, tool transport failures,
unknown or response identity mismatch terminate as unknown, without retry. Core retains approval and business receipt ownership.

Production startup defaults to `core` model authorization: the parent sends the
service Bearer plus the Run grant and binding headers to the fixed Core model
endpoint. Every Start needs a grant, including tool-free Chat. The Core callback
retains user authorization, source checks, budgets and real Gateway invocation.
Model headers bind `X-Soha-Run-ID`, `X-Soha-Run-Attempt`, `X-Soha-Run-Fence`,
`X-Soha-Context-Revision`, `X-Soha-Tool-Catalog-Revision` to the frozen Start.
Each call has a fresh stable `X-Soha-Model-Call-ID` and an `X-Soha-Input-Hash` over
the exact outgoing body bytes. The parent serializes once after its existing
budget cap and trusted tool-name mapping, then hashes and sends the same bytes.
Core mode maps SDK aliases to frozen original names in tool definitions and
assistant tool-call history, and maps complete original tool-call names in Core
SSE back to SDK aliases. Tool callback names remain original. Core sends standard
SSE for `stream:true`; the parent preserves actual content and usage semantics.
Include virtual model `soha-workbench` in the trusted model allowlist when using
Core selection; Core retains the actual route decision.
No user PAT or fabricated token kind is passed to Node. The separate tool callback
uses the complete configured `/api/v1/copilot/dsh/tools` URL. Explicit `gateway`
mode keeps the B2 relay-PAT diagnostic route, with no automatic fallback. Direct
programmatic `createRuntime` configurations without a mode retain gateway behavior
for existing diagnostic callers; environment startup always defaults to core.

The parent alone holds any diagnostic PAT and the tool grant. Child processes receive an isolated
home, restricted B1 profile, and a synthetic loopback key, with no native tools,
MCP or plugin installation. The parent caps each upstream request, reserves the
remaining Run token budget (missing usage consumes the reservation), allows at
most 16 requests, and limits total provider stream bytes to 4 MiB. Assistant
output is bounded to 128 KiB and durable message deltas to 16 KiB. Each journal
has at most 4096 events; exhausted limits stop execution rather than evicting
unread events. Receipts are retained until operator-managed retention; there is
no background business queue or automatic prompt replay.

The private data directory has a single-writer PID lock, 0700 directories and
0600 receipt files, atomic file replacement and fsync before 202. A dead writer's
lock is recovered; a live or ambiguous writer prevents startup. Previous
nonterminal receipts become unknown, never automatically resumed. Failed child
reaping retains its private home and reports unknown. Deploy with container/OS
network and filesystem isolation; this profile is not an OS sandbox. Graceful
SIGTERM closes and reaps children before releasing the journal lock.

`npm test` runs bounded HTTP lifecycle checks and the real fixed SDK against a
loopback OpenAI SSE model and Core ToolCall stub, including a wrong-binding
response. It does not call a paid model, switch production routes, or prove B3
Core approval/budget integration. Tests remove their exact temporary homes and
servers. B1 qualification scripts remain available and are separate evidence.
