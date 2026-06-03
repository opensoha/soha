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
  permissionStrategy?: 'self' | 'any-child'
  scopeMode?: 'hidden' | 'passive' | 'cluster' | 'namespace'
  workspace?: WorkspaceType
}

export interface User {
  userId: string
  userName: string
  email: string
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
  status: string
  roles: string[]
  teams: string[]
  projects: string[]
  tags: string[]
  identities: LinkedIdentity[]
  sessions: UserSession[]
  lastLoginAt?: string
}

export interface AuthTokens {
  accessToken: string
  refreshToken: string
}

export interface ApiResponse<T = unknown> {
  data: T
  message?: string
}

export interface ApiItemsResponse<T = unknown> {
  items: T[]
}

export interface AuthResult {
  user: User
  tokens: {
    accessToken: string
    refreshToken: string
    tokenType: string
    expiresIn: number
    expiresAt: string
  }
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

export interface BrandingSettings {
  appTitle: string
  sidebarTitle: string
  loginLogoUrl: string
  expandedLogoUrl: string
  collapsedLogoUrl: string
  faviconUrl: string
}

export interface PaginatedResponse<T = unknown> {
  items: T[]
  total: number
  page: number
  pageSize: number
}
