import type { ConfigurationDetailBase } from '../shared/types'

export interface PriorityClassResource extends ConfigurationDetailBase {
  readonly description?: string
  readonly globalDefault: boolean
  readonly preemptionPolicy?: string
  readonly value: number
}

export const PRIORITY_CLASS_TEMPLATE = `apiVersion: scheduling.k8s.io/v1
kind: PriorityClass
metadata:
  name: example-priority
value: 1000
globalDefault: false
preemptionPolicy: PreemptLowerPriority
description: Explicit priority for selected workloads
`
