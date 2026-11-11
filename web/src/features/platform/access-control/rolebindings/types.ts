import type { AccessControlBindingRecord, AccessControlDetailBase } from '../shared/types'

export interface RoleBindingResource extends AccessControlBindingRecord {
  readonly namespace: string
}

export interface RoleBindingDetail extends AccessControlDetailBase {
  readonly namespace: string
  readonly roleRef: string
  readonly subjects?: string[]
}
