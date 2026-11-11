import { beforeEach, describe, expect, it, vi } from 'vitest'
import {
  decideResourceCreateScope,
  executeResourceCreate,
  generateWorkloadSnapshot,
  listAuthorizedNamespaces,
  preflightResourceCreate,
} from './api'

const apiMocks = vi.hoisted(() => ({
  get: vi.fn(),
  post: vi.fn(),
  postWithHeaders: vi.fn(),
}))

vi.mock('@/services/api-client', () => ({ api: apiMocks }))

describe('resource creation api', () => {
  beforeEach(() => vi.clearAllMocks())

  it('lists namespaces authorized for the current user', async () => {
    const namespaces = [
      { name: 'minio', status: 'Active', labels: {}, annotations: {} },
      { name: 'platform', status: 'Active', labels: {}, annotations: {} },
    ]
    apiMocks.get.mockResolvedValue({ data: namespaces })

    await expect(listAuthorizedNamespaces(' cluster/a ')).resolves.toEqual(namespaces)

    expect(apiMocks.get).toHaveBeenCalledWith('/clusters/cluster%2Fa/namespaces')
  })

  it('uses the cluster-scoped decision and preflight endpoints', async () => {
    apiMocks.post
      .mockResolvedValueOnce({ data: { allowed: true } })
      .mockResolvedValueOnce({ data: { ready: true, contentHash: 'hash', items: [] } })

    await decideResourceCreateScope('cluster/a', {
      namespace: 'minio',
      resourceGroup: 'configuration',
      apiVersion: 'v1',
      kind: 'ConfigMap',
      action: 'create',
    })
    await preflightResourceCreate('cluster/a', {
      source: 'list',
      defaultNamespace: 'minio',
      resourceGroup: 'configuration',
      expectedKind: 'ConfigMap',
      content: 'kind: ConfigMap',
    })

    expect(apiMocks.post).toHaveBeenNthCalledWith(
      1,
      '/clusters/cluster%2Fa/resource-creation/scope-decision',
      expect.objectContaining({ kind: 'ConfigMap', action: 'create' }),
    )
    expect(apiMocks.post).toHaveBeenNthCalledWith(
      2,
      '/clusters/cluster%2Fa/resource-creation/preflight',
      expect.objectContaining({ source: 'list', defaultNamespace: 'minio' }),
    )
  })

  it('generates a workload snapshot through the resource creation endpoint', async () => {
    const snapshot = {
      content: 'apiVersion: batch/v1\nkind: Job\n',
      sourceUid: 'deployment-uid',
      selectedContainer: 'app',
      containers: [{ name: 'app', image: 'registry.example.com/app:1.0' }],
      warnings: [],
    }
    apiMocks.post.mockResolvedValue({ data: snapshot })
    const request = {
      namespace: 'minio',
      sourceKind: 'Deployment' as const,
      sourceName: 'api',
      targetKind: 'Job' as const,
      targetName: 'api-maintenance',
      restartPolicy: 'Never' as const,
    }

    await expect(generateWorkloadSnapshot('cluster/a', request)).resolves.toEqual(snapshot)
    expect(apiMocks.post).toHaveBeenCalledWith(
      '/clusters/cluster%2Fa/resource-creation/workload-snapshot',
      request,
    )
  })

  it('sends execute idempotency as a header instead of expanding the request body', async () => {
    apiMocks.postWithHeaders.mockResolvedValue({
      data: { operationId: 'op-1', status: 'succeeded', contentHash: 'hash', items: [] },
    })
    const request = { source: 'global_yaml' as const, content: 'kind: ConfigMap' }

    await executeResourceCreate('cluster-a', request, 'request-123')

    expect(apiMocks.postWithHeaders).toHaveBeenCalledWith(
      '/clusters/cluster-a/resource-creation/execute',
      request,
      { 'Idempotency-Key': 'request-123' },
    )
  })
})
