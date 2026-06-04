import type {
  WorkbenchAgentCapability,
  WorkbenchAgentProvider,
  WorkbenchAgentRun,
} from "@/features/copilot/workbench-types";

export const TRACES_BACKEND_OPTIONS: Array<{ value: string; label: string }> = [
  { value: "jaeger", label: "jaeger" },
  { value: "skywalking", label: "skywalking" },
];

export type AgentRuntimeState =
  | "connected"
  | "disabled"
  | "error"
  | "failed"
  | "healthy"
  | "idle"
  | "in-process"
  | "observed"
  | "queued"
  | "ready"
  | "running"
  | "unavailable"
  | "unknown"
  | "waiting";

export interface AgentRuntimeSummary {
  queuedRuns: number;
  runningRuns: number;
  recentFailures: number;
  lastRun?: WorkbenchAgentRun;
  lastAgentId?: string;
  lastHeartbeatAt?: string;
  lastCompletedAt?: string;
}

export type AgentProviderRuntimeRow = WorkbenchAgentProvider & {
  runtimeState: AgentRuntimeState | string;
  runtimeSummary: AgentRuntimeSummary;
};

export const AGENT_RUNTIME_STATE_LABELS: Record<string, string> = {
  connected: "有心跳",
  disabled: "已停用",
  error: "异常",
  failed: "失败",
  healthy: "健康",
  idle: "空闲",
  "in-process": "内置同步",
  observed: "有记录",
  queued: "排队中",
  ready: "就绪",
  running: "运行中",
  unavailable: "不可用",
  unknown: "未知",
  waiting: "等待任务",
};

export function agentRuntimeStateColor(state?: string) {
  switch ((state || "").toLowerCase()) {
    case "connected":
    case "healthy":
    case "idle":
    case "in-process":
    case "observed":
    case "ready":
    case "running":
      return "success";
    case "queued":
    case "waiting":
    case "unknown":
      return "warning";
    case "disabled":
      return "default";
    case "error":
    case "failed":
    case "unavailable":
      return "error";
    default:
      return "default";
  }
}

function agentRunTimestamp(run?: WorkbenchAgentRun) {
  if (!run) return 0;
  const values = [
    run.updatedAt,
    run.completedAt,
    run.lastHeartbeatAt,
    run.startedAt,
    run.queuedAt,
    run.createdAt,
  ];
  return values.reduce((latest, value) => {
    if (!value) return latest;
    const timestamp = new Date(value).getTime();
    return Number.isFinite(timestamp) ? Math.max(latest, timestamp) : latest;
  }, 0);
}

function latestAgentRuntimeTime(
  runs: WorkbenchAgentRun[],
  field: "lastHeartbeatAt" | "completedAt",
) {
  let latest = "";
  let latestTimestamp = 0;
  for (const run of runs) {
    const value = run[field];
    if (!value) continue;
    const timestamp = new Date(value).getTime();
    if (Number.isFinite(timestamp) && timestamp > latestTimestamp) {
      latest = value;
      latestTimestamp = timestamp;
    }
  }
  return latest;
}

function agentRunMatchesProvider(
  run: WorkbenchAgentRun,
  provider: WorkbenchAgentProvider,
) {
  return (
    run.providerId === provider.id ||
    run.providerId === provider.kind ||
    run.providerKind === provider.kind ||
    run.providerKind === provider.id
  );
}

