/** @vitest-environment jsdom */
import { beforeEach, expect, it } from 'vitest'
import { createSessionDockStore } from './store'
import {
  normalizeRealtimeSession,
  sessionReferenceMatchesScope,
  type RealtimeSessionReference,
} from './types'
beforeEach(() => localStorage.clear())

const reference: RealtimeSessionReference = {
  applicationId: 'app-a',
  viewEnvironmentId: 'view-env',
  sourceEnvironmentId: 'source-env',
  serviceId: 'svc',
  referenceId: 'ref-1',
  referenceRevision: 1,
  targetId: 'target-1',
  rootUid: 'root-uid',
}
const referencedInput = {
  kind: 'logs' as const,
  clusterId: 'c',
  namespace: 'ns',
  podName: 'pod',
  podUid: 'pod-uid',
  reference,
}
const deliveryScope = JSON.stringify([reference.applicationId, reference.viewEnvironmentId])
const deliveryKey = `soha-live-sessions:v3:${JSON.stringify(['a', 'delivery', deliveryScope])}`
const platformKey = `soha-live-sessions:v2:${JSON.stringify(['a', 'platform'])}`

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

it('keeps platform v2 and delivery v3 storage keys backward compatible', () => {
  const local = normalizeRealtimeSession({
    kind: 'logs',
    clusterId: 'c',
    namespace: 'ns',
    podName: 'pod',
  })!
  createSessionDockStore('platform', 'a').setState({ sessions: [local] })
  createSessionDockStore('delivery', 'a', deliveryScope).setState({ sessions: [local] })
  expect(localStorage.getItem(platformKey)).not.toBeNull()
  expect(localStorage.getItem(deliveryKey)).not.toBeNull()
  expect(createSessionDockStore('platform', 'a').getState().sessions).toEqual([local])
  expect(createSessionDockStore('delivery', 'a', deliveryScope).getState().sessions).toEqual([
    local,
  ])
})

it('persists allowlisted reference fields without runtime hints, tickets or output', () => {
  const store = createSessionDockStore('delivery', 'a', deliveryScope)
  const session = normalizeRealtimeSession({
    ...referencedInput,
    streamingDisabledReason: 'runtime-secret',
    ticket: 'ticket-secret',
    output: 'output-secret',
  } as typeof referencedInput & {
    streamingDisabledReason: string
    ticket: string
    output: string
  })!
  store.setState({ sessions: [session] })
  const saved = localStorage.getItem(deliveryKey)!
  expect(saved).toContain('"applicationId":"app-a"')
  expect(saved).toContain('"viewEnvironmentId":"view-env"')
  expect(saved).toContain('"sourceEnvironmentId":"source-env"')
  expect(saved).toContain('"serviceId":"svc"')
  expect(saved).toContain('"referenceId":"ref-1"')
  expect(saved).toContain('"referenceRevision":1')
  expect(saved).toContain('"targetId":"target-1"')
  expect(saved).toContain('"rootUid":"root-uid"')
  expect(saved).toContain('"podUid":"pod-uid"')
  expect(saved).not.toContain('runtime-secret')
  expect(saved).not.toContain('ticket-secret')
  expect(saved).not.toContain('output-secret')
  const [restored] = createSessionDockStore('delivery', 'a', deliveryScope).getState().sessions
  expect(restored.reference).toEqual(reference)
  expect('ticket' in restored).toBe(false)
  expect('output' in restored).toBe(false)
})

it('includes reference revision and split identities in the session id', () => {
  const session = normalizeRealtimeSession(referencedInput)!
  expect(session.reference).toEqual(reference)
  const ids = [
    session.id,
    normalizeRealtimeSession({
      ...referencedInput,
      reference: { ...reference, applicationId: 'app-b' },
    })!.id,
    normalizeRealtimeSession({
      ...referencedInput,
      reference: { ...reference, viewEnvironmentId: 'view-b' },
    })!.id,
    normalizeRealtimeSession({
      ...referencedInput,
      reference: { ...reference, sourceEnvironmentId: 'source-b' },
    })!.id,
    normalizeRealtimeSession({
      ...referencedInput,
      reference: { ...reference, serviceId: 'svc-b' },
    })!.id,
    normalizeRealtimeSession({
      ...referencedInput,
      reference: { ...reference, referenceId: 'ref-b' },
    })!.id,
    normalizeRealtimeSession({
      ...referencedInput,
      reference: { ...reference, referenceRevision: 2 },
    })!.id,
    normalizeRealtimeSession({
      ...referencedInput,
      reference: { ...reference, targetId: 'target-b' },
    })!.id,
    normalizeRealtimeSession({
      ...referencedInput,
      reference: { ...reference, rootUid: 'root-b' },
    })!.id,
  ]
  expect(new Set(ids).size).toBe(ids.length)
  expect(normalizeRealtimeSession({ ...referencedInput, reference: undefined })!.id).not.toBe(
    session.id,
  )
})

