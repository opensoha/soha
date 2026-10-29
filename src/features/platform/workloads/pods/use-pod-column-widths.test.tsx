/** @vitest-environment jsdom */
import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { expect, it, vi } from 'vitest'
import type { Pod } from './types'
import { podContentWidths, usePodColumnWidths } from './use-pod-column-widths'

const pod: Pod = {
  name: 'pod-a',
  namespace: 'default',
  phase: 'Running',
  readyContainers: '1/1',
  restarts: 0,
  nodeName: 'node-a',
  podIp: '10.0.0.1',
  ageSeconds: 60,
  cpu: '2m',
  memory: '80Mi',
}

it('budgets init and app squares, long addresses and long resource values from all results', () => {
  const normal = podContentWidths([pod], true)
  const long = podContentWidths(
    [
      {
        ...pod,
        namespace: 'a'.repeat(63),
        podIp: '2001:db8:ffff:ffff:ffff:ffff:ffff:ffff',
        cpu: '123456789m',
        containerStatuses: Array.from({ length: 9 }, (_, index) => ({
          name: `init-${index}`,
          kind: 'init',
          state: 'terminated',
          ready: false,
        })),
      },
    ],
    true,
  )
  expect(long.namespace).toBeLessThanOrEqual(176)
  expect(long.podIp).toBeGreaterThan(normal.podIp)
  expect(long.readyContainers).toBeGreaterThan(normal.readyContainers)
  expect(long.cpu).toBeGreaterThan(normal.cpu)
})

it('keeps content widths stable on refresh and refits explicitly or on scope change', async () => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  const container = document.createElement('div')
  const root = createRoot(container)
  let layout: ReturnType<typeof usePodColumnWidths>
  function Harness({ pods, scope }: { pods: Pod[]; scope: string }) {
    layout = usePodColumnWidths(pods, scope, true, true, false)
    return <div ref={layout.ref} />
  }
  try {
    await act(async () => root.render(<Harness pods={[pod]} scope="a" />))
    const original = layout!.widths.podIp
    const changed = { ...pod, podIp: '2001:db8:ffff:ffff:ffff:ffff:ffff:ffff' }
    await act(async () => root.render(<Harness pods={[changed]} scope="a" />))
    expect(layout!.widths.podIp).toBe(original)
    await act(async () => layout!.fit())
    expect(layout!.widths.podIp).toBeGreaterThan(original)
    await act(async () => root.render(<Harness pods={[pod]} scope="b" />))
    expect(layout!.widths.podIp).toBe(original)
  } finally {
    await act(async () => root.unmount())
    vi.unstubAllGlobals()
  }
})
