# soha-cli

`soha-cli` is the local entry point for soha AI Gateway. It handles login, local profiles, AI client context, MCP stdio startup, and capability inspection for the current identity.

The CLI does not execute real platform actions. MCP tool calls are proxied to the soha backend:

```text
soha-cli mcp start
  -> GET /api/v1/ai-gateway/capabilities
  -> POST /api/v1/ai-gateway/tools/:toolName/invoke
  -> backend permissionKeys / scope grants / MCP tool grants / access policies / skill bindings / audit
  -> owning application service
```

## Build And Help

```bash
go run ./cmd/soha-cli help
go build -o ./bin/soha-cli ./cmd/soha-cli
```

Current first-version command surface:

- `login`
- `profile list|show|use`
- `context show|set`
- `capabilities`
- `mcp start`
- `mcp install`
- `skill list`
- `skill install`
- `diagnose`

`skill install` installs AI-readable workflow guidance only; it does not grant extra permissions. Authorization still comes from Gateway manifests, permission keys, scope grants, and MCP tool grants.

## Login

```bash
soha-cli login \
  --server http://localhost:8080 \
  --login admin \
  --profile local \
  --ai-client codex \
  --ai-client-id codex-local
```

If `--password` is omitted, the CLI reads the password from standard input.

The default local config path is:

```text
~/.soha/config.json
```

Set `SOHA_CONFIG=/abs/path/config.json` to override it. The config file is written with `0600` permissions and the parent directory is created with `0700` permissions.

`profile show` displays redacted tokens only. Do not write full tokens, kubeconfigs, environment variables, or service-account secrets to logs, issues, AI conversations, or diagnostic attachments.

## Profile And Context

```bash
soha-cli profile list
soha-cli profile show local
soha-cli profile use local
```

AI client context is sent to Gateway as headers for audit and tool-grant filtering:

```bash
soha-cli context set \
  --profile local \
  --ai-client-id codex-local \
  --ai-client Codex \
  --skill-id delivery-developer \
  --source soha-cli
```

Headers:

- `X-Soha-AI-Client-ID`
- `X-Soha-AI-Client`
- `X-Soha-Skill-ID`
- `X-Soha-Source`

## Capability Check

```bash
soha-cli capabilities --profile local
soha-cli capabilities --profile local --output names
```

`capabilities` calls `GET /api/v1/ai-gateway/capabilities` and prints the current tools, resources, prompts, skills, permission keys, and manifest summary.

If a capability is missing, check:

- The profile is logged in to the intended server.
- The user or token has `ai.gateway.view`.
- The business tool also has the required domain permission, such as `delivery.*`.
- `mcp_tool_grants` did not narrow the tool away.
- `ai_access_policies` or `ai_gateway_skill_bindings` did not narrow the current AI client, role, or subject away.

## MCP stdio Server

Start locally:

```bash
soha-cli mcp start --profile local
```

Generate MCP client configuration:

```bash
soha-cli mcp install --profile local --command /usr/local/bin/soha-cli
```

Output shape:

```json
{
  "mcpServers": {
    "soha": {
      "command": "/usr/local/bin/soha-cli",
      "args": ["mcp", "start", "--profile", "local"]
    }
  }
}
```

If `--profile` is omitted, `mcp install` uses the current profile.

The MCP server supports:

- `initialize`
- `tools/list`
- `tools/call`
- `resources/list`
- `prompts/list`

`tools/list` is generated from the Gateway manifest. `tools/call` proxies only to `POST /api/v1/ai-gateway/tools/:toolName/invoke`; it never accesses PostgreSQL, Kubernetes, Docker, runner workspaces, or local kubeconfigs directly.

First-version invokable tools:

- `delivery.applications.list`
- `delivery.applications.create`
- `delivery.application_environments.list`
- `delivery.actions.trigger`
- `delivery.release_bundles.list`
- `delivery.execution_tasks.list`
- `k8s.pods.list`
- `k8s.pods.logs`
- `k8s.deployments.list`
- `k8s.services.list`
- `k8s.events.list`
- `diagnosis.release_failure.analyze`

k8s tools require `clusterId`; `namespace` follows the platform scope semantics and may be empty for an aggregated view. `k8s.pods.logs` also requires `podName`, with optional `container`, `tailLines`, `sinceSeconds`, and `previous`. Gateway applies basic sensitive-field redaction to log outputs.

## Skills

The repository ships the first Gateway Skills:

- `delivery-developer`
- `delivery-tester`
- `k8s-sre`
- `security-change`

List installable Skills:

```bash
soha-cli skill list --source skills/ai-gateway
```

Install one Skill:

```bash
soha-cli skill install \
  --source skills/ai-gateway \
  --dest ~/.soha/skills \
  delivery-developer
```

Install all Skills:

```bash
soha-cli skill install --all
```

The default source is `skills/ai-gateway`, overrideable with `SOHA_SKILLS_SOURCE=/abs/path`. The default destination is `~/.soha/skills`, overrideable with `SOHA_SKILLS_DIR=/abs/path`.

Skills are workflow instructions, not security boundaries. After a client installs a Skill, it still must work through the MCP tools visible to the current identity.

## Diagnose

```bash
soha-cli diagnose --profile local
```

`diagnose` validates the profile, server, token, and Gateway capability path, then prints tool/resource/prompt/skill counts. It does not print tokens.
