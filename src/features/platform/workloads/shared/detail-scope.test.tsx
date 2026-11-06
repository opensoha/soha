/** @vitest-environment jsdom */
import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import { useIsCurrentDetailTarget, useWorkloadDetailScope } from './detail-scope'

let root: ReturnType<typeof createRoot>
let container: HTMLDivElement
function DetailScope() {
  return <output>{JSON.stringify(useWorkloadDetailScope())}</output>
}
beforeEach(() => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  usePlatformScopeStore.setState({ clusterId: 'cluster-a', namespace: null })
  container = document.createElement('div')
  document.body.append(container)
  root = createRoot(container)
})
afterEach(async () => {
  await act(async () => root.unmount())
  container.remove()
  usePlatformScopeStore.setState({ clusterId: null, namespace: null })
  vi.unstubAllGlobals()
})
async function render(search: string) {
  await act(async () =>
    root.render(
      <MemoryRouter initialEntries={['/workloads/pods/api' + search]}>
        <DetailScope />
      </MemoryRouter>,
    ),
  )
  return JSON.parse(container.querySelector('output')!.textContent!)
}
it.each([null, 'monitoring', 'other-team'])(
  'uses the explicit resource namespace without changing list scope %s',
  async (selected) => {
    usePlatformScopeStore.setState({ namespace: selected })
    expect(await render('?clusterId=cluster-a&namespace=monitoring')).toEqual({
      clusterId: 'cluster-a',
      namespace: 'monitoring',
    })
    expect(usePlatformScopeStore.getState().namespace).toBe(selected)
  },
)
it('falls back to the same-cluster selection only when the detail URL has no namespace', async () => {
  usePlatformScopeStore.setState({ namespace: 'team-a' })
  expect(await render('?clusterId=cluster-a')).toEqual({
    clusterId: 'cluster-a',
    namespace: 'team-a',
  })
})
it('does not borrow the previous cluster namespace for a different cluster', async () => {
  usePlatformScopeStore.setState({ namespace: 'team-a' })
  expect(await render('?clusterId=cluster-b')).toEqual({ clusterId: 'cluster-b', namespace: null })
  expect(usePlatformScopeStore.getState()).toMatchObject({
    clusterId: 'cluster-b',
    namespace: null,
  })
})
it('keeps an explicit cross-cluster detail target independent of the reset list scope', async () => {
  usePlatformScopeStore.setState({ namespace: 'team-a' })
  expect(await render('?clusterId=cluster-b&namespace=team-b')).toEqual({
    clusterId: 'cluster-b',
    namespace: 'team-b',
  })
  expect(usePlatformScopeStore.getState()).toMatchObject({
    clusterId: 'cluster-b',
    namespace: null,
  })
})

it('rejects a late completion after the detail target changes or unmounts', async () => {
  let checkCurrent!: () => boolean
  function Target({ identity }: { identity: string }) {
    checkCurrent = useIsCurrentDetailTarget(identity)
    return null
  }
  await act(async () => root.render(<Target identity="cluster-a/team-a/app" />))
  const first = checkCurrent
  expect(first()).toBe(true)
  await act(async () => root.render(<Target identity="cluster-b/team-b/app" />))
  const second = checkCurrent
  expect(first()).toBe(false)
  expect(second()).toBe(true)
  await act(async () => root.render(<Target identity="cluster-a/team-a/app" />))
  expect(first()).toBe(false)
  const third = checkCurrent
  expect(third()).toBe(true)
  await act(async () => root.render(null))
  expect(third()).toBe(false)
})
