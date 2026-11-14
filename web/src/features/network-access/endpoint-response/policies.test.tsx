/** @vitest-environment jsdom */
import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { App } from 'antd'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { ApiError, isApiErrorPresented } from '@/services/api-error'
import { EndpointResponsePolicies } from './policies'
import EndpointResponsePage, { EndpointMaintenance, EndpointResponseEvents } from './page'
import { responsePolicyDefaults } from './policy-fields'

const permissionState = vi.hoisted(() => ({ keys: [] as string[] }))
vi.mock('@/features/auth', () => ({
  usePermissionSnapshot: () => ({
    data: { data: { permissionKeys: permissionState.keys } },
    isLoading: false,
    isError: false,
  }),
  hasPermission: (_snapshot: unknown, key: string) => permissionState.keys.includes(key),
}))

const mocks = vi.hoisted(() => ({
  listEndpointResponsePolicies: vi.fn(),
  saveEndpointResponsePolicy: vi.fn(),
  publishEndpointResponsePolicy: vi.fn(),
  rollbackEndpointResponsePolicy: vi.fn(),
  deleteEndpointResponsePolicy: vi.fn(),
  endpointResponseRevisions: vi.fn(),
  listEndpointResponseEvents: vi.fn(),
  endpointResponseEvent: vi.fn(),
  restoreEndpointResponse: vi.fn(),
  fillDraft: vi.fn<(values: Record<string, unknown>) => void>(),
  listEndpointMaintenance: vi.fn(),
  createEndpointMaintenance: vi.fn(),
  endEndpointMaintenance: vi.fn(),
}))
vi.mock('./api', async (original) => ({ ...(await original<typeof import('./api')>()), ...mocks }))
vi.mock('./policy-fields', async (original) => {
  const actual = await original<typeof import('./policy-fields')>()
  const { Form } = await import('antd')
  return {
    ...actual,
    EndpointResponsePolicyFields: (
      props: Parameters<typeof actual.EndpointResponsePolicyFields>[0],
    ) => {
      const form = Form.useFormInstance()
      mocks.fillDraft.mockImplementation((values) => form.setFieldsValue(values))
      return <actual.EndpointResponsePolicyFields {...props} />
    },
  }
})
const policy = {
  id: 'policy-1',
  revision: 3,
  publishedRevision: 2,
  configuration: { ...responsePolicyDefaults, name: 'Endpoint policy', siteIds: ['site-1'] },
  createdAt: '2026-10-14T00:00:00Z',
  updatedAt: '2026-10-14T00:00:00Z',
}
const roots: Root[] = []
beforeAll(() => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  vi.stubGlobal(
    'ResizeObserver',
    class {
      observe() {}
      unobserve() {}
      disconnect() {}
    },
  )
  Object.defineProperty(window, 'matchMedia', {
    writable: true,
    value: () => ({
      matches: false,
      addListener() {},
      removeListener() {},
      addEventListener() {},
      removeEventListener() {},
    }),
  })
  const original = window.getComputedStyle.bind(window)
  vi.spyOn(window, 'getComputedStyle').mockImplementation((element) => original(element))
})
beforeEach(() => {
  vi.clearAllMocks()
  mocks.listEndpointResponsePolicies.mockResolvedValue([policy])
  mocks.listEndpointMaintenance.mockResolvedValue([])
  mocks.endpointResponseRevisions.mockResolvedValue([
    {
      revision: 1,
      configuration: policy.configuration,
      createdAt: policy.createdAt,
      actorId: 'admin',
    },
  ])
})
afterEach(async () => {
  await act(async () => roots.splice(0).forEach((root) => root.unmount()))
  document.body.innerHTML = ''
})
async function settle(client: QueryClient) {
  for (let i = 0; i < 30; i++) {
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 0))
    })
    if (!client.isFetching() && !client.isMutating()) return
  }
}
async function render(
  keys: string[],
  view: 'policies' | 'maintenance' | 'events' | 'page' = 'policies',
) {
  permissionState.keys = keys
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  const node = document.createElement('div')
  document.body.appendChild(node)
  const root = createRoot(node)
  roots.push(root)
  await act(async () =>
    root.render(
      <App>
        <QueryClientProvider client={client}>
          {view === 'page' ? (
            <EndpointResponsePage />
          ) : view === 'events' ? (
            <EndpointResponseEvents can={(key) => keys.includes(key)} filters={{}} />
          ) : view === 'maintenance' ? (
            <EndpointMaintenance can={(key) => keys.includes(key)} filters={{}} />
          ) : (
            <EndpointResponsePolicies can={(key) => keys.includes(key)} filters={{}} />
          )}
        </QueryClientProvider>
      </App>,
    ),
  )
  await settle(client)
  return client
}
async function click(text: string) {
  const button = Array.from(document.querySelectorAll('button')).find(
    (item) => item.textContent?.replace(/\s/g, '') === text,
  )
  expect(button).toBeDefined()
  await act(async () => button?.dispatchEvent(new MouseEvent('click', { bubbles: true })))
}
async function confirm() {
  const button = document.querySelector<HTMLButtonElement>('.ant-popconfirm .ant-btn-primary')
  expect(button).not.toBeNull()
  await act(async () => button?.click())
}
function button(text: string) {
  return Array.from(document.querySelectorAll('button')).find(
    (item) => item.textContent?.replace(/\s/g, '') === text,
  )!
}
async function setValue(id: string, value: string) {
  const input = document.getElementById(id) as HTMLInputElement | HTMLTextAreaElement
  expect(input).not.toBeNull()
  await act(async () => {
    Object.getOwnPropertyDescriptor(
      input.tagName === 'TEXTAREA' ? HTMLTextAreaElement.prototype : HTMLInputElement.prototype,
      'value',
    )!.set!.call(input, value)
    input.dispatchEvent(new Event('input', { bubbles: true }))
  })
}
describe('endpoint response policies', () => {
  it('opens only maintenance for staff with the maintenance permission alone', async () => {
    await render(['network_access.endpoint_response_events.maintenance'], 'page')
    expect(document.body.textContent).toContain('维护例外')
    expect(document.body.textContent).not.toContain('联动规则')
    expect(document.body.textContent).not.toContain('会话处置')
    expect(mocks.listEndpointMaintenance).toHaveBeenCalled()
    expect(mocks.listEndpointResponsePolicies).not.toHaveBeenCalled()
    expect(mocks.listEndpointResponseEvents).not.toHaveBeenCalled()
  })

  it('keeps cached policies visible and marks a failed refresh', async () => {
    const client = await render(['network_access.endpoint_response_policies.view'])
    mocks.listEndpointResponsePolicies.mockRejectedValue(new Error('Core unavailable'))
    const refresh = document.querySelector('[aria-label="刷新策略"]')
    await act(async () => refresh?.dispatchEvent(new MouseEvent('click', { bubbles: true })))
    await settle(client)
    expect(document.body.textContent).toContain('Endpoint policy')
    expect(document.body.textContent).toContain('列表未更新，正在展示上次读取结果')
    expect(document.body.textContent).toContain('Core unavailable')
  }, 20_000)
  it.each([401, 403])('hides cached policies after a %i response', async (status) => {
    const client = await render(['network_access.endpoint_response_policies.view'])
    expect(document.body.textContent).toContain('Endpoint policy')
    const error = new ApiError(status, 'Policy access revoked', {
      method: 'GET',
      path: '/network-access/endpoint-response-policies',
      requestId: 'policy-access-request',
    })
    mocks.listEndpointResponsePolicies.mockRejectedValueOnce(error)
    const refresh = document.querySelector('[aria-label="刷新策略"]')
    await act(async () => refresh?.dispatchEvent(new MouseEvent('click', { bubbles: true })))
    await settle(client)
    expect(document.body.textContent).not.toContain('Endpoint policy')
    expect(document.body.textContent).toContain(status === 401 ? '登录状态失效' : '无访问权限')
    expect(document.body.textContent).toContain('policy-access-request')
    expect(isApiErrorPresented(error)).toBe(status !== 401)
    expect(mocks.publishEndpointResponsePolicy).not.toHaveBeenCalled()
  })
  it('retains an authorized empty result when a background read fails', async () => {
    mocks.listEndpointResponsePolicies.mockResolvedValueOnce([])
    const client = await render(['network_access.endpoint_response_policies.view'])
    mocks.listEndpointResponsePolicies.mockRejectedValueOnce(new Error('Core unavailable'))
    const refresh = document.querySelector('[aria-label="刷新策略"]')
    await act(async () => refresh?.dispatchEvent(new MouseEvent('click', { bubbles: true })))
    await settle(client)
    expect(document.querySelector('.ant-table')).not.toBeNull()
    expect(document.body.textContent).toContain('列表未更新，正在展示上次读取结果')
    expect(document.body.textContent).toContain('Core unavailable')
  })
  it('does not expose mutations for a read-only viewer', async () => {
    await render(['network_access.endpoint_response_policies.view'])
    expect(document.querySelector('[aria-label="编辑策略"]')).toBeNull()
    expect(document.body.textContent).not.toContain('新增规则')
    expect(document.body.textContent).not.toContain('删除')
    expect(mocks.publishEndpointResponsePolicy).not.toHaveBeenCalled()
  })
  it('publishes the reviewed revision and retains the failure reason', async () => {
    const error = new ApiError(409, 'scope changed', {
      method: 'POST',
      path: '/network-access/endpoint-response-policies/policy-1/publish',
      requestId: 'policy-publish-request',
    })
    mocks.publishEndpointResponsePolicy.mockRejectedValue(error)
    const client = await render([
      'network_access.endpoint_response_policies.view',
      'network_access.endpoint_response_policies.publish',
    ])
    await click('发布')
    await click('确认发布')
    await settle(client)
    expect(mocks.publishEndpointResponsePolicy.mock.calls[0][0]).toEqual({
      policyId: 'policy-1',
      expectedRevision: 3,
    })
    expect(document.body.textContent).toContain('scope changed')
    expect(document.body.textContent).toContain('发布联动规则：操作请求失败')
    expect(document.body.textContent).toContain('policy-publish-request')
    expect(isApiErrorPresented(error)).toBe(true)
  })
  it('keeps the draft editor open when saving fails and never publishes it', async () => {
    mocks.saveEndpointResponsePolicy.mockRejectedValue(new Error('revision conflict'))
    const client = await render([
      'network_access.endpoint_response_policies.view',
      'network_access.endpoint_response_policies.update',
    ])
    const edit = document.querySelector('[aria-label="编辑策略"]')
    await act(async () => edit?.dispatchEvent(new MouseEvent('click', { bubbles: true })))
    await click('保存草稿')
    await settle(client)
    expect(mocks.saveEndpointResponsePolicy.mock.calls[0][0]).toMatchObject({
      policyId: 'policy-1',
      input: { expectedRevision: 3, configuration: policy.configuration },
    })
    expect(document.querySelector('[role="dialog"]')).not.toBeNull()
    expect(document.body.textContent).toContain('revision conflict')
    expect(mocks.publishEndpointResponsePolicy).not.toHaveBeenCalled()
  })
  it('fences a create with an unknown outcome across refresh and editor reopen', async () => {
    mocks.saveEndpointResponsePolicy.mockRejectedValue(
      new ApiError(504, 'Response lost', {
        method: 'POST',
        path: '/network-access/endpoint-response-policies',
        requestId: 'unknown-draft',
      }),
    )
    const client = await render([
      'network_access.endpoint_response_policies.view',
      'network_access.endpoint_response_policies.create',
    ])
    await click('新增规则')
    await act(async () => mocks.fillDraft({ name: 'New policy', siteIds: ['site-1'] }))
    await click('保存草稿')
    await settle(client)
    expect(mocks.saveEndpointResponsePolicy.mock.calls[0][0]).toMatchObject({
      policyId: undefined,
      input: { expectedRevision: 0 },
    })
    expect(document.body.textContent).toContain('请求结果不明')
    expect(document.body.textContent).toContain('unknown-draft')
    const saveButton = Array.from(document.querySelectorAll('button')).find(
      (button) => button.textContent === '保存草稿',
    )!
    expect(saveButton.disabled).toBe(true)
    await click('保存草稿')
    await click('取消')
    await act(async () =>
      document
        .querySelector('[aria-label="刷新策略"]')
        ?.dispatchEvent(new MouseEvent('click', { bubbles: true })),
    )
    await settle(client)
    expect(document.body.textContent).toContain('unknown-draft')
    await click('新增规则')
    expect(mocks.saveEndpointResponsePolicy).toHaveBeenCalledTimes(1)
    expect(mocks.publishEndpointResponsePolicy).not.toHaveBeenCalled()
  })
  it('fences an uncertain maintenance create and keeps the request ID after closing', async () => {
    mocks.createEndpointMaintenance.mockRejectedValue(
      new ApiError(504, 'Response lost', {
        method: 'POST',
        path: '/network-access/endpoint-maintenance',
        requestId: 'unknown-maintenance',
      }),
    )
    const client = await render(
      ['network_access.endpoint_response_events.maintenance'],
      'maintenance',
    )
    await click('新增维护例外')
    for (const [id, value] of [
      ['deviceId', 'device-1'],
      ['siteId', 'site-1'],
      ['reason', 'Repair endpoint'],
    ]) {
      const input = document.getElementById(id) as HTMLInputElement | HTMLTextAreaElement
      await act(async () => {
        Object.getOwnPropertyDescriptor(
          input.tagName === 'TEXTAREA' ? HTMLTextAreaElement.prototype : HTMLInputElement.prototype,
          'value',
        )!.set!.call(input, value)
        input.dispatchEvent(new Event('input', { bubbles: true }))
      })
    }
    await click('创建')
    await settle(client)
    expect(mocks.createEndpointMaintenance).toHaveBeenCalledTimes(1)
    expect(document.body.textContent).toContain('请求结果不明')
    await click('创建')
    await click('取消')
    await act(async () =>
      document
        .querySelector('[aria-label="刷新维护例外"]')
        ?.dispatchEvent(new MouseEvent('click', { bubbles: true })),
    )
    await settle(client)
    expect(document.body.textContent).toContain('unknown-maintenance')
    await click('新增维护例外')
    expect(mocks.createEndpointMaintenance).toHaveBeenCalledTimes(1)
  })

  it.each(['publish', 'delete'] as const)(
    'fences an uncertain %s after a refresh',
    async (action) => {
      const mutation =
        action === 'publish'
          ? mocks.publishEndpointResponsePolicy
          : mocks.deleteEndpointResponsePolicy
      const label = action === 'publish' ? '发布' : '删除'
      mutation.mockRejectedValue(
        new ApiError(504, 'Response lost', {
          method: action === 'publish' ? 'POST' : 'DELETE',
          path: `/network-access/endpoint-response-policies/policy-1/${action}`,
          requestId: `unknown-${action}`,
        }),
      )
      const client = await render([
        'network_access.endpoint_response_policies.view',
        'network_access.endpoint_response_policies.publish',
        'network_access.endpoint_response_policies.delete',
        'network_access.endpoint_response_policies.create',
      ])
      await click(label)
      await confirm()
      await settle(client)
      expect(mutation).toHaveBeenCalledTimes(1)
      expect(mutation.mock.calls[0][0]).toEqual({ policyId: 'policy-1', expectedRevision: 3 })
      expect(document.body.textContent).toContain('请求结果不明')
      expect(document.body.textContent).toContain(`unknown-${action}`)
      expect(button('发布').disabled).toBe(true)
      expect(button('删除').disabled).toBe(true)
      expect(button('新增规则').disabled).toBe(true)
      await act(async () => document.querySelector<HTMLElement>('[aria-label="刷新策略"]')?.click())
      await settle(client)
      await click(label)
      expect(mutation).toHaveBeenCalledTimes(1)
      expect(document.body.textContent).toContain(`unknown-${action}`)
    },
  )

  it('retains an uncertain rollback receipt after closing its drawer', async () => {
    mocks.rollbackEndpointResponsePolicy.mockRejectedValue(
      new ApiError(504, 'Response lost', {
        method: 'POST',
        path: '/network-access/endpoint-response-policies/policy-1/rollback',
        requestId: 'unknown-rollback',
      }),
    )
    const client = await render([
      'network_access.endpoint_response_policies.view',
      'network_access.endpoint_response_policies.publish',
    ])
    await act(async () => document.querySelector<HTMLElement>('[aria-label="查看与预览"]')?.click())
    await settle(client)
    const history = Array.from(document.querySelectorAll<HTMLElement>('[role="tab"]')).find((tab) =>
      tab.textContent?.includes('发布历史'),
    )
    expect(history).toBeDefined()
    await act(async () => history?.click())
    await click('回滚发布')
    await confirm()
    await settle(client)
    expect(mocks.rollbackEndpointResponsePolicy).toHaveBeenCalledTimes(1)
    expect(mocks.rollbackEndpointResponsePolicy.mock.calls[0][0]).toEqual({
      policyId: 'policy-1',
      expectedRevision: 3,
      revision: 1,
    })
    expect(document.querySelector('.ant-drawer')?.textContent).toContain('unknown-rollback')
    expect(button('回滚发布').disabled).toBe(true)
    await act(async () => document.querySelector<HTMLElement>('.ant-drawer-close')?.click())
    await settle(client)
    expect(document.body.textContent).toContain('unknown-rollback')
    expect(button('发布').disabled).toBe(true)
  })

  it('fences an uncertain maintenance end without allowing a new exception', async () => {
    mocks.listEndpointMaintenance.mockResolvedValue([
      {
        id: 'window-1',
        deviceId: 'device-1',
        siteId: 'site-1',
        reason: 'Repair',
        expiresAt: '2026-10-26T23:00:00Z',
      },
    ])
    mocks.endEndpointMaintenance.mockRejectedValue(
      new ApiError(504, 'Response lost', {
        method: 'DELETE',
        path: '/network-access/endpoint-maintenance-windows/window-1',
        requestId: 'unknown-maintenance-end',
      }),
    )
    const client = await render(
      ['network_access.endpoint_response_events.maintenance'],
      'maintenance',
    )
    await click('结束')
    await confirm()
    await settle(client)
    expect(mocks.endEndpointMaintenance).toHaveBeenCalledTimes(1)
    expect(mocks.endEndpointMaintenance.mock.calls[0][0]).toBe('window-1')
    expect(button('结束').disabled).toBe(true)
    expect(button('新增维护例外').disabled).toBe(true)
    await act(async () =>
      document.querySelector<HTMLElement>('[aria-label="刷新维护例外"]')?.click(),
    )
    await settle(client)
    await click('结束')
    expect(mocks.endEndpointMaintenance).toHaveBeenCalledTimes(1)
    expect(document.body.textContent).toContain('unknown-maintenance-end')
    expect(mocks.createEndpointMaintenance).not.toHaveBeenCalled()
  })

  it('fences an uncertain recovery and retains its request ID outside both overlays', async () => {
    const event = {
      id: 'event-1',
      policyId: 'policy-1',
      policyRevision: 1,
      revision: 2,
      deviceId: 'device-1',
      subjectId: 'user-1',
      sessionId: 'session-1',
      nasId: 'nas-1',
      status: 'enforcement_unknown',
      signal: 'service_unavailable',
      action: 'restrict',
      stage: 0,
      reasonCode: 'nas_action_unknown',
      notificationState: 'not_requested',
      commandIds: [],
      restrictionEpoch: 1,
      triggeredAt: policy.createdAt,
      updatedAt: policy.updatedAt,
    }
    mocks.listEndpointResponseEvents.mockResolvedValue([event])
    mocks.endpointResponseEvent.mockResolvedValue(event)
    mocks.restoreEndpointResponse.mockRejectedValue(
      new ApiError(504, 'Response lost', {
        method: 'POST',
        path: '/network-access/endpoint-response-events/event-1/restore',
        requestId: 'unknown-restore',
      }),
    )
    const client = await render(
      [
        'network_access.endpoint_response_events.view',
        'network_access.endpoint_response_events.restore',
      ],
      'events',
    )
    await act(async () =>
      document.querySelector<HTMLElement>('[aria-label="查看会话处置"]')?.click(),
    )
    await settle(client)
    await click('申请受控恢复')
    await setValue('reason', 'Check current authorization')
    await click('重新评估并恢复')
    await settle(client)
    expect(mocks.restoreEndpointResponse).toHaveBeenCalledTimes(1)
    expect(mocks.restoreEndpointResponse.mock.calls[0][0]).toEqual({
      eventId: 'event-1',
      input: { expectedRevision: 2, reason: 'Check current authorization' },
    })
    expect(button('重新评估并恢复').disabled).toBe(true)
    expect(document.querySelector('.ant-modal')?.textContent).toContain('unknown-restore')
    await click('取消')
    await act(async () => document.querySelector<HTMLElement>('.ant-drawer-close')?.click())
    await settle(client)
    expect(document.body.textContent).toContain('unknown-restore')
    expect(document.body.textContent).toContain('请求结果不明')
    expect(document.body.textContent).not.toContain('恢复未完成，限制仍保留')
    await act(async () =>
      document.querySelector<HTMLElement>('[aria-label="查看会话处置"]')?.click(),
    )
    await settle(client)
    expect(button('申请受控恢复').disabled).toBe(true)
    expect(mocks.restoreEndpointResponse).toHaveBeenCalledTimes(1)
  })
})
