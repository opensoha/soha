/** @vitest-environment jsdom */
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { Pod } from '@/types'
import { mergePodObservations, usePodObservations } from './use-pod-observations'
import { workloadKeys } from './keys'

const state = vi.hoisted(() => ({ permissions: ['platform.pods.view'], read: vi.fn() }))
vi.mock('@/features/auth', async (original) => ({
  ...(await original<typeof import('@/features/auth')>()),
  usePermissionSnapshot: () => ({ data: { data: { permissionKeys: state.permissions } } }),
}))
vi.mock('../pods/api', () => ({ listPods: (...args: unknown[]) => state.read(...args) }))

let client: QueryClient
let root: ReturnType<typeof createRoot>
let container: HTMLDivElement
let result: ReturnType<typeof usePodObservations>
const scope = { clusterId: 'cluster-a', namespace: 'apps' }
function Probe({ active = true }: { active?: boolean }) {
  result = usePodObservations(scope, [pod('api')], active)
  return null
}
async function render(active = true) {
  await act(async () => {
    root.render(
      <QueryClientProvider client={client}>
        <Probe active={active} />
      </QueryClientProvider>,
    )
    await new Promise((resolve) => setTimeout(resolve, 0))
  })
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 0))
  })
}
beforeEach(() => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  state.permissions = ['platform.pods.view']
  state.read.mockReset().mockResolvedValue([{ ...pod('api'), cpu: '125m' }])
  client = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } })
  container = document.createElement('div')
  document.body.append(container)
  root = createRoot(container)
})
afterEach(async () => {
  await act(async () => root.unmount())
  client.clear()
  container.remove()
  vi.unstubAllGlobals()
})

const pod = (name: string, namespace = 'apps'): Pod => ({
  name,
  namespace,
  phase: 'Running',
  readyContainers: '1/1',
  restarts: 0,
  ageSeconds: 60,
})
it('keeps controller membership and order while joining observations by namespace and name', () => {
  const members = [pod('worker'), pod('api')]
  const result = mergePodObservations(members, [
    { ...pod('api', 'other'), cpu: '999m' },
    { ...pod('api'), cpu: '125m', memory: '128Mi' },
    { ...pod('unrelated'), cpu: '2' },
  ])
  expect(result.map((item) => item.name)).toEqual(['worker', 'api'])
  expect(result[0]).toBe(members[0])
  expect(result[1].cpu).toBe('125m')
  expect(result[1].memory).toBe('128Mi')
  expect(members[1].cpu).toBeUndefined()
})
it('uses actual container observations and authoritative row permissions without inferring missing actions', () => {
  const result = mergePodObservations(
    [{ ...pod('api'), allowedActions: ['logs'] }],
    [
      {
        ...pod('api'),
        containerStatuses: [
          { name: 'init', kind: 'init', ready: false, state: 'terminated', exitCode: 0 },
          { name: 'app', kind: 'container', ready: true, state: 'running' },
        ],
      },
    ],
  )
  expect(result[0].containerStatuses?.map((item) => item.kind)).toEqual(['init', 'container'])
  expect(result[0].allowedActions).toBeUndefined()
})
it('keeps unknown usage unknown and preserves resource configuration when observations omit it', () => {
  const configured = { ...pod('api'), requests: { cpu: '50m' }, cpu: '10m' }
  const [result] = mergePodObservations([configured], [pod('api')])
  expect(result.requests).toEqual({ cpu: '50m' })
  expect(result.cpu).toBeUndefined()
  expect(result.memory).toBeUndefined()
})

it('reads observations only when the child tab is active and reuses the scoped Pod query', async () => {
  await render(false)
  expect(state.read).not.toHaveBeenCalled()
  await render()
  expect(state.read).toHaveBeenCalledExactlyOnceWith(scope)
  expect(result.pods[0].cpu).toBe('125m')
  expect(client.getQueryData(workloadKeys.list('pods', scope))).toEqual([
    { ...pod('api'), cpu: '125m' },
  ])
  await act(async () => {
    await client.invalidateQueries({ queryKey: workloadKeys.list('pods', scope) })
  })
  expect(state.read).toHaveBeenCalledTimes(2)
})
it('does not read or expose cached observations after Pod permission is removed', async () => {
  client.setQueryData(workloadKeys.list('pods', scope), [
    { ...pod('api'), cpu: '2', allowedActions: ['logs'] },
  ])
  state.permissions = []
  await render()
  expect(state.read).not.toHaveBeenCalled()
  expect(result.query.isEnabled).toBe(false)
  expect(result.pods).toEqual([pod('api')])
})
it('keeps the parent membership readable when the supplemental read fails', async () => {
  state.read.mockRejectedValue(new Error('Pod usage unavailable'))
  await render()
  expect(result.query.isError).toBe(true)
  expect(result.query.error?.message).toBe('Pod usage unavailable')
  expect(result.pods).toEqual([pod('api')])
})
