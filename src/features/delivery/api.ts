import { api } from '@/services/api-client'
import type { ApiResponse } from '@/types'
import type {
  HelmChartInspection,
  HelmChartInspectionInput,
  BuildpacksCapability,
  BuildTriggerRequest,
  RepositoryAnalysis,
  RepositoryAnalysisInput,
  RegistryConnectionListEnvelope,
  ProgressiveRolloutStatus,
  ProgressiveRolloutControlInput,
} from '@opensoha/contracts/gen/ts/sohaapi'
import type {
  DeliveryDocumentSourceInfo,
  DeliveryTrigger,
  DeliveryTriggerInput,
  DeliveryTriggerEvent,
  DeliveryTemplateSource,
  DeliveryTemplateSourceInput,
  DeliveryTemplateSourceAssociation,
  DeliveryTemplateSourceRemoveInput,
  DeliveryTemplateSyncInput,
  DeliveryTemplateSyncRun,
  DeliveryTemplateSyncApplyInput,
  DeliveryDocumentKind,
  DeliveryDocumentPreviewInput,
  DeliveryDocumentPreview,
  DeliveryDocumentApplyInput,
  DeliveryDocumentImport,
  DeliveryDocumentExport,
  DeliveryBatch,
  WorkflowCatalogParams,
  DeliveryExecutionHistoryParams,
  DeliveryExecutionHistoryPage,
  WorkflowCatalogPage,
  DeliveryBatchInput,
  DeliveryBatchListParams,
  DeliveryBatchActionInput,
  DeliveryWorkflow,
  DeliveryWorkflowInput,
  ApplicationEnvironment,
  ApplicationRuntimeDetail,
  ApplicationServiceComponent,
  ApplicationWorkloadRuntimeDetail,
  ApplicationWorkflowInput,
  BuildRecord,
  BuildTemplate,
  BuildTemplateInput,
  ServiceDeploymentTemplate,
  ServiceDeploymentTemplateInput,
  DeploymentTemplatePreview,
  DeploymentTemplatePreviewInput,
  DeliveryApplication,
  DeliveryApplicationDetail,
  DeliveryApplicationEnvironmentDetail,
  DeliveryBlueprint,
  DeliveryClusterList,
  DeliveryDeploymentRef,
  DeliveryDeploymentRollbackInput,
  DeliveryEnvironment,
  DeliveryExecutionLog,
  DeliveryGatewayManifest,
  DeliveryGatewayReadinessParams,
  DeliveryListParams,
  RepositoryListParams,
  GitReferenceParams,
  GitCommitParams,
  DeliveryRepository,
  GitProject,
  GitReference,
  GitCommit,
  DeliveryPlan,
  DeliveryPlanConfirmResult,
  DeliveryPlanRequest,
  DeliveryRecordInput,
  DeliveryRuntimeDetail,
  DeliveryRuntimeKind,
  DeliveryTargetCandidatePage,
  DeliveryTargetCandidateParams,
  DeliveryWorkloadMetricsRef,
  DeliveryWorkloadRef,
  DeliveryWorkloadRestartInput,
  ExecutionArtifact,
  ExecutionCallbackInput,
  ExecutionTask,
  ExecutionTaskActionInput,
  RegistryRecord,
  RegistryInput,
  ReleaseBoardEntry,
  ReleaseBundle,
  ReleaseRecord,
  RenderedDeliverySpec,
  ResourceMetrics,
  RolloutHistoryRecord,
  TemplateUsageSummary,
  WorkflowDecisionInput,
  WorkflowRun,
  WorkflowTemplate,
  KubernetesServiceImportInput,
  KubernetesServiceImportResult,
  HelmReleaseCandidate,
  HelmReleaseImportInput,
  HelmReleaseImportResult,
} from './types'

async function unwrap<T>(request: Promise<ApiResponse<T>>): Promise<T> {
  const response = await request
  return response.data
}

async function discard(request: Promise<unknown>): Promise<void> {
  await request
}

function segment(value: string) {
  return encodeURIComponent(value.trim())
}

