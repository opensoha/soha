# State, operation feedback and relationship exploration

Use these rules for resource state, operations and relationship exploration with
product-experience.md. Choose composition from the task; this is not a universal page template.

## Shared implementation ownership

Use `src/components/request-feedback.tsx` for domain-independent reading and request feedback,
and `src/utils/query-feedback.ts` for readable cache and uncertain-write decisions. AdminTable
uses that same reading feedback; keep query, task permissions and evidence routes with each
feature. Ordinary CRUD and configuration requests can reuse `components/use-operation-mutation`;
its uncertain-write fence lasts only while the owner stays mounted and is not durable reconciliation.
Keep one visible feedback owner per request, inside its active overlay when necessary.
Compute's feedback module only adapts Compute task receipts, not global UI policy.

Persistent error feedback claims its API presentation through the shared helper so the global
handler remains a fallback rather than a second notification. Authentication expiry keeps its
notification and redirect. Keep modal-visible errors, validation and partial-batch results;
do not remove a toast merely because a feedback panel exists behind the modal.

## State observation

- Keep object identity and action scope visible across details, metrics and history. Running or
  healthy is a static fact, not perpetual loading.
- Separate resource, task and session state. Accepted is not completed; task success is not
  verified resource readiness; running is not VNC/SSH connected.
- Label the source/time actually supplied. Record updatedAt is not an independent Provider
  observation. Stale/unknown is not stopped, failed or healthy.
- Distinguish missing, empty, denied, failed and genuine zero. Specifications are allocations,
  metrics are sampled usage; current-page counts are not fleet totals or a true cursor total.

## Operation feedback

- Classify the existing endpoint: synchronous record save/delete, async submission or read.
  Use completion language only for the result it establishes. Returned task IDs need a visible
  route to progress/evidence.
- Keep target, action, result/task and ongoing error readable beyond a toast. Reuse domain
  task/detail/log reads and gates. If progress is unreadable, say so; do not query unauthorized
  logs or infer a terminal task from predicted resource state.
- Keep action names accessible and stable through pending/result transitions; decorative
  loading icons must not replace or contaminate the action label.
- Separate queued/running/canceling from terminal results. Use real progress or evidence-backed
  stages. Unknown/timeout/canceled is not successful rollback or Provider cancellation.
- Preserve allowedActions, confirmations, plans/preflight and server ownership. Prevent duplicate
  in-flight writes; uncertain responses require evidence lookup, not automatic replay. A local
  submission fence does not establish cross-client idempotency.
- Closing a modal, leaving a page or aborting UI streaming does not cancel a durable task.
  Cancel/retry follows actual endpoints and capabilities; retain failure and retry context.

## Reading, refresh and continuity

- First load may use skeleton/spinner. Background reads retain content, selection, filters,
  pagination, range, edits and focus with local loading feedback. Scope changes isolate data.
  Denial must hide protected cache; temporary read failure may retain labeled previous data.
- Fresh create/clone/edit dialogs apply the new target's defaults during rapid close/reopen;
  correctness must not depend on the previous closing animation finishing.
- Provide retry on first-load/read errors. Cached errors state that data did not update; stop or
  back off failed polling according to actual query behavior.
- Refresh rereads the named scope. Provider discovery/sync is a separate operation that may
  persist data or return a task. Do not hard-reload or rebuild sessions/charts to refresh data.
- Place detail refresh with resource actions, list refresh in its toolbar and metric/log refresh
  beside that region's controls. Use existing icon actions with accessible names for secondary
  reads. Compact controls need breathing room and keyboard targets; preserve form/shell scale.

## Motion

- Use the application's duration/easing and semantic colors. Animate expansion, selection and
  observed transitions; do not simulate progress or flow.
- Consume `--soha-motion-duration-fast/mid/slow`, `--soha-motion-ease[-out]` and activity timing
  from app-theme. System reduced motion updates CSS and the single ConfigProvider together.
  Share visibility via `useDocumentHidden`; do not reinject equivalent scene token sets.
- Animate active tasks/reads rather than static resource states. Keep activity text accessible.
  Honor reduced motion; pause decorative loops while hidden without stopping required traffic.
- Background refill must not replay page entrance or remount professional engines. Preserve
  object keys, ticket lifecycle and explicit reconnect behavior.

## Relationship exploration

- Show the queried object, actual type/direction, endpoints, source and observation/stale metadata
  when supplied. Dependency/provisioning arrows do not prove network traffic or live calls.
- Offer filtering, selection and local neighbor highlighting where useful. When offered, support
  keyboard selection; a plain list keeps endpoint navigation keyboard-accessible. Retain a
  valid choice during refresh, clear removed/out-of-scope choices and
  distinguish filtered-empty from no relationships.
- Exploration is read-only unless an existing authorized mutation supports editing. Selection,
  properties or endpoint navigation must not run discovery, sync, provisioning or AI writes.
- Use verified routes and permission gates. Unsupported kinds remain readable text. Preserve
  existing graph view/lifecycle; a relation list does not require installing another engine.
- Retain labeled prior evidence on read failure where allowed. Separate inaccessible,
  unsupported and empty; inferred/stale relations do not become confirmed inventory.

## Completion evidence

Verify page integration: load/refresh/error/denied, async acceptance/evidence, target/permission
isolation, relation selection/read-only behavior, units/zero, keyboard/focus, reduced motion and
session continuity. Use existing gates. Separate synthetic data, real reads, writes and published
artifacts; compilation/screenshots do not establish Provider readiness or user approval.
