/** @vitest-environment jsdom */
import { act, StrictMode, useEffect, type ComponentProps } from 'react'
import { createRoot } from 'react-dom/client'
import type Editor from '@monaco-editor/react'
import type { DiffEditor } from '@monaco-editor/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import ConfigurationValueEditor from './value-editor'

const runtime = vi.hoisted(() => ({
  diffProps: {} as ComponentProps<typeof DiffEditor>,
  editProps: {} as ComponentProps<typeof Editor>,
  value: '',
  listeners: new Set<() => void>(),
}))
vi.mock('monaco-editor', () => ({}))
vi.mock('@/i18n', () => ({ useI18n: () => ({ localeCode: 'en_US' }) }))
vi.mock('@/stores/preferences-store', () => ({ usePreferencesStore: () => 'light' }))
vi.mock('@monaco-editor/react', () => ({
  loader: { config: vi.fn() },
  useMonaco: () => null,
  default: (props: ComponentProps<typeof Editor>) => {
    runtime.editProps = props
    return <div data-testid="edit" />
  },
  DiffEditor: (props: ComponentProps<typeof DiffEditor>) => {
    runtime.diffProps = props
    runtime.value = props.modified ?? ''
    useEffect(() => {
      props.onMount?.(
        {
          getModifiedEditor: () => ({
            getValue: () => runtime.value,
            onDidChangeModelContent: (listener: () => void) => {
              runtime.listeners.add(listener)
              return { dispose: () => runtime.listeners.delete(listener) }
            },
          }),
        } as unknown as Parameters<NonNullable<typeof props.onMount>>[0],
        {} as never,
      )
    }, [])
    return <div data-testid="diff" />
  },
}))

let container: HTMLDivElement
let root: ReturnType<typeof createRoot>
let props: ComponentProps<typeof ConfigurationValueEditor>
beforeEach(() => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  container = document.createElement('div')
  document.body.appendChild(container)
  root = createRoot(container)
  props = {
    name: 'app.json',
    value: 'draft',
    original: 'before',
    comparing: false,
    readOnly: false,
    onChange: vi.fn(),
  }
})
afterEach(() => {
  act(() => root.unmount())
  expect(runtime.listeners.size).toBe(0)
  container.remove()
  vi.unstubAllGlobals()
})
async function render(overrides: Partial<typeof props> = {}) {
  props = { ...props, ...overrides }
  await act(async () => {
    root.render(
      <StrictMode>
        <ConfigurationValueEditor {...props} />
      </StrictMode>,
    )
    await Promise.resolve()
  })
}

it('shares the draft model across views, forwards edits and disposes StrictMode subscriptions', async () => {
  await render()
  expect(runtime.editProps.path).toBe(runtime.diffProps.modifiedModelPath)
  expect(runtime.editProps.keepCurrentModel).toBe(true)
  expect(runtime.diffProps.options).toMatchObject({
    originalEditable: false,
    ignoreTrimWhitespace: false,
    useInlineViewWhenSpaceIsLimited: true,
  })
  act(() => {
    runtime.value = 'changed'
    runtime.listeners.forEach((listener) => listener())
  })
  expect(props.onChange).toHaveBeenCalledWith('changed')
  const path = runtime.diffProps.modifiedModelPath
  await render({ comparing: true, value: 'changed' })
  expect(runtime.diffProps.original).toBe('before')
  expect(runtime.diffProps.modified).toBe('changed')
  expect(runtime.diffProps.modifiedModelPath).toBe(path)
  expect(container.querySelector('[data-testid="edit"]')).toBeNull()
  await render({ readOnly: true })
  expect(runtime.diffProps.options?.readOnly).toBe(true)
  vi.mocked(props.onChange).mockClear()
  act(() => {
    runtime.value = 'unauthorized'
    runtime.listeners.forEach((listener) => listener())
  })
  expect(props.onChange).not.toHaveBeenCalled()
})

it('uses a read-only full content editor without diff models when browsing', async () => {
  await render({ original: undefined, readOnly: true })
  expect(container.querySelector('[data-testid="diff"]')).toBeNull()
  expect(runtime.editProps.value).toBe('draft')
  expect(runtime.editProps.options?.readOnly).toBe(true)
  expect(runtime.editProps.language).toBe('json')
})
