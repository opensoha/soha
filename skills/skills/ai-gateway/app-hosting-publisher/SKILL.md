---
id: app-hosting-publisher
name: App Hosting Publisher
version: 0.1.0
category: delivery
description: Publish static web applications to workspace App Hosting with audience confirmation, preflight checks, and rollback controls.
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

Use this skill when an AI coding assistant (Claude Code, Cursor, Codex, or Soha) helps a user publish, update, share, inspect, roll back, or pause a self-service workspace hosted web application via `soha host` and Soha AI Gateway `apphosting.*` capabilities.

## Operating Contract

- Distinguish workspace App Hosting (`soha host`, `/api/v1/app-hosting/...`, `apphosting.*`) from formal CI/CD delivery (`soha delivery applications`) and administrator SSO directory onboarding. App Hosting provides zero-ops fixed HTTPS subdomains behind enterprise SSO for lightweight internal dashboards, reports, and tools.
- Discover live Gateway capabilities first (`gateway.manifest.read` or `soha capabilities --query apphosting`). Do not assume `apphosting.*` MCP tools are registered on older servers; use `soha host` CLI commands for local bundle packaging and binary `application/gzip` streaming uploads rather than passing bundle bytes through model context.
- Inspect the local project structure before packaging:
  - Built static output (`dist/`, `build/`, `out/`, or a static folder containing a root `index.html`): target that built output directory directly and pass `--spa` when client-side routing needs extensionless route fallback to `index.html`.
  - Unbuilt frontend source (`package.json` and `src/` without built `index.html`): run or ask the user to run the project build command first, then deploy the generated output folder rather than raw source or `node_modules/`.
  - Backend-dependent or stateful applications: warn clearly that current static App Hosting serves immutable static assets from private object storage, and dynamic HTTP applications must never persist business data to ephemeral local container files (such as local SQLite `.db` or writable JSON files) because scale-to-zero and instance recycling wipe local disk state; persistent business state belongs in a managed `DATABASE_URL` store.
- Default visibility for every newly created hosted application is `private` (`仅自己`, accessible only to the current owner). Never widen visibility to `selected` or `organization` unless the user explicitly asks to share.
- Preserve fixed application identity (`--app-id <id>`) when updating an existing hosted app so its immutable `dnsLabel` and HTTPS URL remain unchanged, and omit `--visibility` on routine updates unless the user explicitly asked to change who can access the app.
- Keep an explicit or recorded `--idempotency-key` across uncertain network retries so retries return the original deployment receipt without creating duplicate applications or revisions.

## Workflow

