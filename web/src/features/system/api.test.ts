import { beforeEach, describe, expect, it, vi } from 'vitest'
import { resolveSystemEndpointScope, systemApi } from './api'

const apiMocks = vi.hoisted(() => ({
  delete: vi.fn(),
  get: vi.fn(),
  post: vi.fn(),
  put: vi.fn(),
}))

vi.mock('@/services/api-client', () => ({ api: apiMocks }))

describe('systemApi', () => {
  beforeEach(() => vi.clearAllMocks())

  it('uses the canonical online-user endpoint contract', async () => {
    const session = {
      id: 'session-1',
      userId: 'user-1',
      userName: 'operator',
      email: 'operator@example.test',
      providerType: 'oidc',
      status: 'active',
      createdAt: '2026-07-10T00:00:00Z',
      lastSeenAt: '2026-07-10T01:00:00Z',
      expiresAt: '2026-07-11T00:00:00Z',
      metadata: { source: 'console', sourceIp: '127.0.0.1', userAgent: 'browser' },
    }
    apiMocks.get.mockResolvedValue({ data: [session] })

    expect(resolveSystemEndpointScope('/identity/audit')).toBe('identity')
    expect(resolveSystemEndpointScope('/system/online-users')).toBe('system')
    expect(resolveSystemEndpointScope('/system/audit')).toBe('system')
    await expect(systemApi.sessions.list()).resolves.toEqual([
      expect.objectContaining({
        id: 'session-1',
        loginTime: session.createdAt,
        expiry: session.expiresAt,
        source: 'console',
      }),
    ])
    expect(apiMocks.get).toHaveBeenCalledWith('/auth/sessions')
  })

  it('uses the matching audit endpoint and serializes normalized filters', async () => {
    apiMocks.get.mockResolvedValue({ data: [] })

    await systemApi.audit.list('identity', {
      action: ' login ',
      clusterId: ' cluster-a ',
      limit: 8,
      namespace: ' prod ',
      requestMethod: ' POST ',
      requestPath: ' /api/v1/auth/login ',
      result: '',
      metadataKey: 'usageSnapshot.templateId',
      metadataValue: ' template/a ',
    })
    await systemApi.audit.list('system', { result: 'failure' })
    await systemApi.operationLogs.list({
      operationType: ' platform.resource.apply ',
      requestMethod: ' PATCH ',
      requestPath: ' /api/v1/resources/deployment ',
      metadataKey: '',
      metadataValue: ' workload/a ',
    })

    expect(apiMocks.get).toHaveBeenNthCalledWith(
      1,
      '/identity/audit/events?action=login&clusterId=cluster-a&limit=8&namespace=prod&requestMethod=POST&requestPath=%2Fapi%2Fv1%2Fauth%2Flogin&metadataKey=usageSnapshot.templateId&metadataValue=template%2Fa',
    )
    expect(apiMocks.get).toHaveBeenNthCalledWith(2, '/audit/logs?result=failure')
    expect(apiMocks.get).toHaveBeenNthCalledWith(
      3,
      '/operations/logs?operationType=platform.resource.apply&requestMethod=PATCH&requestPath=%2Fapi%2Fv1%2Fresources%2Fdeployment&metadataValue=workload%2Fa',
    )
  })

  it('uses server summaries for audit and operation totals', async () => {
    apiMocks.get.mockResolvedValueOnce({ data: { total: 12 } })
    apiMocks.get.mockResolvedValueOnce({ data: { total: 8, failureCount: 1 } })

    await expect(systemApi.audit.summary()).resolves.toEqual({ total: 12 })
    await expect(systemApi.operationLogs.summary()).resolves.toEqual({
      total: 8,
      failureCount: 1,
    })
    expect(apiMocks.get).toHaveBeenNthCalledWith(1, '/audit/summary')
    expect(apiMocks.get).toHaveBeenNthCalledWith(2, '/operations/summary')
  })

  it('scopes application authentication audits before applying the server limit', async () => {
    apiMocks.get.mockResolvedValue({ data: [] })
    await systemApi.audit.list('identity', {
      actionPrefixes: 'oidc.authorize,identity.saml.sso,proxy.deny',
      resourceKind: 'IdentityProvider',
      limit: 500,
      from: '2026-09-01T00:00:00.000Z',
      to: '2026-09-14T23:59:59.999Z',
    })
    const url = new URL(apiMocks.get.mock.calls[0][0], 'https://soha.test')
    expect(url.pathname).toBe('/identity/audit/events')
    expect(Object.fromEntries(url.searchParams)).toEqual({
      actionPrefixes: 'oidc.authorize,identity.saml.sso,proxy.deny',
      resourceKind: 'IdentityProvider',
      limit: '500',
      from: '2026-09-01T00:00:00.000Z',
      to: '2026-09-14T23:59:59.999Z',
    })
  })

  it('unwraps menu CRUD and encodes record identifiers', async () => {
    const menu = { id: 'menu/a', labelZh: '菜单 A' }
    apiMocks.post.mockResolvedValue({ data: menu })
    apiMocks.put.mockResolvedValue({ data: menu })
    apiMocks.delete.mockResolvedValue(undefined)

    await expect(systemApi.menus.create({ id: 'menu/a' })).resolves.toBe(menu)
    await expect(
      systemApi.menus.update({ id: 'menu/a', values: { labelZh: '菜单 A' } }),
    ).resolves.toBe(menu)
    await expect(systemApi.menus.remove('menu/a')).resolves.toBeUndefined()

    expect(apiMocks.post).toHaveBeenCalledWith('/menus', { id: 'menu/a' })
    expect(apiMocks.put).toHaveBeenCalledWith('/menus/menu%2Fa', { labelZh: '菜单 A' })
    expect(apiMocks.delete).toHaveBeenCalledWith('/menus/menu%2Fa')
  })

  it('lists announcement receipts with encoded filters', async () => {
    const page = {
      items: [],
      total: 2,
      page: 1,
      pageSize: 15,
      readCount: 1,
      unreadCount: 1,
    }
    apiMocks.get.mockResolvedValue({ data: page })

    await expect(
      systemApi.announcements.receipts('announcement/a', {
        keyword: ' Ada ',
        state: 'unread',
        page: 2,
        pageSize: 15,
      }),
    ).resolves.toBe(page)
    expect(apiMocks.get).toHaveBeenCalledWith(
      '/announcements/announcement%2Fa/receipts?keyword=Ada&state=unread&page=2&pageSize=15',
    )
  })
})
