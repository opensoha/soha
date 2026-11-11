/** @vitest-environment jsdom */
import { beforeEach, expect, it } from 'vitest'
import { createSessionDockStore } from './store'
import { normalizeRealtimeSession } from './types'
beforeEach(() => localStorage.clear())
it('persists only descriptors and isolates users and workbenches', () => {
  const store = createSessionDockStore('platform', 'a')
  const session = normalizeRealtimeSession({
    kind: 'terminal',
    clusterId: 'c',
    namespace: 'ns',
    podName: 'pod',
    podUid: 'uid-original',
    streamingDisabledReason: 'private runtime hint',
  })!
  store.setState({
    sessions: [session],
    activeSessionKeys: { c: session.id },
    selectedClusterId: 'missing',
  })
  const saved = localStorage.getItem('soha-live-sessions:v2:["a","platform"]')!
  expect(saved).not.toContain('private runtime hint')
  expect(saved).toContain('uid-original')
  expect(normalizeRealtimeSession({ ...session, podUid: 'replacement' })?.id).not.toBe(session.id)
  expect(createSessionDockStore('platform', 'a').getState().sessions[0].podName).toBe('pod')
  expect(createSessionDockStore('platform', 'a').getState().selectedClusterId).toBe('c')
  expect(createSessionDockStore('platform', 'b').getState().sessions).toEqual([])
  expect(createSessionDockStore('delivery', 'a').getState().sessions).toEqual([])
  store.setState({ sessions: [] })
  expect(createSessionDockStore('platform', 'a').getState().sessions).toEqual([])
})
it('rejects corrupted descriptors and never persists anonymous sessions', () => {
  localStorage.setItem(
    'soha-live-sessions:v2:["a","platform"]',
    JSON.stringify({
      state: { sessions: [{ kind: 'terminal', clusterId: 3 }], activeSessionKeys: { bad: 'id' } },
    }),
  )
  expect(createSessionDockStore('platform', 'a').getState().sessions).toEqual([])
  const store = createSessionDockStore('platform')
  store.setState({
    sessions: [
      normalizeRealtimeSession({ kind: 'logs', clusterId: 'c', namespace: 'ns', podName: 'pod' })!,
    ],
  })
  expect(localStorage.getItem('soha-live-sessions:v2:["anonymous","platform"]')).toBeNull()
})

it('does not attribute legacy mixed sessions to either workbench', () => {
  const legacy = JSON.stringify({
    state: { sessions: [{ kind: 'logs', clusterId: 'c', namespace: 'ns', podName: 'pod' }] },
  })
  localStorage.setItem('soha-live-sessions:a', legacy)
  expect(createSessionDockStore('platform', 'a').getState().sessions).toEqual([])
  expect(createSessionDockStore('delivery', 'a').getState().sessions).toEqual([])
  expect(localStorage.getItem('soha-live-sessions:a')).toBe(legacy)
})

it('restores delivery tabs only for the same application and environment', () => {
  const session = normalizeRealtimeSession({
    kind: 'logs',
    clusterId: 'c',
    namespace: 'ns',
    podName: 'pod',
  })!
  createSessionDockStore('delivery', 'a', 'app-a/test').setState({ sessions: [session] })
  expect(createSessionDockStore('delivery', 'a', 'app-a/prod').getState().sessions).toEqual([])
  expect(createSessionDockStore('delivery', 'a', 'app-b/test').getState().sessions).toEqual([])
  expect(createSessionDockStore('delivery', 'a').getState().sessions).toEqual([])
  expect(createSessionDockStore('delivery', 'a', 'app-a/test').getState().sessions).toEqual([
    session,
  ])
})
