import type {
  DeliveryDocumentKind,
  WorkflowCatalogParams,
  DeliveryExecutionHistoryParams,
  DeliveryDeploymentRef,
  DeliveryGatewayReadinessParams,
  DeliveryListParams,
  DeliveryBatchListParams,
  DeliveryRuntimeKind,
  DeliveryTargetCandidateParams,
  DeliveryWorkloadMetricsRef,
  DeliveryWorkloadRef,
  RepositoryListParams,
  GitReferenceParams,
  GitCommitParams,
} from './types'

const ROOT = ['delivery'] as const
const APPLICATIONS = [...ROOT, 'applications'] as const
const ENVIRONMENTS = [...ROOT, 'environments'] as const
const ENVIRONMENT_CATALOG = [...ROOT, 'environment-catalog'] as const
const BUILD_TEMPLATES = [...ROOT, 'templates', 'build'] as const
const DEPLOYMENT_TEMPLATES = [...ROOT, 'templates', 'deployment'] as const
const WORKFLOW_TEMPLATES = [...ROOT, 'templates', 'workflow'] as const
const BLUEPRINTS = [...ROOT, 'blueprints'] as const
const BUILDS = [...ROOT, 'builds'] as const
const WORKFLOWS = [...ROOT, 'workflows'] as const
const RELEASES = [...ROOT, 'releases'] as const
const REGISTRIES = [...ROOT, 'registries'] as const
const RELEASE_BOARD = [...ROOT, 'release-board'] as const
const RELEASE_BUNDLES = [...ROOT, 'release-bundles'] as const
const EXECUTION_TASKS = [...ROOT, 'execution-tasks'] as const
const RUNTIME = [...ROOT, 'runtime'] as const
const WORKLOADS = [...ROOT, 'workloads'] as const
const DEPLOYMENTS = [...ROOT, 'deployments'] as const
const GATEWAY = [...ROOT, 'gateway'] as const
const PLANS = [...ROOT, 'plans'] as const
const DEPENDENCIES = [...ROOT, 'dependencies'] as const
const REPOSITORIES = [...ROOT, 'repositories'] as const

export function normalizeDeliveryId(id: string) {
  return id.trim()
}

export function normalizeDeliveryListParams(params: DeliveryListParams = {}): DeliveryListParams {
  const applicationId = params.applicationId?.trim()
  const limit =
    typeof params.limit === 'number' && Number.isFinite(params.limit) && params.limit > 0
      ? Math.floor(params.limit)
      : undefined
  return {
    ...(applicationId ? { applicationId } : {}),
    ...(limit ? { limit } : {}),
    ...(params.buildSourceId?.trim() ? { buildSourceId: params.buildSourceId.trim() } : {}),
    ...(params.applicationEnvironmentId?.trim()
      ? { applicationEnvironmentId: params.applicationEnvironmentId.trim() }
      : {}),
  }
}

export function normalizeRepositoryListParams(params: RepositoryListParams = {}) {
  return {
    applicationId: params.applicationId?.trim() || undefined,
    search: params.search?.trim() || undefined,
    limit: params.limit,
  }
}
export function normalizeGitReferenceParams(params: GitReferenceParams) {
  return {
    projectId: params.projectId.trim(),
    search: params.search?.trim() || undefined,
    limit: params.limit,
  }
}
export function normalizeGitCommitParams(params: GitCommitParams) {
  return { ...normalizeGitReferenceParams(params), page: params.page }
}

export function normalizeTargetCandidateParams(
  params: DeliveryTargetCandidateParams,
): DeliveryTargetCandidateParams {
  return {
    clusterId: params.clusterId.trim(),
    namespace: params.namespace.trim(),
    workloadKind: params.workloadKind,
    crdName: params.crdName?.trim() || undefined,
    search: params.search?.trim() || undefined,
    limit: params.limit,
  }
}

export function normalizeWorkloadRef(ref: DeliveryWorkloadRef): DeliveryWorkloadRef {
  return {
    applicationId: ref.applicationId.trim(),
    applicationEnvironmentId: ref.applicationEnvironmentId.trim(),
    workloadName: ref.workloadName.trim(),
  }
}

export function normalizeWorkloadMetricsRef(
  ref: DeliveryWorkloadMetricsRef,
): Required<DeliveryWorkloadMetricsRef> {
  return {
    clusterId: ref.clusterId.trim(),
    namespace: ref.namespace.trim(),
    workloadName: ref.workloadName.trim(),
    rangeMinutes: ref.rangeMinutes ?? 60,
  }
}

