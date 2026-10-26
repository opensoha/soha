/** @vitest-environment jsdom */
import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { App } from 'antd'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { api } from '@/services/api-client'
import { ImportKubernetesServicesModal } from './import-kubernetes-services-modal'
import type { DeliveryApplication } from '../types'

vi.mock('@/services/api-client', () => ({
  api: { get: vi.fn(), getEnvelope: vi.fn(), post: vi.fn() },
}))
let root: Root
let client: QueryClient
let container: HTMLDivElement
const pause = () => new Promise((resolve) => setTimeout(resolve, 0))
async function click(element: Element | undefined | null) {
  expect(element).toBeTruthy()
  await act(async () => {
    ;(element as HTMLElement).click()
    await pause()
    await pause()
  })
}
async function button(text: string) {
  await click(
    Array.from(document.querySelectorAll('button')).find(
      (node) => node.textContent?.replace(/\s/g, '') === text.replace(/\s/g, ''),
    ),
  )
}
async function select(label: string, option: string) {
  const target = Array.from(document.querySelectorAll('label')).find((node) =>
    node.textContent?.includes(label),
  )
  expect(target, `Missing ${label}: ${document.body.textContent}`).toBeTruthy()
  const input = document.getElementById(target!.htmlFor)!
  await act(async () => {
    input
      .closest('.ant-select')!
      .querySelector('.ant-select-content')
      ?.dispatchEvent(new MouseEvent('mousedown', { bubbles: true }))
    input.dispatchEvent(new MouseEvent('mousedown', { bubbles: true }))
    await pause()
  })
  const choices = Array.from(
    document.querySelectorAll(
      '.ant-select-dropdown:not(.ant-select-dropdown-hidden) .ant-select-item-option',
    ),
  )
  await click(choices.find((node) => node.textContent?.includes(option)))
}
async function render() {
  const app = { id: 'app', key: 'shop', name: 'Shop', enabled: true } as DeliveryApplication
  vi.mocked(api.get).mockImplementation(
    async (path) =>
      ({
        data:
          path === '/clusters'
            ? [{ id: 'c', name: 'Lab' }]
            : path.includes('/namespaces')
              ? [{ name: 'orders' }]
              : path === '/applications/app/services'
                ? [{ id: 'logical-api', key: 'api', name: 'Orders API', enabled: true }]
                : [],
      }) as never,
  )
  vi.mocked(api.getEnvelope).mockResolvedValue({
    items: [
      {
        clusterId: 'c',
        namespace: 'orders',
        workloadKind: 'Deployment',
        workloadName: 'api',
        desiredReplicas: 1,
        readyReplicas: 1,
        relatedResources: [],
        source: { resource: { uid: 'root-uid', kind: 'Deployment' }, specDigest: 'digest' },
      },
    ],
    truncated: false,
  } as never)
  vi.mocked(api.post).mockResolvedValue({
    data: {
      application: { created: false },
      environment: { created: false },
      services: [],
      targets: [],
      ownershipMode: 'observe_only',
    },
  } as never)
  container = document.createElement('div')
  document.body.append(container)
  root = createRoot(container)
  client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  await act(async () => {
    root.render(
      <App>
        <QueryClientProvider client={client}>
          <ImportKubernetesServicesModal
            applications={[app]}
            bindings={[{ environmentId: 'test', environmentKey: 'test-b' } as never]}
            open
            onClose={() => {}}
            canCreateApplication={false}
            canCreateService={false}
            canMapExistingService
          />
        </QueryClientProvider>
      </App>,
    )
    await pause()
    await pause()
  })
}
afterEach(async () => {
  if (root) await act(async () => root.unmount())
  client?.clear()
  container?.remove()
  vi.clearAllMocks()
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})
describe('logical service import', () => {
  beforeEach(() => {
    vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
    vi.spyOn(window, 'getComputedStyle').mockReturnValue({
      width: '0px',
      height: '0px',
      overflow: 'auto',
      getPropertyValue: () => '',
    } as never)
    vi.stubGlobal(
      'ResizeObserver',
      class {
        observe() {}
        unobserve() {}
        disconnect() {}
      },
    )
    vi.stubGlobal(
      'matchMedia',
      vi.fn(() => ({
        matches: false,
        media: '',
        onchange: null,
        addListener() {},
        removeListener() {},
        addEventListener() {},
        removeEventListener() {},
        dispatchEvent() {},
      })),
    )
  })
  it('requires an explicit existing service when only update is permitted and preserves resource identity', async () => {
    await render()
    await select('集群', 'Lab')
    await select('命名空间', 'orders')
    await button('下一步')
    await vi.waitFor(async () => {
      await act(async () => {
        await pause()
      })
      expect(
        document.querySelector<HTMLInputElement>('tbody tr.ant-table-row input[type="checkbox"]'),
        document.body.textContent ?? '',
      ).toBeTruthy()
    })
    expect(
      document.querySelector<HTMLInputElement>('tbody tr.ant-table-row input[type="checkbox"]')
        ?.disabled,
    ).toBe(false)
    await click(document.querySelector('tbody tr.ant-table-row .ant-checkbox-wrapper'))
    expect(
      document.querySelector<HTMLInputElement>('tbody tr.ant-table-row input[type="checkbox"]')
        ?.checked,
    ).toBe(true)
    await button('下一步')
    await select('应用', 'Shop')
    await select('环境', 'test-b')
    await button('导入1个服务')
    expect(api.post).not.toHaveBeenCalled()
    await select('Deployment / api', 'Orders API')
    await button('导入1个服务')
    expect(api.post).toHaveBeenCalledWith(
      '/application-environments/imports/mapped',
      expect.objectContaining({
        applicationKey: 'shop',
        environmentKey: 'test-b',
        namespace: 'orders',
        ownershipMode: 'observe_only',
        workloads: [
          expect.objectContaining({
            serviceId: 'logical-api',
            expectedUid: 'root-uid',
            expectedSpecDigest: 'digest',
          }),
        ],
      }),
    )
  })
})