function withQuery(path: string, params: Record<string, unknown> = {}) {
  const search = new URLSearchParams()
  Object.entries(params).forEach(([key, value]) => {
    if (value !== undefined && value !== null && value !== '') {
      search.set(key, String(value))
    }
  })
  const query = search.toString()
  return query ? `${path}?${query}` : path
}

export function deliveryRuntimeDetailPath(kind: DeliveryRuntimeKind, id: string) {
  const encoded = segment(id)
  switch (kind) {
    case 'build':
      return `/delivery/runtime/builds/${encoded}`
    case 'workflow':
      return `/delivery/runtime/workflows/${encoded}`
    case 'release':
      return `/delivery/runtime/releases/${encoded}`
    case 'release_bundle':
      return `/delivery/runtime/release-bundles/${encoded}`
    case 'execution_task':
      return `/delivery/runtime/execution-tasks/${encoded}`
  }
}

function applicationListPath(
  base: '/builds' | '/releases' | '/workflows',
  params: DeliveryListParams,
) {
  return withQuery(base, {
    applicationId: params.applicationId?.trim(),
    ...(base === '/builds' ? { buildSourceId: params.buildSourceId?.trim() } : {}),
    ...(base === '/workflows'
      ? { applicationEnvironmentId: params.applicationEnvironmentId?.trim() }
      : {}),
    limit: params.limit,
  })
}

function workloadRuntimePath(ref: DeliveryWorkloadRef) {
  return `/applications/${segment(ref.applicationId)}/application-environments/${segment(ref.applicationEnvironmentId)}/workloads/${segment(ref.workloadName)}/runtime`
}

function workloadMetricsPath(ref: DeliveryWorkloadMetricsRef) {
  return withQuery(
    `/clusters/${segment(ref.clusterId)}/workloads/deployments/${segment(ref.workloadName)}/metrics`,
    {
      namespace: ref.namespace.trim(),
      rangeMinutes: ref.rangeMinutes ?? 60,
    },
  )
}

function deploymentRolloutsPath(ref: DeliveryDeploymentRef) {
  return withQuery(
    `/clusters/${segment(ref.clusterId)}/workloads/deployments/${segment(ref.workloadName)}/rollouts`,
    { namespace: ref.namespace.trim() },
  )
}

function gatewayReadinessPath(params: DeliveryGatewayReadinessParams) {
  return withQuery('/ai-gateway/capabilities', {
    source: params.source?.trim() || 'delivery-workbench',
    skillId: params.skillId.trim(),
  })
}

