import type { AdminTableProps } from '@/components/admin-table'
import { ReadFeedback } from '@/components/request-feedback'
import type { AuditLog } from '@/features/system'
import { ApiError } from '@/services/api-error'
import type { ReactNode } from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import { beforeEach, expect, it, vi } from 'vitest'
import { IdentityLoginRecordsPage } from './page'

const state = vi.hoisted(() => ({
  permissions: [] as string[],
  enabled: [] as boolean[],
  error: null as Error | null,
  records: [] as AuditLog[],
}))
vi.mock('@/components/management-data-page', () => ({
  ManagementDataPage: ({ table, tableNode }: { table?: AdminTableProps; tableNode?: ReactNode }) =>
    tableNode ?? (
      <div>
        {table ? (
          <ReadFeedback
            label="登录记录"
            query={{
              data: table.hasReadData ? table.dataSource : undefined,
              error: table.error ?? null,
              isError: Boolean(table.error),
              isFetching: false,
              refetch: table.onRetry,
            }}
          />
        ) : null}
        {table?.dataSource.map((record, index) => (
          <div key={record.id}>
            {table.columns.map((column) => (
              <span key={column.dataIndex}>
                {column.render
                  ? column.render(record[column.dataIndex], record, index)
                  : record[column.dataIndex]}
              </span>
            ))}
          </div>
        ))}
      </div>
    ),
}))
vi.mock('@/features/auth', () => ({
  usePermissionSnapshot: () => ({ data: { data: {} } }),
  hasPermission: (_snapshot: unknown, permission: string) => state.permissions.includes(permission),
}))
vi.mock('@/i18n', async (original) => {
  const { zhCN } = await import('@/i18n/locales/zh_CN')
  return {
    ...(await original<typeof import('@/i18n')>()),
    useI18n: () => ({
      localeCode: 'zh_CN',
      t: (key: string, fallback = key) => zhCN[key] || fallback,
    }),
  }
})
vi.mock('@tanstack/react-query', async (original) => ({
  ...(await original<typeof import('@tanstack/react-query')>()),
  useQuery: (options: { queryKey: string[]; enabled: boolean }) => {
    state.enabled.push(options.enabled)
    return {
      data:
        options.queryKey[0] === 'audit-logs' ? state.records : [{ id: 'app-1', name: 'Docs App' }],
      error: options.queryKey[0] === 'audit-logs' ? state.error : null,
      isPending: false,
      isError: false,
      isFetching: false,
      refetch: vi.fn(),
    }
  },
}))

beforeEach(() => {
  state.permissions = ['identity.audit.view', 'identity.applications.view']
  state.enabled = []
  state.error = null
  const record: AuditLog = {
    id: 'success',
    action: 'oidc.authorize',
    result: 'success',
    actorId: 'alice',
    actorName: 'Alice',
    createdAt: '2026-09-14T09:00:00Z',
    resourceKind: 'IdentityProvider',
    resourceName: 'provider-1',
    summary: '',
    metadata: { applicationId: 'app-1' },
  }
  state.records = [
    record,
    {
      ...record,
      id: 'denied',
      action: 'proxy.login',
      result: 'denied',
      actorId: 'system',
      actorName: '',
      metadata: { applicationId: 'app-1', reason: 'authentication required' },
    },
  ]
})

it('shows the user, application, authentication stage and actual success/failure result', () => {
  const html = renderToStaticMarkup(<IdentityLoginRecordsPage />)
  for (const text of [
    'Alice',
    'Docs App',
    'OIDC 登录授权',
    '成功',
    '失败',
    '未识别用户',
    '需要登录',
  ])
    expect(html).toContain(text)
  expect(state.enabled).toEqual([true, true])
})

it('does not request or expose cached records without audit permission', () => {
  state.permissions = ['identity.applications.view']
  const html = renderToStaticMarkup(<IdentityLoginRecordsPage />)
  expect(html).toContain('没有查看应用登录记录的权限')
  expect(html).not.toContain('Alice')
  expect(state.enabled).toEqual([false, false])
})

it('falls back to application IDs when the audit reader cannot view applications', () => {
  state.permissions = ['system.audit.view']
  const html = renderToStaticMarkup(<IdentityLoginRecordsPage />)
  expect(html).toContain('app-1')
  expect(html).not.toContain('Docs App')
  expect(state.enabled).toEqual([true, false])
})

it('labels stale cached audit rows and keeps them readable after a transient failure', () => {
  state.error = new Error('audit unavailable')
  const html = renderToStaticMarkup(<IdentityLoginRecordsPage />)
  expect(html).toContain('未更新，正在展示上次读取结果')
  expect(html.replace(/\s/g, '')).toContain('重试')
  expect(html).toContain('Alice')
})

it('removes cached audit records after a real permission refusal', () => {
  state.error = new ApiError(403, 'audit denied', { method: 'GET', path: '/audit-logs' })
  const html = renderToStaticMarkup(<IdentityLoginRecordsPage />)
  expect(html).toContain('无访问权限')
  expect(html).not.toContain('Alice')
})
