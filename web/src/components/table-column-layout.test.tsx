/** @vitest-environment jsdom */
import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { useTableColumnLayout } from './table-column-layout'

let layout: ReturnType<typeof useTableColumnLayout>
let root: ReturnType<typeof createRoot>
let container: HTMLDivElement
function Harness({ layoutKey }: { layoutKey: string }) {
  layout = useTableColumnLayout(layoutKey)
  return null
}
async function render(layoutKey: string) {
  await act(async () => root.render(<Harness layoutKey={layoutKey} />))
}
beforeEach(() => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  localStorage.clear()
  container = document.createElement('div')
  document.body.append(container)
  root = createRoot(container)
})
afterEach(async () => {
  await act(async () => root.unmount())
  container.remove()
  vi.restoreAllMocks()
})

describe('opt-in table column preferences', () => {
  it('keeps callbacks stable for a layout and replaces them when the user key changes', async () => {
    await render('pods:user-a')
    const original = layout
    await render('pods:user-a')
    expect(layout.setWidth).toBe(original.setWidth)
    expect(layout.setVisible).toBe(original.setVisible)
    expect(layout.fit).toBe(original.fit)
    expect(layout.reset).toBe(original.reset)
    await act(async () => layout.setWidth('name', 320))
    expect(layout.setWidth).toBe(original.setWidth)
    await render('pods:user-b')
    expect(layout.setWidth).not.toBe(original.setWidth)
    await act(async () => layout.setWidth('name', 400))
    expect(JSON.parse(localStorage.getItem('soha-table-layout:pods:user-a')!).widths.name).toBe(320)
    expect(JSON.parse(localStorage.getItem('soha-table-layout:pods:user-b')!).widths.name).toBe(400)
  })
  it('keeps same-value state while persisting the final unchanged drag position', async () => {
    await render('pods:a')
    await act(async () => layout.setWidth('name', 360, false))
    const widths = layout.widths
    expect(localStorage.getItem('soha-table-layout:pods:a')).toBeNull()
    await act(async () => layout.setWidth('name', 360))
    expect(layout.widths).toBe(widths)
    expect(JSON.parse(localStorage.getItem('soha-table-layout:pods:a')!).widths.name).toBe(360)
    await act(async () => layout.setVisible(['name']))
    const visible = layout.visible
    await act(async () => layout.setVisible(['name']))
    expect(layout.visible).toBe(visible)
  })
  it('restores widths and visibility, isolates users, and resets explicit choices', async () => {
    await render('pods:user-a')
    await act(async () => {
      layout.setWidth('name', 320)
      layout.setVisible(['name', 'cpu'])
    })
    await render('pods:user-b')
    expect(layout.widths).toEqual({})
    expect(layout.visible).toBeUndefined()
    await render('pods:user-a')
    expect(layout.widths).toEqual({ name: 320 })
    expect(layout.visible).toEqual(['name', 'cpu'])
    await act(async () => layout.reset())
    expect(layout.widths).toEqual({})
    expect(layout.visible).toBeUndefined()
    await render('pods:user-b')
    await render('pods:user-a')
    expect(layout.widths).toEqual({})
  })
  it('persists a drag only when committed and fit keeps visibility', async () => {
    await render('pods:a')
    await act(async () => layout.setWidth('name', 360, false))
    expect(localStorage.getItem('soha-table-layout:pods:a')).toBeNull()
    await act(async () => {
      layout.setWidth('name', 368)
      layout.setVisible(['name'])
    })
    expect(JSON.parse(localStorage.getItem('soha-table-layout:pods:a')!).widths.name).toBe(368)
    await act(async () => layout.fit())
    expect(layout.widths).toEqual({})
    expect(layout.visible).toEqual(['name'])
  })
  it('rejects corrupt storage and invalid stored widths', async () => {
    localStorage.setItem('soha-table-layout:broken', '{')
    await render('broken')
    expect(layout.widths).toEqual({})
    localStorage.setItem(
      'soha-table-layout:invalid',
      JSON.stringify({
        version: 1,
        widths: { negative: -2, huge: 9000, text: '120', valid: 128 },
        visible: [1],
      }),
    )
    await render('invalid')
    expect(layout.widths).toEqual({ valid: 128 })
    expect(layout.visible).toBeUndefined()
  })
  it('retains usable in-memory preferences when storage is denied', async () => {
    await render('pods:a')
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new Error('Storage unavailable')
    })
    await act(async () => layout.setWidth('name', 300))
    expect(layout.widths.name).toBe(300)
  })
})