export function summarizeAgentProviderRuntime(
  provider: WorkbenchAgentProvider,
  runs: WorkbenchAgentRun[],
): AgentRuntimeSummary {
  const relatedRuns = runs.filter((run) =>
    agentRunMatchesProvider(run, provider),
  );
  const latestRun = relatedRuns.reduce<WorkbenchAgentRun | undefined>(
    (latest, run) =>
      agentRunTimestamp(run) > agentRunTimestamp(latest) ? run : latest,
    undefined,
  );
  const runtime = provider.runtimeStatus;
  return {
    queuedRuns:
      runtime?.queuedRuns ??
      relatedRuns.filter((run) => run.status === "queued").length,
    runningRuns:
      runtime?.runningRuns ??
      relatedRuns.filter((run) => run.status === "running").length,
    recentFailures:
      runtime?.recentFailures ??
      relatedRuns.filter((run) =>
        ["callback_timeout", "failed"].includes(run.status),
      ).length,
    lastRun: latestRun,
    lastAgentId: runtime?.lastAgentId || latestRun?.claimedByAgentId,
    lastHeartbeatAt:
      runtime?.lastHeartbeatAt ||
      latestAgentRuntimeTime(relatedRuns, "lastHeartbeatAt"),
    lastCompletedAt:
      runtime?.lastCompletedAt ||
      latestAgentRuntimeTime(relatedRuns, "completedAt"),
  };
}

export function resolveAgentRuntimeState(
  provider: WorkbenchAgentProvider,
  summary: AgentRuntimeSummary,
): AgentRuntimeState | string {
  if (!provider.enabled) return "disabled";
  if (!provider.supportsAsync) return "in-process";
  const runtimeState = provider.runtimeStatus?.state?.trim();
  if (runtimeState) return runtimeState;
  if (summary.runningRuns > 0) return "running";
  if (summary.queuedRuns > 0) return "queued";
  if (summary.lastHeartbeatAt) return "connected";
  if (summary.lastRun) return "observed";
  return "waiting";
}

export function agentCapabilityLabels(
  capabilityIds: string[] | undefined,
  capabilities: WorkbenchAgentCapability[],
) {
  const nameById = new Map(capabilities.map((item) => [item.id, item.name]));
  return (capabilityIds ?? []).map((id) => nameById.get(id) || id);
}

export interface AISettings {
  provider?: {
    id?: string;
    name?: string;
    providerKind?: string;
    enabled: boolean;
    baseUrl: string;
    apiKey: string;
    model: string;
  };
  providers?: Array<{
    id: string;
    name: string;
    providerKind: string;
    enabled: boolean;
    baseUrl: string;
    apiKey: string;
    model: string;
  }>;
  defaultProviderId?: string;
  enabled: boolean;
  baseUrl: string;
  apiKey: string;
  model: string;
  skillsRegistry?: Array<{
    id: string;
    name: string;
    category?: string;
    ownerModule?: string;
    description?: string;
    enabled: boolean;
    scopes?: string[];
    capabilityRefs?: string[];
    blueprintRefs?: string[];
    scopeRules?: string[];
    inputSchema?: Record<string, unknown>;
    outputSchema?: Record<string, unknown>;
  }>;
}

export interface AISkillSetting {
  id: string;
  name: string;
  category?: string;
  ownerModule?: string;
  description?: string;
  enabled: boolean;
  scopes?: string[];
  capabilityRefs?: string[];
  blueprintRefs?: string[];
  scopeRules?: string[];
  inputSchema?: Record<string, unknown>;
  outputSchema?: Record<string, unknown>;
}

export interface DataSource {
  id: string;
  name: string;
  sourceKind: string;
  backendType: string;
  enabled: boolean;
  credentialRef?: string;
  mcpAdapter: string;
  scope?: Record<string, unknown>;
  queryBudget?: Record<string, unknown>;
  redactionPolicy?: Record<string, unknown>;
  config?: Record<string, unknown>;
  validationStatus?: string;
  validationMessage?: string;
  lastValidatedAt?: string;
}

export interface AnalysisProfile {
  id: string;
  name: string;
  mode: string;
  enabledSources?: string[];
  enabledPlaybooks?: string[];
  remediationPolicy: string;
  enabled: boolean;
  queryBudgets?: Record<string, unknown>;
  outputStyle?: Record<string, unknown>;
}

export interface AutomationPolicy {
  id: string;
  name: string;
  triggerType: string;
  analysisKinds?: string[];
  analysisProfileId: string;
  remediationPolicy: string;
  enabled: boolean;
  dedupWindowSeconds: number;
  triggerConditions?: Record<string, unknown>;
  approvalPolicy?: Record<string, unknown>;
}

