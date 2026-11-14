import { beforeEach, describe, expect, it, vi } from 'vitest'
import {
  createCluster,
  createAgentInstallation,
  deleteCluster,
  getClusterDetail,
  listClusterNodes,
  listClusters,
  updateCluster,
  getAgentUpgradeStatus,
  upgradeAgent,
} from './api'

const apiMocks = vi.hoisted(() => ({
  delete: vi.fn(),
  get: vi.fn(),
  post: vi.fn(),
  put: vi.fn(),
}))

vi.mock('@/services/api-client', () => ({ api: apiMocks }))

const scope = { clusterId: 'cluster/a', namespace: null }

describe('cluster api', () => {
  beforeEach(() => vi.clearAllMocks())

  it('unwraps list, detail, and node snapshot responses', async () => {
    apiMocks.get
      .mockResolvedValueOnce({ data: [{ id: 'cluster/a' }] })
      .mockResolvedValueOnce({ data: { summary: { id: 'cluster/a' } } })
      .mockResolvedValueOnce({ data: [{ name: 'node-a' }] })

    await expect(listClusters()).resolves.toEqual([{ id: 'cluster/a' }])
    await expect(getClusterDetail({ scope })).resolves.toMatchObject({
      summary: { id: 'cluster/a' },
    })
    await expect(listClusterNodes({ scope })).resolves.toEqual([{ name: 'node-a' }])

    expect(apiMocks.get).toHaveBeenNthCalledWith(1, '/clusters')
    expect(apiMocks.get).toHaveBeenNthCalledWith(2, '/clusters/cluster%2Fa/detail')
    expect(apiMocks.get).toHaveBeenNthCalledWith(3, '/clusters/cluster%2Fa/infrastructure/nodes')
  })

  it('preserves create and update payloads while unwrapping entities', async () => {
    const values = { name: 'demo', labels: { provider: 'gke' } }
    apiMocks.post.mockResolvedValueOnce({ data: { id: 'created' } })
    apiMocks.put.mockResolvedValueOnce({ data: { id: 'cluster/a' } })

    await expect(createCluster(values)).resolves.toEqual({ id: 'created' })
    await expect(updateCluster({ scope, values })).resolves.toEqual({ id: 'cluster/a' })
    expect(apiMocks.post).toHaveBeenCalledWith('/clusters', values)
    expect(apiMocks.put).toHaveBeenCalledWith('/clusters/cluster%2Fa', values)
  })

  it('creates an Agent installation for the selected cluster', async () => {
    apiMocks.post.mockResolvedValueOnce({
      data: { clusterId: 'cluster/a', command: 'kubectl apply -f https://soha.test/agent.yaml' },
    })

    await expect(createAgentInstallation({ scope })).resolves.toMatchObject({
      clusterId: 'cluster/a',
    })
    expect(apiMocks.post).toHaveBeenCalledWith('/clusters/cluster%2Fa/agent-installation')
  })

  it('reads and updates the Agent only in the selected cluster', async () => {
    apiMocks.get.mockResolvedValueOnce({ data: { version: 'v0.1.6', canUpgrade: false } })
    apiMocks.post.mockResolvedValueOnce({
      data: { targetImage: 'ghcr.io/opensoha/soha-agent:v0.1.7' },
    })
    await expect(getAgentUpgradeStatus({ scope })).resolves.toMatchObject({
      version: 'v0.1.6',
      canUpgrade: false,
    })
    await expect(upgradeAgent({ scope, input: { version: 'v0.1.7' } })).resolves.toMatchObject({
      targetImage: 'ghcr.io/opensoha/soha-agent:v0.1.7',
    })
    expect(apiMocks.get).toHaveBeenCalledWith('/clusters/cluster%2Fa/agent-upgrade')
    expect(apiMocks.post).toHaveBeenCalledWith('/clusters/cluster%2Fa/agent-upgrade', {
      version: 'v0.1.7',
    })
  })

  it('returns void for deletes and rejects an empty cluster scope', async () => {
    apiMocks.delete.mockResolvedValueOnce({ data: { deleted: true } })

    await expect(deleteCluster({ scope })).resolves.toBeUndefined()
    expect(apiMocks.delete).toHaveBeenCalledWith('/clusters/cluster%2Fa')
    await expect(getClusterDetail({ scope: { clusterId: null, namespace: null } })).rejects.toThrow(
      'A cluster is required',
    )
  })
})
