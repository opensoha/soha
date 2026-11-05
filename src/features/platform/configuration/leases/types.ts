import type { ConfigurationDetailBase } from '../shared/types'

export interface LeaseResource extends ConfigurationDetailBase {
  readonly namespace: string
  readonly acquireTime?: string
  readonly holderIdentity?: string
  readonly leaseDurationSeconds?: number
  readonly renewTime?: string
}

export const LEASE_TEMPLATE = `apiVersion: coordination.k8s.io/v1
kind: Lease
metadata:
  name: example-lease
  namespace: default
spec:
  leaseDurationSeconds: 60
`
