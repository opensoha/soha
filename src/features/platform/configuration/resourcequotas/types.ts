import type { ConfigurationDetailBase } from '../shared/types'

export interface ResourceQuotaResource extends ConfigurationDetailBase {
  readonly namespace: string
  readonly hard?: Record<string, string>
  readonly scopes?: string[]
  readonly used?: Record<string, string>
}

export const RESOURCE_QUOTA_TEMPLATE = `apiVersion: v1
kind: ResourceQuota
metadata:
  name: example-quota
spec:
  hard:
    pods: "10"
    requests.cpu: "2"
    requests.memory: 4Gi
    limits.cpu: "4"
    limits.memory: 8Gi
`
