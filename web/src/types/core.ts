import type {
  BrandingSettings as ContractBrandingSettings,
  LoginOptions as ContractLoginOptions,
} from '@opensoha/contracts/gen/ts/sohaapi'

export type {
  ApiItemsResponse,
  ApiResponse,
  AuthProvider,
  AuthResult,
  AuthTokens,
  ErrorEnvelope,
  StreamTicket,
} from '@opensoha/contracts/gen/ts/sohaapi'

export type WorkspaceType = 'application' | 'resource' | 'system'
export type BusinessWorkspaceType = Exclude<WorkspaceType, 'system'>

export interface RouteMeta {
  id: string
  path: string
  title: string
  description: string
  icon: string
  group: string
  workbenchId?: string
  requiresAuth: boolean
  tabbar: boolean
  navVisible: boolean
  parentId?: string
  redirectTo?: string
  menuId?: string
  permissionKey?: string
  permissionKeysAny?: string[]
  permissionKeysAll?: string[]
  permissionStrategy?: 'self' | 'any-child'
  scopeMode?: 'hidden' | 'passive' | 'cluster' | 'namespace'
  workspace?: WorkspaceType
}

export interface User {
  userId: string
  userName: string
  email: string
  avatarUrl?: string
  avatarFit?: string
  roles: string[]
  teams: string[]
  projects: string[]
  tags: string[] | null
  displayName?: string
  phone?: string
  status?: string
  username?: string
}

export interface LinkedIdentity {
  id: string
  providerType: string
  providerId: string
  providerUserId: string
  displayName?: string
  email?: string
  lastLoginAt?: string
}

export interface UserSession {
  id: string
  userId: string
  userName: string
  email: string
  providerType: string
  status: string
  expiresAt: string
  lastSeenAt: string
  createdAt: string
  refreshTokenId: string
  metadata?: Record<string, unknown>
}

export interface UserProfile {
  userId: string
  username: string
  displayName: string
  email: string
  phone?: string
  avatarUrl?: string
  avatarFit?: string
  status: string
  roles: string[]
  teams: string[]
  projects: string[]
  tags: string[]
  identities: LinkedIdentity[]
  sessions: UserSession[]
  lastLoginAt?: string
}

export interface VisibleMenu {
  id: string
  parentId?: string
  path: string
  labelZh?: string
  labelEn?: string
  iconKey?: string
  section?: string
  sortOrder?: number
  enabled?: boolean
}

export interface WorkbenchModuleDescriptor {
  id: string
  name: string
  defaultPath: string
  enabledConfigKey?: string
  dependencies?: string[]
  visiblePermissions?: string[]
  seedMenus?: string[]
}

export interface WorkbenchModuleStatus {
  descriptor: WorkbenchModuleDescriptor
  enabled: boolean
  features?: Record<string, boolean>
}

export interface RuntimeMenuNode {
  id: string
  parentId?: string
  path: string
  labelZh: string
  labelEn: string
  iconKey: string
  section: string
  sortOrder: number
  enabled: boolean
  workspace?: WorkspaceType
  workbenchId?: string
  route?: RouteMeta
  children?: RuntimeMenuNode[]
}

export interface PermissionSnapshot {
  permissionKeys: string[]
  visibleMenuIds: string[]
  visibleMenus: VisibleMenu[]
}

// Optional additions also work with the currently published contracts package.
export interface BrandingSettings extends ContractBrandingSettings {
  darkExpandedLogoUrl?: string
  darkCollapsedLogoUrl?: string
}

export type LoginOptions = Omit<ContractLoginOptions, 'branding'> & {
  branding?: BrandingSettings
}

export interface PaginatedResponse<T = unknown> {
  items: T[]
  total: number
  page: number
  pageSize: number
}
