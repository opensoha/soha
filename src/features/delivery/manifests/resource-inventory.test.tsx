/** @vitest-environment jsdom */
import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { expect, it, vi } from 'vitest'
import { ManifestResourceInventoryTable } from './resource-inventory'
import type { ManifestResourceInventory } from './types'

it('keeps missing observations distinct from zero and labels Operator synchronization and deletion', async () => {
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
    configurable: true,
    value: () => ({
      matches: false,
      addListener() {},
      removeListener() {},
      addEventListener() {},
      removeEventListener() {},
    }),
  })
  const host = document.createElement('div')
  document.body.append(host)
  const root = createRoot(host)
  const base: ManifestResourceInventory = {
    deploymentId: 'deployment',
    generation: 12,
    apiVersion: 'workloads.soha.io/v1alpha1',
    kind: 'WorkloadCronJob',
    name: 'periodic',
    namespace: 'test',
    resourceGeneration: 3,
    uid: '01234567-89ab-cdef-0123-456789abcdef',
    desiredObjectDigest: 'desired',
    observedObjectDigest: 'observed',
    health: 'healthy',
    lastObservedAt: '2026-09-14T00:00:00Z',
  }
  try {
    await act(async () => root.render(<ManifestResourceInventoryTable items={[base]} />))
    expect(host.textContent).toContain('未报告 / 3')
    expect(host.textContent).toContain('配置已同步')
    expect(host.textContent).toContain(base.uid)
    expect(host.querySelector('summary')?.textContent).toBe('UID 01234567…cdef')
    expect(host.querySelector('details')?.open).toBe(false)
    await act(async () =>
      root.render(
        <ManifestResourceInventoryTable
          items={[
            {
              ...base,
              observedResourceGeneration: 0,
              deletingAt: '2026-09-14T00:00:00Z',
              finalizers: ['test.soha.io/cleanup'],
            },
          ]}
        />,
      ),
    )
    expect(host.textContent).toContain('0 / 3')
    expect(host.textContent).toContain('删除中')
    expect(host.textContent).toContain('test.soha.io/cleanup')
    expect(host.textContent).not.toContain('配置已同步')
  } finally {
    await act(async () => root.unmount())
    host.remove()
    vi.unstubAllGlobals()
  }
})