it('rejects incomplete, split-collapsed or unsafe reference identities', () => {
  const invalid = [
    { ...referencedInput, podUid: undefined },
    { ...referencedInput, podUid: ' ' },
    { ...referencedInput, reference: { ...reference, applicationId: ' ' } },
    { ...referencedInput, reference: { ...reference, viewEnvironmentId: '' } },
    { ...referencedInput, reference: { ...reference, serviceId: '\t' } },
    { ...referencedInput, reference: { ...reference, referenceId: '' } },
    { ...referencedInput, reference: { ...reference, targetId: ' ' } },
    { ...referencedInput, reference: { ...reference, rootUid: '' } },
    {
      ...referencedInput,
      reference: { ...reference, sourceEnvironmentId: reference.viewEnvironmentId },
    },
    { ...referencedInput, reference: { ...reference, referenceRevision: 0 } },
    { ...referencedInput, reference: { ...reference, referenceRevision: -1 } },
    { ...referencedInput, reference: { ...reference, referenceRevision: 1.5 } },
    { ...referencedInput, reference: { ...reference, referenceRevision: Number.NaN } },
    {
      ...referencedInput,
      reference: { ...reference, referenceRevision: Number.POSITIVE_INFINITY },
    },
    {
      ...referencedInput,
      reference: { ...reference, referenceRevision: Number.MAX_SAFE_INTEGER + 1 },
    },
  ]
  for (const input of invalid) expect(normalizeRealtimeSession(input)).toBeNull()
})

it('matches referenced sessions only inside their delivery view scope', () => {
  expect(sessionReferenceMatchesScope(undefined, 'platform')).toBe(true)
  expect(sessionReferenceMatchesScope(undefined, 'delivery')).toBe(true)
  expect(sessionReferenceMatchesScope(reference, 'platform', deliveryScope)).toBe(false)
  expect(sessionReferenceMatchesScope(reference, 'delivery')).toBe(false)
  expect(sessionReferenceMatchesScope(reference, 'delivery', 'app-a/view-env')).toBe(false)
  expect(sessionReferenceMatchesScope(reference, 'delivery', deliveryScope)).toBe(true)
})

it('discards malformed and foreign references without downgrading them to local sessions', () => {
  const valid = normalizeRealtimeSession(referencedInput)!
  const foreign = normalizeRealtimeSession({
    ...referencedInput,
    reference: { ...reference, viewEnvironmentId: 'other-view' },
  })!
  const foreignApplication = normalizeRealtimeSession({
    ...referencedInput,
    reference: { ...reference, applicationId: 'other-app' },
  })!
  localStorage.setItem(
    deliveryKey,
    JSON.stringify({
      state: {
        sessions: [
          valid,
          foreign,
          foreignApplication,
          { ...valid, reference: { ...reference, referenceRevision: '1' } },
          { ...valid, reference: { ...reference, rootUid: '' } },
        ],
      },
    }),
  )
  expect(createSessionDockStore('delivery', 'a', deliveryScope).getState().sessions).toEqual([
    valid,
  ])

  localStorage.setItem(platformKey, JSON.stringify({ state: { sessions: [valid] } }))
  expect(createSessionDockStore('platform', 'a').getState().sessions).toEqual([])
})

it('deduplicates referenced ids but keeps revision variants distinct', () => {
  const first = normalizeRealtimeSession(referencedInput)!
  const second = normalizeRealtimeSession({
    ...referencedInput,
    reference: { ...reference, referenceRevision: 2 },
  })!
  localStorage.setItem(deliveryKey, JSON.stringify({ state: { sessions: [first, first, second] } }))
  const sessions = createSessionDockStore('delivery', 'a', deliveryScope).getState().sessions
  expect(sessions.map((session) => session.id)).toEqual([first.id, second.id])
})