export interface AIProviderConnection {
  id: string;
  name: string;
  providerKind: string;
  enabled: boolean;
  baseUrl: string;
  apiKey: string;
  model: string;
}

export function normalizeAIProviderConnection(
  item?: Partial<AIProviderConnection> | null,
): AIProviderConnection {
  return {
    id: String(item?.id || "default"),
    name: String(item?.name || "default"),
    providerKind: String(item?.providerKind || "openai-compatible"),
    enabled: Boolean(item?.enabled),
    baseUrl: String(item?.baseUrl || ""),
    apiKey: String(item?.apiKey || ""),
    model: String(item?.model || ""),
  };
}

export const PLAYBOOK_OPTIONS = [
  { value: "release-correlation", label: "release-correlation" },
  { value: "cluster-health", label: "cluster-health" },
  { value: "access-drift", label: "access-drift" },
  { value: "runtime-instability", label: "runtime-instability" },
  { value: "alert-pressure", label: "alert-pressure" },
  { value: "build-queue", label: "build-queue" },
  { value: "error-burst", label: "error-burst" },
  { value: "dependency-timeout", label: "dependency-timeout" },
];

export const SEVERITY_OPTIONS = [
  { value: "critical", label: "critical" },
  { value: "warning", label: "warning" },
  { value: "info", label: "info" },
];

export const STATUS_OPTIONS = [
  { value: "firing", label: "firing" },
  { value: "resolved", label: "resolved" },
];

export function buildDataSourceFormValues(item?: DataSource | null) {
  return {
    id: item?.id,
    name: item?.name ?? "",
    sourceKind: item?.sourceKind ?? "logs",
    backendType: item?.backendType ?? "es",
    enabled: item?.enabled ?? true,
    credentialRef: item?.credentialRef ?? "",
    mcpAdapter: item?.mcpAdapter ?? "logs.v1",
    scopeClusterId: String(item?.scope?.clusterId ?? ""),
    scopeNamespace: String(item?.scope?.namespace ?? ""),
    scopeService: String(item?.scope?.service ?? ""),
    scopeWorkload: String(item?.scope?.workload ?? ""),
    budgetMaxQueries: Number(item?.queryBudget?.maxQueries ?? 12),
    budgetMaxLogBytes: Number(item?.queryBudget?.maxLogBytes ?? 20_000_000),
    budgetTimeoutSeconds: Number(item?.queryBudget?.timeoutSeconds ?? 90),
    redactionMaskFields: Array.isArray(item?.redactionPolicy?.maskFields)
      ? (item?.redactionPolicy?.maskFields as string[])
      : [],
    redactionMaskPatterns: Array.isArray(item?.redactionPolicy?.maskPatterns)
      ? (item?.redactionPolicy?.maskPatterns as string[])
      : [],
    redactionTruncateLongLines: Boolean(
      item?.redactionPolicy?.truncateLongLines ?? true,
    ),
    configEndpoint: String(item?.config?.endpoint ?? ""),
    configIndex: String(item?.config?.index ?? ""),
    configTable: String(item?.config?.table ?? ""),
    configUsername: String(item?.config?.username ?? ""),
    configPassword: String(item?.config?.password ?? ""),
    configBearerToken: String(item?.config?.bearerToken ?? ""),
    configTimestampField: String(item?.config?.timestampField ?? "@timestamp"),
    configMessageField: String(item?.config?.messageField ?? "message"),
    configSeverityField: String(item?.config?.severityField ?? "level"),
    configServiceField: String(item?.config?.serviceField ?? "service"),
    configWorkloadField: String(item?.config?.workloadField ?? "workload"),
    configNamespaceField: String(item?.config?.namespaceField ?? "namespace"),
    configClusterField: String(item?.config?.clusterField ?? "cluster"),
    lokiLabelCluster: String(
      (item?.config?.labelKeys as Record<string, unknown> | undefined)
        ?.cluster ?? "cluster",
    ),
    lokiLabelNamespace: String(
      (item?.config?.labelKeys as Record<string, unknown> | undefined)
        ?.namespace ?? "namespace",
    ),
    lokiLabelService: String(
      (item?.config?.labelKeys as Record<string, unknown> | undefined)
        ?.service ?? "service",
    ),
    lokiLabelWorkload: String(
      (item?.config?.labelKeys as Record<string, unknown> | undefined)
        ?.workload ?? "workload",
    ),
    lokiLabelSeverity: String(
      (item?.config?.labelKeys as Record<string, unknown> | undefined)
        ?.severity ?? "level",
    ),
  };
}

