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
  vi.unstubAllGlobals()
})

async function renderCell(
  node: ReactNode,
  overflow: boolean,
  beforeHover?: (container: HTMLElement) => void,
) {
  vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(function (
    this: HTMLElement,
  ) {
    const right = this.tagName === 'EM' ? (overflow ? 200 : 20) : 100
    return { left: 0, top: 0, bottom: 20, right, width: right, height: 20 } as DOMRect
  })
  vi.spyOn(HTMLElement.prototype, 'scrollWidth', 'get').mockReturnValue(overflow ? 200 : 20)
  vi.spyOn(HTMLElement.prototype, 'clientWidth', 'get').mockReturnValue(100)
  const container = document.createElement('div')
  document.body.appendChild(container)
  root = createRoot(container)
  await act(async () => root.render(node))
  await act(async () => beforeHover?.(container))
  const text = container.querySelector('.soha-table-cell-text')!
  await act(async () => {
    text.dispatchEvent(new MouseEvent('mouseover', { bubbles: true }))
  })
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 180))
  })
  return container
}

describe('table cell content', () => {
  it('does not measure closed cells during mount or unrelated parent renders', async () => {
    const measure = vi.spyOn(HTMLElement.prototype, 'scrollWidth', 'get')
    const container = document.createElement('div')
    document.body.append(container)
    root = createRoot(container)
    await act(async () => root.render(<TableCellText value="resource-a" />))
    await act(async () => root.render(<TableCellText value="resource-a" />))
    expect(measure).not.toHaveBeenCalled()
    expect(container.textContent).toBe('resource-a')
  })
  it('closes an open tooltip when a column resize makes its text fit', async () => {
    const callbacks: (() => void)[] = []
    class Observer {
      constructor(private callback: ResizeObserverCallback) {}
      observe(target: Element) {
        if (target.classList.contains('soha-table-cell-text'))
          callbacks.push(() => this.callback([], this as unknown as ResizeObserver))
      }
      unobserve() {}
      disconnect() {}
    }
    vi.stubGlobal('ResizeObserver', Observer)
    const container = await renderCell(<TableCellText value="long-value" />, true)
    const text = container.querySelector('.soha-table-cell-text')!
    expect(text.getAttribute('aria-describedby')).toBeTruthy()
    expect(callbacks.length).toBeGreaterThan(0)
    vi.spyOn(HTMLElement.prototype, 'scrollWidth', 'get').mockReturnValue(20)
    await act(async () => callbacks.forEach((callback) => callback()))
    expect(text.getAttribute('aria-describedby')).toBeNull()
  })
  it('closes a visible tooltip when refreshed text becomes a placeholder', async () => {
    await renderCell(<TableCellText value="long-value" />, true)
    expect(document.querySelector('[role="tooltip"]')?.textContent).toBe('long-value')
    await act(async () => root.render(<TableCellText value={null} />))
    expect(document.querySelector('[role="tooltip"]')).toBeNull()
  })
  it('keeps a focused link button intact and clickable while first-hover mounts its text tooltip', async () => {
    const onClick = vi.fn()
    let original!: HTMLButtonElement
    const container = await renderCell(
      <TableCellLink label="long-resource-name" onClick={onClick} />,
      true,
      (host) => {
        original = host.querySelector('button')!
        original.focus()
      },
    )
    expect(container.querySelector('button')).toBe(original)
    expect(document.activeElement).toBe(original)
    expect(document.querySelector('[role="tooltip"]')?.textContent).toBe('long-resource-name')
    await act(async () => original.click())
    expect(onClick).toHaveBeenCalledOnce()
  })
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

  it('shows a truncated link name on first focus and dismisses it on Escape or blur', async () => {
    vi.spyOn(HTMLElement.prototype, 'scrollWidth', 'get').mockImplementation(function (
      this: HTMLElement,
    ) {
      return this.textContent === 'long-resource-name' ? 200 : 20
    })
    vi.spyOn(HTMLElement.prototype, 'clientWidth', 'get').mockReturnValue(100)
    const container = document.createElement('div')
    document.body.append(container)
    root = createRoot(container)
    await act(async () =>
      root.render(
        <>
          <a href="#long">
            <TableCellText value="long-resource-name" />
          </a>
          <a href="#short">
            <TableCellText value="short" />
          </a>
        </>,
      ),
    )
    const [longLink, shortLink] = container.querySelectorAll('a')
    await act(async () => longLink.focus())
    expect(document.activeElement).toBe(longLink)
    expect(document.querySelector('[role="tooltip"]')?.textContent).toBe('long-resource-name')
    await act(async () => {
      longLink.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    })
    expect(document.activeElement).toBe(longLink)
    expect(
      longLink.querySelector('.soha-table-cell-text')?.getAttribute('aria-describedby'),
    ).toBeNull()
    await act(async () => shortLink.focus())
    await act(async () => longLink.focus())
    expect(document.querySelector('[role="tooltip"]')?.textContent).toBe('long-resource-name')
    await act(async () => shortLink.focus())
    expect(
      longLink.querySelector('.soha-table-cell-text')?.getAttribute('aria-describedby'),
    ).toBeNull()
    expect(
      shortLink.querySelector('.soha-table-cell-text')?.getAttribute('aria-describedby'),
    ).toBeNull()
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
