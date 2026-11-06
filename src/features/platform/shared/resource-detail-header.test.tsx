/** @vitest-environment jsdom */
import { QueryClient, QueryClientProvider, useQuery } from '@tanstack/react-query'
import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { MemoryRouter, useLocation } from 'react-router-dom'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import {
  ResourceDetailHeader,
  ResourceDetailTabs,
  useResourceDetailTab,
} from './resource-detail-header'

vi.mock('@/i18n', () => ({ useI18n: () => ({ localeCode: 'zh_CN' }) }))
let root: ReturnType<typeof createRoot>
let container: HTMLDivElement
let client: QueryClient
const reads = {
  detail: vi.fn(async () => 'detail'),
  yaml: vi.fn(async () => 'yaml'),
  other: vi.fn(async () => 'other'),
}
function Detail(props: Partial<React.ComponentProps<typeof ResourceDetailHeader>>) {
  const [active, change] = useResourceDetailTab()
  const location = useLocation()
  useQuery({ queryKey: ['resource', 'cluster-a', 'api'], queryFn: reads.detail })
  useQuery({
    queryKey: ['resource', 'cluster-a', 'api', 'yaml'],
    queryFn: reads.yaml,
    enabled: active === 'yaml',
  })
  useQuery({ queryKey: ['resource', 'cluster-b', 'api'], queryFn: reads.other })
  return (
    <>
      <ResourceDetailHeader
        kind="ConfigMap"
        name="api"
        namespace="team-a"
        queryKey={['resource', 'cluster-a', 'api']}
        {...props}
      />
      <ResourceDetailTabs
        activeKey={active}
        onChange={change}
        items={[
          { key: 'overview', label: '概览', children: <p>overview content</p> },
          { key: 'yaml', label: 'YAML', children: <p>yaml content</p> },
        ]}
      />
      <output>{location.search}</output>
    </>
  )
}
beforeEach(() => {
  usePlatformScopeStore.setState({ clusterId: 'cluster-a', namespace: 'team-a' })
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  window.matchMedia = vi
    .fn()
    .mockReturnValue({ matches: false, addEventListener() {}, removeEventListener() {} })
  Object.values(reads).forEach((fn) => fn.mockClear())
  container = document.createElement('div')
  document.body.append(container)
  root = createRoot(container)
  client = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } })
})
afterEach(async () => {
  await act(async () => root.unmount())
  client.clear()
  container.remove()
  usePlatformScopeStore.setState({ clusterId: null, namespace: null })
  vi.unstubAllGlobals()
})
async function settle() {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 0))
  })
}
async function render(
  tab = 'overview',
  props: Partial<React.ComponentProps<typeof ResourceDetailHeader>> = {},
) {
  await act(async () =>
    root.render(
      <QueryClientProvider client={client}>
        <MemoryRouter
          initialEntries={[
            `/configuration/configmaps/api?clusterId=cluster-a&namespace=team-a&tab=${tab}&range=60&container=init&edit=true&webhook=hook`,
          ]}
        >
          <Detail {...props} />
        </MemoryRouter>
      </QueryClientProvider>,
    ),
  )
  await settle()
}
it('keeps scope in navigation and refreshes only active reads for this resource', async () => {
  await render()
  const back = container.querySelector<HTMLAnchorElement>('a[aria-label="返回ConfigMap列表"]')
  expect(back?.getAttribute('href')).toBe(
    '/configuration/configmaps?clusterId=cluster-a&namespace=team-a',
  )
  expect(reads.detail).toHaveBeenCalledOnce()
  expect(reads.yaml).not.toHaveBeenCalled()
  await act(async () =>
    container.querySelector<HTMLButtonElement>('button[aria-label="刷新 ConfigMap"]')?.click(),
  )
  await settle()
  expect(reads.detail).toHaveBeenCalledTimes(2)
  expect(reads.yaml).not.toHaveBeenCalled()
  expect(reads.other).toHaveBeenCalledOnce()
  await act(async () =>
    Array.from(container.querySelectorAll<HTMLElement>('[role="tab"]'))
      .find((tab) => tab.textContent === 'YAML')
      ?.click(),
  )
  await settle()
  expect(reads.yaml).toHaveBeenCalledOnce()
  expect(container.querySelector('output')?.textContent).toContain(
    'clusterId=cluster-a&namespace=team-a&tab=yaml&range=60',
  )
})
it('shows the readable overview for an unavailable URL tab without reading YAML', async () => {
  await render('removed-tab')
  expect(container.querySelector('[role="tab"][aria-selected="true"]')?.textContent).toBe('概览')
  expect(container.querySelector('[role="tabpanel"]')?.textContent).toBe('overview content')
  expect(reads.yaml).not.toHaveBeenCalled()
})

it.each([
  { selected: null, expected: '/configuration/configmaps?clusterId=cluster-a' },
  {
    selected: 'selected-team',
    expected: '/configuration/configmaps?clusterId=cluster-a&namespace=selected-team',
  },
])('returns to the selected list filter $selected', async ({ selected, expected }) => {
  usePlatformScopeStore.setState({ namespace: selected })
  await render()
  expect(container.querySelector('a[aria-label="返回ConfigMap列表"]')?.getAttribute('href')).toBe(
    expected,
  )
  expect(container.querySelector('.soha-k8s-detail-meta')?.textContent).toContain('team-a')
})

it('normalizes an explicit owner return query using the selected list filter', async () => {
  usePlatformScopeStore.setState({ namespace: 'selected-team' })
  await render('overview', {
    backTo: '/configuration/secrets?clusterId=owner&namespace=other&edit=true&sort=name',
  })
  expect(container.querySelector('a[aria-label="返回ConfigMap列表"]')?.getAttribute('href')).toBe(
    '/configuration/secrets?clusterId=owner&namespace=selected-team&sort=name',
  )
})

it('omits stale namespace context for cluster resources', async () => {
  await render('overview', { kind: 'Node', namespace: null, backTo: '/cluster-resources/nodes' })
  expect(container.querySelector('a[aria-label="返回Node列表"]')?.getAttribute('href')).toBe(
    '/cluster-resources/nodes?clusterId=cluster-a',
  )
})
