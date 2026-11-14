import type { ConfigurationDetailBase } from '../shared/types'
import type { WorkloadCondition } from '@/types'

export interface HorizontalPodAutoscalerMetric {
  readonly type: string
  readonly name?: string
  readonly target?: string
  readonly current?: string
}

export interface HorizontalPodAutoscalerResource extends ConfigurationDetailBase {
  readonly namespace: string
  readonly currentReplicas: number
  readonly desiredReplicas: number
  readonly maxReplicas: number
  readonly minReplicas: number
  readonly targetRef: string
  readonly metrics?: HorizontalPodAutoscalerMetric[]
  readonly conditions?: WorkloadCondition[]
}

export const HPA_TEMPLATE = `apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: example-autoscaler
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: example-app
  minReplicas: 1
  maxReplicas: 3
  metrics:
    - type: Resource
      resource:
        name: cpu
        target:
          type: Utilization
          averageUtilization: 75
`
