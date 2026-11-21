---
id: app-hosting-publisher
name: App Hosting Publisher
version: 0.1.0
category: delivery
description: Publish static web applications or dynamic Node/Python services to workspace App Hosting with audience approval awareness, collaborator controls, workspace publishing, preflight checks, and rollback.
capabilityRefs:
  - gateway.manifest.read
permissionKeys:
  - ai.gateway.invoke
  - apphosting.apps.view
  - apphosting.apps.publish
requiredScopes:
  - aiClient
  - skill
---

# App Hosting Publisher

Use this skill when an AI coding assistant (Claude Code, Cursor, Codex, or the Soha Web / App built-in assistant) helps a user publish, update, share, manage collaborators, inspect, roll back, pause, or delete a self-service workspace hosted web application or dynamic service via `soha host` and Soha AI Gateway `apphosting.*` capabilities.

## Operating Contract

- Distinguish workspace App Hosting (`soha host`, `/api/v1/app-hosting/...`, `apphosting.*`) from formal CI/CD delivery (`soha delivery applications`) and administrator SSO directory onboarding. App Hosting provides zero-ops fixed HTTPS subdomains behind enterprise SSO for lightweight internal dashboards, reports, and tools.
- Discover live Gateway capabilities first (`gateway.manifest.read` or `soha capabilities --query apphosting`) and choose the packaging channel that matches the runtime environment without ever reading built static asset contents into conversation context:
  - **Soha Web / App built-in personal assistant (`/data/<dir>` or conversation artifacts)**: when `apphosting.workspace.publish` is available in the live Gateway manifest and the built output lives in the user's personal workspace `/data/<dir>` (or a saved conversation artifact), invoke `apphosting.workspace.publish` directly (`source.kind: "personal_environment"` with relative `source.sourcePath` under `/data`, or `source.kind: "conversation_artifact"`) so the Soha backend packages and streams the bundle server-side without reading asset files into model context.
  - **External local CLI / IDE assistants**: use `soha host` CLI commands (`soha host deploy <dir>`) for local bundle scanning, preflight, and binary `application/gzip` streaming uploads rather than passing bundle bytes or file contents through tool arguments or conversation context.
- Inspect the project structure (directory listing and build configuration only — never dump compiled asset files into prompts) before packaging:
  - Built static output (`dist/`, `build/`, `out/`, or a static folder containing a root `index.html`): target that built output directory directly and enable SPA fallback (`--spa` or `spaFallback: true`) when client-side routing needs extensionless route fallback to `index.html`.
  - Unbuilt frontend source (`package.json` and `src/` without built `index.html`): run or ask the user to run the project build command first, then deploy the generated output folder rather than raw source or `node_modules/`.
  - Dynamic source (`soha host deploy <dir> --kind dynamic`): include a root `soha-host.json` with `template` (`node` or `python`) and `database` (boolean). When the database is enabled, optional `schemaVersion` accepts `1` or `2` and defaults to `1`. Node additionally requires root `server.js`, `package.json`, and `package-lock.json`; Python requires root `app.py` and `requirements.txt`. Omit `--spa` for dynamic apps. Exclude repository metadata and local dependency or cache directories. Listen on `PORT=8080` and provide a read-only `GET /healthz`.
  - Backend-dependent or stateful applications: warn clearly that static App Hosting serves immutable static assets from private object storage, and dynamic HTTP applications must never persist business data to ephemeral local container files (such as local SQLite `.db` or writable JSON files) because scale-to-zero and instance recycling wipe local disk state; persistent business state belongs in a managed `DATABASE_URL` store.
- Default visibility for every newly created hosted application is `private` (`仅自己`, accessible only to the current owner). Never widen visibility to `selected` or `organization` unless the user explicitly asks to share.
- Respect platform organization-visibility governance (`organizationVisibilityPolicy`):
  - Under `require_approval`, requesting `organization` visibility (`soha host visibility <app-id> --mode organization [--reason "<text>"]`, `soha host deploy --visibility organization`, `apphosting.apps.visibility.set`, or `apphosting.workspace.publish`) creates a `pendingVisibilityApproval` (`status: "pending"`) instead of immediately opening the application to the entire organization. Until an administrator approves that request, effective `visibility` stays at its current scope (`private` or `selected`); never report the app as already organization-public while approval is pending.
  - Under `disabled`, organization-wide visibility is blocked by platform policy; report the policy error clearly and offer `private` or `selected` sharing instead.
