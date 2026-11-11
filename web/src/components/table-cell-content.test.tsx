/** @vitest-environment jsdom */

import { act, type ReactNode } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest'
import { TableCellLink, TableCellText } from './table-cell-content'

let root: ReturnType<typeof createRoot>

beforeAll(() => {
  Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
})

afterEach(async () => {
  await act(async () => root?.unmount())
  document.body.replaceChildren()
  vi.restoreAllMocks()
})

async function renderCell(node: ReactNode, overflow: boolean) {
  vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(function (
    this: HTMLElement,
  ) {
    const right = this.tagName === 'EM' ? (overflow ? 200 : 20) : 100
    return { left: 0, top: 0, bottom: 20, right, width: right, height: 20 } as DOMRect
  })
  const container = document.createElement('div')
  document.body.appendChild(container)
  root = createRoot(container)
  await act(async () => root.render(node))
  const text = container.querySelector('.ant-typography')!
  await act(async () => {
    text.dispatchEvent(new MouseEvent('mouseover', { bubbles: true }))
  })
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 180))
  })
  return container
}

describe('table cell content', () => {
  it('does not show a tooltip for fully visible text or the empty placeholder', async () => {
    const container = await renderCell(<TableCellText value="" />, false)
    expect(container.textContent).toBe('-')
    expect(document.querySelector('[role="tooltip"]')).toBeNull()
    expect(container.querySelector('[title]')).toBeNull()
  })

  it('shows the full text only when the rendered text overflows', async () => {
    const container = await renderCell(<TableCellText value="long-value" />, true)
    expect(container.textContent).toBe('long-value')
    expect(document.querySelector('[role="tooltip"]')?.textContent).toBe('long-value')
  })

  it('keeps a fully visible name link clickable without a tooltip', async () => {
    const onClick = vi.fn()
    const container = await renderCell(
      <TableCellLink label="resource-name" onClick={onClick} />,
      false,
    )
    expect(document.querySelector('[role="tooltip"]')).toBeNull()
    await act(async () => container.querySelector('button')?.click())
    expect(onClick).toHaveBeenCalledOnce()
  })
})
