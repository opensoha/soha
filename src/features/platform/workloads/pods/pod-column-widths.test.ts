import { expect, it } from 'vitest'
import type { Pod } from './types'
import { podContentWidths } from './pod-column-widths'

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
