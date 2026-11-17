/** @vitest-environment jsdom */
import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { useResourceListViewport } from './use-resource-list-viewport'

type Row = { id: string }
const scrollTo = vi.fn()
let viewport: ReturnType<typeof useResourceListViewport<Row>>
let root: ReturnType<typeof createRoot>
let container: HTMLDivElement
let frame: FrameRequestCallback | undefined
function Harness({ rows, scope }: { rows: Row[]; scope: string }) {
  viewport = useResourceListViewport(rows, 'id', scope)
  Object.assign(viewport.tableRef, { current: { scrollTo } })
  return null
}
async function render(ids: string[], scope = 'cluster-a') {
  await act(async () => root.render(<Harness rows={ids.map((id) => ({ id }))} scope={scope} />))
}
function scroll(capture = true) {
  const holder = document.createElement('div')
  holder.scrollTop = 2000
  holder.getBoundingClientRect = () => ({ top: 100 }) as DOMRect
  const row = document.createElement('div')
  row.dataset.rowKey = 'resource-50'
  row.getBoundingClientRect = () => ({ top: 92, bottom: 132 }) as DOMRect
  holder.append(row)
  container.append(holder)
  viewport.onScroll({ currentTarget: holder } as Parameters<typeof viewport.onScroll>[0])
  if (capture) {
    frame?.(0)
    frame = undefined
  }
  return row
}
beforeEach(() => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  vi.stubGlobal('requestAnimationFrame', (callback: FrameRequestCallback) => {
    frame = callback
    return 1
  })
  vi.stubGlobal('cancelAnimationFrame', () => {
    frame = undefined
  })
  container = document.createElement('div')
  document.body.append(container)
  root = createRoot(container)
})
afterEach(async () => {
  await act(async () => root.unmount())
  container.remove()
  scrollTo.mockClear()
  vi.unstubAllGlobals()
})
it('captures the committed virtual row window and restores its key and clipped offset', async () => {
  await render(['resource-50', 'resource-51'])
  const row = scroll(false)
  row.dataset.rowKey = 'resource-51'
  frame?.(0)
  await render(['resource-01', 'resource-50', 'resource-51'])
  expect(scrollTo).toHaveBeenLastCalledWith({ key: 'resource-51', align: 'start', offset: -8 })
})
it('does not restore on a same-order refresh, including one before the capture frame', async () => {
  await render(['resource-50', 'resource-51'])
  scroll()
  await render(['resource-50', 'resource-51'])
  expect(scrollTo).not.toHaveBeenCalled()
  scroll(false)
  await render(['resource-50', 'resource-51'])
  expect(scrollTo).not.toHaveBeenCalled()
})
it('restores a locally sorted anchor that stays mounted at a different offset', async () => {
  await render(['resource-50', 'resource-51'])
  const row = scroll()
  row.getBoundingClientRect = () => ({ top: 132, bottom: 172 }) as DOMRect
  await render(['resource-50', 'resource-51'])
  expect(scrollTo).toHaveBeenLastCalledWith({ key: 'resource-50', align: 'start', offset: -8 })
})
it('restores a locally sorted anchor that moves out of the mounted window', async () => {
  await render(['resource-50', 'resource-51'])
  const row = scroll()
  row.dataset.rowKey = 'resource-51'
  await render(['resource-50', 'resource-51'])
  expect(scrollTo).toHaveBeenLastCalledWith({ key: 'resource-50', align: 'start', offset: -8 })
})
it('does not mistake a pending user scroll delta for a local-sort change', async () => {
  await render(['resource-50', 'resource-51'])
  const row = scroll()
  const holder = row.parentElement!
  holder.scrollTop = 2024
  row.getBoundingClientRect = () => ({ top: 68, bottom: 108 }) as DOMRect
  viewport.onScroll({ currentTarget: holder } as Parameters<typeof viewport.onScroll>[0])
  await render(['resource-50', 'resource-51'])
  expect(scrollTo).not.toHaveBeenCalled()
})
it('keeps the latest user delta when local sorting moves the anchor before capture', async () => {
  await render(['resource-50', 'resource-51'])
  const row = scroll()
  const holder = row.parentElement!
  holder.scrollTop = 2160
  viewport.onScroll({ currentTarget: holder } as Parameters<typeof viewport.onScroll>[0])
  row.dataset.rowKey = 'resource-51'
  await render(['resource-50', 'resource-51'])
  expect(scrollTo).toHaveBeenLastCalledWith({ key: 'resource-50', align: 'start', offset: -168 })
})
it.each([
  ['resource-01', 'resource-50', 'resource-51'],
  ['resource-50'],
  ['resource-51', 'resource-50'],
])('retains the anchor after keys change to %j', async (...ids) => {
  await render(['resource-50', 'resource-51'])
  scroll()
  await render(ids)
  expect(scrollTo).toHaveBeenLastCalledWith({ key: 'resource-50', align: 'start', offset: -8 })
})
it('preserves a newer user scroll delta when rows change before the next capture', async () => {
  await render(['resource-50', 'resource-51'])
  const row = scroll()
  const holder = row.parentElement!
  holder.scrollTop = 2160
  viewport.onScroll({ currentTarget: holder } as Parameters<typeof viewport.onScroll>[0])
  await render(['resource-01', 'resource-50', 'resource-51'])
  expect(scrollTo).toHaveBeenLastCalledWith({ key: 'resource-50', align: 'start', offset: -168 })
})
it('preserves the clipped anchor when a preceding resource disappears', async () => {
  await render(['resource-01', 'resource-50', 'resource-51'])
  scroll()
  await render(['resource-50', 'resource-51'])
  expect(scrollTo).toHaveBeenLastCalledWith({ key: 'resource-50', align: 'start', offset: -8 })
})
it('keeps the latest offset if a refresh precedes the first anchor capture', async () => {
  await render(['resource-50', 'resource-51'])
  scroll(false)
  await render(['resource-01', 'resource-50', 'resource-51'])
  expect(scrollTo).toHaveBeenLastCalledWith({ top: 2000 })
})
it('falls back to the previous offset when the resource disappears', async () => {
  await render(['resource-50', 'resource-51'])
  scroll()
  await render(['resource-51'])
  expect(scrollTo).toHaveBeenLastCalledWith({ top: 2000 })
})
it('resets scope and filters and cancels a pending old-scope capture', async () => {
  await render(['resource-50'])
  scroll(false)
  await render(['resource-50'], 'cluster-b:query')
  expect(frame).toBeUndefined()
  expect(scrollTo).toHaveBeenLastCalledWith({ top: 0 })
  scrollTo.mockClear()
  await render(['resource-50'], 'cluster-b:query')
  expect(scrollTo).not.toHaveBeenCalled()
})
it('forgets the anchor on sorting and when readable results disappear', async () => {
  await render(['resource-50'])
  scroll()
  viewport.resetPosition()
  expect(scrollTo).toHaveBeenLastCalledWith({ top: 0 })
  scrollTo.mockClear()
  await render(['resource-50'])
  expect(scrollTo).not.toHaveBeenCalled()
  scroll()
  await render([])
  expect(scrollTo).toHaveBeenLastCalledWith({ top: 0 })
})
