/** @vitest-environment jsdom */

import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'
import { afterAll, afterEach, beforeAll, describe, expect, it, vi } from 'vitest'
import { LegacySourceConnectionDetailRedirect } from './legacy-detail-redirect'

let root: ReturnType<typeof createRoot> | undefined
let container: HTMLDivElement | undefined

beforeAll(() => vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true))
afterAll(() => vi.unstubAllGlobals())

afterEach(() => {
  act(() => root?.unmount())
  container?.remove()
  root = undefined
  container = undefined
})

describe('LegacySourceConnectionDetailRedirect', () => {
  it('preserves the integration id, query, and hash', async () => {
    let location = ''
    container = document.createElement('div')
    document.body.appendChild(container)
    root = createRoot(container)

    function LocationProbe() {
      const current = useLocation()
      location = `${current.pathname}${current.search}${current.hash}`
      return null
    }

    await act(async () => {
      root?.render(
        <MemoryRouter
          initialEntries={['/settings/system-integrations/source-control/source%2Fone?tab=usage#history']}
        >
          <Routes>
            <Route
              path="/settings/system-integrations/source-control/:integrationId"
              element={<LegacySourceConnectionDetailRedirect />}
            />
            <Route path="*" element={<LocationProbe />} />
          </Routes>
        </MemoryRouter>,
      )
    })

    expect(location).toBe('/settings/source-control/source%2Fone?tab=usage#history')
  })
})
