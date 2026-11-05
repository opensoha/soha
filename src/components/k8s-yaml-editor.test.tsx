/** @vitest-environment jsdom */

import { act, StrictMode, useEffect, type ComponentProps } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type Editor from '@monaco-editor/react'
import type { DiffEditor } from '@monaco-editor/react'
import { K8sYamlEditor } from './k8s-yaml-editor'

const monaco = vi.hoisted(() => ({
  props: {} as ComponentProps<typeof DiffEditor>,
  detach: vi.fn(),
  releaseOriginal: vi.fn(),
  releaseDraft: vi.fn(),
  modified: '',
  changes: 0,
  edits: new Set<() => void>(),
  diffs: new Set<() => void>(),
  navigate: vi.fn(),
  reveal: vi.fn(),
  position: vi.fn(),
  editProps: {} as ComponentProps<typeof Editor>,
}))

vi.mock('@monaco-editor/react', () => ({
  loader: { config: vi.fn() },
  useMonaco: () => null,
  default: (props: ComponentProps<typeof Editor>) => {
    monaco.editProps = props
    return <div data-testid="plain-editor" />
  },
  DiffEditor: (props: ComponentProps<typeof DiffEditor>) => {
    monaco.props = props
    monaco.modified = props.modified ?? ''
    useEffect(() => {
      const subscribe = (listeners: Set<() => void>, callback: () => void) => {
        listeners.add(callback)
        return { dispose: () => listeners.delete(callback) }
      }
      const models = {
        original: { dispose: monaco.releaseOriginal },
        modified: { dispose: monaco.releaseDraft },
      }
      const editor = {
        getModel: () => models,
        setModel: monaco.detach,
        getLineChanges: () =>
          Array.from({ length: monaco.changes }, () => ({
            originalStartLineNumber: 1,
            originalEndLineNumber: 1,
            modifiedStartLineNumber: 1,
            modifiedEndLineNumber: 1,
          })),
        getOriginalEditor: () => ({ getValue: () => monaco.props.original ?? '' }),
        onDidUpdateDiff: (callback: () => void) => subscribe(monaco.diffs, callback),
        getModifiedEditor: () => ({
          getValue: () => monaco.modified,
          onDidChangeModelContent: (callback: () => void) => subscribe(monaco.edits, callback),
          setPosition: monaco.position,
          revealLineInCenter: monaco.reveal,
        }),
        goToDiff: monaco.navigate,
      }
      props.onMount?.(
        editor as unknown as Parameters<NonNullable<typeof props.onMount>>[0],
        {} as never,
      )
      // The real wrapper calls onMount once per editor instance.
    }, [])
    return <div data-testid="diff-editor" />
  },
}))
vi.mock('monaco-yaml', () => ({ configureMonacoYaml: vi.fn() }))
vi.mock('monaco-editor', () => ({}))
vi.mock('@/i18n', () => ({
  useI18n: () => ({ localeCode: 'en_US', t: (_: string, fallback: string) => fallback }),
}))
vi.mock('@/stores/preferences-store', () => ({ usePreferencesStore: () => 'light' }))

let container: HTMLDivElement
let root: ReturnType<typeof createRoot>
let props: ComponentProps<typeof K8sYamlEditor>

function render(overrides: Partial<typeof props> = {}) {
  props = { ...props, ...overrides }
  act(() =>
    root.render(
      <StrictMode>
        <K8sYamlEditor {...props} />
      </StrictMode>,
    ),
  )
}

function button(text: string) {
  const result = [...container.querySelectorAll('button')].find(
    (item) => item.textContent === text || item.getAttribute('aria-label') === text,
  )
  if (!result) throw new Error(`Missing button: ${text}`)
  return result
}

beforeEach(() => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  monaco.changes = 0
  monaco.modified = ''
  monaco.navigate.mockClear()
  monaco.reveal.mockClear()
  monaco.position.mockClear()
  props = {
    value: 'replicas: 1',
    original: 'replicas: 1',
    onChange: vi.fn(),
    onReset: vi.fn(),
    onSave: vi.fn(),
    onApply: vi.fn(),
  }
  container = document.createElement('div')
  document.body.appendChild(container)
  root = createRoot(container)
})

afterEach(() => {
  act(() => root.unmount())
  expect(monaco.edits.size).toBe(0)
  expect(monaco.diffs.size).toBe(0)
  container.remove()
  vi.unstubAllGlobals()
})

