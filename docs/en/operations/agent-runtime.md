# Agent Runtime

## Positioning

The kubecrux agent process now has two runtime responsibilities:

- Remote cluster agent: serves resource reads and limited operations for Kubernetes clusters registered with `connectionMode=agent`.
- Control-plane runner: claims delivery, Docker, and AI Agent Runtime work from the kubecrux control plane and reports status through callbacks.

AI Agent Runtime is the shared execution abstraction used by the AI workbench, automation policies, and business diagnostics. Pages and business modules depend on kubecrux capability contracts instead of depending directly on Hermes, OpenClaw, or any provider-specific agent.

Core abstractions:

- `AgentProvider`: a pluggable executor such as `internal` or `hermes`; future providers can include OpenClaw or other agents.
- `AgentCapability`: a platform capability such as `root_cause`, `performance`, `trace`, `inspection_review`, `delivery_failure`, or `docker_diagnosis`.
- `AgentToolBinding`: binds a capability to MCP adapters, platform read tools, or provider-native tools.
- `AgentSkillBinding`: maps kubecrux skills to Hermes skills, prompt templates, or future provider skill systems.
- `AgentRun`: a durable agent execution with provider, capability, skills, toolset, scope, status, callback token, tool records, and output artifacts.

Hermes Agent is the first external provider. The kubecrux agent runner claims `ai_agent_runs`, invokes the Hermes CLI or Hermes Agent capability, and writes results back as unified `AnalysisArtifact` records.

Each `AgentRun` stores a creation-time `toolBindings` and `skillBindings` snapshot. A runner claim receives the full kubecrux capability contract: scope, toolset, budgets, platform playbooks, provider skill mappings, and MCP/internal tool mappings. Future OpenClaw, internal-agent, or other provider adapters should consume that snapshot instead of making pages or business modules derive Hermes/OpenClaw-specific parameters.

## Control-Plane Contract

The AI workbench safe catalog exposes Agent Runtime summaries:

- `GET /api/v1/copilot/workbench/catalog`
- `GET /api/v1/copilot/agent-providers`
- `GET /api/v1/copilot/agent-runs`

These authenticated user APIs return provider, capability, tool binding, skill binding, MCP adapter, data-source, analysis-profile, and skill-registry summaries. Full provider secrets and data-source credentials remain owned by Settings > AI.

Runner-facing APIs are protected by `runtime.execution_runner_token`:

- `POST /api/v1/copilot/agent-runs/claim`
- `POST /api/v1/copilot/agent-runs/callback`
- `POST /api/v1/copilot/agent-runs/tool-call`

`tool-call` is the controlled gateway for external providers to invoke kubecrux tools. Requests must include both the runner bearer token and the current `AgentRun.callbackToken`, and may only call read-only tools captured in that run's `toolBindings` snapshot. The control plane currently executes log, metric, trace, platform-event, delivery-release, delivery-build, and alert queries, then records each call as `ToolExecution` for the final `AnalysisArtifact`. Provider adapters should not bypass this gateway to reach kubecrux data sources or credentials directly.

The current Hermes/CLI provider POC in the runner prefetches up to 3 prefetchable read-only tool results before invoking the provider command, such as `events.query`, `logs.query`, `metrics.query`, `traces.query`, `delivery.releases.list`, `delivery.builds.list`, and `alerts.list`, then injects those results as `prefetchedToolResults` into the provider prompt and final output. This lets a provider consume kubecrux-controlled tool context before it supports a private tool-call protocol. If Hermes or another agent is later connected as an MCP client, the runner adapter should wrap the same `tool-call` gateway as the provider-visible MCP/tool server and still avoid exposing kubecrux data-source credentials.

The catalog also declares capability bindings such as `delivery.execution_tasks.list`, `platform.resources.snapshot`, `docker.operations.list`, `docker.services.list`, `virtualization.operations.list`, and `oncall.routes.resolve`. These are stable Agent Runtime catalog contracts. They become executable only after the matching reader or adapter is wired into `executeAgentToolBindingOutput`; until then, the runner does not include them in the default prefetch set, and direct provider calls receive an explicit unsupported error.

`ai_agent_runs` is the durable queue table for AI Agent Runtime. Status values are:

- `queued`
- `running`
- `completed`
- `failed`
- `canceled`
- `callback_timeout`

## Analysis Flow

AI workbench explicit analysis flow:

1. The user selects provider, analysis profile, toolset, and analysis mode in an `/ai-workbench` session.
2. The frontend calls `POST /api/v1/copilot/sessions/:sessionID/analyze`.
3. The `internal` provider continues to run the kubecrux in-process analysis path.
4. External providers enqueue an `AgentRun` and write an assistant message in queued state.
5. The agent runner claims the run.
6. The Hermes provider runner translates scope, toolset, skills, budget, and prompt into a Hermes invocation.
7. If the provider needs logs, metrics, traces, events, delivery release/build records, or alerts, the runner adapter should call kubecrux tools through `agent-runs/tool-call`.
8. The runner posts `running`, `completed`, or `failed` callbacks.
9. The control plane converts provider output into kubecrux `AnalysisArtifact`, preserving evidence, hypotheses, recommendations, graph, tool calls, and data-source snapshots.

