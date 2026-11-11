import { renderToStaticMarkup } from 'react-dom/server'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, expect, it, vi } from 'vitest'
import { ApplicationAuditPanel } from './application-audit-panel'

const state = vi.hoisted(() => ({
  permission: false,
  queries: [] as Array<{ enabled: boolean; queryKey: unknown[] }>,
}))
vi.mock('@/features/auth', () => ({
  usePermissionSnapshot: () => ({ data: { data: {} } }),
  hasPermission: () => state.permission,
}))
vi.mock('@tanstack/react-query', async (original) => ({
  ...(await original<typeof import('@tanstack/react-query')>()),
  useQueries: ({ queries }: { queries: typeof state.queries }) => {
    state.queries = queries
    return queries.map((_, index) => ({
      data: [
        {
          id: index === 2 ? 'new' : 'old',
          action: index === 2 ? 'oidc.token' : 'proxy.allow',
          createdAt: index === 2 ? '2026-09-13T01:00:00Z' : '2026-09-13T00:00:00Z',
          actorName: 'Alice',
          result: 'success',
          summary: 'Allowed',
        },
      ],
      isError: false,
      isPending: false,
    }))
  },
}))
beforeEach(() => {
  state.permission = false
  state.queries = []
})
const render = () =>
  renderToStaticMarkup(
    <MemoryRouter>
      <ApplicationAuditPanel id="app-1" providerId="provider-1" />
    </MemoryRouter>,
  )
it('never requests or exposes cached records without audit permission', () => {
  expect(render()).not.toContain('Alice')
  expect(state.queries.every((query) => query.enabled === false)).toBe(true)
})
it('queries current and legacy associations and deduplicates records in time order', () => {
  state.permission = true
  const html = render()
  expect(state.queries.every((query) => query.enabled)).toBe(true)
  expect(JSON.stringify(state.queries.map((query) => query.queryKey))).toContain(
    'event.applicationId',
  )
  expect(JSON.stringify(state.queries.map((query) => query.queryKey))).toContain('provider-1')
  expect(html.match(/proxy.allow/g)).toHaveLength(1)
  expect(html.indexOf('oidc.token')).toBeLessThan(html.indexOf('proxy.allow'))
})