- Preserve fixed application identity (`--app-id <id>` or `appId`) when updating an existing hosted app so its immutable `dnsLabel` and HTTPS URL remain unchanged, and omit `--visibility` on routine updates unless the user explicitly asked to change who can access the app.
- Keep an explicit or recorded `--idempotency-key` (`idempotencyKey`) across uncertain network retries so retries return the original deployment receipt without creating duplicate applications or revisions.

## Workflow

1. Locate the source directory (via local directory listing or `/data/<dir>` workspace inspection without reading asset or source contents into chat context). For static, find the built output with root `index.html` and verify whether SPA fallback (`--spa` / `spaFallback: true`) is needed. For dynamic, verify `soha-host.json` and the template-specific required files, and confirm `--kind dynamic` with no `--spa`. Check for forbidden files (`.env`, `.env.*`, `*.pem`, `*.key`, `id_rsa*`, `credentials.json`, symlinks) and dependency/cache directories.
2. Discover live capabilities (`gateway.manifest.read` or `soha host list --output json`) and check whether the user is updating an existing hosted application (`--app-id` / `appId`) or creating a new one (`--name`). An existing app's `kind` is immutable here: if it does not match the requested `--kind`, stop before any mutation instead of attempting an update.
3. Determine target visibility:
  - If the user did not specify an audience, keep the default `private` (`仅自己`).
  - If the user asks to share with specific people or departments (including departments outside the author's own team), query `apphosting.audience.search` (via Gateway MCP) or `soha host audience --query <keyword> --output json` to resolve exact `userIds` and `teamIds`.
  - If the user asks for `organization` (`全公司可见`), collect an optional business `--reason` (`reason`) in case the platform enforces `require_approval`.
  - Present the deployment summary (source directory, app name or `appId`, create vs update, SPA fallback, visibility mode, and resolved colleague/department names and IDs) and obtain explicit user confirmation before publishing or broadening access.
4. Execute preflight and publish through the appropriate channel with a stable idempotency key:
  - **Built-in Soha Web / App assistant (`/data/<dir>`)**: invoke `apphosting.workspace.publish` with `source: { kind: "personal_environment", sourcePath: "<relative-dir-under-/data>", environmentVersion: <n> }`, confirmed `visibility`, and `idempotencyKey`. Set the top-level `kind` to `dynamic` when creating a dynamic app. Use `spaFallback` and `conversation_artifact` only for static publishing. Keep source and artifact contents out of conversation messages.
  - **Local CLI / external IDE assistant**: run `soha host deploy <dir> --kind <static|dynamic> --name "<name>" --dry-run --output json` for a local-only bundle check. With `--app-id <id>`, the dry run also gets a server preflight verdict. Stop on `allowed: false` or blockers. After user confirmation, run `soha host deploy <dir> --kind <static|dynamic> ... --idempotency-key <key> --yes --output json`. Reuse the same key and app ID on uncertain transport errors. If the revision is confirmed failed, correct the cause and retry with `--app-id` from the receipt and a new key.
5. Report the deployment receipt accurately:
  - Report `App ID`, `Revision` (`revisionId` and `revisionNumber`), `kind`, effective `Visibility`, app `Status`, and the fixed HTTPS `URL` (`https://<dnsLabel>.<appsDomain>`).
  - For dynamic revisions, report `revision.status`, `buildPhase`, and `imageRef` only when the server returns them. Never call a revision `ready` unless `revision.status == "ready"`; for other states, show the revision ID and tell the user how to check it with `soha host revisions <app-id> --revision <revision-id> --output json`. Do not invent build progress, phase transitions, or logs.
  - If the returned `HostedApp` carries `pendingVisibilityApproval` (`status == "pending"`), explicitly tell the user that the organization-public approval request (`pendingVisibilityApproval.id`) has been submitted and that effective visibility remains at the current `<mode>` (`private` or `selected`) until approved — never state that the app is already open to the whole organization.
6. For post-publish lifecycle, collaboration, and governance requests:
  - **Dynamic runtime configuration**: use My Works for encrypted variable updates, database usage, redacted revision events, and visitor attribute requests. The CLI stages an existing process environment value with `soha host config <app-id> --set-env NAME --yes` or removes it with `--remove-env NAME`. Use variable names in commands and keep values out of conversation messages, command arguments, and deployment receipts. Publish a new revision to apply changes. Rollback rejects versions that depended on a removed or replaced value.
  - **Audience and approval management**: use `soha host visibility <app-id> --mode <private|selected|organization> [--user ID]... [--team ID]... [--reason "<text>"] --yes` (`apphosting.apps.visibility.set`). Warn before narrowing to `private` or removing users/teams that `routeGeneration` advances immediately and removed colleagues' saved links and active sessions are denied (`403`) on their next request. To cancel a pending organization-public request, run `soha host visibility <app-id> --withdraw [--reason "<text>"]` (effective visibility stays unchanged).
  - **Collaborator maintainers**: inspect current collaborators and sanitized user cards with `soha host maintainers <app-id>`, update collaborators with `soha host maintainers <app-id> --users <u1,u2>` (or `apphosting.apps.maintainers` after resolving user IDs via `apphosting.audience.search`), or remove all collaborators with `soha host maintainers <app-id> --clear`. Always remind the user that collaborator maintainers can only upload new revisions and roll back — they cannot change visibility, manage collaborators, toggle online/offline, or delete the app, and visitor access remains strictly controlled by `visibility`.
  - **Version inspection and rollback**: list history with `soha host revisions <app-id>` (`apphosting.apps.revisions.list`), confirm the target ready `revisionId` with the user, and run `soha host rollback <app-id> --revision <revision-id> --idempotency-key <key> --yes` (`apphosting.apps.rollback`). Rollback preserves current `visibility` and administrative suspension state.
  - **Pause, resume, and permanent deletion**: use `soha host offline <app-id> --reason "<text>" --yes` (`apphosting.apps.offline`) to pause access while retaining revisions, and `soha host online <app-id> --yes` (`apphosting.apps.online`) to restore service. Only when the owner explicitly asks to permanently remove an app and purge all stored revisions, confirm the exact display name with the user and run `soha host delete <app-id> --confirm "<exactAppName>" [--reason "<text>"]` (secondary confirmation with the exact application name is mandatory).

## Examples

### Input Example

User asks: "Publish the built dashboard in `./dist` as `Q4 Ops Dashboard`, add Bob as a collaborator maintainer, and request organization-wide visibility for quarterly review."

### Expected Tool Calls

1. `gateway.manifest.read` (or `soha capabilities --query apphosting`) to verify available `apphosting.*` tools (including whether `apphosting.workspace.publish` or `apphosting.apps.maintainers` is available in the current runtime).
2. `apphosting.audience.search` (or `soha host audience --query bob --output json`) to resolve Bob's exact `userId` and sanitized profile card.
3. `apphosting.apps.list` (or `soha host list --query "Q4 Ops Dashboard" --output json`) to check whether an owned app already exists.
4. On a local CLI workspace, `soha host deploy ./dist --name "Q4 Ops Dashboard" --spa --dry-run --output json` to check the local bundle without creating an app or uploading files. For an existing app, pass `--app-id` to also check server limits. In the built-in Soha workspace, prepare `apphosting.workspace.publish` with `source.kind: "personal_environment"` and `source.sourcePath: "q4-ops/dist"` without reading asset files into chat context.
5. After explicit user confirmation of Bob's `userId` and the organization visibility request, execute `soha host deploy ./dist --app-id <appId> --visibility organization --reason "Quarterly review for all staff" --spa --idempotency-key <key> --yes --output json` (or `apphosting.workspace.publish`), followed by `soha host maintainers <appId> --users <bobUserId>` (`apphosting.apps.maintainers`), and clearly report if `pendingVisibilityApproval` is `pending` (keeping effective visibility `private` until approved) and that Bob only holds revision upload/rollback rights.

## Permission Boundaries

- Requires `apphosting.apps.view` to list or inspect hosted apps and revisions, and `apphosting.apps.publish` (plus `ai.gateway.invoke` when calling Gateway MCP tools, and `observe.ai.chat` when invoking `apphosting.workspace.publish` from the built-in assistant workspace) to search audience candidates, create apps, run preflight, upload revisions, publish from `/data/<dir>`, change or withdraw visibility requests, manage collaborator maintainers, roll back, toggle online/offline status, or permanently delete owned apps.
- Collaborator maintainers hold only revision upload and rollback rights on that app; changing visibility, withdrawing visibility approvals, updating maintainers, taking the app offline/online, transferring ownership, and permanent deletion are restricted to the owner (or platform administrators).
- Never access object storage buckets, S3/MinIO endpoints, PostgreSQL, or `app-gateway` internal endpoints directly; all control operations flow through authenticated Soha APIs, Gateway tools, or `soha host`.
- Never read, print, or transmit any access token, refresh token, password, private key, secret, credential, or `.env` environment variable content in prompts, tool arguments, or deployment bundles.
- Administrative platform settings, quota changes, deciding organization-visibility approvals, and lifting `suspended_by_admin` require `apphosting.admin.manage` and are outside this publisher skill.

## Forbidden Actions

- Do not package or upload `.env`, `.env.*`, `*.pem`, `*.key`, `id_rsa*`, `credentials.json`, private key files, symlink targets outside the source directory, or dependency/cache directories (`node_modules`, `.venv`, `venv`, `__pycache__`, and repository metadata).
- Do not pass `--spa` for `--kind dynamic`, and do not change an existing app's `kind`; create a new app instead when the user needs a different runtime kind.
- Do not read built static artifact files (`index.html`, bundled `.js`/`.css`, sourcemaps, media files, or `.tar.gz` archives) into conversation context, and do not pass raw bundle bytes through LLM tool JSON arguments; use `apphosting.workspace.publish` for `/data/<dir>` in Soha Web / App built-in assistants or `soha host deploy` on local machines.
- Do not set `--visibility organization` or `--visibility selected` without explicit user instruction and confirmation, and never tell the user an app is already organization-public when `pendingVisibilityApproval.status` is `pending`.
- Do not run `soha host delete` without explicit user intent for permanent deletion and exact application name secondary confirmation (`--confirm "<exactAppName>"`).
- Do not claim collaborator maintainers automatically receive visitor access outside `visibility` or owner-only governance permissions, and do not claim dynamic apps using local SQLite or local disk JSON files retain data across scale-to-zero cycles.
- Do not attempt to bypass `suspended_by_admin` or `organizationVisibilityPolicy` restrictions by creating duplicate applications under new subdomains.

## Guardrails

- Redact any access token, refresh token, private key, password, secret, credential, or sensitive environment variable encountered in build logs or error output.
- Always run `--dry-run` preflight on local CLI deployments and resolve local sensitive-file or missing-`index.html` blockers before executing a non-interactive `--yes` deployment.
- For dynamic deployments, resolve missing `soha-host.json` or template-required files before upload; after upload, poll `soha host revisions <app-id> --revision <revision-id> --output json` instead of assuming the build finished or reporting fabricated progress.
- Reuse the original `--idempotency-key` (`idempotencyKey`) and `--app-id` (`appId`) when recovering from an interrupted `soha host deploy`, `apphosting.workspace.publish`, or `soha host rollback` request.
- When `pendingVisibilityApproval` is returned with `status: "pending"`, report the approval ID and the unchanged effective visibility mode (`private` or `selected`) clearly.
- When narrowing visibility to `private` or `selected`, explicitly inform the user that removed colleagues lose access immediately even if they already opened the link.
