/** @vitest-environment jsdom */
import { act, type ReactNode } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest'
import { ServiceSelectorDisplay } from './selector-display'
import { ServicePortDisplay } from './port-display'
import { renderNetworkTextList } from '../shared/renderers'

const overlays = vi.hoisted(() => ({ popover: vi.fn(), tooltip: vi.fn() }))
vi.mock('antd', async (original) => {
  const actual = await original<typeof import('antd')>()
  return {
    ...actual,
    Popover: (props: import('antd').PopoverProps) => {
      overlays.popover()
      return <actual.Popover {...props} />
    },
    Tooltip: (props: import('antd').TooltipProps) => {
      overlays.tooltip()
      return <actual.Tooltip {...props} />
    },
  }
})

let root: Root
let container: HTMLDivElement
const flush = async () => {
  await new Promise((resolve) => setTimeout(resolve, 30))
}
async function render(node: ReactNode) {
  if (!container) {
    container = document.createElement('div')
    document.body.append(container)
    root = createRoot(container)
  }
  await act(async () => {
    root.render(<MemoryRouter>{node}</MemoryRouter>)
    await flush()
  })
}
beforeAll(() => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  vi.stubGlobal(
    'ResizeObserver',
    class {
      observe() {}
      unobserve() {}
      disconnect() {}
    },
  )
  Object.defineProperty(window, 'matchMedia', {
    value: () => ({
      matches: false,
      addListener() {},
      removeListener() {},
      addEventListener() {},
      removeEventListener() {},
    }),
  })
})
afterEach(async () => {
  await act(async () => root?.unmount())
  container?.remove()
  container = undefined!
  vi.clearAllMocks()
})

describe('resource list rich cells', () => {
  it('mounts the click-only selector on demand and retains links, Escape focus and identity close', async () => {
    const selector = { z: 'last', app: 'api', tier: 'backend' }
    await render(
      <ServiceSelectorDisplay
        compact
        selector={selector}
        backendPath="/backends/a"
        backendLabel="API backends"
      />,
    )
    expect(overlays.popover).not.toHaveBeenCalled()
    expect(container.querySelector('a')?.getAttribute('href')).toBe('/backends/a')
    expect(container.querySelectorAll('.soha-service-selector-tag')).toHaveLength(2)
    await act(async () => {
      container
        .querySelector('button')!
        .dispatchEvent(new MouseEvent('mouseover', { bubbles: true }))
      await flush()
    })
    expect(overlays.popover).not.toHaveBeenCalled()
    await act(async () => {
      container.querySelector('button')!.click()
      await flush()
    })
    expect(overlays.popover).toHaveBeenCalled()
    expect(container.querySelector('button')?.getAttribute('aria-expanded')).toBe('true')
    expect(document.querySelector('.soha-service-selector-expanded')?.textContent).toContain(
      'z=last',
    )
    await act(flush)
    expect(document.activeElement?.closest('.soha-service-selector-expanded')).not.toBeNull()
    await act(async () => {
      document.activeElement!.dispatchEvent(
        new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }),
      )
      await flush()
    })
    expect(container.querySelector('button')?.getAttribute('aria-expanded')).toBe('false')
    expect(document.activeElement).toBe(container.querySelector('button'))
    await act(async () => {
      container.querySelector('button')!.click()
      await flush()
    })
    expect(container.querySelector('button')?.getAttribute('aria-expanded')).toBe('true')
    await render(
      <ServiceSelectorDisplay
        compact
        selector={selector}
        backendPath="/backends/b"
        backendLabel="New backends"
      />,
    )
    expect(container.querySelector('button')?.getAttribute('aria-expanded')).toBe('false')
    expect(container.querySelector('a')?.getAttribute('href')).toBe('/backends/b')
    await act(async () => {
      container.querySelector('button')!.click()
      await flush()
    })
    await render(
      <ServiceSelectorDisplay compact selector={{ app: 'changed' }} backendPath="/backends/b" />,
    )
    expect(container.querySelector('button')?.getAttribute('aria-expanded')).toBe('false')
    expect(container.textContent).toContain('app=changed')
  })

  it('defers overflow Tooltip until focus, then preserves full values and Escape dismissal', async () => {
    await render(renderNetworkTextList(['one', 'two', 'three', 'four', 'five']))
    expect(overlays.tooltip).not.toHaveBeenCalled()
    await act(async () => {
      container.querySelector<HTMLElement>('[tabindex]')!.focus()
      await flush()
    })
    expect(overlays.tooltip).toHaveBeenCalled()
    expect(document.querySelector('[role="tooltip"]')?.textContent).toBe('four, five')
    expect(document.activeElement).toBe(container.querySelector('[tabindex]'))
    await act(async () => {
      document.activeElement!.dispatchEvent(
        new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }),
      )
      await flush()
    })
    expect(document.querySelector('.ant-tooltip-hidden')).not.toBeNull()
  })

  it('updates every port field and supports fallback raw ports without stale memoized content', async () => {
    await render(
      <ServicePortDisplay
        portMappings={[
          { name: 'api', protocol: 'TCP', port: 80, targetPort: 'http', nodePort: 30001 },
        ]}
      />,
    )
    expect(container.textContent).toContain('NodePort: 30001')
    await render(
      <ServicePortDisplay
        detailed
        portMappings={[
          { name: 'dns', protocol: 'UDP', port: 53, targetPort: 'dns', nodePort: 30053 },
        ]}
      />,
    )
    expect(container.textContent).not.toContain('30001')
    expect(container.querySelector('[data-port-field="name"]')?.textContent).toBe('dns')
    expect(container.querySelector('[data-port-field="protocol"]')?.textContent).toBe('UDP')
    expect(container.querySelector('[data-port-field="port"]')?.textContent).toBe('53')
    expect(container.querySelector('[data-port-field="targetPort"]')?.textContent).toBe('dns')
    expect(container.textContent).toContain('NodePort: 30053')
    await render(<ServicePortDisplay ports={['443/TCP']} />)
    expect(container.textContent).toBe('443/TCP')
  })
})
