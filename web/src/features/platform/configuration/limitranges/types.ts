import type { ConfigurationDetailBase } from '../shared/types'

export interface LimitRangeRule {
  readonly type: string
  readonly min?: Record<string, string>
  readonly max?: Record<string, string>
  readonly default?: Record<string, string>
  readonly defaultRequest?: Record<string, string>
  readonly maxLimitRequestRatio?: Record<string, string>
}

export interface LimitRangeResource extends ConfigurationDetailBase {
  readonly namespace: string
  readonly limits: number
  readonly rules?: LimitRangeRule[]
}

export const LIMIT_RANGE_TEMPLATE = `apiVersion: v1
kind: LimitRange
metadata:
  name: example-limits
spec:
  limits:
    - type: Container
      min:
        cpu: 50m
        memory: 32Mi
      max:
        cpu: "1"
        memory: 1Gi
      defaultRequest:
        cpu: 100m
        memory: 64Mi
      default:
        cpu: 500m
        memory: 256Mi
`
