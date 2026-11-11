import type { ConfigurationDetailBase } from '../shared/types'
import type { Pod, PodRelatedResource, WorkloadCondition } from '@/types'

export interface PodDisruptionBudgetResource extends ConfigurationDetailBase {
  readonly namespace: string
  readonly currentHealthy: number
  readonly desiredHealthy: number
  readonly disruptionsAllowed: number
  readonly maxUnavailable?: string
  readonly minAvailable?: string
  readonly selector?: string
  readonly pods?: Pod[]
  readonly workload?: PodRelatedResource
  readonly conditions?: WorkloadCondition[]
}

export const PDB_TEMPLATE = `apiVersion: policy/v1
kind: PodDisruptionBudget
metadata:
  name: example-budget
spec:
  minAvailable: 1
  selector:
    matchLabels:
      app: example-app
`
