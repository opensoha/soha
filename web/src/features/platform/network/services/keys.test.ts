import { describe, expect, it } from 'vitest'
import { serviceKeys } from './keys'

describe('serviceKeys', () => {
  it('normalizes diagnostic pod scope under the service hierarchy', () => {
    expect(serviceKeys.diagnosticPods({ clusterId: ' cluster-a ', namespace: ' team-a ' })).toEqual(
      [...serviceKeys.all, 'diagnostics', 'pods', { clusterId: 'cluster-a', namespace: 'team-a' }],
    )
  })
})
