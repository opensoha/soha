import type {
  Application,
  ApplicationEnvironment,
  KubernetesClusterSummary,
  MFAAdminResetRequest,
  PermissionCatalog,
  PermissionDefinition,
} from '@opensoha/contracts/gen/ts/sohaapi'

export type { PermissionCatalog, PermissionDefinition }

export interface AccessUser {
  id: string
  username: string
  email: string
  displayName: string
  avatarUrl?: string
  avatarFit?: string
  status: string
  lastLoginAt?: string
  tags: string[]
  roles: string[]
  teams: string[]
  projects: string[]
  loginSources: AccessUserLoginSource[]
}

export interface AccessUserLoginSource {
  type: string
  providerId?: string
}

export interface AccessRole {
  id: string
  name: string
  scope: string
  capabilities: string[]
  permissionKeys?: string[]
  userCount: number
}

export interface AccessTeam {
  id: string
  parentId?: string
  name: string
  slug: string
  path?: string
  source?: string
  externalId?: string
  metadata: Record<string, unknown>
  userCount: number
}

export interface AccessPolicy {
  id: string
  name: string
  effect: string
  priority: number
  subjects: {
    roles: string[]
    teams: string[]
    projects: string[]
    users: string[]
    tags: string[]
  }
  clusters: {
    ids: string[]
    regions: string[]
    environments: string[]
    labels: Record<string, string[]>
  }
  namespaces: {
    names: string[]
    ownerTeams: string[]
    labels: Record<string, string[]>
  }
  resources: {
    kinds: string[]
    names: string[]
    labels: Record<string, string[]>
  }
  actions: string[]
  conditions: {
    sources: string[]
    approvalStates: string[]
  }
  reason: string
}

export type AccessApplicationOption = Pick<Application, 'businessLineId' | 'group' | 'id' | 'name'>
export type AccessApplicationEnvironment = Pick<
  ApplicationEnvironment,
  | 'applicationGroup'
  | 'applicationId'
  | 'businessLineId'
  | 'environmentId'
  | 'environmentKey'
  | 'id'
>
export type AccessClusterOption = Pick<
  KubernetesClusterSummary,
  'environment' | 'id' | 'name' | 'region'
>

export interface AccessLoginProviderRef {
  id: string
  name: string
  type: string
  enabled?: boolean
}

export type AccessMutationValues = Record<string, unknown>

export interface AccessUpdateVariables {
  id: string
  values: AccessMutationValues
}

export interface ResetAccessUserMFAVariables {
  id: string
  input: MFAAdminResetRequest
}

export interface AccessScopeGrant {
  id: string
  subjectType: 'team' | 'user'
  subjectId: string
  businessLineId: string
  environmentIds?: string[]
  applicationIds?: string[]
  scopeType: 'delivery' | 'legacy' | 'platform'
  clusterIds?: string[]
  namespaces?: string[]
  namespaceSelector?: string
  resourceGroups?: string[]
  resourceKinds?: string[]
  role: string
  effect: 'allow' | 'deny'
  enabled: boolean
  createdAt: string
  updatedAt: string
}

export type AccessScopeGrantSubject = Pick<AccessScopeGrant, 'subjectId' | 'subjectType'>
