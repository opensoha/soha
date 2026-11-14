import type { AccessControlDetailBase, AccessControlResourceRecord } from '../shared/types'

export type ClusterRoleResource = AccessControlResourceRecord

export interface ClusterRoleDetail extends AccessControlDetailBase {
  readonly aggregationRules: number
  readonly ruleSummaries?: string[]
  readonly rules: number
}
