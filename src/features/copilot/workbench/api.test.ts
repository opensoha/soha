import { beforeEach, describe, expect, it, vi } from 'vitest'
import { Blob as NodeBlob } from 'node:buffer'
import { webcrypto } from 'node:crypto'

const apiMocks = vi.hoisted(() => ({
  get: vi.fn(),
  post: vi.fn(),
  put: vi.fn(),
  patch: vi.fn(),
  delete: vi.fn(),
  postResponse: vi.fn(),
}))

vi.mock('@/services/api-client', () => ({ api: apiMocks }))

import { workbenchApi } from './api'

describe('workbenchApi', () => {
  it('selects a profile through its session endpoint and verifies the saved selection', async () => {
    apiMocks.put.mockResolvedValueOnce({ data: { id: 'session/1', metadata: { runtimeProfileId: 'ops' } } })
    await workbenchApi.runtimeProfiles.select({ sessionId: 'session/1', profileId: 'ops' })
    expect(apiMocks.put).toHaveBeenCalledWith('/copilot/sessions/session%2F1/runtime-profile', { profileId: 'ops' })
    apiMocks.put.mockResolvedValueOnce({ data: { id: 'session/1', metadata: {} } })
    await workbenchApi.runtimeProfiles.select({ sessionId: 'session/1', profileId: '' })
    apiMocks.put.mockResolvedValueOnce({ data: { id: 'other', metadata: {} } })
    await expect(workbenchApi.runtimeProfiles.select({ sessionId: 'session/1', profileId: '' })).rejects.toThrow('不匹配')
  })
  it('recovers shared workspace state with GET and rejects another assistant', async () => {
    apiMocks.get.mockResolvedValueOnce({ data: { assistantId: 'mine', environmentVersion: 9, latest: { id: 'original' } } })
    await workbenchApi.assistant.workspaceRuntime('mine')
    expect(apiMocks.get).toHaveBeenCalledWith('/copilot/assistant/environment/runtime')
    expect(apiMocks.post).not.toHaveBeenCalled()
    apiMocks.get.mockResolvedValueOnce({ data: { assistantId: 'other' } })
    await expect(workbenchApi.assistant.workspaceRuntime('mine')).rejects.toThrow('助手不匹配')
    apiMocks.get.mockResolvedValueOnce({ data: { assistantId: 'mine', id: 'different' } })
    await expect(workbenchApi.assistant.runtimeReceipt('mine', 'original')).rejects.toThrow('原运行回执不匹配')
  })
  it('uses dedicated activation and personal-session endpoints without changing ordinary chat', async () => {
    await workbenchApi.personalAgent.access()
    await workbenchApi.personalAgent.request()
    await workbenchApi.personalAgent.sessions()
    await workbenchApi.personalAgent.decide({ userId: 'user / 1', status: 'approved', version: 3 })
    expect(apiMocks.get).toHaveBeenCalledWith('/copilot/assistant/access')
    expect(apiMocks.get).toHaveBeenCalledWith('/copilot/assistant/sessions')
    expect(apiMocks.post).toHaveBeenCalledWith('/copilot/assistant/access', {})
    expect(apiMocks.patch).toHaveBeenCalledWith(
      '/copilot/assistant/access/requests/user%20%2F%201',
      { status: 'approved', version: 3 },
    )
    await workbenchApi.searchMessages('hello', '', true)
    await workbenchApi.searchMessages('hello')
    expect(apiMocks.get).toHaveBeenCalledWith('/copilot/messages/search?q=hello&workspace=heya')
    expect(apiMocks.get).toHaveBeenCalledWith('/copilot/messages/search?q=hello&workspace=chat')
  })
  it.each(['native', 'dsh'])('recovers %s by read and rejects foreign receipts', async (providerKind) => {
    apiMocks.get.mockResolvedValueOnce({
      data: { id: 'original', sessionId: 'session / 1', providerKind },
    })
    await workbenchApi.agentRuns.findRequest('session / 1', 'opaque+key/id')
    expect(apiMocks.get).toHaveBeenCalledExactlyOnceWith(
      '/copilot/sessions/session%20%2F%201/native-request?requestId=opaque%2Bkey%2Fid',
    )
    expect(apiMocks.post).not.toHaveBeenCalled()
    apiMocks.get.mockResolvedValueOnce({ data: { sessionId: 'foreign', providerKind: 'native' } })
    await expect(workbenchApi.agentRuns.findRequest('session / 1', 'key')).rejects.toThrow('不匹配')
  })
  it('exports one exact version and rejects corrupted or foreign downloads', async () => {
    vi.stubGlobal('Blob', NodeBlob)
    vi.stubGlobal('crypto', webcrypto)
    const body = 'archive fixture'
    const digest = Array.from(
      new Uint8Array(await webcrypto.subtle.digest('SHA-256', new TextEncoder().encode(body))),
      (byte) => byte.toString(16).padStart(2, '0'),
    ).join('')
    const input = { assistantId: 'mine', environmentVersion: 3 }
    const signal = new AbortController().signal
    const response = (owner = 'mine', hash = digest) =>
      new Response(body, {
        headers: {
          'Content-Length': String(body.length),
          'X-Assistant-ID': owner,
          'X-Content-SHA256': hash,
        },
      })
    try {
      apiMocks.postResponse.mockResolvedValueOnce(response())
      const blob = await workbenchApi.assistant.exportEnvironmentData(input, signal)
      expect(await blob.text()).toBe(body)
      expect(apiMocks.postResponse).toHaveBeenCalledExactlyOnceWith(
        '/copilot/assistant/environment/export',
        input,
        signal,
      )
      apiMocks.postResponse.mockResolvedValueOnce(response('other'))
      await expect(workbenchApi.assistant.exportEnvironmentData(input, signal)).rejects.toThrow(
        '助手不匹配',
      )
      apiMocks.postResponse.mockResolvedValueOnce(response('mine', '0'.repeat(64)))
      await expect(workbenchApi.assistant.exportEnvironmentData(input, signal)).rejects.toThrow(
        '摘要不匹配',
      )
    } finally {
      vi.unstubAllGlobals()
    }
  })
  it('sends one confirmed data deletion and rejects a foreign receipt', async () => {
    const input = { assistantId: 'mine', environmentVersion: 3, confirmationName: 'Soha' }
    apiMocks.delete.mockResolvedValueOnce({ data: { assistantId: 'mine', state: 'deleted' } })
    await workbenchApi.assistant.deleteEnvironmentData(input)
    expect(apiMocks.delete).toHaveBeenCalledExactlyOnceWith('/copilot/assistant/environment', input)
    apiMocks.delete.mockResolvedValueOnce({ data: { assistantId: 'other', state: 'deleted' } })
    await expect(workbenchApi.assistant.deleteEnvironmentData(input)).rejects.toThrow(
      '回执与助手不匹配',
    )
  })
  it('posts only the selected lifecycle action and rejects foreign operation receipts', async () => {
    apiMocks.post.mockResolvedValueOnce({ data: { assistantId: 'mine', state: 'sleeping' } })
    await workbenchApi.assistant.changeEnvironment('mine', { action: 'sleep' })
    expect(apiMocks.post).toHaveBeenCalledWith('/copilot/assistant/environment', {
      action: 'sleep',
    })
    apiMocks.post.mockResolvedValueOnce({ data: { assistantId: 'other', state: 'ready' } })
    await expect(
      workbenchApi.assistant.changeEnvironment('mine', { action: 'wake', templateId: 'personal' }),
    ).rejects.toThrow('回执与助手不匹配')
    expect(apiMocks.post).toHaveBeenCalledTimes(2)
  })
  it('reads only the matching assistant environment and rejects foreign receipts', async () => {
    apiMocks.get.mockResolvedValueOnce({
      data: { assistantId: 'mine', state: 'unknown', version: 1, dataInitialized: false },
    })
    await expect(workbenchApi.assistant.environment('mine')).resolves.toMatchObject({
      data: { state: 'unknown' },
    })
    apiMocks.get.mockResolvedValueOnce({ data: { assistantId: 'other', state: 'ready' } })
    await expect(workbenchApi.assistant.environment('mine')).rejects.toThrow('回执与助手不匹配')
    expect(apiMocks.get).toHaveBeenCalledWith('/copilot/assistant/environment')
    expect(apiMocks.post).not.toHaveBeenCalled()
  })
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('uses the existing read endpoints', async () => {
    apiMocks.get.mockResolvedValue({ data: [] })
    await workbenchApi.sessions.all()
    await workbenchApi.sessions.detail('session-1')
    await workbenchApi.sessions.messages('session-1')
    await workbenchApi.catalog()
    await workbenchApi.agentRuns.all()
    await workbenchApi.agentRuns.session('session / 1')

    expect(apiMocks.get.mock.calls).toEqual([
      ['/copilot/sessions'],
      ['/copilot/sessions/session-1'],
      ['/copilot/sessions/session-1/messages'],
      ['/copilot/workbench/catalog'],
      ['/copilot/agent-runs'],
      ['/copilot/agent-runs?sessionId=session%20%2F%201'],
    ])
  })

  it('gets or creates the main chat without accepting a client owner or role', async () => {
    await workbenchApi.sessions.main('internal')
    expect(apiMocks.post).toHaveBeenCalledWith('/copilot/sessions/main', {
      agentProviderId: 'internal',
    })
  })

  it('preserves session mutation paths and payloads', async () => {
    const createInput = {
      title: '支付告警调查',
      mode: 'root_cause' as const,
      agentProviderId: 'internal',
      scope: { namespace: 'payments' },
      tags: [],
    }
    const patchBody = { mode: 'trace' }
    const inspectionBody = {
      title: '支付告警调查 巡检模板',
      scopeType: 'namespace',
      namespace: 'payments',
      checks: ['cluster_health'],
      enabled: true,
      intervalMinutes: 30,
      metadata: {},
    }

    await workbenchApi.sessions.create(createInput)
    await workbenchApi.sessions.patch({ sessionId: 'session-1', body: patchBody })
    await workbenchApi.sessions.archive('session-1')
    await workbenchApi.sessions.createInspectionTask({
      sessionId: 'session-1',
      body: inspectionBody,
    })

    expect(apiMocks.post).toHaveBeenNthCalledWith(1, '/copilot/sessions', createInput)
    expect(apiMocks.patch).toHaveBeenCalledWith('/copilot/sessions/session-1', patchBody)
    expect(apiMocks.delete).toHaveBeenCalledWith('/copilot/sessions/session-1')
    expect(apiMocks.post).toHaveBeenNthCalledWith(
      2,
      '/copilot/sessions/session-1/inspection-task',
      inspectionBody,
    )
  })
})
