import { describe, expect, it } from 'vitest'
import { selectResourceGitOpsStatus } from './resource-gitops-status'
import { platformSharedKeys } from './keys'

describe('selectResourceGitOpsStatus', () => {
  it('normalizes resource GitOps status scope', () => {
    expect(
      platformSharedKeys.resourceGitOpsStatus(' cluster-a ', ' team-a ', ' Deployment ', ' api '),
    ).toEqual(['platform', 'gitops-status', 'cluster-a', 'team-a', 'deployment', 'api'])
  })

  it('prefers matching drift evidence over a managed inventory match', () => {
    const result = selectResourceGitOpsStatus(
      [
        {
          id: 'managed',
          packageId: 'package-a',
          generation: 2,
          spec: { desiredRevision: 4, reconcilePolicy: 'continuous', driftPolicy: 'report' },
          status: {
            phase: 'converged',
            appliedRevision: 4,
            inventory: [{ kind: 'Deployment', namespace: 'team-a', name: 'api' }],
          },
        },
        {
          id: 'drifted',
          packageId: 'package-b',
          generation: 3,
          spec: { desiredRevision: 5, reconcilePolicy: 'continuous', driftPolicy: 'repair' },
          status: {
            phase: 'drifted',
            drift: {
              drifted: true,
              resources: [
                {
                  kind: 'deployment',
                  namespace: 'team-a',
                  name: 'api',
                  fields: [
                    { path: 'spec.replicas' },
                    { path: 'spec.template.spec.containers[0].image' },
                  ],
                },
              ],
            },
          },
        },
      ],
      { kind: 'Deployment', namespace: 'team-a', name: 'api' },
    )

    expect(result).toMatchObject({ deploymentId: 'drifted', drifted: true, fieldCount: 2 })
  })
})
