export interface ScopeGrant {
  id: string
  subjectType: string
  subjectId: string
  businessLineId: string
  environmentIds: string[]
  applicationIds: string[]
  role: string
  effect: string
  enabled: boolean
  createdAt: string
  updatedAt: string
}
