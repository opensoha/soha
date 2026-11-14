/** @vitest-environment jsdom */

import type Editor from '@monaco-editor/react'
import type { DiffEditor } from '@monaco-editor/react'
import { act, useEffect, type ComponentProps } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { YamlDraftDiffEditor } from './yaml-draft-diff-editor'

const monaco = vi.hoisted(() => ({
  left: {} as ComponentProps<typeof Editor>,
  right: {} as ComponentProps<typeof DiffEditor>,
  value: '',
  change: null as (() => void) | null,
  dispose: vi.fn(),
}))

vi.mock('monaco-editor', () => ({}))
vi.mock('@monaco-editor/react', () => ({
  loader: { config: vi.fn() },
  useMonaco: () => null,
  default: (props: ComponentProps<typeof Editor>) => {
    monaco.left = props
    return null
  },
  DiffEditor: (props: ComponentProps<typeof DiffEditor>) => {
    monaco.right = props
    useEffect(() => {
      props.onMount?.(
        {
          getModel: () => null,
          setModel: vi.fn(),
          getModifiedEditor: () => ({
            getValue: () => monaco.value,
            onDidChangeModelContent: (listener: () => void) => {
              monaco.change = listener
              return { dispose: monaco.dispose }
            },
          }),
        } as unknown as Parameters<NonNullable<typeof props.onMount>>[0],
        {} as never,
      )
      // Monaco calls onMount once per editor instance.
    }, [])
    return null
  },
}))

let container: HTMLElement
let root: ReturnType<typeof createRoot>

beforeEach(() => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  monaco.change = null
  monaco.dispose.mockClear()
  container = document.createElement('div')
  document.body.appendChild(container)
  root = createRoot(container)
})

afterEach(async () => {
  await act(async () => root.unmount())
  container.remove()
  vi.unstubAllGlobals()
})

it('shows the baseline on the left and tracks edits in the modified diff model when requested', async () => {
  const onChange = vi.fn()
  await act(async () =>
    root.render(
      <YamlDraftDiffEditor
        baselineOnLeft
        original="baseline"
        modified="draft"
        onChange={onChange}
        title="Values"
      />,
    ),
  )
  expect(monaco.left).toMatchObject({ value: 'baseline', options: { readOnly: true } })
  expect(monaco.right).toMatchObject({
    original: 'baseline',
    modified: 'draft',
    options: { readOnly: false },
  })
  monaco.value = 'draft'
  monaco.change?.()
  expect(onChange).not.toHaveBeenCalled()
  monaco.value = 'edited draft'
  monaco.change?.()
  expect(onChange).toHaveBeenCalledExactlyOnceWith('edited draft')
})

it('preserves the default draft-left and baseline-right mode', async () => {
  await act(async () =>
    root.render(<YamlDraftDiffEditor original="baseline" modified="draft" title="YAML" />),
  )
  expect(monaco.left).toMatchObject({ value: 'draft', options: { readOnly: false } })
  expect(monaco.right).toMatchObject({
    original: 'draft',
    modified: 'baseline',
    options: { readOnly: true },
  })
  expect(monaco.change).toBeNull()
})
