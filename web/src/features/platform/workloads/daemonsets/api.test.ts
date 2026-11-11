import { beforeEach, describe, expect, it, vi } from 'vitest'
import {
  deleteDaemonSet,
  getDaemonSetDetail,
  getDaemonSetMetrics,
  listDaemonSetEvents,
  listDaemonSets,
  restartDaemonSet,
  listDaemonSetRollouts,
  getDaemonSetRevision,
} from './api'

const apiMocks = vi.hoisted(() => ({
  delete: vi.fn(),
  get: vi.fn(),
  post: vi.fn(),
}))

vi.mock('@/services/api-client', () => ({ api: apiMocks }))

const scope = { clusterId: 'cluster-a', namespace: 'team/a' }
const target = { scope, name: 'node/agent' }

describe('daemonset api', () => {
  beforeEach(() => vi.clearAllMocks())

  it('unwraps list, detail, and metrics endpoints', async () => {
    apiMocks.get
      .mockResolvedValueOnce({ data: [{ name: 'node/agent' }] })
      .mockResolvedValueOnce({
        data: { name: 'node/agent', namespace: 'team/a', pods: [{ name: 'agent-1' }] },
      })
      .mockResolvedValueOnce({ data: { resourceKind: 'DaemonSet' } })

    await expect(listDaemonSets(scope)).resolves.toEqual([{ name: 'node/agent' }])
    await expect(getDaemonSetDetail(target)).resolves.toMatchObject({
      name: 'node/agent',
      pods: [{ name: 'agent-1' }],
    })
    await expect(getDaemonSetMetrics(target)).resolves.toMatchObject({ resourceKind: 'DaemonSet' })

    expect(apiMocks.get.mock.calls[0][0]).toBe(
      '/clusters/cluster-a/workloads/daemonsets?namespace=team%2Fa',
    )
    expect(apiMocks.get.mock.calls[1][0]).toContain('/daemonsets/node%2Fagent/detail?')
    expect(apiMocks.get.mock.calls[2][0]).toContain('/daemonsets/node%2Fagent/metrics?')
  })

  it('filters shared events without loading a Pod list', async () => {
    apiMocks.get.mockResolvedValueOnce({
      data: [
        { involvedKind: 'DaemonSet', involvedName: 'node/agent' },
        { involvedKind: 'Pod', involvedName: 'node/agent' },
      ],
    })

    await expect(listDaemonSetEvents(target)).resolves.toEqual([
      { involvedKind: 'DaemonSet', involvedName: 'node/agent' },
    ])
    expect(apiMocks.get.mock.calls[0][0]).toContain('/events?namespace=team%2Fa&limit=100')
    expect(apiMocks.get).toHaveBeenCalledTimes(1)
  })

  it('preserves action bodies and encoded delete paths', async () => {
    apiMocks.post.mockResolvedValue({ data: null })
    apiMocks.delete.mockResolvedValue({ data: null })

    await expect(restartDaemonSet(target)).resolves.toBeUndefined()
    await expect(deleteDaemonSet(target)).resolves.toBeUndefined()

    expect(apiMocks.post).toHaveBeenCalledWith('/clusters/cluster-a/workloads/daemonsets/restart', {
      namespace: 'team/a',
      name: 'node/agent',
    })
    expect(apiMocks.delete).toHaveBeenCalledWith(
      '/clusters/cluster-a/workloads/daemonsets/node%2Fagent?namespace=team%2Fa',
    )
  })

  it('requires a namespace for actions', async () => {
    await expect(
      restartDaemonSet({ scope: { clusterId: 'cluster-a', namespace: null }, name: 'agent' }),
    ).rejects.toThrow('A namespace is required')
    expect(apiMocks.post).not.toHaveBeenCalled()
  })
  it('encodes retained revision paths and keeps native matching metadata', async () => {
    const record = {
      name: 'node/agent-abc',
      namespace: 'team/a',
      revision: '7',
      replicas: 1,
      readyReplicas: 1,
      podRevisionHash: 'abc',
      matchesCurrentTemplate: false,
    }
    apiMocks.get.mockResolvedValueOnce({ data: [record] })
    await expect(listDaemonSetRollouts(target)).resolves.toEqual([record])
    expect(apiMocks.get.mock.calls[0][0]).toBe(
      '/clusters/cluster-a/workloads/daemonsets/node%2Fagent/rollouts?namespace=team%2Fa',
    )
    apiMocks.get.mockResolvedValueOnce({
      data: {
        content:
          'kind: ControllerRevision\nmetadata: {name: node/agent-abc, namespace: team/a}\nrevision: 7\ndata: {spec: {template: {spec: {containers: [{name: agent, image: agent:v2}]}}}}',
      },
    })
    await expect(getDaemonSetRevision(target, record)).resolves.toMatchObject({
      template: expect.stringContaining('agent:v2'),
    })
    expect(apiMocks.get.mock.calls[1][0]).toBe(
      '/clusters/cluster-a/workloads/daemonsets/node%2Fagent/rollouts/node%2Fagent-abc/yaml?namespace=team%2Fa',
    )
  })
})
