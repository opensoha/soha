/** @vitest-environment jsdom */
import { act, type ReactNode } from 'react'
import { createRoot } from 'react-dom/client'
import { App } from 'antd'
import { beforeEach, afterEach, expect, it, vi, type Mock } from 'vitest'
import {
  useKubernetesResourceActions,
  kubernetesDeleteConfirmation,
} from './use-kubernetes-resource-actions'
const scope = vi.hoisted(() => ({ clusterId: 'a', namespace: 'team' }))
vi.mock('@/stores/platform-scope-store', () => ({ usePlatformScopeStore: () => scope }))
vi.mock('@/i18n', async (original) => ({
  ...(await original<typeof import('@/i18n')>()),
  useI18n: () => ({ localeCode: 'zh_CN', t: (_key: string, text: string) => text }),
}))
type Row = { name: string; namespace: string; uid: string }
let records: Row[], allowed: boolean, disabled: boolean, visible: boolean
let run: Mock<(row: Row) => Promise<void>>
let host: HTMLDivElement, root: ReturnType<typeof createRoot>
function Harness() {
  const result = useKubernetesResourceActions({
    resourceKey: 'sample',
    resourceLabel: 'Sample',
    records,
    getName: (row) => row.name,
    getKey: (row) => row.uid,
    actions: (row) =>
      allowed
        ? [
            {
              key: 'delete',
              label: '删除',
              disabled,
              reason: disabled ? '能力受限' : '能力说明',
              danger: true,
              confirm: kubernetesDeleteConfirmation(row, 'zh_CN'),
              run: () => run(row),
            },
          ]
        : [],
  })
  return (
    <>
      {visible
        ? records.map((row, index) => (
            <div key={row.uid}>{result.column.render?.(undefined, row, index) as ReactNode}</div>
          ))
        : null}
      {allowed && (
        <button onClick={() => result.selectAction(records[0], 'delete')}>Explicit delete</button>
      )}
      {result.confirmation}
    </>
  )
}
async function render() {
  await act(async () =>
    root.render(
      <App>
        <Harness />
      </App>,
    ),
  )
}
async function click(node: Element | null | undefined) {
  expect(node).toBeTruthy()
  await act(async () => {
    node?.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    await new Promise((r) => setTimeout(r, 0))
  })
}
async function openDelete() {
  await click(host.querySelector('button[aria-label="更多操作：api"]'))
  await click(document.querySelector('[role="menuitem"]'))
  return document.querySelector<HTMLElement>('[role="dialog"]')!
}
const ok = () => document.querySelector<HTMLButtonElement>('.ant-modal-footer .ant-btn-primary')
beforeEach(() => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  const computedStyle = window.getComputedStyle.bind(window)
  vi.spyOn(window, 'getComputedStyle').mockImplementation((element) => computedStyle(element))
  window.matchMedia = vi.fn().mockReturnValue({
    matches: false,
    addListener() {},
    removeListener() {},
    addEventListener() {},
    removeEventListener() {},
  })
  scope.clusterId = 'a'
  scope.namespace = 'team'
  records = [{ name: 'api', namespace: 'team', uid: 'first' }]
  allowed = true
  disabled = false
  visible = true
  run = vi.fn<(row: Row) => Promise<void>>().mockResolvedValue(undefined)
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
})
afterEach(async () => {
  await act(async () => root.unmount())
  host.remove()
  document.body.replaceChildren()
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})
it('keeps confirmation outside unmounted virtual cells and submits only after confirmation', async () => {
  await render()
  const dialog = await openDelete()
  expect(dialog.textContent).toContain('team / api')
  expect(run).not.toHaveBeenCalled()
  visible = false
  await render()
  expect(document.body.contains(dialog)).toBe(true)
  await click(ok())
  expect(run).toHaveBeenCalledExactlyOnceWith(records[0])
})
it('revalidates permission, UID and scope without targeting a replacement object', async () => {
  await render()
  await openDelete()
  records = [{ ...records[0], uid: 'replacement' }]
  await render()
  expect(ok()?.disabled).toBe(true)
  await click(ok())
  expect(run).not.toHaveBeenCalled()
  scope.clusterId = 'b'
  await render()
  expect(document.querySelector('[role="dialog"]')?.getAttribute('aria-hidden') ?? 'closed').toBe(
    'closed',
  )
})
it('prevents duplicate confirmation and retains failed context for retry', async () => {
  let reject!: (error: Error) => void
  run.mockImplementationOnce(
    () =>
      new Promise((_resolve, fail) => {
        reject = fail
      }),
  )
  await render()
  await openDelete()
  await click(ok())
  await click(ok())
  expect(run).toHaveBeenCalledTimes(1)
  expect(ok()?.getAttribute('aria-label')).toBe('删除')
  expect(ok()?.getAttribute('aria-busy')).toBe('true')
  await act(async () => {
    reject(new Error('Rejected target'))
    await new Promise((r) => setTimeout(r, 0))
  })
  expect(document.querySelector('[role="dialog"]')?.textContent).toContain('Rejected target')
  expect(ok()?.disabled).toBe(false)
  await click(ok())
  expect(run).toHaveBeenCalledTimes(2)
})
it('hides denied actions and exposes disabled capability reasons', async () => {
  allowed = false
  await render()
  expect(host.querySelector('button')).toBeNull()
  allowed = true
  disabled = true
  await render()
  await click(host.querySelector('button[aria-label="更多操作：api"]'))
  const item = document.querySelector('[role="menuitem"]')
  expect(item?.getAttribute('aria-disabled')).toBe('true')
  expect(item?.textContent).toContain('能力受限')
  await click(item)
  expect(run).not.toHaveBeenCalled()
})

