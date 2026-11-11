/** @vitest-environment jsdom */

import { act } from 'react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createRoot, type Root } from 'react-dom/client'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { BrowserHandoffPage } from './browser-handoff-page'
import { completeBrowserHandoff, inspectBrowserHandoff } from './browser-handoff-api'

vi.mock('./browser-handoff-api', () => ({
  completeBrowserHandoff: vi.fn(),
  inspectBrowserHandoff: vi.fn(),
}))

const roots: Root[] = []

async function flushReact() {
  await act(async () => {
    await Promise.resolve()
    await Promise.resolve()
  })
}

async function waitForText(container: HTMLElement, text: string) {
  for (let attempt = 0; attempt < 50; attempt += 1) {
    if (container.textContent?.includes(text)) return
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 10))
    })
  }
}

async function renderPage(navigate = vi.fn()) {
  const container = document.createElement('div')
  document.body.appendChild(container)
  const root = createRoot(container)
  roots.push(root)
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })

  await act(async () => {
    root.render(
      <QueryClientProvider client={queryClient}>
        <MemoryRouter initialEntries={['/auth/browser-handoff/handoff-1']}>
          <Routes>
            <Route
              path="/auth/browser-handoff/:handoffId"
              element={<BrowserHandoffPage navigate={navigate} />}
            />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    )
  })
  await flushReact()
  return { container, navigate }
}

beforeAll(() => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  Object.defineProperty(window, 'matchMedia', {
    writable: true,
    value: vi.fn().mockImplementation(() => ({
      addEventListener: vi.fn(),
      addListener: vi.fn(),
      matches: false,
      removeEventListener: vi.fn(),
      removeListener: vi.fn(),
    })),
  })
})

beforeEach(() => {
  vi.clearAllMocks()
  vi.mocked(inspectBrowserHandoff).mockResolvedValue({
    status: 'pending',
    expiresAt: '2026-08-31T01:01:00Z',
    accountName: 'OpenSoha',
    application: {
      id: 'app-1',
      name: 'Soha Console',
    },
  })
  vi.mocked(completeBrowserHandoff).mockResolvedValue({
    status: 'completed',
    destinationUrl: '/portal',
  })
})

afterEach(async () => {
  await act(async () => {
    for (const root of roots.splice(0)) root.unmount()
  })
  document.body.innerHTML = ''
})

describe('browser handoff page', () => {
  it('shows the bound App account and does not complete on page load', async () => {
    const { container } = await renderPage()
    await waitForText(container, 'OpenSoha')

    expect(container.textContent).toContain('OpenSoha')
    expect(container.textContent).not.toContain('@soha.local')
    expect(container.textContent).toContain('Soha Console')
    expect(completeBrowserHandoff).not.toHaveBeenCalled()
  })

  it('completes only after explicit confirmation', async () => {
    const storageWrite = vi.spyOn(Storage.prototype, 'setItem')
    const { container, navigate } = await renderPage()
    await waitForText(container, 'OpenSoha')
    const continueButton = [...container.querySelectorAll('button')].find((button) =>
      button.textContent?.includes('继续'),
    )

    await act(async () => continueButton?.click())
    await waitForText(container, 'Soha Console')

    expect(completeBrowserHandoff).toHaveBeenCalledWith('handoff-1')
    expect(navigate).toHaveBeenCalledWith('http://localhost:3000/portal')
    expect(storageWrite).not.toHaveBeenCalled()
    storageWrite.mockRestore()
  })

  it('renders inspection failures without completing', async () => {
    vi.mocked(inspectBrowserHandoff).mockRejectedValueOnce(new Error('handoff expired'))
    const { container } = await renderPage()
    await waitForText(container, 'handoff expired')
    expect(container.textContent).toContain('handoff expired')
    expect(completeBrowserHandoff).not.toHaveBeenCalled()
  })

  it('keeps the confirmation page visible when the browser account conflicts', async () => {
    vi.mocked(completeBrowserHandoff).mockRejectedValueOnce(new Error('browser account conflict'))
    const { container, navigate } = await renderPage()
    await waitForText(container, 'OpenSoha')
    const continueButton = [...container.querySelectorAll('button')].find((button) =>
      button.textContent?.includes('继续'),
    )

    await act(async () => continueButton?.click())
    await waitForText(container, 'browser account conflict')

    expect(container.textContent).toContain('browser account conflict')
    expect(navigate).not.toHaveBeenCalled()
  })
})
