import { beforeEach, describe, expect, it, vi } from 'vitest'
import {
  applyNodeYAML,
  createNamespace,
  deleteNamespace,
  deleteNode,
  drainNode,
  getNodeDetail,
  getNodeYAML,
  listNamespaces,
  listNodes,
  setNodeSchedulability,
  updateNamespace,
  updateNode,
} from './api'

const apiMocks = vi.hoisted(() => ({
  delete: vi.fn(),
  get: vi.fn(),
  post: vi.fn(),
  put: vi.fn(),
}))

vi.mock('@/services/api-client', () => ({ api: apiMocks }))

const scope = { clusterId: 'cluster/a', namespace: null } as const

describe('cluster resources api', () => {
  beforeEach(() => vi.clearAllMocks())

  it('unwraps node list, detail, and YAML responses with encoded paths', async () => {
    apiMocks.get
      .mockResolvedValueOnce({ data: [{ name: 'node/a' }] })
      .mockResolvedValueOnce({ data: { name: 'node/a', labels: {} } })
      .mockResolvedValueOnce({ data: { kind: 'Node', name: 'node/a', content: 'kind: Node' } })

    await expect(listNodes(scope)).resolves.toEqual([{ name: 'node/a' }])
    await expect(getNodeDetail({ scope, name: 'node/a' })).resolves.toMatchObject({
      name: 'node/a',
    })
    await expect(getNodeYAML({ scope, name: 'node/a' })).resolves.toMatchObject({
      content: 'kind: Node',
    })

    expect(apiMocks.get.mock.calls.map(([path]) => path)).toEqual([
      '/clusters/cluster%2Fa/infrastructure/nodes',
      '/clusters/cluster%2Fa/infrastructure/nodes/node%2Fa/detail',
      '/clusters/cluster%2Fa/infrastructure/nodes/node%2Fa/yaml',
    ])
  })

  it('preserves node mutation payloads and unwraps their results', async () => {
    apiMocks.put
      .mockResolvedValueOnce({ data: { name: 'node/a', labels: { role: 'worker' } } })
      .mockResolvedValueOnce({ data: { kind: 'Node', name: 'node/a', content: 'updated' } })
    apiMocks.delete.mockResolvedValueOnce(undefined)
    apiMocks.post.mockResolvedValueOnce(undefined)

    await expect(
      updateNode({
        scope,
        name: 'node/a',
        input: { labels: { role: 'worker' }, taints: [] },
      }),
    ).resolves.toMatchObject({ name: 'node/a' })
    await expect(
      applyNodeYAML({ scope, name: 'node/a', content: 'updated' }),
    ).resolves.toMatchObject({ content: 'updated' })
    await expect(deleteNode({ scope, name: 'node/a' })).resolves.toBeUndefined()
    await expect(
      setNodeSchedulability({ scope, name: 'node/a', unschedulable: true }),
    ).resolves.toBeUndefined()
    await expect(
      drainNode({
        scope,
        name: 'node/a',
        force: false,
        deleteEmptyDirData: true,
        timeoutSeconds: 10,
      }),
    ).resolves.toBeUndefined()

    expect(apiMocks.put).toHaveBeenNthCalledWith(
      1,
      '/clusters/cluster%2Fa/infrastructure/nodes/node%2Fa',
      { labels: { role: 'worker' }, taints: [] },
    )
    expect(apiMocks.put).toHaveBeenNthCalledWith(
      2,
      '/clusters/cluster%2Fa/infrastructure/nodes/node%2Fa/yaml',
      { content: 'updated' },
    )
    expect(apiMocks.delete).toHaveBeenCalledWith(
      '/clusters/cluster%2Fa/infrastructure/nodes/node%2Fa',
    )
    expect(apiMocks.put).toHaveBeenNthCalledWith(
      3,
      '/clusters/cluster%2Fa/infrastructure/nodes/node%2Fa/schedulability',
      { unschedulable: true },
    )
    expect(apiMocks.post).toHaveBeenCalledWith(
      '/clusters/cluster%2Fa/infrastructure/nodes/node%2Fa/drain',
      { force: false, deleteEmptyDirData: true, timeoutSeconds: 10 },
    )
  })

  it('unwraps namespace CRUD while preserving the wire body', async () => {
    const input = { name: 'team/a', labels: { team: 'a' }, annotations: { owner: 'ops' } }
    apiMocks.get.mockResolvedValueOnce({ data: [input] })
    apiMocks.post.mockResolvedValueOnce({ data: input })
    apiMocks.put.mockResolvedValueOnce({ data: input })
    apiMocks.delete.mockResolvedValueOnce(undefined)

    await expect(listNamespaces(scope)).resolves.toEqual([input])
    await expect(createNamespace({ scope, input })).resolves.toEqual(input)
    await expect(updateNamespace({ scope, name: 'team/a', input })).resolves.toEqual(input)
    await expect(deleteNamespace({ scope, name: 'team/a' })).resolves.toBeUndefined()

    expect(apiMocks.post).toHaveBeenCalledWith('/clusters/cluster%2Fa/namespaces', input)
    expect(apiMocks.put).toHaveBeenCalledWith('/clusters/cluster%2Fa/namespaces/team%2Fa', input)
    expect(apiMocks.delete).toHaveBeenCalledWith('/clusters/cluster%2Fa/namespaces/team%2Fa')
  })
})