1. Inspect the local workspace directory to locate the built static output containing root `index.html`, verify whether SPA fallback (`--spa`) is needed, and check for excluded or forbidden files (`.env`, `.env.*`, `*.pem`, `*.key`, `id_rsa*`, `credentials.json`, symlinks).
2. Discover live capabilities (`gateway.manifest.read` or `soha host list --output json`) and check whether the user is updating an existing hosted application (`--app-id`) or creating a new one (`--name`).
3. Determine target visibility:
   - If the user did not specify an audience, keep the default `private` (`仅自己`).
   - If the user asks to share with specific people or departments (including departments outside the author's own team), query `apphosting.audience.search` (via Gateway MCP) or `soha host audience --query <keyword> --output json` to resolve exact `userIds` and `teamIds`.
   - Present the deployment summary (target directory, app name or `appId`, create vs update, SPA fallback, visibility mode, and resolved colleague/department names and IDs) and ask for user confirmation before publishing or broadening access.
4. Run preflight first without uploading:
   - Execute `soha host deploy <dir> --name "<name>" --dry-run --output json` (or `--app-id <id> --dry-run --output json`, or `apphosting.apps.preflight` when checking an existing app via MCP).
   - If preflight reports `allowed: false` or blockers (quota reached, oversize bundle, missing `index.html`, flagged files), stop, explain the exact blocker, and keep any existing online revision untouched.
5. Execute the confirmed publish with a stable idempotency key:
   - Run `soha host deploy <dir> ... --idempotency-key <key> --yes --output json` (reusing `--app-id <appId>` returned by preflight or prior `list`/`get` so the draft or existing app is updated cleanly).
   - If the upload times out or returns an uncertain transport error, retry with the exact same `--idempotency-key` and `--app-id`.
6. Report the deployment receipt clearly: `App ID`, `Revision` (`revisionId` and `revisionNumber`), `Visibility`, `Status`, and the fixed HTTPS `URL` (`https://<dnsLabel>.<appsDomain>`). If an update fails, reassure the user that any previously active online revision remains unaffected.
7. For post-publish lifecycle requests:
   - Audience changes: use `soha host visibility <app-id> --mode <private|selected|organization> [--user ID]... [--team ID]... --yes` (or `apphosting.apps.visibility.set`). Warn before narrowing to `private` or removing users/teams that `routeGeneration` advances immediately and removed colleagues' saved links and active browser sessions are denied (`403`) on their next request.
   - Version inspection and rollback: list history with `soha host revisions <app-id>` (`apphosting.apps.revisions.list`), confirm the target ready `revisionId` with the user, and run `soha host rollback <app-id> --revision <revision-id> --idempotency-key <key> --yes` (`apphosting.apps.rollback`). Rollback preserves current `visibility` and administrative suspension state.
   - Pause and resume: use `soha host offline <app-id> --reason "<text>" --yes` (`apphosting.apps.offline`) to immediately block all visitor and owner traffic while retaining revisions, and `soha host online <app-id> --yes` (`apphosting.apps.online`) to restore service (unless the app is `suspended_by_admin`, which requires administrator action).

## Examples

### Input Example

User asks: "Publish the built dashboard in `./dist` as `Q4 Ops Dashboard` and share it with the Finance department and Carol."

### Expected Tool Calls

1. `gateway.manifest.read` (or `soha capabilities --query apphosting`) to verify available `apphosting.*` tools in the current session.
2. `apphosting.audience.search` (or `soha host audience --query finance --output json` and `soha host audience --query carol --output json`) to resolve exact `teamIds` and `userIds`.
3. `apphosting.apps.list` (or `soha host list --query "Q4 Ops Dashboard" --output json`) to check whether an owned app already exists.
4. `soha host deploy ./dist --name "Q4 Ops Dashboard" --spa --dry-run --output json` to validate local bundle safety and server preflight limits without uploading.
5. After explicit user confirmation of the resolved Finance `teamId` and Carol `userId`, `soha host deploy ./dist --app-id <appId> --visibility selected --team <teamId> --user <userId> --spa --idempotency-key <key> --yes --output json` to stream the bundle and activate the revision.

## Permission Boundaries

- Requires `apphosting.apps.view` to list or inspect hosted apps and revisions, and `apphosting.apps.publish` (plus `ai.gateway.invoke` when calling Gateway MCP tools) to search audience candidates, create apps, run preflight, upload revisions, change visibility, roll back, or toggle online/offline status.
- Never access object storage buckets, S3/MinIO endpoints, PostgreSQL, or `app-gateway` internal token endpoints directly; all control operations flow through authenticated Soha APIs or `soha host`.
- Never read, print, or transmit any access token, refresh token, password, private key, secret, credential, or `.env` environment variable content in prompts, tool arguments, or deployment bundles.
- Administrative platform settings, quota changes, and lifting `suspended_by_admin` require `apphosting.admin.manage` and are outside this publisher skill.

## Forbidden Actions

- Do not package or upload `.env`, `.env.*`, `*.pem`, `*.key`, `id_rsa*`, `credentials.json`, private key files, symlink targets outside the build output directory, or `node_modules/`.
- Do not set `--visibility organization` or `--visibility selected` without explicit user instruction and user confirmation of the resolved `teamIds` / `userIds`.
- Do not upload raw `.tar.gz` bytes through LLM tool JSON arguments; always use `soha host deploy` on the local machine for streaming binary upload.
- Do not claim a dynamic backend using local SQLite or local disk JSON files will retain user submissions across idle scale-to-zero cycles.
- Do not attempt to bypass `suspended_by_admin` by creating a duplicate application under a new subdomain to evade governance controls.

## Guardrails

- Redact any access token, refresh token, private key, password, secret, credential, or sensitive environment variable encountered in build logs or error output.
- Always run `--dry-run` preflight and resolve local sensitive-file or missing-`index.html` blockers before executing a non-interactive `--yes` deployment.
- Reuse the original `--idempotency-key` and `--app-id` when recovering from an interrupted `soha host deploy` or `soha host rollback` request.
- When narrowing visibility to `private` or `selected`, explicitly inform the user that removed colleagues lose access immediately even if they already opened the link.
