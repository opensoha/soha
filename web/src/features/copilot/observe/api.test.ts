import { beforeEach, describe, expect, it, vi } from 'vitest'
import { observeApi } from './api'

const apiMocks = vi.hoisted(() => ({
  delete: vi.fn(),
  get: vi.fn(),
  patch: vi.fn(),
  post: vi.fn(),
  put: vi.fn(),
}))

vi.mock('@/services/api-client', () => ({ api: apiMocks }))

describe('observeApi', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    apiMocks.delete.mockResolvedValue(undefined)
    apiMocks.get.mockResolvedValue({ data: [] })
    apiMocks.patch.mockResolvedValue({ data: { id: 'session-1' } })
    apiMocks.post.mockResolvedValue({ data: { id: 'created-1' } })
    apiMocks.put.mockResolvedValue({ data: { id: 'updated-1' } })
  })

  it('keeps overview and operation read paths and unwraps responses', async () => {
    await expect(observeApi.overview.sessions()).resolves.toEqual([])
    await observeApi.overview.insights()
    await observeApi.overview.analysisRuns()
    await observeApi.overview.inspectionRuns()
    await observeApi.operations.tasks()
    await observeApi.operations.runs()
    await observeApi.operations.policies()
    await observeApi.operations.catalog()
    await observeApi.tools.catalog()
    await observeApi.tools.session('session/1')

    expect(apiMocks.get.mock.calls.map(([path]) => path)).toEqual([
      '/copilot/sessions',
      '/copilot/insights',
      '/copilot/analysis/runs',
      '/copilot/inspection-runs',
      '/copilot/inspection-tasks',
      '/copilot/inspection-runs',
      '/copilot/automation-policies',
      '/copilot/workbench/catalog',
      '/copilot/workbench/catalog',
      '/copilot/sessions/session/1',
    ])
  })

  it('pins manual capability executions to the original revision and key', async () => {
    await observeApi.operations.executeTask({
      taskId: 'inspection/1',
      idempotencyKey: 'same key&1',
      expectedRevision: 7,
    })
    expect(apiMocks.post).toHaveBeenCalledWith(
      '/copilot/inspection-tasks/inspection%2F1/execute?idempotencyKey=same+key%261&expectedRevision=7',
    )
  })

  it('preserves operation and tool mutation wire shapes', async () => {
    await observeApi.operations.createSession('run-1')
    await observeApi.operations.createTask({ title: '巡检' })
    await observeApi.operations.updateTask({ taskId: 'task-1', values: { enabled: true } })
    await observeApi.operations.deleteTask('task-1')
    await observeApi.operations.createPolicy({ name: '策略' })
    await observeApi.operations.updatePolicy({
      policyId: 'policy-1',
      values: { enabled: false },
    })
    await observeApi.operations.deletePolicy('policy-1')
    await observeApi.operations.executeTask('task-1')
    await observeApi.tools.patchSession({
      sessionId: 'session-1',
      body: { toolset: { enabledAdapterIds: ['platform-native.v1'] } },
    })

    expect(apiMocks.post).toHaveBeenNthCalledWith(1, '/copilot/inspection-runs/run-1/session')
    expect(apiMocks.post).toHaveBeenNthCalledWith(2, '/copilot/inspection-tasks', {
      title: '巡检',
    })
    expect(apiMocks.put).toHaveBeenNthCalledWith(1, '/copilot/inspection-tasks/task-1', {
      enabled: true,
    })
    expect(apiMocks.delete).toHaveBeenCalledWith('/copilot/inspection-tasks/task-1')
    expect(apiMocks.post).toHaveBeenNthCalledWith(3, '/copilot/automation-policies', {
      name: '策略',
    })
    expect(apiMocks.put).toHaveBeenNthCalledWith(2, '/copilot/automation-policies/policy-1', {
      enabled: false,
    })
    expect(apiMocks.delete).toHaveBeenCalledWith('/copilot/automation-policies/policy-1')
    expect(apiMocks.post).toHaveBeenNthCalledWith(4, '/copilot/inspection-tasks/task-1/execute')
    expect(apiMocks.patch).toHaveBeenCalledWith('/copilot/sessions/session-1', {
      toolset: { enabledAdapterIds: ['platform-native.v1'] },
    })
  })
})