it('blocks a confirmation after permission is withdrawn', async () => {
  await render()
  await openDelete()
  allowed = false
  await render()
  expect(ok()?.disabled).toBe(true)
  expect(document.querySelector('[role="dialog"]')?.textContent).toContain('资源或操作权限已变化')
  await click(ok())
  expect(run).not.toHaveBeenCalled()
})
it('does not show an old scope failure in a new confirmation', async () => {
  let reject!: (error: Error) => void
  run.mockImplementationOnce(
    () =>
      new Promise((_resolve, fail) => {
        reject = fail
      }),
  )
  await render()
  await openDelete()
  await click(ok())
  scope.clusterId = 'b'
  await render()
  await openDelete()
  await act(async () => {
    reject(new Error('Old scope failure'))
    await new Promise((resolve) => setTimeout(resolve, 0))
  })
  expect(document.querySelector('[role="dialog"]')?.textContent).not.toContain('Old scope failure')
  expect(ok()?.disabled).toBe(false)
})

it('closes the old confirmation on scope change even when resource UID and name are identical', async () => {
  await render()
  await openDelete()
  const previousButton = ok()!
  scope.clusterId = 'b'
  await render()
  expect(records[0].uid).toBe('first')
  expect(document.querySelector('[role="dialog"]')).toBeNull()
  await click(previousButton)
  expect(run).not.toHaveBeenCalled()
})

it('keeps enabled action labels concise even when capability documentation is available', async () => {
  await render()
  await click(host.querySelector('button[aria-label="更多操作：api"]'))
  const item = document.querySelector('[role="menuitem"]')!
  expect(item.textContent).toBe('删除')
  expect(item.textContent).not.toContain('能力说明')
})

it('explicit action buttons use the same confirmation and current permission checks', async () => {
  await render()
  await click(
    Array.from(host.querySelectorAll('button')).find((b) => b.textContent === 'Explicit delete'),
  )
  expect(run).not.toHaveBeenCalled()
  allowed = false
  await render()
  expect(ok()?.disabled).toBe(true)
  await click(ok())
  expect(run).not.toHaveBeenCalled()
})
