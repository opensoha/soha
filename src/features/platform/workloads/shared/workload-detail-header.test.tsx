/** @vitest-environment jsdom */
import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { Link, MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import { WorkloadDetailHeader } from './workload-detail-header'

vi.mock('@/i18n', () => ({ useI18n: () => ({ localeCode: 'zh_CN' }) }))
let root: ReturnType<typeof createRoot>
let container: HTMLDivElement
beforeEach(() => {
  usePlatformScopeStore.setState({ clusterId: 'a', namespace: 'apps' })
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  container = document.createElement('div')
  document.body.append(container)
  root = createRoot(container)
})
afterEach(async () => {
  await act(async () => root.unmount())
  container.remove()
  usePlatformScopeStore.setState({ clusterId: null, namespace: null })
  vi.unstubAllGlobals()
})
async function render(props: Partial<React.ComponentProps<typeof WorkloadDetailHeader>> = {}) {
  await act(async () =>
    root.render(
      <MemoryRouter
        initialEntries={[
          '/workloads/statefulsets/database?clusterId=a&namespace=apps&tab=pods&range=60&container=init&edit=true&webhook=hook',
        ]}
      >
        <WorkloadDetailHeader kind="StatefulSet" name="database" namespace="apps" {...props} />
      </MemoryRouter>,
    ),
  )
}
it('keeps resource scope in back navigation and uses one namespace tag', async () => {
  await render({ facts: <span>就绪 1 / 1</span> })
  expect(container.querySelector('a[aria-label="返回StatefulSet列表"]')?.getAttribute('href')).toBe(
    '/workloads/statefulsets?clusterId=a&namespace=apps',
  )
  expect(container.querySelector('h1')?.textContent).toBe('database')
  expect(container.querySelectorAll('.soha-workload-detail-namespace')).toHaveLength(1)
  expect(container.querySelector('.soha-workload-detail-namespace')?.getAttribute('title')).toBe(
    'apps',
  )
  expect(container.querySelector('.soha-workload-detail-meta')?.textContent).toBe('apps就绪 1 / 1')
})
it('retains owner-provided namespace navigation and actions', async () => {
  const refresh = vi.fn()
  await render({
    namespaceContent: <Link to="/cluster-resources/namespaces?namespace=apps">apps</Link>,
    actions: <button onClick={refresh}>刷新</button>,
    status: <span>运行中</span>,
    backTo: '/workloads/pods?namespace=apps',
    backLabel: '返回 Pods',
  })
  expect(container.querySelector('.soha-workload-detail-namespace a')?.getAttribute('href')).toBe(
    '/cluster-resources/namespaces?namespace=apps',
  )
  await act(async () =>
    container.querySelector<HTMLButtonElement>('.soha-workload-detail-actions button')!.click(),
  )
  expect(refresh).toHaveBeenCalledOnce()
  expect(container.querySelector('a[aria-label="返回 Pods"]')?.getAttribute('href')).toBe(
    '/workloads/pods?namespace=apps',
  )
})
it('does not invent a namespace or status when unavailable', async () => {
  await render({ namespace: '—' })
  expect(container.querySelector('.soha-workload-detail-namespace')).toBeNull()
  expect(container.querySelector('.soha-workload-detail-meta')?.textContent).toBe('')
  expect(container.querySelector('a[aria-label="返回StatefulSet列表"]')?.getAttribute('href')).toBe(
    '/workloads/statefulsets?clusterId=a',
  )
})

it.each([
  { selected: null, expected: '/workloads/statefulsets?clusterId=a' },
  {
    selected: 'selected-team',
    expected: '/workloads/statefulsets?clusterId=a&namespace=selected-team',
  },
])('returns to the selected list filter $selected', async ({ selected, expected }) => {
  usePlatformScopeStore.setState({ namespace: selected })
  await render()
  expect(container.querySelector('a[aria-label="返回StatefulSet列表"]')?.getAttribute('href')).toBe(
    expected,
  )
  expect(container.querySelector('.soha-workload-detail-namespace')?.getAttribute('title')).toBe(
    'apps',
  )
})

it('normalizes owner return queries using the selected filter', async () => {
  usePlatformScopeStore.setState({ namespace: 'selected-team' })
  await render({
    backTo:
      '/workloads/jobs?clusterId=b&namespace=other&tab=pods&range=30&container=init&edit=true&webhook=hook&sort=name',
  })
  expect(container.querySelector('a[aria-label="返回StatefulSet列表"]')?.getAttribute('href')).toBe(
    '/workloads/jobs?clusterId=b&namespace=selected-team&sort=name',
  )
})

it('removes the owner return namespace when all namespaces are selected', async () => {
  usePlatformScopeStore.setState({ namespace: null })
  await render({ backTo: '/workloads/pods?clusterId=a&namespace=apps' })
  expect(container.querySelector('a[aria-label="返回StatefulSet列表"]')?.getAttribute('href')).toBe(
    '/workloads/pods?clusterId=a',
  )
})
