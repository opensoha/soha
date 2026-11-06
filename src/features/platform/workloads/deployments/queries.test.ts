import { QueryClient } from '@tanstack/react-query'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { workloadKeys } from '@/features/platform/workloads/shared/keys'
import { deploymentQueries } from './queries'

const apiMocks = vi.hoisted(() => ({
  getWorkloadYAML: vi.fn(),
  getDeploymentDetail: vi.fn(),
  getDeploymentMetrics: vi.fn(),
  getDeploymentRolloutStatus: vi.fn(),
  listDeploymentEvents: vi.fn(),
  listDeploymentRollouts: vi.fn(),
  listDeployments: vi.fn(),
}))

vi.mock('./api', () => apiMocks)
vi.mock('../shared/api', () => ({ getWorkloadYAML: apiMocks.getWorkloadYAML }))

const scope = { clusterId: 'cluster-a', namespace: 'team-a' }

describe('deployment query options', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('uses canonical keys and enables queries only for valid scope', () => {
    expect(deploymentQueries.list(scope).queryKey).toEqual(workloadKeys.list('deployments', scope))
    expect(deploymentQueries.list({ clusterId: null, namespace: null }).enabled).toBe(false)
    expect(deploymentQueries.detail(scope, 'api').enabled).toBe(true)
    expect(
      deploymentQueries.detail({ clusterId: 'cluster-a', namespace: null }, 'api').enabled,
    ).toBe(false)
  })

  it('reads a retained revision from the exact ReplicaSet scope and isolates revision keys', async () => {
    const record = {
      name: 'api-rs',
      namespace: 'team-a',
      revision: '2',
      replicas: 1,
      readyReplicas: 1,
    }
    const options = deploymentQueries.revisionTemplate(scope, record)
    expect(options.enabled).toBe(true)
    expect(
      deploymentQueries.revisionTemplate({ ...scope, namespace: 'other' }, record).enabled,
    ).toBe(false)
    expect(
      deploymentQueries.revisionTemplate(scope, { ...record, revision: '3' }).queryKey,
    ).not.toEqual(options.queryKey)
    apiMocks.getWorkloadYAML.mockResolvedValueOnce({
      content:
        'kind: ReplicaSet\nmetadata:\n  name: api-rs\n  namespace: team-a\n  annotations:\n    deployment.kubernetes.io/revision: "2"\nspec:\n  template:\n    spec:\n      containers: []\n',
    })
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const result = await client.fetchQuery(options)
    expect(result.template).toContain('containers: []')
    expect(apiMocks.getWorkloadYAML).toHaveBeenCalledWith('replicasets', scope, 'api-rs')
  })

  it('keeps rollout keys and query functions together', async () => {
    apiMocks.getDeploymentRolloutStatus.mockResolvedValueOnce({ status: 'ready' })
    apiMocks.listDeploymentRollouts.mockResolvedValueOnce([{ revision: '7' }])
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })

    await expect(
      queryClient.fetchQuery(deploymentQueries.rolloutStatus(scope, 'api')),
    ).resolves.toEqual({ status: 'ready' })
    await expect(queryClient.fetchQuery(deploymentQueries.rollouts(scope, 'api'))).resolves.toEqual(
      [{ revision: '7' }],
    )
    expect(apiMocks.getDeploymentRolloutStatus).toHaveBeenCalledWith({ scope, name: 'api' })
    expect(apiMocks.listDeploymentRollouts).toHaveBeenCalledWith({ scope, name: 'api' })
  })
})
