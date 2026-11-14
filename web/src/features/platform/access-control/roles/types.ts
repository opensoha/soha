import type { AccessControlDetailBase, AccessControlResourceRecord } from '../shared/types'

export interface RoleResource extends AccessControlResourceRecord {
  readonly namespace: string
}

export interface RoleDetail extends AccessControlDetailBase {
  readonly namespace: string
  readonly ruleSummaries?: string[]
  readonly rules: number
}
