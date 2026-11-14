/** @vitest-environment jsdom */
import { act, type ComponentProps } from 'react'
import { createRoot } from 'react-dom/client'
import { App } from 'antd'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { ConfigMapDataTab } from './data-tab'

vi.mock('@/i18n', () => ({
  useI18n: () => ({ localeCode: 'en_US' }),
  localeText: (_locale: string, _zh: string, en: string) => en,
}))
vi.mock('../shared/value-editor', () => ({
  default: ({
    value,
    original,
    comparing,
    readOnly,
    onChange,
  }: {
    value: string
    original?: string
    comparing: boolean
    readOnly: boolean
    onChange: (value: string) => void
  }) => (
    <>
      {comparing ? <pre data-testid="original">{original}</pre> : null}
      <textarea
        aria-label="Configuration content"
        value={value}
        readOnly={readOnly}
        onChange={(event) => onChange(event.target.value)}
      />
    </>
  ),
}))

let container: HTMLDivElement
let root: ReturnType<typeof createRoot>
let props: ComponentProps<typeof ConfigMapDataTab>

beforeEach(() => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  window.matchMedia = vi
    .fn()
    .mockReturnValue({ matches: false, addListener() {}, removeListener() {} })
  props = {
    canEdit: true,
    detail: {
      name: 'demo',
      namespace: 'team',
      ageSeconds: 1,
      immutable: false,
      data: { 'a.conf': 'first\nsecond', 'b.conf': 'other' },
      binaryData: { 'binary.bin': 'AP8=' },
    },
    onApply: vi.fn(),
  }
  container = document.createElement('div')
  document.body.appendChild(container)
  root = createRoot(container)
})
afterEach(() => {
  act(() => root.unmount())
  container.remove()
  vi.unstubAllGlobals()
})

async function render(overrides: Partial<typeof props> = {}) {
  props = { ...props, ...overrides }
  await act(async () => {
    root.render(
      <App>
        <ConfigMapDataTab {...props} />
      </App>,
    )
    await Promise.resolve()
  })
}
function button(label: string) {
  const found = [...container.querySelectorAll('button')].find(
    (item) => item.textContent === label || item.getAttribute('aria-label') === label,
  )
  if (!found) throw new Error(`Missing button ${label}`)
  return found
}
async function click(label: string) {
  await act(async () => button(label).click())
}
async function select(key: string) {
  await act(async () =>
    [...container.querySelectorAll<HTMLButtonElement>('.soha-configuration-data-key')]
      .find((item) => item.textContent?.includes(key))
      ?.click(),
  )
}
async function input(selector: string, value: string) {
  const element = container.querySelector<HTMLInputElement | HTMLTextAreaElement>(selector)!
  await act(async () => {
    Object.getOwnPropertyDescriptor(
      element instanceof HTMLTextAreaElement
        ? HTMLTextAreaElement.prototype
        : HTMLInputElement.prototype,
      'value',
    )!.set!.call(element, value)
    element.dispatchEvent(new Event('input', { bubbles: true }))
  })
}
const content = () => container.querySelector<HTMLTextAreaElement>('textarea')!

it('browses full values, keeps binaryData read-only and respects immutable/permission gates', async () => {
  await render()
  expect(content().value).toBe('first\nsecond')
  expect(content().readOnly).toBe(true)
  expect(container.querySelector('table')).toBeNull()
  await select('binary.bin')
  expect(content().value).toBe('AP8=')
  await click('Edit Data')
  expect(content().readOnly).toBe(true)
  expect(container.textContent).toContain('Base64 · Read-only')
  expect(button('Apply changes').disabled).toBe(true)
  await click('Cancel editing')
  await render({ canEdit: false })
  expect(button('Edit Data').disabled).toBe(true)
  await render({ canEdit: true, detail: { ...props.detail, immutable: true } })
  expect(button('Edit Data').disabled).toBe(true)
})

it('retains drafts across keys and compares with the original value before applying', async () => {
  await render()
  await click('Edit Data')
  await input('textarea', 'first\nchanged')
  await select('b.conf')
  await input('textarea', 'updated')
  await select('a.conf')
  expect(content().value).toBe('first\nchanged')
  await act(async () =>
    container.querySelectorAll<HTMLInputElement>('input[type="radio"]')[1].click(),
  )
  expect(container.querySelector('[data-testid="original"]')?.textContent).toBe('first\nsecond')
  expect(container.textContent).toContain('2 keys to apply')
  await click('Apply changes')
  expect(props.onApply).toHaveBeenCalledWith({ 'a.conf': 'first\nchanged', 'b.conf': 'updated' })
  expect(button('Edit Data')).toBeDefined()
})

it('validates keys including binary collisions, and supports rename, deletion and restoration', async () => {
  await render()
  await click('Edit Data')
  await click('Add data key')
  await click('Apply changes')
  expect(props.onApply).not.toHaveBeenCalled()
  expect(container.textContent).toContain('Keys must contain only')
  await input('input[aria-label="Data key"]', 'binary.bin')
  await click('Apply changes')
  expect(container.textContent).toContain('Duplicate key: binary.bin')
  await input('input[aria-label="Data key"]', 'new.conf')
  await input('textarea', 'new content')
  await select('a.conf')
  await input('input[aria-label="Data key"]', 'renamed.conf')
  await select('b.conf')
  await click('Delete data key')
  expect(content().readOnly).toBe(true)
  await click('Restore key')
  expect(content().value).toBe('other')
  await click('Delete data key')
  await click('Apply changes')
  expect(props.onApply).toHaveBeenCalledWith({
    'renamed.conf': 'first\nsecond',
    'new.conf': 'new content',
  })
})

it('keeps failed drafts, blocks server conflicts and rechecks permission during editing', async () => {
  await render({ onApply: vi.fn().mockRejectedValue(new Error('RBAC denied')) })
  await click('Edit Data')
  await input('textarea', 'draft')
  await click('Apply changes')
  expect(container.textContent).toContain('RBAC denied')
  expect(content().value).toBe('draft')
  await render({ canEdit: false })
  expect(content().readOnly).toBe(true)
  expect(button('Apply changes').disabled).toBe(true)
  await render({
    canEdit: true,
    detail: { ...props.detail, data: { 'a.conf': 'remote', 'b.conf': 'other' } },
  })
  expect(content().value).toBe('draft')
  expect(container.textContent).toContain('Cluster data changed')
  expect(button('Apply changes').disabled).toBe(true)
  await click('Cancel editing')
  expect(content().value).toBe('remote')
})

it('adds the first key to an empty ConfigMap and can remove every text key', async () => {
  await render({ detail: { ...props.detail, data: {}, binaryData: {} } })
  expect(container.textContent).toContain('No data. Edit data')
  await click('Edit Data')
  await click('Add data key')
  await input('input[aria-label="Data key"]', 'empty')
  await click('Apply changes')
  expect(props.onApply).toHaveBeenCalledWith({ empty: '' })
  await render({ detail: { ...props.detail, data: { empty: '' } } })
  await click('Edit Data')
  await click('Delete data key')
  await click('Apply changes')
  expect(props.onApply).toHaveBeenLastCalledWith({})
})
