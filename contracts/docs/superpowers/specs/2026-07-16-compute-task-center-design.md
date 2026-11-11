# Compute Task Center Design

## Problem

The Compute workbench currently exposes three menu entries: Sync Tasks, Build Tasks, and
Operation Records. All three routes render the same list page. Sync and Build are only fixed
category filters, while Operation Records is the unfiltered superset. The split suggests three
independent capabilities even though the pages do not create tasks and do not currently expose
the cancel, retry, and log actions already represented by the task model.

Task creation belongs to resource context. A virtualization connection starts synchronization,
a runtime host starts provisioning, and a Docker project starts deployment. Cross-resource task
observation and control belong to a single task center. System operation logs remain a separate
governance/audit concern under `/system/operations`.

## Decision

Adopt a hybrid interaction model:

- Resource pages create tasks and expose compact controls for the relevant recent task.
- The Compute Task Center is the canonical full-history and execution-control surface.
- Resource-page controls and the Task Center use the same public Compute API. Frontend code does
  not branch to virtualization- or Docker-specific action endpoints.
- The Task Center menu replaces the Sync Tasks, Build Tasks, and Operation Records menu entries.
- Existing Sync and Build URLs remain as compatibility redirects to filtered Task Center URLs.

The initial contextual integrations are limited to Compute-owned resources:

- virtualization connections/clusters;
- Docker runtime hosts;
- Docker projects and their deployment operations.

Application delivery and Kubernetes workload execution are outside this change because they are
not currently projected by the Compute task service.

## Information Architecture

The canonical route remains `/compute/tasks/operations` for compatibility, but its title and menu
label become `任务中心` / `Task Center`. It displays every task visible to the current principal.

Compatibility routes are not menu entries:

- `/compute/tasks/sync` redirects to `/compute/tasks/operations?category=sync`;
- `/compute/tasks/build` redirects to `/compute/tasks/operations?category=build`.

Redirects preserve compatible query parameters such as domain, provider, status, and resource
filters. The old server-seeded menu records are disabled so existing databases do not continue to
render duplicate entries.

The Task Center exposes shareable URL state:

- `domain`, `category`, `status`, and `providerKey` for ordinary filtering;
- `resourceKind` and `resourceId` for resource-context filtering;
- `taskId` and `view=logs` to select a task and open its log drawer.

## Public Contract

Contracts change first. Existing task detail, cancel, and retry operations become implemented
core behavior, and a unified log operation is added:

```text
GET  /compute/tasks/{domain}/{id}
GET  /compute/tasks/{domain}/{id}/logs?limit=200
POST /compute/tasks/{domain}/{id}/cancel
POST /compute/tasks/{domain}/{id}/retry
```

`GET /compute/tasks` gains optional `resourceKind` and `resourceId` filters. `resourceId` matches
the normalized task resource projection and must not search arbitrary payload fields.

The log response uses a normalized `ComputeTaskLog`:

```text
id, taskId, logLevel, message, payload?, createdAt
```

and returns a `ComputeTaskLogListEnvelope` with an `items` array. An available task with no logs
returns `200` and an empty array. Unknown tasks return `404`.

Cancel and retry accept the existing optional reason and idempotency key. A successful cancel
returns the updated task. A successful retry returns the newly created task. Invalid state
transitions return `409`; permission failures return `403`; missing tasks return `404`.

## Core Delegation

The Compute application service owns the stable cross-domain contract. It consumes narrow ports
for virtualization tasks and Docker operations:

- get task;
- list logs;
- cancel;
- retry.

For `domain=virtualization`, it delegates to the virtualization application service and maps the
result through the existing task projection. For `domain=container_runtime`, it delegates to the
Docker application service. Unsupported domain values fail as invalid arguments before any
delegation.

Authorization stays server-side. Read operations use the domain's view permission. Cancel and
retry require its management permission and still enforce the source domain's state machine. The
frontend renders actions from `availableActions`; it does not infer action availability from raw
status strings.

The Compute handler registers all four routes and remains limited to validation, principal
extraction, input binding, service invocation, and response mapping.

## Task Center UI

The Task Center remains a dense management table and adds:

- a category control for All, Sync, Build, Lifecycle, and Other;
- resource filters derived from URL state;
- a fixed action column with icon buttons for logs, cancel, and retry;
- confirmation for cancel and retry;
- a right-side large Drawer containing task metadata and chronological logs;
- direct opening of the log drawer from `taskId` and `view=logs` URL parameters.

Cancel and retry mutations invalidate the task list, selected task detail, and Compute overview.
Retry selects the new task returned by the server. Failed mutations retain the current selection
and show the server's stable error message. The Drawer distinguishes loading, empty, and error
states and never renders raw secret-bearing payload fields as primary log text.

The current page module is renamed to reflect Task Center ownership. Sync and Build route modules
become redirect-only compatibility adapters rather than duplicate UI leaves.

## Resource Context UI

Resource pages do not embed the full Task Center. They show a compact latest-task treatment close
to the action that created it:

- queued/running task: status, cancel, logs;
- failed/timeout task: status, retry, logs;
- completed task: status and logs;
- `查看全部任务` links to the Task Center with domain and resource filters.

Log actions navigate to the Task Center deep link and open the canonical Drawer. Cancel and retry
execute in place through the shared Compute mutation options, then invalidate both the
resource page's relevant task query and Task Center prefixes. This keeps the common action and
error semantics in one data layer while preserving the resource workflow.

The virtualization cluster page reuses its existing selected-operation affordance but routes log
inspection through the Task Center. Runtime host and Docker project pages add the same compact
task treatment without duplicating the full history table.

## Data Ownership

`src/features/compute/tasks/` owns task API calls, keys, queries, mutations, URL parsing, action
controls, and the log Drawer. Resource pages consume a narrow public task-action surface from the
Compute feature. They do not deep-import Task Center route pages.

TanStack Query owns list, detail, and log server state. URL search parameters own shareable
filters and Drawer selection. Confirmation and transient loading state remain local. No new
global store is introduced.

## Testing

Contracts tests verify response schemas, filters, generated SDK output, and compatibility.

Core tests cover both domains for get, logs, cancel, and retry; permission separation; invalid
domains; missing tasks; invalid state transitions; normalized log mapping; and resource filters.
Handler tests verify parameter binding and HTTP status behavior.

Frontend tests cover:

- one visible Task Center menu and hidden compatibility routes;
- Sync and Build redirects with query preservation;
- category/resource URL round trips;
- API paths, keys, query options, mutation invalidation, and retry selection;
- action visibility from `availableActions`;
- cancel/retry confirmations and errors;
- log Drawer loading, empty, success, and error states;
- contextual deep links from cluster, host, and project pages.

Browser verification covers direct refresh of filtered URLs, log deep links, responsive table
actions, Drawer layout, and the absence of duplicate menus.

## Out of Scope

- Creating arbitrary tasks from the Task Center.
- Folding `/system/operations` audit/operation logs into Compute tasks.
- Projecting application-delivery or Kubernetes workload tasks into Compute.
- Bulk cancel or bulk retry in this iteration.
- Streaming logs; the initial Drawer uses bounded polling/refetch of persisted logs.