export const deliveryApi = {
  documents: {
    source: (kind: DeliveryDocumentKind, id: string, version?: number) =>
      unwrap(
        api.get<ApiResponse<DeliveryDocumentSourceInfo>>(
          withQuery(`/delivery/documents/${segment(kind)}/${segment(id)}/source`, { version }),
        ),
      ),
    preview: (input: DeliveryDocumentPreviewInput) =>
      unwrap(api.post<ApiResponse<DeliveryDocumentPreview>>('/delivery/documents/preview', input)),
    apply: (previewId: string, input: DeliveryDocumentApplyInput) =>
      unwrap(
        api.post<ApiResponse<DeliveryDocumentImport>>(
          `/delivery/documents/imports/${segment(previewId)}/apply`,
          input,
        ),
      ),
    export: (kind: DeliveryDocumentKind, id: string, format: 'yaml' | 'json', version?: number) =>
      unwrap(
        api.get<ApiResponse<DeliveryDocumentExport>>(
          withQuery(`/delivery/documents/${segment(kind)}/${segment(id)}/export`, {
            format,
            version,
          }),
        ),
      ),
  },
  triggers: {
    list: (targetKind: string, targetId: string, offset = 0) =>
      unwrap(
        api.get<ApiResponse<DeliveryTrigger[]>>(
          withQuery('/delivery/triggers', { targetKind, targetId, offset, limit: 50 }),
        ),
      ),
    create: (input: DeliveryTriggerInput) =>
      unwrap(api.post<ApiResponse<DeliveryTrigger>>('/delivery/triggers', input)),
    update: (id: string, input: DeliveryTriggerInput) =>
      unwrap(api.put<ApiResponse<DeliveryTrigger>>(`/delivery/triggers/${segment(id)}`, input)),
    events: (id: string, offset = 0) =>
      unwrap(
        api.get<ApiResponse<DeliveryTriggerEvent[]>>(
          withQuery(`/delivery/triggers/${segment(id)}/events`, { offset, limit: 50 }),
        ),
      ),
  },
  templateSources: {
    list: (offset = 0, limit = 50) =>
      unwrap(
        api.get<ApiResponse<DeliveryTemplateSource[]>>(
          withQuery('/delivery/template-sources', { offset, limit }),
        ),
      ),
    detail: (id: string) =>
      unwrap(
        api.get<ApiResponse<DeliveryTemplateSource>>(`/delivery/template-sources/${segment(id)}`),
      ),
    create: (input: DeliveryTemplateSourceInput) =>
      unwrap(api.post<ApiResponse<DeliveryTemplateSource>>('/delivery/template-sources', input)),
    update: (id: string, input: DeliveryTemplateSourceInput) =>
      unwrap(
        api.put<ApiResponse<DeliveryTemplateSource>>(
          `/delivery/template-sources/${segment(id)}`,
          input,
        ),
      ),
    remove: (id: string, input: DeliveryTemplateSourceRemoveInput) =>
      discard(api.delete(`/delivery/template-sources/${segment(id)}`, input)),
    objects: (id: string, offset = 0, limit = 50) =>
      unwrap(
        api.get<ApiResponse<DeliveryTemplateSourceAssociation[]>>(
          withQuery(`/delivery/template-sources/${segment(id)}/objects`, { offset, limit }),
        ),
      ),
    sync: (id: string, input: DeliveryTemplateSyncInput) =>
      unwrap(
        api.post<ApiResponse<DeliveryTemplateSyncRun>>(
          `/delivery/template-sources/${segment(id)}/sync`,
          input,
        ),
      ),
    runs: (id: string, offset = 0, limit = 50) =>
      unwrap(
        api.get<ApiResponse<DeliveryTemplateSyncRun[]>>(
          withQuery(`/delivery/template-sources/${segment(id)}/sync-runs`, { offset, limit }),
        ),
      ),
    run: (id: string, runId: string) =>
      unwrap(
        api.get<ApiResponse<DeliveryTemplateSyncRun>>(
          `/delivery/template-sources/${segment(id)}/sync-runs/${segment(runId)}`,
        ),
      ),
    apply: (id: string, runId: string, input: DeliveryTemplateSyncApplyInput) =>
      unwrap(
        api.post<ApiResponse<DeliveryTemplateSyncRun>>(
          `/delivery/template-sources/${segment(id)}/sync-runs/${segment(runId)}/apply`,
          input,
        ),
      ),
    detach: (
      id: string,
      kind: DeliveryDocumentKind,
      objectId: string,
      input: DeliveryTemplateSourceRemoveInput,
    ) =>
      discard(
        api.post(
          `/delivery/template-sources/${segment(id)}/objects/${segment(kind)}/${segment(objectId)}/detach`,
          input,
        ),
      ),
  },
  deliveryWorkflows: {
    list: () => unwrap(api.get<ApiResponse<DeliveryWorkflow[]>>('/delivery-workflows')),
    detail: (id: string) =>
      unwrap(api.get<ApiResponse<DeliveryWorkflow>>(`/delivery-workflows/${segment(id)}`)),
    create: (input: DeliveryWorkflowInput) =>
      unwrap(api.post<ApiResponse<DeliveryWorkflow>>('/delivery-workflows', input)),
    update: (id: string, input: DeliveryWorkflowInput) =>
      unwrap(api.put<ApiResponse<DeliveryWorkflow>>(`/delivery-workflows/${segment(id)}`, input)),
  },
  batches: {
    list: (params: DeliveryBatchListParams = {}) =>
      unwrap(
        api.get<ApiResponse<DeliveryBatch[]>>(
          withQuery('/delivery-batches', {
            applicationId: params.applicationId?.trim(),
            serviceId: params.serviceId?.trim(),
            workflowId: params.workflowId?.trim(),
            limit: params.limit,
          }),
        ),
      ),
    detail: (id: string) =>
      unwrap(api.get<ApiResponse<DeliveryBatch>>(`/delivery-batches/${segment(id)}`)),
    create: (input: DeliveryBatchInput) =>
      unwrap(api.post<ApiResponse<DeliveryBatch>>('/delivery-batches', input)),
    cancel: (id: string, input: DeliveryBatchActionInput = {}) =>
      unwrap(
        api.post<ApiResponse<DeliveryBatch>>(`/delivery-batches/${segment(id)}/cancel`, input),
      ),
  },
  repositories: {
    list: (params: RepositoryListParams = {}) =>
      unwrap(
        api.get<ApiResponse<DeliveryRepository[]>>(
          withQuery('/repositories', {
            applicationId: params.applicationId?.trim(),
            search: params.search?.trim(),
            limit: params.limit,
          }),
        ),
      ),
    create: (payload: DeliveryRecordInput) =>
      unwrap(api.post<ApiResponse<DeliveryRepository>>('/repositories', payload)),
    update: (id: string, payload: DeliveryRecordInput) =>
      unwrap(api.put<ApiResponse<DeliveryRepository>>(`/repositories/${segment(id)}`, payload)),
    delete: (id: string) => discard(api.delete(`/repositories/${segment(id)}`)),
  },
  gitlab: {
    projects: async (params: Omit<RepositoryListParams, 'applicationId'> = {}) =>
      (
        await api.get<{ items: GitProject[] }>(
          withQuery('/integrations/gitlab/projects', {
            search: params.search?.trim(),
            limit: params.limit,
          }),
        )
      ).items,
    branches: async (params: GitReferenceParams) =>
      (
        await api.get<{ items: GitReference[] }>(
          withQuery('/integrations/gitlab/branches', {
            projectId: params.projectId,
            search: params.search,
            limit: params.limit,
          }),
        )
      ).items,
    tags: async (params: GitReferenceParams) =>
      (
        await api.get<{ items: GitReference[] }>(
          withQuery('/integrations/gitlab/tags', {
            projectId: params.projectId,
            search: params.search,
            limit: params.limit,
          }),
        )
      ).items,
    commits: (params: GitCommitParams) =>
      api.get<{ items: GitCommit[]; page: number; limit: number; hasMore: boolean }>(
        withQuery('/integrations/gitlab/commits', {
          projectId: params.projectId,
          search: params.search,
          limit: params.limit,
          page: params.page,
        }),
      ),
  },
  applications: {
    inspectHelmChart: (applicationId: string, payload: HelmChartInspectionInput) =>
      unwrap(
        api.post<ApiResponse<HelmChartInspection>>(
          `/applications/${segment(applicationId)}/helm-chart`,
          payload,
        ),
      ),
    buildpacksCapability: (applicationId: string) =>
      unwrap(
        api.get<ApiResponse<BuildpacksCapability>>(
          `/applications/${segment(applicationId)}/buildpacks-capability`,
        ),
      ),
    analyzeRepository: (applicationId: string, input: RepositoryAnalysisInput) =>
      unwrap(
        api.post<ApiResponse<RepositoryAnalysis>>(
          `/applications/${segment(applicationId)}/repository-analysis`,
          input,
        ),
      ),
    list: () => unwrap(api.get<ApiResponse<DeliveryApplication[]>>('/applications')),
    detail: (id: string) =>
      unwrap(
        api.get<ApiResponse<DeliveryApplicationDetail>>(`/applications/${segment(id)}/detail`),
      ),
    runtime: (id: string) =>
      unwrap(
        api.get<ApiResponse<ApplicationRuntimeDetail>>(`/applications/${segment(id)}/runtime`),
      ),
    create: (payload: DeliveryRecordInput) =>
      unwrap(api.post<ApiResponse<DeliveryApplication>>('/applications', payload)),
    update: (id: string, payload: DeliveryRecordInput) => {
      const {
        version,
        createdAt: _createdAt,
        updatedAt: _updatedAt,
        environmentCount: _environmentCount,
        ...input
      } = payload
      const expectedVersion = input.expectedVersion ?? version
      if (
        typeof expectedVersion !== 'number' ||
        !Number.isSafeInteger(expectedVersion) ||
        expectedVersion < 1
      )
        return Promise.reject(new Error('应用配置缺少有效版本，请重新加载后保存。'))
      return discard(api.put(`/applications/${segment(id)}`, { ...input, expectedVersion }))
    },
    delete: (id: string) => discard(api.delete(`/applications/${segment(id)}`)),
    services: (applicationId: string) =>
      unwrap(
        api.get<ApiResponse<ApplicationServiceComponent[]>>(
          `/applications/${segment(applicationId)}/services`,
        ),
      ),
    createService: (applicationId: string, payload: DeliveryRecordInput) =>
      unwrap(
        api.post<ApiResponse<ApplicationServiceComponent>>(
          `/applications/${segment(applicationId)}/services`,
          payload,
        ),
      ),
    updateService: (applicationId: string, serviceId: string, payload: DeliveryRecordInput) =>
      discard(
        api.put(`/applications/${segment(applicationId)}/services/${segment(serviceId)}`, payload),
      ),
    deleteService: (applicationId: string, serviceId: string) =>
      discard(api.delete(`/applications/${segment(applicationId)}/services/${segment(serviceId)}`)),
  },
  environmentCatalog: {
    list: () => unwrap(api.get<ApiResponse<DeliveryEnvironment[]>>('/delivery/environments')),
  },
  environments: {
    list: () => unwrap(api.get<ApiResponse<ApplicationEnvironment[]>>('/application-environments')),
    detail: (id: string) =>
      unwrap(
        api.get<ApiResponse<DeliveryApplicationEnvironmentDetail>>(
          `/application-environments/${segment(id)}/detail`,
        ),
      ),
    targetSource: (bindingId: string, targetId: string, signal?: AbortSignal) =>
      unwrap(
        api.get<ApiResponse<{ content: string }>>(
          `/application-environments/${segment(bindingId)}/targets/${segment(targetId)}/source`,
          { signal },
        ),
      ),
    targetCandidates: (params: DeliveryTargetCandidateParams) =>
      api.getEnvelope<DeliveryTargetCandidatePage>(
        withQuery('/application-environments/target-candidates', {
          clusterId: params.clusterId.trim(),
          namespace: params.namespace.trim(),
          workloadKind: params.workloadKind,
          crdName: params.crdName?.trim(),
          search: params.search?.trim(),
          limit: params.limit,
        }),
      ),
    importKubernetesServices: (payload: KubernetesServiceImportInput) =>
      unwrap(
        api.post<ApiResponse<KubernetesServiceImportResult>>(
          '/application-environments/imports',
          payload,
        ),
      ),
    helmReleases: (clusterId: string, namespace: string) =>
      unwrap(
        api.get<ApiResponse<HelmReleaseCandidate[]>>(
          withQuery(`/clusters/${segment(clusterId)}/helm/releases`, {
            namespace: namespace.trim(),
          }),
        ),
      ),
    importHelmReleases: (payload: HelmReleaseImportInput) =>
      unwrap(
        api.post<ApiResponse<HelmReleaseImportResult>>(
          '/application-environments/helm-release-imports',
          payload,
        ),
      ),
    create: (payload: DeliveryRecordInput) =>
      discard(api.post('/application-environments', payload)),
    update: (id: string, payload: DeliveryRecordInput) =>
      discard(api.put(`/application-environments/${segment(id)}`, payload)),
    saveWorkflow: (applicationId: string, id: string, payload: ApplicationWorkflowInput) =>
      unwrap(
        api.put<ApiResponse<ApplicationEnvironment>>(
          `/applications/${segment(applicationId)}/application-environments/${segment(id)}/workflow`,
          payload,
        ),
      ),
    delete: (id: string) => discard(api.delete(`/application-environments/${segment(id)}`)),
  },
  deploymentTemplates: {
    detail: (id: string) =>
      unwrap(
        api.get<ApiResponse<ServiceDeploymentTemplate>>(`/deployment-templates/${segment(id)}`),
      ),
    versions: (id: string) =>
      unwrap(
        api.get<ApiResponse<ServiceDeploymentTemplate[]>>(
          `/deployment-templates/${segment(id)}/versions`,
        ),
      ),
    version: (id: string, version: number) =>
      unwrap(
        api.get<ApiResponse<ServiceDeploymentTemplate>>(
          `/deployment-templates/${segment(id)}/versions/${version}`,
        ),
      ),
    publish: (id: string, expectedRevision: number) =>
      unwrap(
        api.post<ApiResponse<ServiceDeploymentTemplate>>(
          `/deployment-templates/${segment(id)}/publish`,
          {
            expectedRevision,
          },
        ),
      ),
    list: () => unwrap(api.get<ApiResponse<ServiceDeploymentTemplate[]>>('/deployment-templates')),
    create: (payload: ServiceDeploymentTemplateInput) =>
      unwrap(api.post<ApiResponse<ServiceDeploymentTemplate>>('/deployment-templates', payload)),
    update: (id: string, payload: ServiceDeploymentTemplateInput) =>
      unwrap(
        api.put<ApiResponse<ServiceDeploymentTemplate>>(
          `/deployment-templates/${segment(id)}`,
          payload,
        ),
      ),
    delete: (id: string) => discard(api.delete(`/deployment-templates/${segment(id)}`)),
    preview: (applicationId: string, payload: DeploymentTemplatePreviewInput) =>
      unwrap(
        api.post<ApiResponse<DeploymentTemplatePreview>>(
          `/applications/${segment(applicationId)}/deployment-template-preview`,
          payload,
        ),
      ),
  },
  buildTemplates: {
    detail: (id: string) =>
      unwrap(api.get<ApiResponse<BuildTemplate>>(`/build-templates/${segment(id)}`)),
    versions: (id: string) =>
      unwrap(api.get<ApiResponse<BuildTemplate[]>>(`/build-templates/${segment(id)}/versions`)),
    version: (id: string, version: number) =>
      unwrap(
        api.get<ApiResponse<BuildTemplate>>(`/build-templates/${segment(id)}/versions/${version}`),
      ),
    publish: (id: string, expectedRevision: number) =>
      unwrap(
        api.post<ApiResponse<BuildTemplate>>(`/build-templates/${segment(id)}/publish`, {
          expectedRevision,
        }),
      ),
    list: () => unwrap(api.get<ApiResponse<BuildTemplate[]>>('/build-templates')),
    usage: (id: string) =>
      unwrap(api.get<ApiResponse<TemplateUsageSummary>>(`/build-templates/${segment(id)}/usage`)),
    create: (payload: BuildTemplateInput) =>
      unwrap(api.post<ApiResponse<BuildTemplate>>('/build-templates', payload)),
    update: (id: string, payload: BuildTemplateInput) =>
      unwrap(api.put<ApiResponse<BuildTemplate>>(`/build-templates/${segment(id)}`, payload)),
    delete: (id: string) => discard(api.delete(`/build-templates/${segment(id)}`)),
  },
  workflowTemplates: {
    detail: (id: string) =>
      unwrap(api.get<ApiResponse<WorkflowTemplate>>(`/workflow-templates/${segment(id)}`)),
    versions: (id: string) =>
      unwrap(
        api.get<ApiResponse<WorkflowTemplate[]>>(`/workflow-templates/${segment(id)}/versions`),
      ),
    version: (id: string, version: number) =>
      unwrap(
        api.get<ApiResponse<WorkflowTemplate>>(
          `/workflow-templates/${segment(id)}/versions/${version}`,
        ),
      ),
    publish: (id: string, expectedRevision: number) =>
      unwrap(
        api.post<ApiResponse<WorkflowTemplate>>(`/workflow-templates/${segment(id)}/publish`, {
          expectedRevision,
        }),
      ),
    list: () => unwrap(api.get<ApiResponse<WorkflowTemplate[]>>('/workflow-templates')),
    usage: (id: string) =>
      unwrap(
        api.get<ApiResponse<TemplateUsageSummary>>(`/workflow-templates/${segment(id)}/usage`),
      ),
    create: (payload: DeliveryRecordInput) =>
      unwrap(api.post<ApiResponse<WorkflowTemplate>>('/workflow-templates', payload)),
    update: (id: string, payload: DeliveryRecordInput) =>
      unwrap(api.put<ApiResponse<WorkflowTemplate>>(`/workflow-templates/${segment(id)}`, payload)),
    delete: (id: string) => discard(api.delete(`/workflow-templates/${segment(id)}`)),
  },
  blueprints: {
    list: () => unwrap(api.get<ApiResponse<DeliveryBlueprint[]>>('/delivery/blueprints')),
    usage: (id: string) =>
      unwrap(
        api.get<ApiResponse<TemplateUsageSummary>>(`/delivery/blueprints/${segment(id)}/usage`),
      ),
    create: (payload: DeliveryRecordInput) =>
      unwrap(api.post<ApiResponse<DeliveryBlueprint>>('/delivery/blueprints', payload)),
    update: (id: string, payload: DeliveryRecordInput) =>
      unwrap(
        api.put<ApiResponse<DeliveryBlueprint>>(`/delivery/blueprints/${segment(id)}`, payload),
      ),
    renderSpec: (id: string) =>
      unwrap(
        api.post<ApiResponse<RenderedDeliverySpec>>(
          `/delivery/blueprints/${segment(id)}/render-spec`,
          {},
        ),
      ),
  },
  builds: {
    trigger: (payload: BuildTriggerRequest) =>
      unwrap(api.post<ApiResponse<BuildRecord>>('/builds/trigger', payload)),
    list: (params: DeliveryListParams = {}) =>
      unwrap(api.get<ApiResponse<BuildRecord[]>>(applicationListPath('/builds', params))),
  },
  workflows: {
    list: (params: DeliveryListParams = {}) =>
      unwrap(api.get<ApiResponse<WorkflowRun[]>>(applicationListPath('/workflows', params))),
    approve: ({ id, comment }: WorkflowDecisionInput) =>
      discard(api.post(`/workflows/${segment(id)}/approve`, { comment })),
    reject: ({ id, comment }: WorkflowDecisionInput) =>
      discard(api.post(`/workflows/${segment(id)}/reject`, { comment })),
  },
  releases: {
    list: (params: DeliveryListParams = {}) =>
      unwrap(api.get<ApiResponse<ReleaseRecord[]>>(applicationListPath('/releases', params))),
  },
  registries: {
    list: async () => (await api.getEnvelope<RegistryConnectionListEnvelope>('/registries')).items,
    create: (payload: RegistryInput) => discard(api.post('/registries', payload)),
    update: (id: string, payload: RegistryInput) =>
      discard(api.put(`/registries/${segment(id)}`, payload)),
    delete: (id: string) => discard(api.delete(`/registries/${segment(id)}`)),
  },
  releaseBoard: {
    list: () => unwrap(api.get<ApiResponse<ReleaseBoardEntry[]>>('/delivery/release-board')),
  },
  executionHistory: {
    list: (params: DeliveryExecutionHistoryParams = {}) =>
      unwrap(
        api.get<ApiResponse<DeliveryExecutionHistoryPage>>(
          withQuery('/delivery/execution-history', params),
        ),
      ),
  },
  workflowCatalog: {
    list: (params: WorkflowCatalogParams = {}) =>
      unwrap(
        api.get<ApiResponse<WorkflowCatalogPage>>(withQuery('/delivery/workflow-catalog', params)),
      ),
  },
  releaseBundles: {
    list: () => unwrap(api.get<ApiResponse<ReleaseBundle[]>>('/delivery/release-bundles')),
    artifacts: (id: string) =>
      unwrap(
        api.get<ApiResponse<ExecutionArtifact[]>>(
          `/delivery/release-bundles/${segment(id)}/artifacts`,
        ),
      ),
  },
  executionTasks: {
    rollout: (id: string) =>
      unwrap(
        api.get<ApiResponse<ProgressiveRolloutStatus>>(
          `/delivery/execution-tasks/${segment(id)}/rollout`,
        ),
      ),
    controlRollout: (id: string, input: ProgressiveRolloutControlInput) =>
      unwrap(
        api.post<ApiResponse<ProgressiveRolloutStatus>>(
          `/delivery/execution-tasks/${segment(id)}/rollout`,
          input,
        ),
      ),
    get: (id: string) =>
      unwrap(api.get<ApiResponse<ExecutionTask>>(`/delivery/execution-tasks/${segment(id)}`)),
    list: () => unwrap(api.get<ApiResponse<ExecutionTask[]>>('/delivery/execution-tasks')),
    logs: (id: string) =>
      unwrap(
        api.get<ApiResponse<DeliveryExecutionLog[]>>(
          `/delivery/execution-tasks/${segment(id)}/logs`,
        ),
      ),
    artifacts: (id: string) =>
      unwrap(
        api.get<ApiResponse<ExecutionArtifact[]>>(
          `/delivery/execution-tasks/${segment(id)}/artifacts`,
        ),
      ),
    callback: (payload: ExecutionCallbackInput) =>
      discard(api.post('/delivery/execution-callbacks', payload)),
    cancel: ({ id, reason }: ExecutionTaskActionInput) =>
      discard(api.post(`/delivery/execution-tasks/${segment(id)}/cancel`, { reason })),
    retry: ({ id, reason }: ExecutionTaskActionInput) =>
      discard(api.post(`/delivery/execution-tasks/${segment(id)}/retry`, { reason })),
  },
  runtime: {
    detail: (kind: DeliveryRuntimeKind, id: string) =>
      unwrap(api.get<ApiResponse<DeliveryRuntimeDetail>>(deliveryRuntimeDetailPath(kind, id))),
  },
  workloads: {
    runtime: (ref: DeliveryWorkloadRef) =>
      unwrap(api.get<ApiResponse<ApplicationWorkloadRuntimeDetail>>(workloadRuntimePath(ref))),
    metrics: (ref: DeliveryWorkloadMetricsRef) =>
      unwrap(api.get<ApiResponse<ResourceMetrics>>(workloadMetricsPath(ref))),
    restart: (input: DeliveryWorkloadRestartInput) =>
      discard(
        api.post(`/clusters/${segment(input.clusterId)}/workloads/deployments/restart`, {
          namespace: input.namespace,
          name: input.workloadName,
        }),
      ),
  },
  deployments: {
    rollouts: (ref: DeliveryDeploymentRef) =>
      unwrap(api.get<ApiResponse<RolloutHistoryRecord[]>>(deploymentRolloutsPath(ref))),
    rollback: (input: DeliveryDeploymentRollbackInput) =>
      discard(
        api.post(`/clusters/${segment(input.clusterId)}/workloads/deployments/rollback`, {
          namespace: input.namespace,
          name: input.workloadName,
          revision: input.revision,
        }),
      ),
  },
  gateway: {
    readiness: (params: DeliveryGatewayReadinessParams) =>
      unwrap(api.get<ApiResponse<DeliveryGatewayManifest>>(gatewayReadinessPath(params))),
  },
  plans: {
    detail: (id: string) =>
      unwrap(api.get<ApiResponse<DeliveryPlan>>(`/delivery/plans/${segment(id)}`)),
    create: (payload: DeliveryPlanRequest) =>
      unwrap(api.post<ApiResponse<DeliveryPlan>>('/delivery/plans', payload)),
    confirm: (id: string) =>
      unwrap(
        api.post<ApiResponse<DeliveryPlanConfirmResult>>(
          `/delivery/plans/${segment(id)}/confirm`,
          {},
        ),
      ),
    approval: (id: string, payload: { action: 'approve' | 'reject'; comment?: string }) =>
      unwrap(
        api.post<ApiResponse<DeliveryPlan>>(`/delivery/plans/${segment(id)}/approval`, payload),
      ),
  },
  dependencies: {
    clusters: () => unwrap(api.get<ApiResponse<DeliveryClusterList>>('/clusters')),
  },
}

export type { RegistryRecord }