export function normalizeDeploymentRef(ref: DeliveryDeploymentRef): DeliveryDeploymentRef {
  return {
    clusterId: ref.clusterId.trim(),
    namespace: ref.namespace.trim(),
    workloadName: ref.workloadName.trim(),
  }
}

export function normalizeGatewayReadinessParams(
  params: DeliveryGatewayReadinessParams,
): Required<DeliveryGatewayReadinessParams> {
  return {
    skillId: params.skillId.trim(),
    source: params.source?.trim() || 'delivery-workbench',
  }
}

export function normalizeBatchListParams(params: DeliveryBatchListParams = {}) {
  const serviceId = params.serviceId?.trim()
  return {
    ...normalizeDeliveryListParams(params),
    ...(serviceId ? { serviceId } : {}),
    ...(params.workflowId?.trim() ? { workflowId: params.workflowId.trim() } : {}),
  }
}

export const deliveryKeys = {
  documents: {
    source: (kind: DeliveryDocumentKind, id: string, version?: number) =>
      [...ROOT, 'document-source', kind, id, version] as const,
    export: (
      kind: DeliveryDocumentKind,
      id: string,
      version: number | undefined,
      format: 'yaml' | 'json',
    ) => [...ROOT, 'document-export', kind, id, version, format] as const,
  },
  triggers: {
    all: [...ROOT, 'triggers'] as const,
    list: (kind: string, id: string, offset = 0) =>
      [...ROOT, 'triggers', kind, normalizeDeliveryId(id), offset] as const,
    events: (id: string, offset = 0) =>
      [...ROOT, 'triggers', normalizeDeliveryId(id), 'events', offset] as const,
  },
  templateSources: {
    list: (offset = 0) => [...ROOT, 'template-sources', 'list', offset] as const,
    detail: (id: string) => [...ROOT, 'template-sources', id] as const,
    objects: (id: string, offset = 0) =>
      [...ROOT, 'template-sources', id, 'objects', offset] as const,
    runs: (id: string, offset = 0) => [...ROOT, 'template-sources', id, 'runs', offset] as const,
    run: (id: string, runId: string) => [...ROOT, 'template-sources', id, 'run', runId] as const,
  },
  executionHistory: (params: DeliveryExecutionHistoryParams = {}) =>
    [...ROOT, 'execution-history', params] as const,
  workflowCatalog: (params: WorkflowCatalogParams = {}) =>
    [...ROOT, 'workflow-catalog', params] as const,
  deliveryWorkflows: {
    all: [...ROOT, 'delivery-workflows'] as const,
    list: () => [...ROOT, 'delivery-workflows', 'list'] as const,
    detail: (id: string) =>
      [...ROOT, 'delivery-workflows', 'detail', normalizeDeliveryId(id)] as const,
  },
  batches: {
    all: [...ROOT, 'batches'] as const,
    list: (params: DeliveryBatchListParams = {}) =>
      [...ROOT, 'batches', 'list', normalizeBatchListParams(params)] as const,
    detail: (id: string) => [...ROOT, 'batches', 'detail', normalizeDeliveryId(id)] as const,
  },
  all: ROOT,
  applications: {
    all: APPLICATIONS,
    buildpacksCapability: (id: string) =>
      [...APPLICATIONS, 'detail', normalizeDeliveryId(id), 'buildpacks-capability'] as const,
    lists: [...APPLICATIONS, 'list'] as const,
    list: () => [...APPLICATIONS, 'list'] as const,
    details: [...APPLICATIONS, 'detail'] as const,
    detail: (id: string) => [...APPLICATIONS, 'detail', normalizeDeliveryId(id)] as const,
    runtime: (id: string) =>
      [...APPLICATIONS, 'detail', normalizeDeliveryId(id), 'runtime'] as const,
    services: (id: string) =>
      [...APPLICATIONS, 'detail', normalizeDeliveryId(id), 'services'] as const,
  },
  environmentCatalog: {
    all: ENVIRONMENT_CATALOG,
    list: () => [...ENVIRONMENT_CATALOG, 'list'] as const,
  },
  environments: {
    all: ENVIRONMENTS,
    lists: [...ENVIRONMENTS, 'list'] as const,
    list: () => [...ENVIRONMENTS, 'list'] as const,
    details: [...ENVIRONMENTS, 'detail'] as const,
    detail: (id: string) => [...ENVIRONMENTS, 'detail', normalizeDeliveryId(id)] as const,
    targetSource: (bindingId: string, targetId: string, uid: string) =>
      [...ENVIRONMENTS, 'target-source', bindingId, targetId, uid] as const,
    targetCandidates: (params: DeliveryTargetCandidateParams) =>
      [...ENVIRONMENTS, 'target-candidates', normalizeTargetCandidateParams(params)] as const,
    helmReleases: (clusterId: string, namespace: string) =>
      [...ENVIRONMENTS, 'helm-releases', clusterId.trim(), namespace.trim()] as const,
  },
  deploymentTemplates: {
    versions: (id: string) =>
      [...DEPLOYMENT_TEMPLATES, 'detail', normalizeDeliveryId(id), 'versions'] as const,
    version: (id: string, version: number) =>
      [...DEPLOYMENT_TEMPLATES, 'detail', normalizeDeliveryId(id), 'versions', version] as const,
    all: DEPLOYMENT_TEMPLATES,
    list: () => [...DEPLOYMENT_TEMPLATES, 'list'] as const,
    detail: (id: string) => [...DEPLOYMENT_TEMPLATES, 'detail', normalizeDeliveryId(id)] as const,
  },
  buildTemplates: {
    versions: (id: string) =>
      [...BUILD_TEMPLATES, 'detail', normalizeDeliveryId(id), 'versions'] as const,
    version: (id: string, version: number) =>
      [...BUILD_TEMPLATES, 'detail', normalizeDeliveryId(id), 'versions', version] as const,
    all: BUILD_TEMPLATES,
    list: () => [...BUILD_TEMPLATES, 'list'] as const,
    detail: (id: string) => [...BUILD_TEMPLATES, 'detail', normalizeDeliveryId(id)] as const,
    usage: (id: string) =>
      [...BUILD_TEMPLATES, 'detail', normalizeDeliveryId(id), 'usage'] as const,
  },
  workflowTemplates: {
    documentCompatibility: (id?: string, revision?: number, key?: string) =>
      [...WORKFLOW_TEMPLATES, 'document-compatibility', id, revision, key] as const,
    versions: (id: string) =>
      [...WORKFLOW_TEMPLATES, 'detail', normalizeDeliveryId(id), 'versions'] as const,
    version: (id: string, version: number) =>
      [...WORKFLOW_TEMPLATES, 'detail', normalizeDeliveryId(id), 'versions', version] as const,
    all: WORKFLOW_TEMPLATES,
    list: () => [...WORKFLOW_TEMPLATES, 'list'] as const,
    detail: (id: string) => [...WORKFLOW_TEMPLATES, 'detail', normalizeDeliveryId(id)] as const,
    usage: (id: string) =>
      [...WORKFLOW_TEMPLATES, 'detail', normalizeDeliveryId(id), 'usage'] as const,
  },
  blueprints: {
    all: BLUEPRINTS,
    list: () => [...BLUEPRINTS, 'list'] as const,
    detail: (id: string) => [...BLUEPRINTS, 'detail', normalizeDeliveryId(id)] as const,
    usage: (id: string) => [...BLUEPRINTS, 'detail', normalizeDeliveryId(id), 'usage'] as const,
  },
  builds: {
    all: BUILDS,
    list: (params: DeliveryListParams = {}) =>
      [...BUILDS, 'list', normalizeDeliveryListParams(params)] as const,
  },
  workflows: {
    all: WORKFLOWS,
    list: (params: DeliveryListParams = {}) =>
      [...WORKFLOWS, 'list', normalizeDeliveryListParams(params)] as const,
  },
  releases: {
    all: RELEASES,
    list: (params: DeliveryListParams = {}) =>
      [...RELEASES, 'list', normalizeDeliveryListParams(params)] as const,
  },
  registries: {
    all: REGISTRIES,
    list: () => [...REGISTRIES, 'list'] as const,
  },
  releaseBoard: {
    all: RELEASE_BOARD,
    list: () => [...RELEASE_BOARD, 'list'] as const,
  },
  releaseBundles: {
    all: RELEASE_BUNDLES,
    list: () => [...RELEASE_BUNDLES, 'list'] as const,
    detail: (id: string) => [...RELEASE_BUNDLES, 'detail', normalizeDeliveryId(id)] as const,
    artifacts: (id: string) =>
      [...RELEASE_BUNDLES, 'detail', normalizeDeliveryId(id), 'artifacts'] as const,
  },
  executionTasks: {
    all: EXECUTION_TASKS,
    rollout: (id: string) =>
      [...EXECUTION_TASKS, 'detail', normalizeDeliveryId(id), 'rollout'] as const,
    list: () => [...EXECUTION_TASKS, 'list'] as const,
    detail: (id: string) => [...EXECUTION_TASKS, 'detail', normalizeDeliveryId(id)] as const,
    logs: (id: string) => [...EXECUTION_TASKS, 'detail', normalizeDeliveryId(id), 'logs'] as const,
    artifacts: (id: string) =>
      [...EXECUTION_TASKS, 'detail', normalizeDeliveryId(id), 'artifacts'] as const,
  },
  runtime: {
    all: RUNTIME,
    detail: (kind: DeliveryRuntimeKind, id: string) =>
      [...RUNTIME, 'detail', kind, normalizeDeliveryId(id)] as const,
  },
  workloads: {
    all: WORKLOADS,
    runtime: (ref: DeliveryWorkloadRef) =>
      [...WORKLOADS, 'runtime', normalizeWorkloadRef(ref)] as const,
    metrics: (ref: DeliveryWorkloadMetricsRef) =>
      [...WORKLOADS, 'metrics', normalizeWorkloadMetricsRef(ref)] as const,
  },
  deployments: {
    all: DEPLOYMENTS,
    rollouts: (ref: DeliveryDeploymentRef) =>
      [...DEPLOYMENTS, 'rollouts', normalizeDeploymentRef(ref)] as const,
  },
  gateway: {
    all: GATEWAY,
    readiness: (params: DeliveryGatewayReadinessParams) =>
      [...GATEWAY, 'readiness', normalizeGatewayReadinessParams(params)] as const,
  },
  plans: {
    all: PLANS,
    detail: (id: string) => [...PLANS, 'detail', normalizeDeliveryId(id)] as const,
  },
  dependencies: {
    all: DEPENDENCIES,
    clusters: () => [...DEPENDENCIES, 'clusters'] as const,
  },
  repositories: {
    all: REPOSITORIES,
    list: (params: RepositoryListParams = {}) =>
      [...REPOSITORIES, 'list', normalizeRepositoryListParams(params)] as const,
    gitProjects: (params: RepositoryListParams = {}) =>
      [...REPOSITORIES, 'gitlab', 'projects', normalizeRepositoryListParams(params)] as const,
    gitBranches: (params: GitReferenceParams) =>
      [...REPOSITORIES, 'gitlab', 'branches', normalizeGitReferenceParams(params)] as const,
    gitTags: (params: GitReferenceParams) =>
      [...REPOSITORIES, 'gitlab', 'tags', normalizeGitReferenceParams(params)] as const,
    gitCommits: (params: GitCommitParams) =>
      [...REPOSITORIES, 'gitlab', 'commits', normalizeGitCommitParams(params)] as const,
  },
}

