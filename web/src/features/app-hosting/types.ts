import type {
  AppHostingAudienceCandidates,
  AppHostingAudienceTeam,
  AppHostingAudienceUser,
  CreateHostedAppRequest,
  DeleteHostedAppRequest,
  DeleteHostedAppResult,
  HostedApp,
  HostedAppApprovalListScope,
  HostedAppDeploymentReceipt,
  HostedAppKind,
  HostedAppLifecycleActionRequest,
  HostedAppList,
  HostedAppListScope,
  HostedAppRevision,
  HostedAppRevisionList,
  HostedAppRevisionStatus,
  HostedAppRollbackRequest,
  HostedAppStatus,
  HostedAppUserSummary,
  HostedAppVisibilityApproval,
  HostedAppVisibilityApprovalList,
  HostedAppVisibilityApprovalStatus,
  HostedAppVisibilityInput,
  HostedAppVisibilityMode,
  HostedAppVisibilityScope,
  PortalHostedAppAuthorSummary,
  PortalHostedAppCatalog,
  PortalHostedAppItem,
  UpdateHostedAppMaintainersRequest,
  UpdateHostedAppRequest,
  UpdateHostedAppVisibilityRequest,
  WorkspaceHostedAppDeploymentRequest,
  WorkspaceHostedAppDeploymentResult,
  WorkspaceHostedAppSource,
  WorkspaceHostedAppSourceKind,
} from '@opensoha/contracts/gen/ts/sohaapi'

export type {
  AppHostingAudienceCandidates,
  AppHostingAudienceTeam,
  AppHostingAudienceUser,
  CreateHostedAppRequest,
  DeleteHostedAppRequest,
  DeleteHostedAppResult,
  HostedApp,
  HostedAppApprovalListScope,
  HostedAppDeploymentReceipt,
  HostedAppKind,
  HostedAppLifecycleActionRequest,
  HostedAppList,
  HostedAppListScope,
  HostedAppRevision,
  HostedAppRevisionList,
  HostedAppRevisionStatus,
  HostedAppRollbackRequest,
  HostedAppStatus,
  HostedAppUserSummary,
  HostedAppVisibilityApproval,
  HostedAppVisibilityApprovalList,
  HostedAppVisibilityApprovalStatus,
  HostedAppVisibilityInput,
  HostedAppVisibilityMode,
  HostedAppVisibilityScope,
  PortalHostedAppAuthorSummary,
  PortalHostedAppCatalog,
  PortalHostedAppItem,
  UpdateHostedAppMaintainersRequest,
  UpdateHostedAppRequest,
  UpdateHostedAppVisibilityRequest,
  WorkspaceHostedAppDeploymentRequest,
  WorkspaceHostedAppDeploymentResult,
  WorkspaceHostedAppSource,
  WorkspaceHostedAppSourceKind,
}

export interface PortalHostedAppsQueryParams {
  query?: string
  kind?: HostedAppKind
  authorUserId?: string
  limit?: number
  offset?: number
}

export interface AudienceCandidatesQueryParams {
  query?: string
  limit?: number
}

export interface MyHostedAppsQueryParams {
  query?: string
  status?: HostedAppStatus
  limit?: number
  offset?: number
}

export interface HostedAppRevisionsQueryParams {
  limit?: number
  offset?: number
}

export interface MyHostedAppApprovalsQueryParams {
  appId?: string
  status?: HostedAppVisibilityApprovalStatus
  limit?: number
  offset?: number
}

export function resolveSafeHostedAppUrl(rawUrl?: string | null): string | null {
  const trimmed = String(rawUrl ?? '').trim()
  if (!trimmed || trimmed.length > 4096) return null
  try {
    const parsed = new URL(trimmed)
    if (parsed.protocol !== 'https:' && parsed.protocol !== 'http:') {
      return null
    }
    if (!parsed.hostname || parsed.username || parsed.password) {
      return null
    }
    return parsed.toString()
  } catch {
    return null
  }
}

export function isAudienceNarrowed(
  previous: HostedAppVisibilityScope | undefined,
  next: HostedAppVisibilityInput,
): boolean {
  if (!previous) return false
  if (previous.mode === 'organization') {
    return next.mode !== 'organization'
  }
  if (previous.mode === 'selected') {
    if (next.mode === 'private') return true
    if (next.mode === 'organization') return false
    const nextUserIds = new Set(next.userIds ?? [])
    const nextTeamIds = new Set(next.teamIds ?? [])
    const removedUser = (previous.userIds ?? []).some((id) => !nextUserIds.has(id))
    const removedTeam = (previous.teamIds ?? []).some((id) => !nextTeamIds.has(id))
    return removedUser || removedTeam
  }
  return false
}

export function formatHostedAppBytes(bytes?: number | null): string {
  if (typeof bytes !== 'number' || !Number.isFinite(bytes) || bytes < 0) return '-'
  if (bytes === 0) return '0 B'
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KiB`
  return `${(bytes / (1024 * 1024)).toFixed(2)} MiB`
}

export function hostedAppKindLabel(kind: HostedAppKind, portalCatalog = false): string {
  if (portalCatalog) {
    return kind === 'dynamic' ? '交互工具' : '页面看板'
  }
  return kind === 'dynamic' ? '动态应用' : '静态页面'
}

export function hostedAppStatusLabel(status: HostedAppStatus): string {
  switch (status) {
    case 'online':
      return '在线'
    case 'offline':
      return '已下线'
    case 'draft':
      return '草稿'
    case 'suspended_by_admin':
      return '管理员封停'
    default:
      return status
  }
}

export function hostedAppStatusTagValue(status: HostedAppStatus): string {
  if (status === 'suspended_by_admin') return 'blocked'
  if (status === 'offline') return 'grey'
  return status
}

export function hostedAppRevisionStatusLabel(status: HostedAppRevisionStatus): string {
  switch (status) {
    case 'ready':
      return '已就绪'
    case 'uploading':
      return '上传中'
    case 'validating':
      return '扫描校验中'
    case 'failed':
      return '失败'
    case 'evicted':
      return '已回收'
    default:
      return status
  }
}

export function hostedAppVisibilitySummary(visibility: HostedAppVisibilityScope): string {
  switch (visibility.mode) {
    case 'private':
      return '仅自己'
    case 'organization':
      return '全公司'
    case 'selected': {
      const teamCount = visibility.teamIds?.length ?? 0
      const userCount = visibility.userIds?.length ?? 0
      const parts: string[] = []
      if (teamCount > 0) parts.push(`${teamCount} 个部门`)
      if (userCount > 0) parts.push(`${userCount} 位同事`)
      return parts.length > 0 ? `指定范围（${parts.join(' / ')}）` : '指定范围'
    }
    default:
      return visibility.mode
  }
}

export function formatApprovalInvalidationReason(reason?: string): string {
  switch (reason) {
    case 'visibility_narrowed':
    case 'visibility_changed':
      return '可见范围已变更或收窄'
    case 'ownership_transferred':
      return '应用归属已发生转交'
    case 'policy_changed':
      return '平台全公司公开策略已更新'
    case 'route_generation_changed':
      return '应用版本或路由配置已变更'
    case 'app_suspended':
    case 'suspended_by_admin':
      return '应用已被管理员强制封停'
    default:
      return reason?.trim() || '应用状态或策略已变更'
  }
}
