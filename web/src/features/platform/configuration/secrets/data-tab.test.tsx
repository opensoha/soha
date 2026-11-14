/** @vitest-environment jsdom */
import { act, type ComponentProps } from 'react'
import { createRoot } from 'react-dom/client'
import { App } from 'antd'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { SecretDataTab } from './data-tab'

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

const encode = (value: string) => btoa(String.fromCharCode(...new TextEncoder().encode(value)))
let container: HTMLDivElement
let root: ReturnType<typeof createRoot>
let props: ComponentProps<typeof SecretDataTab>
beforeEach(() => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  window.matchMedia = vi
    .fn()
    .mockReturnValue({ matches: false, addListener() {}, removeListener() {} })
  container = document.createElement('div')
  document.body.appendChild(container)
  root = createRoot(container)
  props = {
    canEdit: true,
    detail: {
      name: 'fixture',
      namespace: 'team',
      type: 'Opaque',
      immutable: false,
      ageSeconds: 1,
      data: { 'a.conf': encode('first\nsecond'), 'b.conf': encode('其他值') },
    },
    onApply: vi.fn(),
  }
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
        <SecretDataTab {...props} />
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
const content = () => container.querySelector<HTMLTextAreaElement>('textarea')!
async function edit(value: string) {
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value')!.set!.call(
      content(),
      value,
    )
    content().dispatchEvent(new Event('input', { bubbles: true }))
  })
}

it('uses independent cards, hides values, switches full decoded/Base64 views and remasks on key changes', async () => {
  await render()
  expect(container.querySelectorAll('.soha-configuration-data-layout > .ant-card')).toHaveLength(2)
  expect(content()).toBeNull()
  expect(container.textContent).not.toContain(encode('first\nsecond'))
  await click('Show content')
  expect(content().value).toBe('first\nsecond')
  expect(content().readOnly).toBe(true)
  await act(async () =>
    container.querySelectorAll<HTMLInputElement>('input[type="radio"]')[1].click(),
  )
  expect(content().value).toBe(encode('first\nsecond'))
  await select('b.conf')
  expect(content()).toBeNull()
  await select('a.conf')
  expect(content()).toBeNull()
})

it('compares decoded drafts and preserves Unicode, BOM and valid special key names in the text payload', async () => {
  await render({
    detail: {
      ...props.detail,
      data: Object.fromEntries([
        ['a.conf', encode('first\nsecond')],
        ['bom', encode('\uFEFFhello')],
        ['__proto__', encode('kept')],
      ]),
    },
  })
  await select('a.conf')
  await click('Edit Data')
  await edit('更新\nsecond')
  await act(async () =>
    container.querySelectorAll<HTMLInputElement>('input[type="radio"]')[1].click(),
  )
  expect(container.querySelector('[data-testid="original"]')?.textContent).toBe('first\nsecond')
  await click('Apply changes')
  expect(props.onApply).toHaveBeenCalledWith(
    Object.fromEntries([
      ['a.conf', '更新\nsecond'],
      ['bom', '\uFEFFhello'],
      ['__proto__', 'kept'],
    ]),
  )
  expect(content()).toBeNull()
})

it('retains failed drafts, blocks permission loss and respects immutable Secrets', async () => {
  await render({ onApply: vi.fn().mockRejectedValue(new Error('Access denied')) })
  await click('Edit Data')
  await edit('draft')
  await click('Apply changes')
  expect(container.textContent).toContain('Access denied')
  expect(content().value).toBe('draft')
  await render({ canEdit: false })
  expect(content().readOnly).toBe(true)
  expect(button('Apply changes').disabled).toBe(true)
  await click('Cancel editing')
  expect(content()).toBeNull()
  await render({ canEdit: true, detail: { ...props.detail, immutable: true } })
  expect(button('Edit Data').disabled).toBe(true)
})

it('keeps non-UTF8 bytes read-only without sending a lossy full-map update', async () => {
  await render({
    detail: { ...props.detail, data: { text: encode('editable text'), 'blob.bin': 'AP8=' } },
  })
  expect(button('Edit Data').disabled).toBe(true)
  expect(container.textContent).toContain('Edit its YAML to preserve the original bytes')
  await select('blob.bin')
  await click('Show content')
  expect(content().value).toBe('AP8=')
  expect(content().readOnly).toBe(true)
  expect(props.onApply).not.toHaveBeenCalled()
})
