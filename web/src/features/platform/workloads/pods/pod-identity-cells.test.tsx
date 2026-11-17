/** @vitest-environment jsdom */
import { act, createContext, useContext, type ComponentProps } from 'react'
import { createRoot } from 'react-dom/client'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { PodNameCell, PodNamespaceCell, PodPhaseCell, PodRestartCell } from './pod-identity-cells'

const renders = vi.hoisted(() => ({ name: 0, namespace: 0, phase: 0, restarts: 0 }))
const scope = vi.hoisted(() => ({ clusterId: 'cluster-a', setNamespace: vi.fn() }))
const LocaleContext = createContext<'zh_CN' | 'en_US'>('zh_CN')
vi.mock('@/i18n', () => ({ useI18n: () => ({ localeCode: useContext(LocaleContext) }) }))
vi.mock('@/stores/platform-scope-store', () => ({ usePlatformScopeStore: () => scope }))
vi.mock('@/features/platform/shared/namespace-scope-tag', async (importOriginal) => {
  const actual =
    await importOriginal<typeof import('@/features/platform/shared/namespace-scope-tag')>()
  return {
    ...actual,
    NamespaceScopeTag: (props: ComponentProps<typeof actual.NamespaceScopeTag>) => {
      renders.namespace++
      return <actual.NamespaceScopeTag {...props} />
    },
  }
})
vi.mock('@/components/status-tag', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/components/status-tag')>()
  return {
    ...actual,
    StatusTag: (props: ComponentProps<typeof actual.StatusTag>) => {
      renders.phase++
      return <actual.StatusTag {...props} />
    },
  }
})
vi.mock('antd', async (importOriginal) => {
  const actual = await importOriginal<typeof import('antd')>()
  return {
    ...actual,
    Tag: (props: ComponentProps<typeof actual.Tag>) => {
      if (!props.className) renders.restarts++
      return <actual.Tag {...props} />
    },
    Typography: {
      ...actual.Typography,
      Link: (props: ComponentProps<typeof actual.Typography.Link>) => {
        renders.name++
        return <actual.Typography.Link {...props} />
      },
    },
  }
})

let root: ReturnType<typeof createRoot>
let host: HTMLDivElement
let inputs: ComponentProps<typeof PodNameCell> & {
  namespace: string
  phase: string
  restarts: number
  locale: 'zh_CN' | 'en_US'
}
async function render(patch: Partial<typeof inputs> = {}) {
  inputs = { ...inputs, ...patch }
  await act(async () =>
    root.render(
      <MemoryRouter>
        <LocaleContext.Provider value={inputs.locale}>
          <PodNameCell
            name={inputs.name}
            pvcCount={inputs.pvcCount}
            to={inputs.to}
            onNavigate={inputs.onNavigate}
          />
          <PodNamespaceCell namespace={inputs.namespace} />
          <div data-phase>
            <PodPhaseCell value={inputs.phase} />
          </div>
          <div data-restarts>
            <PodRestartCell restarts={inputs.restarts} />
          </div>
        </LocaleContext.Provider>
      </MemoryRouter>,
    ),
  )
}
beforeEach(() => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  Object.assign(renders, { name: 0, namespace: 0, phase: 0, restarts: 0 })
  scope.setNamespace.mockClear()
  inputs = {
    name: 'pod-a',
    pvcCount: 2,
    to: '/pods/pod-a',
    onNavigate: vi.fn(),
    namespace: 'team-a',
    phase: 'Running',
    restarts: 0,
    locale: 'zh_CN',
  }
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
})
afterEach(async () => {
  await act(async () => root.unmount())
  host.remove()
  vi.unstubAllGlobals()
})

it('skips same-value refreshes while preserving the original name, PVC and restart DOM', async () => {
  await render()
  const initial = { ...renders }
  expect(initial).toEqual({ name: 1, namespace: 1, phase: 1, restarts: 1 })
  await render()
  expect(renders).toEqual(initial)
  expect(host.querySelector('.soha-pod-table-name-cell')?.textContent).toContain('pod-a')
  expect(host.querySelector('.soha-pod-table-pvc-tag')?.textContent).toBe('PVC 2')
  expect(host.querySelector('[data-restarts] .ant-tag-default')?.textContent).toBe('0')
  await act(async () => host.querySelector<HTMLElement>('.soha-pod-table-name-link')!.click())
  expect(inputs.onNavigate).toHaveBeenCalledWith('/pods/pod-a')
})

it('updates real props, current navigation callbacks and the original components context', async () => {
  await render()
  const originalNavigate = inputs.onNavigate
  const nextNavigate = vi.fn()
  await render({
    name: 'pod-b',
    pvcCount: 3,
    to: '/pods/pod-b',
    onNavigate: nextNavigate,
    namespace: 'team-b',
    phase: 'Failed',
    restarts: 2,
  })
  expect(renders).toEqual({ name: 2, namespace: 2, phase: 2, restarts: 2 })
  expect(host.querySelector('.soha-pod-table-name-link')?.textContent).toBe('pod-b')
  expect(host.querySelector('.soha-pod-table-pvc-tag')?.textContent).toBe('PVC 3')
  expect(host.querySelector('.soha-namespace-scope-button')?.textContent).toBe('team-b')
  expect(host.querySelector('[data-phase] .ant-tag-error')).not.toBeNull()
  expect(host.querySelector('[data-restarts] .ant-tag-warning')?.textContent).toBe('2')
  await act(async () => host.querySelector<HTMLElement>('.soha-pod-table-name-link')!.click())
  expect(nextNavigate).toHaveBeenLastCalledWith('/pods/pod-b')
  expect(originalNavigate).not.toHaveBeenCalled()
  await render({ to: '/pods/current', pvcCount: 0 })
  expect(host.querySelector('.soha-pod-table-pvc-tag')).toBeNull()
  await act(async () => host.querySelector<HTMLElement>('.soha-pod-table-name-link')!.click())
  expect(nextNavigate).toHaveBeenLastCalledWith('/pods/current')
  const latestNavigate = vi.fn()
  await render({ onNavigate: latestNavigate })
  await act(async () => host.querySelector<HTMLElement>('.soha-pod-table-name-link')!.click())
  expect(latestNavigate).toHaveBeenCalledWith('/pods/current')
  await render({ locale: 'en_US' })
  expect(host.querySelector('.soha-namespace-scope-button')?.getAttribute('aria-label')).toBe(
    'Switch to namespace team-b',
  )
  expect(host.querySelector('[data-phase]')?.textContent).toBe('Failed')
  await act(async () =>
    host.querySelector<HTMLButtonElement>('.soha-namespace-scope-button')!.click(),
  )
  expect(scope.setNamespace).toHaveBeenCalledWith('team-b')
})
