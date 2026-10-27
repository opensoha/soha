/** @vitest-environment jsdom */

import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, expect, it, vi } from 'vitest'
import type { Pod } from './types'
import { usePodListViewport } from './use-pod-list-viewport'

const scrollTo = vi.fn()
let viewport: ReturnType<typeof usePodListViewport>
let root: ReturnType<typeof createRoot>
const container = document.createElement('div')
const pod = (name: string) => ({ name, namespace: 'monitoring' }) as Pod

function Harness({ pods, resetKey }: { pods: Pod[]; resetKey: string }) {
  viewport = usePodListViewport(pods, resetKey)
  Object.assign(viewport.tableRef, { current: { scrollTo, nativeElement: container } })
  return null
}

async function render(pods: Pod[], resetKey = 'cluster-a') {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  root ??= createRoot(container)
  await act(async () => root.render(<Harness pods={pods} resetKey={resetKey} />))
}

function scroll() {
  const holder = document.createElement('div')
  holder.scrollTop = 2000
  holder.getBoundingClientRect = () => ({ top: 100 }) as DOMRect
  const row = document.createElement('div')
  row.dataset.rowKey = 'monitoring/pod-50'
  row.getBoundingClientRect = () => ({ top: 92, bottom: 132 }) as DOMRect
  holder.append(row)
  viewport.onScroll({ currentTarget: holder } as Parameters<typeof viewport.onScroll>[0])
}

afterEach(async () => {
  await act(async () => root.unmount())
  root = undefined as unknown as typeof root
  scrollTo.mockClear()
  vi.unstubAllGlobals()
})

it('restores the resource key and clipped offset after an earlier row is inserted', async () => {
  await render([pod('pod-50'), pod('pod-51')])
  scroll()
  await render([pod('pod-01'), pod('pod-50'), pod('pod-51')])
  expect(scrollTo).toHaveBeenLastCalledWith({
    key: 'monitoring/pod-50',
    align: 'start',
    offset: -8,
  })
})

it('falls back to the previous offset when the anchor is deleted', async () => {
  await render([pod('pod-50'), pod('pod-51')])
  scroll()
  await render([pod('pod-51')])
  expect(scrollTo).toHaveBeenLastCalledWith({ top: 2000 })
})

it('resets on scope or filter changes and forgets the old anchor', async () => {
  await render([pod('pod-50')])
  scroll()
  await render([pod('pod-50')], 'cluster-b:query')
  expect(scrollTo).toHaveBeenLastCalledWith({ top: 0 })
  scrollTo.mockClear()
  await render([pod('pod-50')], 'cluster-b:query')
  expect(scrollTo).not.toHaveBeenCalled()
})

it('forgets the anchor for sorting and when all readable rows disappear', async () => {
  await render([pod('pod-50')])
  scroll()
  viewport.resetPosition()
  expect(scrollTo).toHaveBeenLastCalledWith({ top: 0 })
  scrollTo.mockClear()
  await render([pod('pod-50')])
  expect(scrollTo).not.toHaveBeenCalled()
  scroll()
  await render([])
  expect(scrollTo).toHaveBeenLastCalledWith({ top: 0 })
})
