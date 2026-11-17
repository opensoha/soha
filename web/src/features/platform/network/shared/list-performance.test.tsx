/** @vitest-environment jsdom */
import { act } from 'react'
import { App } from 'antd'
import { createRoot } from 'react-dom/client'
import { QueryClient, QueryClientProvider, type UseQueryResult } from '@tanstack/react-query'
import { afterEach, beforeAll, expect, it, vi } from 'vitest'
import type { NetworkResourceRecord } from './types'
import { NetworkResourceListPage } from './list-page'

const state = vi.hoisted(() => ({
  table: {} as Record<string, unknown>,
  options: undefined as unknown,
}))
vi.mock('@/features/platform/shared/kubernetes-resource-table', () => ({
  KubernetesResourceTable: (props: Record<string, unknown>) => {
    state.table = props
    return null
  },
}))
vi.mock('@/features/platform/shared/use-kubernetes-resource-actions', async (original) => {
  const actual =
    await original<typeof import('@/features/platform/shared/use-kubernetes-resource-actions')>()
  return {
    ...actual,
    useKubernetesResourceActions: (
      options: Parameters<typeof actual.useKubernetesResourceActions<NetworkResourceRecord>>[0],
    ) => {
      state.options = options
      return actual.useKubernetesResourceActions(options)
    },
  }
})
vi.mock('@/features/platform/resource-creation/components/create-entry', () => ({
  CreateEntry: () => null,
}))
vi.mock('@/features/copilot', () => ({
  useAIPageContext: () => undefined,
  encodeAIContextForElement: () => 'context',
}))
vi.mock('@/stores/platform-scope-store', () => ({
  usePlatformScopeStore: () => ({ clusterId: 'cluster-a', namespace: 'team' }),
}))

let root: ReturnType<typeof createRoot>
let container: HTMLDivElement
const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
beforeAll(() => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  Object.defineProperty(window, 'matchMedia', {
    value: () => ({
      matches: false,
      addListener() {},
      removeListener() {},
      addEventListener() {},
      removeEventListener() {},
    }),
  })
})
afterEach(async () => {
  await act(async () => root?.unmount())
  container?.remove()
  client.clear()
})

it('retains table references on query refresh and re-evaluates row permissions, data and callbacks', async () => {
  container = document.createElement('div')
  document.body.append(container)
  root = createRoot(container)
  const rows: NetworkResourceRecord[] = Array.from({ length: 300 }, (_, i) => ({
    name: `service-${i}`,
    namespace: 'team',
    ageSeconds: i,
    allowedActions: ['update', 'delete'],
  }))
  const columns = [{ title: 'Name', dataIndex: 'name' }]
  const searchValues = (record: NetworkResourceRecord) => [record.name, record.namespace]
  const edit = vi.fn(),
    nextEdit = vi.fn()
  async function render(data = rows, onEdit = edit, deletable = true, isFetching = false) {
    const query = {
      data,
      isEnabled: true,
      isLoading: false,
      isFetching,
      error: null,
      refetch: vi.fn(),
      isPlaceholderData: false,
    } as unknown as UseQueryResult<NetworkResourceRecord[], Error>
    await act(async () => {
      root.render(
        <App>
          <QueryClientProvider client={client}>
            <NetworkResourceListPage
              kind="services"
              columns={columns}
              query={query}
              onEdit={onEdit}
              deletable={deletable}
              buildAIPageContext={() => ({
                sourceWorkbench: 'platform',
                sourceRoute: '/network/services',
              })}
              emptyDescription={{ zh_CN: 'Empty', en_US: 'Empty' }}
              noMatchDescription={{ zh_CN: 'None', en_US: 'None' }}
              searchPlaceholder={{ zh_CN: 'Search', en_US: 'Search' }}
              searchValues={searchValues}
              refreshControl={<span />}
            />
          </QueryClientProvider>
        </App>,
      )
    })
  }
  const options = () =>
    state.options as {
      actions: (record: NetworkResourceRecord) => Array<{ key: string; run: () => unknown }>
    }
  await render()
  const initial = { ...state.table },
    actions = options().actions
  expect(initial.allRows).toBe(rows)
  expect((initial.dataSource as unknown[]).length).toBe(300)
  await render(rows, edit, true, true)
  expect(state.table.columns).toBe(initial.columns)
  expect(state.table.onRow).toBe(initial.onRow)
  expect(state.table.empty).toBe(initial.empty)
  expect(state.table.dataSource).toBe(initial.dataSource)
  expect(options().actions).toBe(actions)
  await render(rows, nextEdit, false)
  expect(
    options()
      .actions(rows[0])
      .map((action) => action.key),
  ).toEqual(['edit'])
  options().actions(rows[0])[0].run()
  expect(nextEdit).toHaveBeenCalledWith(rows[0])
  expect(edit).not.toHaveBeenCalled()
  const changed = [{ ...rows[0], allowedActions: ['view'] }]
  await render(changed)
  expect(state.table.allRows).toBe(changed)
  expect(state.table.columns).toBe(columns)
  expect(options().actions(changed[0])).toEqual([])
})
