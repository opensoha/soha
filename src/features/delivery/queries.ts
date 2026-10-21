import { queryOptions } from '@tanstack/react-query'
import { deliveryApi } from './api'
import {
  deliveryKeys,
  normalizeBatchListParams,
  normalizeDeliveryId,
  normalizeDeliveryListParams,
  normalizeDeploymentRef,
  normalizeGatewayReadinessParams,
  normalizeTargetCandidateParams,
  normalizeWorkloadMetricsRef,
  normalizeWorkloadRef,
  normalizeRepositoryListParams,
  normalizeGitReferenceParams,
  normalizeGitCommitParams,
} from './keys'
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

export interface DeliveryQueryOptions {
  enabled?: boolean
  refetchInterval?: number | false
}

function hasValue(value: string) {
  return Boolean(value.trim())
}

function pollingOptions(options: DeliveryQueryOptions) {
  return {
    enabled: options.enabled ?? true,
    ...(options.refetchInterval !== undefined ? { refetchInterval: options.refetchInterval } : {}),
  }
}

export const deliveryQueries = {
  documents: {
    source: (kind: DeliveryDocumentKind, id: string, version?: number) =>
      queryOptions({
        queryKey: deliveryKeys.documents.source(kind, id, version),
        queryFn: () => deliveryApi.documents.source(kind, id, version),
        enabled: Boolean(id),
      }),
  },
  triggers: {
    list: (kind: string, id: string, offset = 0, enabled = true) =>
      queryOptions({
        queryKey: deliveryKeys.triggers.list(kind, id, offset),
        queryFn: () => deliveryApi.triggers.list(kind, id, offset),
        enabled: enabled && hasValue(id),
        refetchInterval: 10000,
      }),
    events: (id: string, offset = 0, enabled = true) =>
      queryOptions({
        queryKey: deliveryKeys.triggers.events(id, offset),
        queryFn: () => deliveryApi.triggers.events(id, offset),
        enabled: enabled && hasValue(id),
        refetchInterval: 5000,
      }),
  },
  templateSources: {
    list: (offset = 0, enabled = true) =>
      queryOptions({
        queryKey: deliveryKeys.templateSources.list(offset),
        queryFn: () => deliveryApi.templateSources.list(offset),
        enabled,
      }),
    detail: (id: string, enabled = true) =>
      queryOptions({
        queryKey: deliveryKeys.templateSources.detail(id),
        queryFn: () => deliveryApi.templateSources.detail(id),
        enabled: enabled && Boolean(id),
      }),
    objects: (id: string, offset = 0) =>
      queryOptions({
        queryKey: deliveryKeys.templateSources.objects(id, offset),
        queryFn: () => deliveryApi.templateSources.objects(id, offset),
        enabled: Boolean(id),
      }),
    runs: (id: string, offset = 0) =>
      queryOptions({
        queryKey: deliveryKeys.templateSources.runs(id, offset),
        queryFn: () => deliveryApi.templateSources.runs(id, offset),
        enabled: Boolean(id),
      }),
    run: (id: string, runId: string) =>
      queryOptions({
        queryKey: deliveryKeys.templateSources.run(id, runId),
        queryFn: () => deliveryApi.templateSources.run(id, runId),
        enabled: Boolean(id && runId),
        refetchInterval: (query) => (query.state.data?.status === 'running' ? 3000 : false),
      }),
  },
  executionHistory: (params: DeliveryExecutionHistoryParams = {}, enabled = true) =>
    queryOptions({
      queryKey: deliveryKeys.executionHistory(params),
      queryFn: () => deliveryApi.executionHistory.list(params),
      enabled,
      refetchInterval: (query) => (query.state.status === 'error' ? false : 5000),
    }),
  workflowCatalog: (params: WorkflowCatalogParams = {}, enabled = true) =>
    queryOptions({
      queryKey: deliveryKeys.workflowCatalog(params),
      queryFn: () => deliveryApi.workflowCatalog.list(params),
      enabled,
    }),
  plans: {
    detail: (id: string, enabled = true) =>
      queryOptions({
        queryKey: deliveryKeys.plans.detail(id),
        queryFn: () => deliveryApi.plans.detail(id),
        enabled: enabled && hasValue(id),
        refetchInterval: (query) =>
          query.state.status === 'error' || query.state.data?.status === 'confirmed' ? false : 2000,
      }),
  },
  deliveryWorkflows: {
    list: (enabled = true) =>
      queryOptions({
        queryKey: deliveryKeys.deliveryWorkflows.list(),
        queryFn: deliveryApi.deliveryWorkflows.list,
        enabled,
      }),
    detail: (id: string, enabled = true) =>
      queryOptions({
        queryKey: deliveryKeys.deliveryWorkflows.detail(id),
        queryFn: () => deliveryApi.deliveryWorkflows.detail(id),
        enabled: enabled && hasValue(id),
      }),
  },
  batches: {
    list: (params: DeliveryBatchListParams = {}, options: DeliveryQueryOptions = {}) => {
      const normalized = normalizeBatchListParams(params)
      return queryOptions({
        queryKey: deliveryKeys.batches.list(normalized),
        queryFn: () => deliveryApi.batches.list(normalized),
        ...pollingOptions(options),
      })
    },
    detail: (id: string, enabled = true) =>
      queryOptions({
        queryKey: deliveryKeys.batches.detail(id),
        queryFn: () => deliveryApi.batches.detail(id),
        enabled: enabled && hasValue(id),
        refetchInterval: (query) =>
          query.state.status === 'error' ||
          ['completed', 'partially_completed', 'failed', 'canceled'].includes(
            query.state.data?.status ?? '',
          )
            ? false
            : 2000,
      }),
  },
  repositories: {
    list: (params: RepositoryListParams = {}, enabled = true) => {
      const normalized = normalizeRepositoryListParams(params)
      return queryOptions({
        queryKey: deliveryKeys.repositories.list(normalized),
        queryFn: () => deliveryApi.repositories.list(normalized),
        enabled,
      })
    },
    gitProjects: (params: RepositoryListParams = {}, enabled = true) => {
      const normalized = normalizeRepositoryListParams(params)
      return queryOptions({
        queryKey: deliveryKeys.repositories.gitProjects(normalized),
        queryFn: () => deliveryApi.gitlab.projects(normalized),
        enabled,
      })
    },
    gitBranches: (params: GitReferenceParams, enabled = true) => {
      const normalized = normalizeGitReferenceParams(params)
      return queryOptions({
        queryKey: deliveryKeys.repositories.gitBranches(normalized),
        queryFn: () => deliveryApi.gitlab.branches(normalized),
        enabled: enabled && hasValue(normalized.projectId),
      })
    },
    gitTags: (params: GitReferenceParams, enabled = true) => {
      const normalized = normalizeGitReferenceParams(params)
      return queryOptions({
        queryKey: deliveryKeys.repositories.gitTags(normalized),
        queryFn: () => deliveryApi.gitlab.tags(normalized),
        enabled: enabled && hasValue(normalized.projectId),
      })
    },
    gitCommits: (params: GitCommitParams, enabled = true) => {
      const normalized = normalizeGitCommitParams(params)
      return queryOptions({
        queryKey: deliveryKeys.repositories.gitCommits(normalized),
        queryFn: () => deliveryApi.gitlab.commits(normalized),
        enabled: enabled && hasValue(normalized.projectId),
      })
    },
  },
  applications: {
    buildpacksCapability: (id: string) => {
      const applicationId = normalizeDeliveryId(id)
      return queryOptions({
        queryKey: deliveryKeys.applications.buildpacksCapability(applicationId),
        queryFn: () => deliveryApi.applications.buildpacksCapability(applicationId),
        enabled: hasValue(applicationId),
      })
    },
    list: (enabled = true) =>
      queryOptions({
        queryKey: deliveryKeys.applications.list(),
        queryFn: deliveryApi.applications.list,
        enabled,
      }),
    detail: (id: string, enabled = true, refetchInterval: number | false = false) => {
      const applicationId = normalizeDeliveryId(id)
      return queryOptions({
        queryKey: deliveryKeys.applications.detail(applicationId),
        queryFn: () => deliveryApi.applications.detail(applicationId),
        refetchInterval,
        enabled: enabled && hasValue(applicationId),
      })
    },
    runtime: (id: string, enabled = true, refetchInterval: number | false = false) => {
      const applicationId = normalizeDeliveryId(id)
      return queryOptions({
        queryKey: deliveryKeys.applications.runtime(applicationId),
        queryFn: () => deliveryApi.applications.runtime(applicationId),
        refetchInterval,
        enabled: enabled && hasValue(applicationId),
      })
    },
    services: (id: string, enabled = true) => {
      const applicationId = normalizeDeliveryId(id)
      return queryOptions({
        queryKey: deliveryKeys.applications.services(applicationId),
        queryFn: () => deliveryApi.applications.services(applicationId),
        enabled: enabled && hasValue(applicationId),
      })
    },
  },
  environmentCatalog: {
    list: (enabled = true) =>
      queryOptions({
        queryKey: deliveryKeys.environmentCatalog.list(),
        queryFn: deliveryApi.environmentCatalog.list,
        enabled,
      }),
  },
  environments: {
    list: (enabled = true) =>
      queryOptions({
        queryKey: deliveryKeys.environments.list(),
        queryFn: deliveryApi.environments.list,
        enabled,
      }),
    detail: (id: string, enabled = true) => {
      const environmentId = normalizeDeliveryId(id)
      return queryOptions({
        queryKey: deliveryKeys.environments.detail(environmentId),
        queryFn: () => deliveryApi.environments.detail(environmentId),
        enabled: enabled && hasValue(environmentId),
      })
    },
    targetSource: (bindingId: string, targetId: string, uid: string, enabled: boolean) =>
      queryOptions({
        queryKey: deliveryKeys.environments.targetSource(bindingId, targetId, uid),
        queryFn: ({ signal }) => deliveryApi.environments.targetSource(bindingId, targetId, signal),
        enabled: enabled && Boolean(bindingId && targetId && uid),
        gcTime: 0,
        staleTime: 0,
        retry: false,
        refetchOnWindowFocus: false,
      }),
    targetCandidates: (params: DeliveryTargetCandidateParams, enabled = true) => {
      const normalized = normalizeTargetCandidateParams(params)
      return queryOptions({
        queryKey: deliveryKeys.environments.targetCandidates(normalized),
        queryFn: () => deliveryApi.environments.targetCandidates(normalized),
        enabled: enabled && hasValue(normalized.clusterId) && hasValue(normalized.namespace),
      })
    },
    helmReleases: (clusterId: string, namespace: string, enabled = true) => {
      const normalizedClusterId = clusterId.trim()
      const normalizedNamespace = namespace.trim()
      return queryOptions({
        queryKey: deliveryKeys.environments.helmReleases(normalizedClusterId, normalizedNamespace),
        queryFn: () =>
          deliveryApi.environments.helmReleases(normalizedClusterId, normalizedNamespace),
        enabled: enabled && hasValue(normalizedClusterId) && hasValue(normalizedNamespace),
      })
    },
  },
  deploymentTemplates: {
    versions: (id: string, enabled = true) =>
      queryOptions({
        queryKey: deliveryKeys.deploymentTemplates.versions(id),
        queryFn: () => deliveryApi.deploymentTemplates.versions(id),
        enabled: enabled && hasValue(id),
      }),
    version: (id: string, version: number, enabled = true) =>
      queryOptions({
        queryKey: deliveryKeys.deploymentTemplates.version(id, version),
        queryFn: () => deliveryApi.deploymentTemplates.version(id, version),
        enabled: enabled && hasValue(id) && version > 0,
      }),
    list: (enabled = true) =>
      queryOptions({
        queryKey: deliveryKeys.deploymentTemplates.list(),
        queryFn: deliveryApi.deploymentTemplates.list,
        enabled,
      }),
    detail: (id: string, enabled = true) =>
      queryOptions({
        queryKey: deliveryKeys.deploymentTemplates.detail(id),
        queryFn: () => deliveryApi.deploymentTemplates.detail(id),
        enabled: enabled && hasValue(id),
      }),
  },
  buildTemplates: {
    versions: (id: string, enabled = true) =>
      queryOptions({
        queryKey: deliveryKeys.buildTemplates.versions(id),
        queryFn: () => deliveryApi.buildTemplates.versions(id),
        enabled: enabled && hasValue(id),
      }),
    version: (id: string, version: number, enabled = true) =>
      queryOptions({
        queryKey: deliveryKeys.buildTemplates.version(id, version),
        queryFn: () => deliveryApi.buildTemplates.version(id, version),
        enabled: enabled && hasValue(id) && version > 0,
      }),
    list: (enabled = true) =>
      queryOptions({
        queryKey: deliveryKeys.buildTemplates.list(),
        queryFn: deliveryApi.buildTemplates.list,
        enabled,
      }),
    usage: (id: string, enabled = true) => {
      const templateId = normalizeDeliveryId(id)
      return queryOptions({
        queryKey: deliveryKeys.buildTemplates.usage(templateId),
        queryFn: () => deliveryApi.buildTemplates.usage(templateId),
        enabled: enabled && hasValue(templateId),
      })
    },
  },
  workflowTemplates: {
    detail: (id: string, enabled = true) =>
      queryOptions({
        queryKey: deliveryKeys.workflowTemplates.detail(id),
        queryFn: () => deliveryApi.workflowTemplates.detail(id),
        enabled: enabled && hasValue(id),
      }),
    versions: (id: string, enabled = true) =>
      queryOptions({
        queryKey: deliveryKeys.workflowTemplates.versions(id),
        queryFn: () => deliveryApi.workflowTemplates.versions(id),
        enabled: enabled && hasValue(id),
      }),
    version: (id: string, version: number, enabled = true) =>
      queryOptions({
        queryKey: deliveryKeys.workflowTemplates.version(id, version),
        queryFn: () => deliveryApi.workflowTemplates.version(id, version),
        enabled: enabled && hasValue(id) && version > 0,
      }),
    list: (enabled = true) =>
      queryOptions({
        queryKey: deliveryKeys.workflowTemplates.list(),
        queryFn: deliveryApi.workflowTemplates.list,
        enabled,
      }),
    usage: (id: string, enabled = true) => {
      const templateId = normalizeDeliveryId(id)
      return queryOptions({
        queryKey: deliveryKeys.workflowTemplates.usage(templateId),
        queryFn: () => deliveryApi.workflowTemplates.usage(templateId),
        enabled: enabled && hasValue(templateId),
      })
    },
  },
  blueprints: {
    list: (enabled = true) =>
      queryOptions({
        queryKey: deliveryKeys.blueprints.list(),
        queryFn: deliveryApi.blueprints.list,
        enabled,
      }),
    usage: (id: string, enabled = true) => {
      const blueprintId = normalizeDeliveryId(id)
      return queryOptions({
        queryKey: deliveryKeys.blueprints.usage(blueprintId),
        queryFn: () => deliveryApi.blueprints.usage(blueprintId),
        enabled: enabled && hasValue(blueprintId),
      })
    },
  },
  builds: {
    list: (params: DeliveryListParams = {}, enabled = true) => {
      const normalized = normalizeDeliveryListParams(params)
      return queryOptions({
        queryKey: deliveryKeys.builds.list(normalized),
        queryFn: () => deliveryApi.builds.list(normalized),
        enabled,
      })
    },
  },
  workflows: {
    list: (params: DeliveryListParams = {}, options: DeliveryQueryOptions = {}) => {
      const normalized = normalizeDeliveryListParams(params)
      return queryOptions({
        queryKey: deliveryKeys.workflows.list(normalized),
        queryFn: () => deliveryApi.workflows.list(normalized),
        ...pollingOptions(options),
      })
    },
  },
  releases: {
    list: (params: DeliveryListParams = {}, options: DeliveryQueryOptions = {}) => {
      const normalized = normalizeDeliveryListParams(params)
      return queryOptions({
        queryKey: deliveryKeys.releases.list(normalized),
        queryFn: () => deliveryApi.releases.list(normalized),
        ...pollingOptions(options),
      })
    },
  },
  registries: {
    list: (enabled = true) =>
      queryOptions({
        queryKey: deliveryKeys.registries.list(),
        queryFn: deliveryApi.registries.list,
        enabled,
      }),
  },
  releaseBoard: {
    list: (options: DeliveryQueryOptions = {}) =>
      queryOptions({
        queryKey: deliveryKeys.releaseBoard.list(),
        queryFn: deliveryApi.releaseBoard.list,
        ...pollingOptions(options),
      }),
  },
  releaseBundles: {
    list: (options: DeliveryQueryOptions = {}) =>
      queryOptions({
        queryKey: deliveryKeys.releaseBundles.list(),
        queryFn: deliveryApi.releaseBundles.list,
        ...pollingOptions(options),
      }),
    artifacts: (id: string, enabled = true) => {
      const bundleId = normalizeDeliveryId(id)
      return queryOptions({
        queryKey: deliveryKeys.releaseBundles.artifacts(bundleId),
        queryFn: () => deliveryApi.releaseBundles.artifacts(bundleId),
        enabled: enabled && hasValue(bundleId),
      })
    },
  },
  executionTasks: {
    rollout: (id: string, enabled: boolean) =>
      queryOptions({
        queryKey: deliveryKeys.executionTasks.rollout(id),
        queryFn: () => deliveryApi.executionTasks.rollout(id),
        enabled: enabled && hasValue(id),
        refetchInterval: (query) => (enabled && query.state.status !== 'error' ? 3000 : false),
        retry: false,
      }),
    detail: (id: string, enabled = true) =>
      queryOptions({
        queryKey: deliveryKeys.executionTasks.detail(id),
        queryFn: () => deliveryApi.executionTasks.get(id),
        enabled: enabled && hasValue(id),
        refetchInterval: (query) =>
          ['completed', 'failed', 'canceled', 'cancelled'].includes(query.state.data?.status ?? '')
            ? false
            : 3000,
      }),
    list: (options: DeliveryQueryOptions = {}) =>
      queryOptions({
        queryKey: deliveryKeys.executionTasks.list(),
        queryFn: deliveryApi.executionTasks.list,
        ...pollingOptions(options),
      }),
    logs: (id: string, options: DeliveryQueryOptions = {}) => {
      const taskId = normalizeDeliveryId(id)
      return queryOptions({
        queryKey: deliveryKeys.executionTasks.logs(taskId),
        queryFn: () => deliveryApi.executionTasks.logs(taskId),
        ...pollingOptions({
          ...options,
          enabled: (options.enabled ?? true) && hasValue(taskId),
        }),
      })
    },
    artifacts: (id: string, enabled = true) => {
      const taskId = normalizeDeliveryId(id)
      return queryOptions({
        queryKey: deliveryKeys.executionTasks.artifacts(taskId),
        queryFn: () => deliveryApi.executionTasks.artifacts(taskId),
        enabled: enabled && hasValue(taskId),
      })
    },
  },
  runtime: {
    detail: (kind: DeliveryRuntimeKind, id: string, enabled = true) => {
      const recordId = normalizeDeliveryId(id)
      return queryOptions({
        queryKey: deliveryKeys.runtime.detail(kind, recordId),
        queryFn: () => deliveryApi.runtime.detail(kind, recordId),
        enabled: enabled && hasValue(recordId),
        refetchInterval: (query) =>
          kind === 'execution_task' &&
          ['queued', 'dispatching', 'running'].includes(query.state.data?.object?.status ?? '')
            ? 3000
            : false,
      })
    },
  },
  workloads: {
    runtime: (
      ref: DeliveryWorkloadRef,
      enabled = true,
      refetchInterval: number | false = false,
    ) => {
      const normalized = normalizeWorkloadRef(ref)
      return queryOptions({
        queryKey: deliveryKeys.workloads.runtime(normalized),
        queryFn: () => deliveryApi.workloads.runtime(normalized),
        refetchInterval,
        enabled:
          enabled &&
          hasValue(normalized.applicationId) &&
          hasValue(normalized.applicationEnvironmentId) &&
          hasValue(normalized.workloadName),
      })
    },
    metrics: (ref: DeliveryWorkloadMetricsRef, enabled = true) => {
      const normalized = normalizeWorkloadMetricsRef(ref)
      return queryOptions({
        queryKey: deliveryKeys.workloads.metrics(normalized),
        queryFn: () => deliveryApi.workloads.metrics(normalized),
        enabled:
          enabled &&
          hasValue(normalized.clusterId) &&
          hasValue(normalized.namespace) &&
          hasValue(normalized.workloadName),
      })
    },
  },
  deployments: {
    rollouts: (ref: DeliveryDeploymentRef, enabled = true) => {
      const normalized = normalizeDeploymentRef(ref)
      return queryOptions({
        queryKey: deliveryKeys.deployments.rollouts(normalized),
        queryFn: () => deliveryApi.deployments.rollouts(normalized),
        enabled:
          enabled &&
          hasValue(normalized.clusterId) &&
          hasValue(normalized.namespace) &&
          hasValue(normalized.workloadName),
      })
    },
  },
  gateway: {
    readiness: (params: DeliveryGatewayReadinessParams, enabled = true) => {
      const normalized = normalizeGatewayReadinessParams(params)
      return queryOptions({
        queryKey: deliveryKeys.gateway.readiness(normalized),
        queryFn: () => deliveryApi.gateway.readiness(normalized),
        enabled,
        retry: false,
        staleTime: 30_000,
      })
    },
  },
  dependencies: {
    clusters: (enabled = true) =>
      queryOptions({
        queryKey: deliveryKeys.dependencies.clusters(),
        queryFn: deliveryApi.dependencies.clusters,
        enabled,
      }),
  },
}

export const runtimeDetailQueries = {
  detail: (kind: DeliveryRuntimeKind, id: string, enabled = true) =>
    deliveryQueries.runtime.detail(kind, id, enabled),
}
