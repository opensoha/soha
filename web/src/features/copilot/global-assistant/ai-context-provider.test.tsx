/** @vitest-environment jsdom */

import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { AIPageContextRegistry, useAIPageContext } from './ai-context-provider'

describe('useAIPageContext', () => {
  const containers: HTMLDivElement[] = []

  afterEach(() => {
    containers.splice(0).forEach((container) => container.remove())
  })

  it('derives sourceRoute from the active router location', async () => {
    const registerPageContext = vi.fn(() => () => undefined)
    const container = document.createElement('div')
    document.body.appendChild(container)
    containers.push(container)
    const root = createRoot(container)

    function Harness() {
      useAIPageContext({
        sourceWorkbench: 'platform',
        sourceTitle: 'Pods',
      })
      return null
    }

    await act(async () => {
      root.render(
        <MemoryRouter initialEntries={['/workloads/pods?namespace=default']}>
          <AIPageContextRegistry.Provider
            value={{
              currentContext: {
                sourceWorkbench: 'ai',
                sourceRoute: '/',
                sourceTitle: 'Soha',
              },
              launchAssistant: vi.fn(),
              openAssistant: vi.fn(),
              openWorkbench: vi.fn(),
              registerPageContext,
            }}
          >
            <Harness />
          </AIPageContextRegistry.Provider>
        </MemoryRouter>,
      )
    })

    expect(registerPageContext).toHaveBeenCalledWith(
      expect.any(String),
      expect.objectContaining({ sourceRoute: '/workloads/pods?namespace=default' }),
      expect.any(String),
    )

    await act(async () => root.unmount())
  })

  it('does not register a disabled embedded page context', async () => {
    const registerPageContext = vi.fn(() => () => undefined)
    const container = document.createElement('div')
    document.body.appendChild(container)
    containers.push(container)
    const root = createRoot(container)

    function Harness() {
      useAIPageContext(
        {
          sourceWorkbench: 'monitoring',
          sourceTitle: 'Embedded metrics',
        },
        false,
      )
      return null
    }

    await act(async () => {
      root.render(
        <MemoryRouter>
          <AIPageContextRegistry.Provider
            value={{
              currentContext: {
                sourceWorkbench: 'ai',
                sourceRoute: '/',
                sourceTitle: 'Soha',
              },
              launchAssistant: vi.fn(),
              openAssistant: vi.fn(),
              openWorkbench: vi.fn(),
              registerPageContext,
            }}
          >
            <Harness />
          </AIPageContextRegistry.Provider>
        </MemoryRouter>,
      )
    })

    expect(registerPageContext).not.toHaveBeenCalled()

    await act(async () => root.unmount())
  })
})
