import { describe, expect, it, vi } from 'vitest'
import { systemRoutes } from './routes'

const routePages = vi.hoisted(() => ({
  announcements: () => null,
  audit: () => null,
  menus: () => null,
  operationLogs: () => null,
  sessions: () => null,
}))

vi.mock('./sessions/page', () => ({ OnlineUsersPage: routePages.sessions }))
vi.mock('./announcements/page', () => ({ AnnouncementsPage: routePages.announcements }))
vi.mock('./menus/page', () => ({ MenusPage: routePages.menus }))
vi.mock('./audit/page', () => ({ AuditLogsPage: routePages.audit }))
vi.mock('./operation-logs/page', () => ({ OperationLogsPage: routePages.operationLogs }))

describe('System route manifest', () => {
  it('maps five canonical UI routes to leaves and removes the sessions compatibility route', async () => {
    type SystemRoute = (typeof systemRoutes)[number]
    type SystemPageRoute = Extract<SystemRoute, { readonly load: unknown }>
    const pageRoutes = systemRoutes.filter((route): route is SystemPageRoute => 'load' in route)
    const loaded = new Map(
      await Promise.all(
        pageRoutes.map(async (route) => [route.meta.path, (await route.load()).default] as const),
      ),
    )

    expect(pageRoutes).toHaveLength(5)
    expect(loaded.get('/system/online-users')).toBe(routePages.sessions)
    expect(loaded.get('/system/audit')).toBe(routePages.audit)
    expect(loaded.get('/system/announcements')).toBe(routePages.announcements)
    expect(loaded.get('/system/menus')).toBe(routePages.menus)
    expect(loaded.get('/system/operations')).toBe(routePages.operationLogs)

    const registeredPaths = new Set<string>(systemRoutes.map((route) => route.meta.path))
    expect(registeredPaths.has('/identity/sessions')).toBe(false)

    const identityAuditRoute = systemRoutes.find((route) => route.meta.path === '/identity/audit')
    expect(identityAuditRoute).toMatchObject({
      redirectTo: '/system/audit',
      meta: {
        navVisible: false,
        tabbar: false,
        permissionKey: 'system.audit.view',
      },
    })
  })

  it('uses canonical system permissions for online-user routes', () => {
    const permissions = Object.fromEntries(
      systemRoutes.map((route) => [
        route.meta.path,
        'permissionKey' in route.meta ? route.meta.permissionKey : undefined,
      ]),
    )

    expect(permissions['/system/online-users']).toBe('system.online-users.view')
    expect(permissions['/system/audit']).toBe('system.audit.view')
    expect(permissions['/identity/audit']).toBe('system.audit.view')
  })
})