export function buildDataSourcePayload(values: Record<string, unknown>) {
  const sourceKind = String(values.sourceKind ?? "logs");
  const backendType = String(values.backendType ?? "es");
  const config: Record<string, unknown> = {
    endpoint: values.configEndpoint || undefined,
    timestampField: values.configTimestampField || undefined,
    messageField: values.configMessageField || undefined,
    severityField: values.configSeverityField || undefined,
    serviceField: values.configServiceField || undefined,
    workloadField: values.configWorkloadField || undefined,
    namespaceField: values.configNamespaceField || undefined,
    clusterField: values.configClusterField || undefined,
    username: values.configUsername || undefined,
    password: values.configPassword || undefined,
    bearerToken: values.configBearerToken || undefined,
  };
  if (backendType === "es") config.index = values.configIndex || undefined;
  if (backendType === "clickhouse")
    config.table = values.configTable || undefined;
  if (backendType === "loki") {
    config.labelKeys = {
      cluster: values.lokiLabelCluster || "cluster",
      namespace: values.lokiLabelNamespace || "namespace",
      service: values.lokiLabelService || "service",
      workload: values.lokiLabelWorkload || "workload",
      severity: values.lokiLabelSeverity || "level",
    };
  }
  return {
    id: values.id,
    name: values.name,
    sourceKind,
    backendType,
    enabled: values.enabled,
    credentialRef: values.credentialRef,
    mcpAdapter: values.mcpAdapter,
    scope: {
      clusterId: values.scopeClusterId || undefined,
      namespace: values.scopeNamespace || undefined,
      service: values.scopeService || undefined,
      workload: values.scopeWorkload || undefined,
    },
    queryBudget: {
      maxQueries: Number(values.budgetMaxQueries || 0),
      maxLogBytes: Number(values.budgetMaxLogBytes || 0),
      timeoutSeconds: Number(values.budgetTimeoutSeconds || 0),
    },
    redactionPolicy: {
      maskFields: values.redactionMaskFields || [],
      maskPatterns: values.redactionMaskPatterns || [],
      truncateLongLines: Boolean(values.redactionTruncateLongLines),
    },
    config,
  };
}

export function buildProfileFormValues(item?: AnalysisProfile | null) {
  return {
    id: item?.id,
    name: item?.name ?? "",
    mode: item?.mode ?? "root_cause",
    enabledSources: item?.enabledSources ?? [],
    enabledPlaybooks: item?.enabledPlaybooks ?? [],
    remediationPolicy: item?.remediationPolicy ?? "suggest_only",
    defaultTimeRangeMinutes: Number(
      (item as unknown as { defaultTimeRangeMinutes?: number } | undefined)
        ?.defaultTimeRangeMinutes ?? 60,
    ),
    timeoutSeconds: Number(
      (item as unknown as { timeoutSeconds?: number } | undefined)
        ?.timeoutSeconds ?? 90,
    ),
    enabled: item?.enabled ?? true,
    budgetMaxQueries: Number(item?.queryBudgets?.maxQueries ?? 12),
    budgetMaxLogBytes: Number(item?.queryBudgets?.maxLogBytes ?? 20_000_000),
    budgetMaxEvidenceItems: Number(item?.queryBudgets?.maxEvidenceItems ?? 20),
    outputSummaryLevel: String(item?.outputStyle?.summaryLevel ?? "standard"),
    outputIncludeEvidenceDetail: Boolean(
      item?.outputStyle?.includeEvidenceDetail ?? true,
    ),
    outputIncludeRecommendations: Boolean(
      item?.outputStyle?.includeRecommendations ?? true,
    ),
    outputIncludeTimeline: Boolean(item?.outputStyle?.includeTimeline ?? false),
  };
}

