/** @vitest-environment jsdom */

import { act, StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { useYamlDraft } from './use-yaml-draft'

let container: HTMLDivElement
let root: ReturnType<typeof createRoot>
let draft: ReturnType<typeof useYamlDraft>

function Harness(props: { resourceKey: string; server?: string; storageKey?: string }) {
  draft = useYamlDraft(props.resourceKey, props.server, props.storageKey)
  return null
}

function render(server?: string, resourceKey = 'deployment-a', storageKey?: string) {
  act(() =>
    root.render(
      <StrictMode>
        <Harness {...{ server, resourceKey, storageKey }} />
      </StrictMode>,
    ),
  )
}

beforeEach(() => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  localStorage.clear()
  container = document.createElement('div')
  document.body.appendChild(container)
  root = createRoot(container)
})

afterEach(() => {
  act(() => root.unmount())
  container.remove()
  localStorage.clear()
  vi.unstubAllGlobals()
})

describe('YAML draft lifecycle', () => {
  it('loads the baseline and follows refreshes until editing starts', () => {
    render()
    expect(draft.original).toBeUndefined()
    render('replicas: 1')
    expect(draft.value).toBe('replicas: 1')
    render('replicas: 2')
    expect(draft.original).toBe('replicas: 2')
    expect(draft.value).toBe('replicas: 2')
    expect(draft.serverChanged).toBe(false)
  })

  it('preserves both the draft and baseline on refresh until explicitly comparing latest', () => {
    render('replicas: 1')
    act(() => draft.setValue('replicas: 3'))
    render('replicas: 2')
    expect(draft).toMatchObject({
      value: 'replicas: 3',
      original: 'replicas: 1',
      serverChanged: true,
    })
    act(() => draft.compareLatest())
    expect(draft).toMatchObject({
      value: 'replicas: 3',
      original: 'replicas: 2',
      serverChanged: false,
    })
    render('replicas: 4')
    act(() => draft.reset())
    expect(draft).toMatchObject({
      value: 'replicas: 4',
      original: 'replicas: 4',
      serverChanged: false,
    })
  })

  it('restores the saved draft without making it the server baseline and clears it on reset', () => {
    localStorage.setItem('draft-a', 'saved draft')
    render(undefined, 'deployment-a', 'draft-a')
    render('server baseline', 'deployment-a', 'draft-a')
    expect(draft).toMatchObject({ value: 'saved draft', original: 'server baseline' })
    act(() => draft.reset())
    expect(draft.value).toBe('server baseline')
    expect(localStorage.getItem('draft-a')).toBeNull()
  })

  it('isolates resource changes and ignores a late apply response for the previous resource', () => {
    render('resource A')
    act(() => draft.setValue('draft A'))
    const previous = draft
    render(undefined, 'deployment-b')
    expect(draft.value).toBe('')
    expect(draft.original).toBeUndefined()
    render('resource B', 'deployment-b')
    act(() => previous.applied('applied A'))
    act(() => previous.setValue('late A'))
    expect(draft).toMatchObject({
      resourceKey: 'deployment-b',
      value: 'resource B',
      original: 'resource B',
    })
  })

  it('uses the accepted apply response as the baseline while the query cache catches up', () => {
    render('old server', 'deployment-a', 'draft-a')
    act(() => draft.setValue('edited'))
    localStorage.setItem('draft-a', 'edited')
    act(() => draft.applied('accepted by server'))
    render('old server', 'deployment-a', 'draft-a')
    expect(draft.value).toBe('accepted by server')
    expect(draft.original).toBe('accepted by server')
    expect(draft.serverChanged).toBe(false)
    expect(localStorage.getItem('draft-a')).toBeNull()
    render('accepted by server', 'deployment-a', 'draft-a')
    expect(draft.serverChanged).toBe(false)
  })

  it('becomes clean when a refresh confirms the draft is already on the server', () => {
    render('original')
    act(() => draft.setValue('edited'))
    render('edited')
    expect(draft).toMatchObject({ value: 'edited', original: 'edited', serverChanged: false })
  })
})
