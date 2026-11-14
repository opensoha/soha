# Personal environment Agent protocol

The source schemas are `agent/personal-environment-request.schema.json` and
`agent/personal-environment-result.schema.json`, reusing the existing lease and
snapshot schemas. This small Go package carries the same wire types without
exposing Docker container IDs, volume names, host paths, or provider errors.

The Agent accepts `POST /api/v1/runtime/personal-environments` from the trusted
Core connection. Its normal `{ "data": ... }` envelope contains `Result`.
Authentication is required even on loopback; each action also requires its
explicit `runtime.personal_environments.<action>` allowlist entry. This feature
is disabled by default. Existing Agents without this endpoint cannot execute it.

Core derives the owner from its authenticated user and persistent assistant
identity. It selects the registered execution node, quotas, scope hash, run ID
and original lease timestamps. These must not come from model-selected identity,
user-entered endpoints, or provider credentials. Each request is limited to
512 KiB; dispatch/observation has a 30 second deadline, and cleanup has an
independent 30 second deadline. Slow cleanup can outlast the HTTP receipt window
and must be reconciled as an unknown outcome. Leases last at most 24 hours.
This Docker backend requires pinned images, cgroup v2 and already-enabled,
consistent Btrfs subvolume quotas. Unsupported enforcement fails acquisition.

- `acquire`: recover the same immutable original lease, or start its task sandbox.
  `createData=true` is permitted only during initial provisioning before Core has
  a ready receipt or allows any task execution. Later acquisition uses false:
  missing volumes or data subvolumes are errors, never replacements with empty data.
- `inspect`: observe this lease, without creating a container. Expired containers
  are stopped before an expired receipt is returned. Missing containers report missing.
- `execute`: bounded absolute argv inside this lease's sandbox. It cannot be
  retried automatically: network loss, timeout or lost receipts mean an unknown
  outcome. Core must persist dispatch state and require reconciliation rather
  than replaying a write. The Agent fences concurrent commands and retains data
  when cancellation or failure stops the task sandbox.
- `release`: stop only this original lease. Persistent data is retained. If its
  container is absent, return missing rather than inventing a release receipt.
- `delete_data`: Core requires the current owner's exact assistant name/version,
  sleeping data and no active task. The Agent takes the physical volume lock,
  removes only that owner's data and retains a non-recreation tombstone. A
  partial deletion is unknown; only a `deleted` receipt confirms it.
- `export_data`: use the dedicated `POST /runtime/personal-environments/export`
  endpoint, with the same server-derived original lease and no argv/task/create
  fields. Ordinary JSON `Result` is never a valid export response. The binary
  stream has big-endian uint32 chunk lengths (maximum 32 KiB), then zero, a
  uint32 JSON-receipt length (maximum 4 KiB), and the export receipt. The TAR
  payload is bounded to 256 MiB. Receivers reject truncation, trailing bytes,
  changed identity, size or SHA-256 before exposing any file. The producer sends
  the final receipt only after its archive command and owned cleanup complete.

Active results include actual quota/cgroup observations. Inactive results omit
observations rather than manufacturing zero usage. `ValidateFor` checks receipt
identity, action completion, bounded command output, and observation identity.
Core must also reject stale receipts. Data export and deletion are separate
explicit owner operations; their allowlist entries remain opt-in. TAR preserves
link entries without following their targets and is never extracted by export.