describe('Kubernetes YAML diff editor', () => {
  it('opens a read-only resource without draft mutation controls', () => {
    render({ original: undefined, readOnly: true, header: <strong>Selected resource</strong> })
    expect(container.querySelector('.soha-k8s-yaml-body')?.textContent).toContain(
      'Selected resource',
    )
    expect(monaco.editProps.options?.readOnly).toBe(true)
    expect(container.textContent).toContain('YAML · Read-only')
    expect(container.textContent).toContain('Resource content · Read-only')
    expect(container.textContent).not.toContain('Draft · Editable')
    expect(container.querySelector<HTMLElement>('.soha-k8s-yaml-workspace')?.style.background).toBe(
      '',
    )
    expect(container.textContent).not.toContain('Save Draft')
    expect(container.textContent).not.toContain('Apply')
    expect(container.textContent).not.toContain('Reset')
  })

  it('keeps the baseline read-only and disallows applying unchanged existing resources', () => {
    render()
    expect(container.querySelector<HTMLInputElement>('input[type="radio"]')?.checked).toBe(true)
    expect(container.querySelector('[data-testid="plain-editor"]')).not.toBeNull()
    expect(container.querySelector('.soha-k8s-yaml-change')).toBeNull()
    expect(container.textContent).toContain('No changes')
    expect(button('Apply').disabled).toBe(true)
    expect(monaco.props.options).toMatchObject({
      originalEditable: false,
      ignoreTrimWhitespace: false,
      useInlineViewWhenSpaceIsLimited: true,
    })
  })

  it('keeps StrictMode subscriptions, displays before/after fields and navigates to their lines', () => {
    render()
    act(() => container.querySelectorAll<HTMLInputElement>('input[type="radio"]')[2].click())
    act(() => {
      monaco.modified = 'replicas: 2'
      monaco.edits.forEach((notify) => notify())
    })
    expect(props.onChange).toHaveBeenCalledWith('replicas: 2')
    render({ value: 'replicas: 2' })
    act(() => {
      monaco.changes = 1
      monaco.diffs.forEach((notify) => notify())
    })
    expect(container.textContent).toContain('1 change')
    expect(container.querySelector('.soha-k8s-yaml-change')?.textContent).toContain('replicas')
    expect(container.querySelector('.soha-k8s-yaml-change-value.is-removed')?.textContent).toBe(
      '−1',
    )
    expect(container.querySelector('.soha-k8s-yaml-change-value.is-added')?.textContent).toBe('+2')
    expect(button('Apply').disabled).toBe(false)
    act(() => button('Next').click())
    expect(monaco.reveal).toHaveBeenCalledWith(1)
    expect(monaco.position).toHaveBeenCalledWith({ lineNumber: 1, column: 1 })
    act(() => button('Save Draft').click())
    expect(props.onSave).toHaveBeenCalledOnce()
    expect(container.textContent).toContain('1 change')
    const modelPath = monaco.props.modifiedModelPath
    act(() => container.querySelectorAll<HTMLInputElement>('input[type="radio"]')[1].click())
    expect(monaco.props.options?.renderSideBySide).toBe(true)
    act(() => button('Next').click())
    expect(monaco.navigate).toHaveBeenCalledWith('next')
    expect(monaco.props.modifiedModelPath).toBe(modelPath)
    act(() => container.querySelectorAll<HTMLInputElement>('input[type="radio"]')[0].click())
    expect(monaco.editProps.path).toBe(modelPath)
    expect(monaco.editProps.keepCurrentModel).toBe(true)
    expect(monaco.editProps.options?.occurrencesHighlight).toBe('off')
    expect(monaco.props.keepCurrentOriginalModel).toBe(true)
    expect(monaco.props.keepCurrentModifiedModel).toBe(true)
    expect(monaco.props.original).toBe('replicas: 1')
    expect(container.querySelector('[data-testid="diff-editor"]')).not.toBeNull()
    act(() => container.querySelectorAll<HTMLInputElement>('input[type="radio"]')[2].click())
    expect(monaco.props.modified).toBe('replicas: 2')
    expect(container.textContent).toContain('1 change')
    render({ value: 'replicas: 3' })
    expect(container.querySelector('.soha-k8s-yaml-change')).toBeNull()
    expect(container.textContent).toContain('Comparing…')
  })

  it('blocks conflicts and failed loads while allowing an explicit latest-version comparison', () => {
    const compare = vi.fn()
    render({ value: 'edited', serverChanged: true, onCompareLatest: compare })
    expect(button('Apply').disabled).toBe(true)
    expect(container.textContent).toContain('Your draft is preserved')
    act(() => button('Compare latest').click())
    expect(compare).toHaveBeenCalledOnce()
    render({ serverChanged: false, error: new Error('Resource unavailable') })
    expect(button('Apply').disabled).toBe(true)
    expect(container.textContent).toContain('Resource unavailable')
    render({ error: null, applying: true })
    expect(monaco.props.options?.readOnly).toBe(true)
    expect(button('Reset').disabled).toBe(true)
  })

  it('preserves permission guards and plain-editor creation/preflight behavior', () => {
    render({ value: 'edited', applyDisabled: true, applyDisabledReason: 'Read-only cluster' })
    expect(button('Apply').disabled).toBe(true)
    expect(container.textContent).toContain('Read-only cluster')
    render({ original: undefined, applyDisabled: false, applyDisabledReason: undefined })
    expect(container.querySelector('[data-testid="plain-editor"]')).not.toBeNull()
    expect(button('Apply').disabled).toBe(false)
    expect(monaco.edits.size).toBe(0)
    act(() => button('Apply').click())
    expect(props.onApply).toHaveBeenCalledOnce()
  })
  it('detaches the comparison before disposing its retained models on unmount', () => {
    render({ original: 'kind: Pod', value: 'kind: Pod' })
    monaco.detach.mockClear()
    monaco.releaseOriginal.mockClear()
    monaco.releaseDraft.mockClear()
    render({ original: undefined })
    act(() => root.unmount())
    expect(monaco.detach).toHaveBeenCalledWith(null)
    expect(monaco.releaseOriginal).toHaveBeenCalledTimes(1)
    expect(monaco.releaseDraft).toHaveBeenCalledTimes(1)
    expect(monaco.detach.mock.invocationCallOrder[0]).toBeLessThan(
      monaco.releaseOriginal.mock.invocationCallOrder[0],
    )
  })
})
