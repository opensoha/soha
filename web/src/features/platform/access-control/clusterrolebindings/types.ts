import type { AccessControlBindingRecord, AccessControlDetailBase } from '../shared/types'

export type ClusterRoleBindingResource = AccessControlBindingRecord

export interface ClusterRoleBindingDetail extends AccessControlDetailBase {
  readonly roleRef: string
  readonly subjects?: string[]
}
