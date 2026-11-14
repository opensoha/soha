import { api } from '@/services/api-client'
import type { ApiResponse } from '@/types'
import type {
  ManifestFilter,
  ManifestPage,
  ManifestPackage,
  ManifestPackageInput,
  ManifestRevision,
  ManifestSource,
  ManifestEnvironmentBinding,
  ManifestEnvironmentBindingUpdateInput,
  ManifestRenderResult,
  ManifestExecutionTask,
  ManifestSyncRun,
  ManifestDeployment,
  ManifestDeliveryIntent,
} from './types'

const ROOT = '/delivery/manifest-packages'

async function unwrap<T>(request: Promise<ApiResponse<T>>) {
  const response = await request
  return response.data
}

function segment(value: string) {
  return encodeURIComponent(value.trim())
}

function listPath(filter: ManifestFilter = {}) {
  const search = new URLSearchParams()
  Object.entries(filter).forEach(([key, value]) => {
    if (value !== undefined && value !== null && value !== '') search.set(key, String(value))
  })
  return search.size > 0 ? `${ROOT}?${search.toString()}` : ROOT
}

export const manifestApi = {
  applicationPackages: async (applicationId: string) => {
    const items: ManifestPackage[] = []
    let page = 1
    let result: ManifestPage
    do {
      result = await manifestApi.list({ applicationId, page, pageSize: 100 })
      items.push(...result.items)
      page += 1
    } while (result.items.length > 0 && items.length < result.total)
    return items
  },
  list: (filter: ManifestFilter = {}) =>
    unwrap(api.get<ApiResponse<ManifestPage>>(listPath(filter))),
  get: (id: string) => unwrap(api.get<ApiResponse<ManifestPackage>>(`${ROOT}/${segment(id)}`)),
  create: (input: ManifestPackageInput) =>
    unwrap(api.post<ApiResponse<ManifestPackage>>(ROOT, input)),
  update: (id: string, input: ManifestPackageInput) =>
    unwrap(api.put<ApiResponse<ManifestPackage>>(`${ROOT}/${segment(id)}`, input)),
  remove: async (id: string) => {
    await api.delete(`${ROOT}/${segment(id)}`)
  },
  publish: (id: string, note: string) =>
    unwrap(api.post<ApiResponse<ManifestPackage>>(`${ROOT}/${segment(id)}/publish`, { note })),
  saveRevision: (id: string, expectedUpdatedAt: string) =>
    unwrap(
      api.post<ApiResponse<ManifestPackage>>(`${ROOT}/${segment(id)}/revisions`, {
        expectedUpdatedAt,
      }),
    ),
  revisions: (id: string) =>
    unwrap(api.get<ApiResponse<ManifestRevision[]>>(`${ROOT}/${segment(id)}/revisions`)),
  source: (id: string) =>
    unwrap(api.get<ApiResponse<ManifestSource>>(`${ROOT}/${segment(id)}/source`)),
  updateSource: (
    id: string,
    input: Pick<
      ManifestSource,
      | 'mode'
      | 'repositoryId'
      | 'refType'
      | 'refValue'
      | 'path'
      | 'includePatterns'
      | 'excludePatterns'
      | 'syncPolicy'
      | 'pollIntervalSeconds'
      | 'autoPublish'
      | 'autoDeploy'
    > & { expectedGeneration: number },
  ) => unwrap(api.put<ApiResponse<ManifestSource>>(`${ROOT}/${segment(id)}/source`, input)),
  bindings: (id: string) =>
    unwrap(api.get<ApiResponse<ManifestEnvironmentBinding[]>>(`${ROOT}/${segment(id)}/bindings`)),
  updateBinding: (id: string, input: ManifestEnvironmentBindingUpdateInput) =>
    unwrap(
      api.put<ApiResponse<ManifestEnvironmentBinding>>(
        `/delivery/manifest-bindings/${segment(id)}`,
        input,
      ),
    ),
  render: (id: string, bindingId: string, revision = 0) =>
    unwrap(
      api.post<ApiResponse<ManifestRenderResult>>(`${ROOT}/${segment(id)}/render`, {
        bindingId,
        ...(revision > 0 ? { revision } : {}),
      }),
    ),
  preflight: (id: string, bindingId: string, revision = 0) =>
    unwrap(
      api.post<ApiResponse<ManifestExecutionTask>>(`${ROOT}/${segment(id)}/preflight`, {
        bindingId,
        ...(revision > 0 ? { revision } : {}),
      }),
    ),
  sync: (id: string, expectedGeneration: number) =>
    unwrap(
      api.postWithHeaders<ApiResponse<{ run: ManifestSyncRun; task: ManifestExecutionTask }>>(
        `${ROOT}/${segment(id)}/sync`,
        { expectedGeneration },
        { 'Idempotency-Key': `manifest-web-${segment(id)}-${Date.now()}` },
      ),
    ),
  syncRuns: (id: string) =>
    unwrap(api.get<ApiResponse<ManifestSyncRun[]>>(`${ROOT}/${segment(id)}/sync-runs`)),
  deployments: async (packageId: string) => {
    const items: ManifestDeployment[] = []
    const pageSize = 100
    let page = 1
    let result: { items: ManifestDeployment[]; total: number; page: number; pageSize: number }
    do {
      result = await unwrap(
        api.get<ApiResponse<typeof result>>(
          `/delivery/manifest-deployments?packageId=${segment(packageId)}&page=${page}&pageSize=${pageSize}`,
        ),
      )
      items.push(...result.items)
      page += 1
    } while (result.items.length > 0 && items.length < result.total)
    return { ...result, items, page: 1 }
  },
  setDesiredRevision: (
    bindingId: string,
    input: { desiredRevision: number; expectedGeneration: number; reconcilePolicy: string },
  ) =>
    unwrap(
      api.post<ApiResponse<{ deployment: ManifestDeployment; task: ManifestExecutionTask }>>(
        `/delivery/manifest-bindings/${segment(bindingId)}/desired-revision`,
        input,
      ),
    ),
  deploymentAction: (
    id: string,
    action: 'reconcile' | 'repair' | 'adopt' | 'rollback',
    input: Record<string, unknown>,
  ) =>
    unwrap(
      api.post<ApiResponse<{ deployment: ManifestDeployment; task: ManifestExecutionTask }>>(
        `/delivery/manifest-deployments/${segment(id)}/${action}`,
        input,
      ),
    ),
  intents: (id: string) =>
    unwrap(
      api.get<ApiResponse<ManifestDeliveryIntent[]>>(`${ROOT}/${segment(id)}/delivery-intents`),
    ),
  decideIntent: (
    id: string,
    decision: 'accept' | 'reject',
    input: { expectedCurrentRevision: number; expectedPackageUpdatedAt: string; comment?: string },
  ) =>
    unwrap(
      api.post<ApiResponse<ManifestDeliveryIntent>>(
        `/delivery/manifest-delivery-intents/${segment(id)}/${decision}`,
        input,
      ),
    ),
}
