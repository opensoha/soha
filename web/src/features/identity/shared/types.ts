export type IdentityProviderType = 'link' | 'oidc' | 'proxy' | 'saml'
export type IdentityApplicationStatus = 'draft' | 'enabled' | 'disabled' | 'maintenance'
export type IdentityAssignmentSubjectType = 'user' | 'role' | 'team' | 'tag'
export type IdentityAssignmentEffect = 'allow' | 'deny'

export interface IdentityPrincipal {
  userId: string
  userName: string
  email: string
  roles: string[]
  teams: string[]
  projects: string[]
  tags: string[]
  permissionKeys?: string[]
}

export interface IdentityApplicationAssignment {
  id?: string
  applicationId?: string
  subjectType: IdentityAssignmentSubjectType
  subjectId: string
  effect: IdentityAssignmentEffect
  createdBy?: string
  createdAt?: string
}

export interface IdentityApplication {
  id: string
  slug: string
  name: string
  description?: string
  iconUrl?: string
  category?: string
  tags: string[]
  launchUrl?: string
  providerId?: string
  providerType: IdentityProviderType
  portalVisible: boolean
  featured: boolean
  sortOrder: number
  status: IdentityApplicationStatus
  metadata?: Record<string, unknown>
  assignments?: IdentityApplicationAssignment[]
  favorite?: boolean
  lastLaunchedAt?: string
  createdBy?: string
  updatedBy?: string
  createdAt: string
  updatedAt: string
}

export interface IdentityApplicationInput {
  slug: string
  name: string
  description: string
  iconUrl: string
  tags: string[]
  launchUrl: string
  providerId: string
  providerType: IdentityProviderType
  portalVisible: boolean
  featured: boolean
  sortOrder: number
  status: IdentityApplicationStatus
  metadata: Record<string, unknown>
  assignments: IdentityApplicationAssignment[]
}

export interface IdentityApplicationPolicyConditions {
  requireMfa: boolean
  allowedCidrs: string[]
  startTimeUtc: string
  endTimeUtc: string
}

export interface IdentityApplicationLaunch {
  id: string
  applicationId: string
  applicationName?: string
  userId: string
  providerId?: string
  providerType: IdentityProviderType
  result: string
  reason?: string
  launchUrl?: string
  sourceIp?: string
  userAgent?: string
  createdAt: string
}
