import { describe, expect, it } from 'vitest'
import {
  configurationQuickEditValuesFromManifest,
  mergeConfigurationQuickEditManifest,
} from './configuration-quick-edit-model'

describe('configuration quick edit model', () => {
  it('updates HPA replica bounds while preserving target, metrics, and behavior', () => {
    const hpa = {
      apiVersion: 'autoscaling/v2',
      kind: 'HorizontalPodAutoscaler',
      spec: {
        minReplicas: 2,
        maxReplicas: 8,
        scaleTargetRef: { apiVersion: 'apps/v1', kind: 'Deployment', name: 'api' },
        metrics: [
          { type: 'Resource', resource: { name: 'cpu', target: { averageUtilization: 70 } } },
        ],
        behavior: { scaleDown: { stabilizationWindowSeconds: 300 } },
      },
    }
    expect(configurationQuickEditValuesFromManifest(hpa, 'hpas')).toEqual({
      minReplicas: 2,
      maxReplicas: 8,
    })
    expect(
      mergeConfigurationQuickEditManifest(hpa, 'hpas', {
        minReplicas: 3,
        maxReplicas: 12,
      }),
    ).toMatchObject({
      spec: {
        minReplicas: 3,
        maxReplicas: 12,
        scaleTargetRef: hpa.spec.scaleTargetRef,
        metrics: hpa.spec.metrics,
        behavior: hpa.spec.behavior,
      },
    })
    expect(() =>
      mergeConfigurationQuickEditManifest(hpa, 'hpas', {
        minReplicas: 9,
        maxReplicas: 4,
      }),
    ).toThrow('minimum replicas')
  })

  it('switches PDB availability mode without changing selectors', () => {
    const pdb = {
      apiVersion: 'policy/v1',
      kind: 'PodDisruptionBudget',
      spec: { minAvailable: '50%', selector: { matchLabels: { app: 'api' } } },
    }
    expect(configurationQuickEditValuesFromManifest(pdb, 'poddisruptionbudgets')).toEqual({
      availabilityMode: 'minAvailable',
      availabilityValue: '50%',
    })
    expect(
      mergeConfigurationQuickEditManifest(pdb, 'poddisruptionbudgets', {
        availabilityMode: 'maxUnavailable',
        availabilityValue: '1',
      }),
    ).toEqual({
      ...pdb,
      spec: { maxUnavailable: 1, selector: pdb.spec.selector },
    })
  })
})
