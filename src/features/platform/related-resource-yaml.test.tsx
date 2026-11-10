/** @vitest-environment jsdom */
import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { RelatedResourceYaml } from './related-resource-yaml'

const state = vi.hoisted(() => ({ denied: false, load: vi.fn() }))
vi.mock('@/features/auth', () => ({
  usePermissionSnapshot: () => ({ data: { data: {} } }),
  hasPermission: () => !state.denied,
}))
vi.mock('./workloads/shared/queries', () => ({
  workloadQueries: {
    yaml: () => ({ queryKey: ['yaml-identity'], queryFn: state.load, enabled: true }),
  },
}))
let root: ReturnType<typeof createRoot>, client: QueryClient
const target = { clusterId: 'cluster', namespace: 'team', kind: 'Deployment', name: 'api' }
const content = (uid: string) =>
  JSON.stringify({
    metadata: { name: 'api', namespace: 'team', uid },
    spec: { image: 'current-private-value' },
  })
beforeEach(() => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  state.denied = false
  state.load.mockReset()
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  const host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
})
afterEach(async () => {
  await act(async () => root.unmount())
  client.clear()
  document.body.innerHTML = ''
  vi.unstubAllGlobals()
})
async function render(uid?: string, fresh = false) {
  await act(async () =>
    root.render(
      <QueryClientProvider client={client}>
        <RelatedResourceYaml target={target} expectedUid={uid} fresh={fresh} />
      </QueryClientProvider>,
    ),
  )
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 25))
  })
}
it('does not display YAML for a replaced or missing resource UID', async () => {
  state.load.mockResolvedValue({ content: content('new-uid') })
  await render('old-uid')
  expect(document.body.textContent).toContain('资源已重建或身份无法确认')
  expect(document.body.textContent).not.toContain('current-private-value')
})
it('displays YAML after a fresh response verifies UID, name and namespace', async () => {
  state.load.mockResolvedValue({ content: content('expected') })
  await render('expected')
  expect(document.querySelector('[aria-label="资源 YAML"]')?.textContent).toContain(
    'current-private-value',
  )
})
it('hides cached YAML until fresh identity validation finishes', async () => {
  client.setQueryData(['yaml-identity'], { content: content('expected') })
  state.load.mockImplementation(() => new Promise(() => {}))
  await render('expected')
  expect(document.body.textContent).not.toContain('current-private-value')
})
it('retains existing unpinned read behavior and never reads without permission', async () => {
  state.load.mockResolvedValue({ content: 'existing read-only content' })
  await render()
  expect(document.body.textContent).toContain('existing read-only content')
  state.denied = true
  await render('expected')
  expect(document.body.textContent).not.toContain('existing read-only content')
  expect(state.load).toHaveBeenCalledTimes(1)
})

it('forces a fresh response before showing a deliberately unpinned current-name lookup', async () => {
  client.setQueryData(['yaml-identity'], { content: 'cached-object' })
  let resolve!: (value: { content: string }) => void
  state.load.mockImplementation(
    () =>
      new Promise((done) => {
        resolve = done
      }),
  )
  await render(undefined, true)
  expect(document.body.textContent).not.toContain('cached-object')
  await act(async () => resolve({ content: 'current-object' }))
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 25))
  })
  expect(document.body.textContent).toContain('current-object')
})