Automation policy flow:

1. kubecrux automation policy remains the primary scheduler for continuous analysis.
2. After an `alert_webhook` event arrives, automation policy matching handles labels, dedup windows, and cooldowns.
3. `agentProviderId=internal` uses the built-in analysis path.
4. `agentProviderId=hermes` or another external provider creates an asynchronous `AgentRun`.
5. Hermes cron is only an optional experimental provider-native capability, not the kubecrux platform scheduler.

## Hermes Runner Config

The server must expose a runner token:

```yaml
runtime:
  execution_runner_token: demo-execution-runner-token
```

The agent enables AI Agent Runtime through `configs/agent.config.yaml`:

```yaml
control_plane:
  enabled: true
  base_url: http://127.0.0.1:8080
  bearer_token: demo-execution-runner-token
  agent_id: local-agent
  poll_interval: 5s
  agent_runtime:
    enabled: true
    worker_id: local-hermes-runner
    provider_ids:
      - hermes
    provider_kinds:
      - hermes
    hermes_command: hermes
    providers:
      hermes:
        command: hermes
        args:
          - chat
        prompt_arg: -q
        skill_arg: -s
        provider_skill_arg: ""
    workspace_root: ./.kubecrux/agent-runtime
    poll_interval: 5s
```

Start the agent:

```bash
go run ./cmd/agent
```

Use a custom config file:

```bash
KC_AGENT_CONFIG_FILE=/abs/path/to/agent.config.yaml go run ./cmd/agent
```

Current Hermes provider POC rules:

- The runner only claims `AgentRun` rows whose provider id or kind matches `hermes`.
- The default command is `hermes`; override it with `control_plane.agent_runtime.hermes_command`.
- The runner converts kubecrux run input plus tool-binding and skill-binding snapshots into a prompt, and passes skill ids to Hermes.
- The runner exposes an `agent-runs/tool-call` client helper that provider adapters can use to execute authorized read-only tools from the run snapshot; the default Hermes CLI POC prefetches a small read-only tool context into the prompt, including logs, metrics, traces, events, delivery release/build, and alert context, but does not assume a private Hermes tool-call protocol.
- Callback `analysisArtifacts` are preferred as final artifacts. If none are returned, the control plane synthesizes a basic `AnalysisArtifact` from the output summary.

The runner now dispatches by provider executor. Hermes is the built-in default executor; other CLI-style providers can be connected through `control_plane.agent_runtime.providers.<providerKind>` first, for example:

```yaml
control_plane:
  agent_runtime:
    provider_ids:
      - openclaw
    provider_kinds:
      - openclaw
    providers:
      openclaw:
        command: openclaw
        args:
          - run
        prompt_arg: --prompt
        skill_arg: ""
        provider_skill_arg: --skill
```

This provider executor still receives only the kubecrux `AgentRun` contract: the prompt includes scope, toolset, toolBindings, skillBindings, and the output schema; provider-native skill arguments come from `AgentSkillBinding.providerSkillRef`. If no executor is configured for a provider, the runner reports the run as failed instead of pretending the unknown provider can run through Hermes.

## Adding Providers

Adding a provider should extend only the adapter/runtime layer, not the AI workbench or business analysis flows:

1. Register an `AgentProvider` in the copilot service provider catalog.
2. Declare the provider-supported `AgentCapability` values.
3. Add `AgentToolBinding` and `AgentSkillBinding` entries for the capabilities.
4. Implement the provider-specific runner executor that translates `AgentRun` input into provider calls.
5. Keep callback output as kubecrux `AnalysisArtifact` or a structure that can be synthesized into one.

Hard boundaries:

- Pages, handlers, and business modules must not call Hermes directly.
- kubecrux owns budget, permissions, menus, audit, data redaction, and operation boundaries.
- Agents are pluggable executors only; they must not bypass kubecrux MCP adapters, scope, toolset, tool-call gateway, or analysis profile contracts.
- High-risk write operations must not be auto-attached to chat flows. They must go through the owning business module's durable operation or approval flow.

## Remote Cluster Agent Scope

The same `cmd/agent` still keeps the remote cluster agent APIs:

- `GET /healthz`
- `GET /api/v1/healthz`
- `GET /api/v1/platform/summary`
- `GET /api/v1/platform/namespaces`
- `GET /api/v1/platform/workloads/pods?namespace=default`
- `GET /api/v1/platform/workloads/deployments?namespace=default`
- `POST /api/v1/platform/actions/deployments/restart`
- `POST /api/v1/platform/actions/deployments/scale`

The remote cluster agent remains intentionally narrow. Logs, YAML, events, exec, and rollout history still need to be expanded under platform-agent semantics and must not be conflated with the AI Agent Runtime provider execution path.
