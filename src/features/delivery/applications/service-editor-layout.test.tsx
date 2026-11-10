/** @vitest-environment jsdom */
import { act, createRef } from 'react'
import { createRoot } from 'react-dom/client'
import { App } from 'antd'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { I18nProvider } from '@/i18n'
import { ServiceEditor } from './service-editor'
import type { DeliveryApplication } from '../types'

vi.mock('./deployment-template-fields', () => ({
  initialDeploymentValues: () => ({}),
  useServiceDeployment: () => ({ prepare: async () => ({}) }),
  DeploymentTemplateFields: () => null,
  DeploymentEnvironmentFields: () => null,
}))
vi.mock('./build-source-fields', () => ({ BuildSourceFields: () => null }))
vi.mock('../queries', () => ({
  deliveryQueries: {
    blueprints: { list: () => ({ queryKey: ['presets'], queryFn: async () => [] }) },
  },
}))
vi.mock('../mutations', () => ({
  deliveryMutations: {
    applications: { saveServiceSetup: () => ({ mutationFn: async () => 'saved' }) },
  },
}))
let root: ReturnType<typeof createRoot>
let container: HTMLDivElement
const closeGuardRef = createRef<((proceed: () => void) => void) | null>()
const onCancel = vi.fn()
beforeEach(async () => {
  Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
  vi.stubGlobal(
    'ResizeObserver',
    class {
      observe() {}
      unobserve() {}
      disconnect() {}
    },
  )
  window.matchMedia = vi.fn().mockImplementation(() => ({
    matches: false,
    addListener: vi.fn(),
    removeListener: vi.fn(),
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
  }))
  container = document.createElement('div')
  document.body.append(container)
  root = createRoot(container)
  await act(async () => {
    root.render(
      <I18nProvider>
        <QueryClientProvider client={new QueryClient()}>
          <App>
            <ServiceEditor
              application={{ id: 'app', buildSources: [] } as unknown as DeliveryApplication}
              service={null}
              services={[]}
              repositories={[]}
              canManageBuild
              closeGuardRef={closeGuardRef}
              onCancel={onCancel}
              onSaved={vi.fn()}
            />
          </App>
        </QueryClientProvider>
      </I18nProvider>,
    )
  })
})
afterEach(async () => {
  await act(async () => root.unmount())
  // Let Antd's delayed field feedback settle before jsdom is disposed.
  await act(async () => new Promise((resolve) => setTimeout(resolve, 350)))
  container.remove()
  onCancel.mockClear()
})
function click(text: string, scope: ParentNode = container) {
  const button = Array.from(scope.querySelectorAll('button')).find(
    (item) => item.textContent?.replace(/\s/g, '') === text,
  )
  if (!button) throw new Error(`Missing button: ${text}`)
  button.click()
}
function change(name: string, value: string) {
  const input = container.querySelector<HTMLInputElement>(`input[id="${name}"]`)!
  Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(input, value)
  input.dispatchEvent(new Event('input', { bubbles: true }))
  input.dispatchEvent(new Event('change', { bubbles: true }))
}
it('skips build for images and preserves values across source changes', async () => {
  expect(container.querySelector('.soha-step-form__steps')?.textContent).not.toContain('构建配置')
  expect(container.textContent).not.toContain('返回服务与构建')
  await act(async () => {
    change('key', 'api')
    change('name', 'API')
    click('下一步')
  })
  expect(
    container.querySelector('.soha-step-form__content > div:not([hidden])')?.textContent,
  ).toContain('产物容器')
  await act(async () => click('上一步'))
  await act(async () => container.querySelector<HTMLInputElement>('input[value="build"]')!.click())
  expect(container.querySelector('.soha-step-form__steps')?.textContent).toContain('构建配置')
  expect(container.querySelector<HTMLInputElement>('input[id="name"]')?.value).toBe('API')
  await act(async () => container.querySelector<HTMLInputElement>('input[value="image"]')!.click())
  await act(async () => click('下一步'))
  await act(async () => click('下一步'))
  expect(
    Array.from(container.querySelectorAll('button')).some(
      (item) => item.textContent === '创建服务',
    ),
  ).toBe(true)
})
it('guards cancel and parent navigation with the same unsaved confirmation', async () => {
  const proceed = vi.fn()
  await act(async () => closeGuardRef.current!(proceed))
  expect(proceed).toHaveBeenCalledOnce()
  await act(async () => change('name', 'draft'))
  await act(async () => click('取消'))
  expect(onCancel).not.toHaveBeenCalled()
  expect(document.body.textContent).toContain('放弃未保存的服务配置？')
  await act(async () => click('继续编辑', document.body))
  await act(async () => closeGuardRef.current!(proceed))
  expect(proceed).toHaveBeenCalledOnce()
  await act(async () => click('放弃修改', document.body))
  expect(proceed).toHaveBeenCalledTimes(2)
})

it('protects browser refresh after a draft edit and removes protection on unmount', async () => {
  const pristine = new Event('beforeunload', { cancelable: true })
  window.dispatchEvent(pristine)
  expect(pristine.defaultPrevented).toBe(false)
  await act(async () => change('name', 'draft'))
  const dirty = new Event('beforeunload', { cancelable: true })
  window.dispatchEvent(dirty)
  expect(dirty.defaultPrevented).toBe(true)
  await act(async () => root.unmount())
  const closed = new Event('beforeunload', { cancelable: true })
  window.dispatchEvent(closed)
  expect(closed.defaultPrevented).toBe(false)
})