export function buildProfilePayload(values: Record<string, unknown>) {
  return {
    id: values.id,
    name: values.name,
    mode: values.mode,
    enabledSources: values.enabledSources || [],
    enabledPlaybooks: values.enabledPlaybooks || [],
    remediationPolicy: values.remediationPolicy,
    defaultTimeRangeMinutes: Number(values.defaultTimeRangeMinutes || 60),
    timeoutSeconds: Number(values.timeoutSeconds || 90),
    enabled: values.enabled,
    queryBudgets: {
      maxQueries: Number(values.budgetMaxQueries || 0),
      maxLogBytes: Number(values.budgetMaxLogBytes || 0),
      maxEvidenceItems: Number(values.budgetMaxEvidenceItems || 0),
    },
    outputStyle: {
      summaryLevel: values.outputSummaryLevel,
      includeEvidenceDetail: Boolean(values.outputIncludeEvidenceDetail),
      includeRecommendations: Boolean(values.outputIncludeRecommendations),
      includeTimeline: Boolean(values.outputIncludeTimeline),
    },
  };
}

export function buildPolicyFormValues(item?: AutomationPolicy | null) {
  const conditions = item?.triggerConditions ?? {};
  const labels =
    (conditions.labels as Record<string, unknown> | undefined) ?? {};
  const approval = item?.approvalPolicy ?? {};
  return {
    id: item?.id,
    name: item?.name ?? "",
    triggerType: item?.triggerType ?? "alert_webhook",
    analysisKinds: item?.analysisKinds ?? ["root_cause"],
    analysisProfileId: item?.analysisProfileId ?? "",
    remediationPolicy: item?.remediationPolicy ?? "suggest_only",
    enabled: item?.enabled ?? true,
    dedupWindowSeconds: Number(item?.dedupWindowSeconds ?? 900),
    cooldownSeconds: Number(
      (item as unknown as { cooldownSeconds?: number } | undefined)
        ?.cooldownSeconds ?? 0,
    ),
    triggerSeverity: Array.isArray(conditions.severity)
      ? (conditions.severity as string[])
      : [],
    triggerStatus: Array.isArray(conditions.status)
      ? (conditions.status as string[])
      : [],
    triggerMinDurationSeconds: Number(conditions.min_duration_seconds ?? 120),
    triggerLabelKey: Object.keys(labels)[0] ?? "",
    triggerLabelValue: String(Object.values(labels)[0] ?? ""),
    triggerTimeRangeMinutes: Number(conditions.time_range_minutes ?? 60),
    approvalRequired: Boolean(approval.required ?? false),
    approvalRoles: Array.isArray(approval.approverRoles)
      ? (approval.approverRoles as string[])
      : [],
  };
}

export function buildPolicyPayload(values: Record<string, unknown>) {
  const labels: Record<string, unknown> = {};
  if (values.triggerLabelKey && values.triggerLabelValue) {
    labels[String(values.triggerLabelKey)] = values.triggerLabelValue;
  }
  return {
    id: values.id,
    name: values.name,
    enabled: values.enabled,
    triggerType: values.triggerType,
    analysisKinds: values.analysisKinds || ["root_cause"],
    analysisProfileId: values.analysisProfileId,
    remediationPolicy: values.remediationPolicy,
    dedupWindowSeconds: Number(values.dedupWindowSeconds || 0),
    cooldownSeconds: Number(values.cooldownSeconds || 0),
    triggerConditions: {
      severity: values.triggerSeverity || [],
      status: values.triggerStatus || [],
      min_duration_seconds: Number(values.triggerMinDurationSeconds || 0),
      time_range_minutes: Number(values.triggerTimeRangeMinutes || 0),
      labels,
    },
    approvalPolicy: {
      required: Boolean(values.approvalRequired),
      approverRoles: values.approvalRoles || [],
    },
  };
}