export const deliveryMutationKeys = {
  all: [...ROOT, 'mutation'] as const,
  applications: (action: string) => [...deliveryMutationKeys.all, 'applications', action] as const,
  repositories: (action: string) => [...deliveryMutationKeys.all, 'repositories', action] as const,
  applicationServices: (action: string) =>
    [...deliveryMutationKeys.all, 'application-services', action] as const,
  environments: (action: string) => [...deliveryMutationKeys.all, 'environments', action] as const,
  deploymentTemplates: (action: string) =>
    [...deliveryMutationKeys.all, 'deployment-templates', action] as const,
  buildTemplates: (action: string) =>
    [...deliveryMutationKeys.all, 'build-templates', action] as const,
  workflowTemplates: (action: string) =>
    [...deliveryMutationKeys.all, 'workflow-templates', action] as const,
  blueprints: (action: string) => [...deliveryMutationKeys.all, 'blueprints', action] as const,
  workflows: (action: string) => [...deliveryMutationKeys.all, 'workflows', action] as const,
  releases: (action: string) => [...deliveryMutationKeys.all, 'releases', action] as const,
  registries: (action: string) => [...deliveryMutationKeys.all, 'registries', action] as const,
  executionTasks: (action: string) =>
    [...deliveryMutationKeys.all, 'execution-tasks', action] as const,
  workloads: (action: string) => [...deliveryMutationKeys.all, 'workloads', action] as const,
  deployments: (action: string) => [...deliveryMutationKeys.all, 'deployments', action] as const,
  plans: (action: string) => [...deliveryMutationKeys.all, 'plans', action] as const,
}
