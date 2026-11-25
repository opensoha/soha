import type { MetadataTagTone } from '@/components/status-tag'
import type {
  AppHostingOrganizationVisibilityPolicy,
  AppHostingSettings,
  HostedAppKind,
  HostedAppRevisionStatus,
  HostedAppStatus,
  HostedAppVisibilityApprovalStatus,
  HostedAppVisibilityScope,
} from './types'

const MIB_BYTES = 1024 * 1024

export function formatBytes(value?: number | null): string {
  if (value == null || !Number.isFinite(value)) return '-'
  if (value <= 0) return '0 B'
  const units = ['B', 'KiB', 'MiB', 'GiB']
  let size = value
  let index = 0
  while (size >= 1024 && index < units.length - 1) {
    size /= 1024
    index += 1
  }
  return `${size >= 10 || index === 0 ? size.toFixed(0) : size.toFixed(1)} ${units[index]}`
}

export function bytesToMiB(bytes?: number | null): number {
  if (bytes == null || !Number.isFinite(bytes) || bytes <= 0) return 1
  const mib = bytes / MIB_BYTES
  return Number.isInteger(mib) ? mib : Number(mib.toFixed(2))
}

export function mibToBytes(mib: number): number {
  if (!Number.isFinite(mib) || mib <= 0) return MIB_BYTES
  return Math.max(1, Math.round(mib * MIB_BYTES))
}

export function resolveSafeExternalUrl(rawUrl?: string | null): string | undefined {
  const trimmed = rawUrl?.trim()
  if (!trimmed || trimmed.length > 4096) return undefined
  try {
    const parsed = new URL(trimmed)
    if (
      (parsed.protocol === 'https:' || parsed.protocol === 'http:') &&
      Boolean(parsed.hostname) &&
      !parsed.username &&
      !parsed.password
    ) {
      return parsed.toString()
    }
  } catch {
    return undefined
  }
  return undefined
}

export function resolveOrganizationVisibilityPolicy(
  settings?: AppHostingSettings | null,
): AppHostingOrganizationVisibilityPolicy {
  if (settings?.organizationVisibilityPolicy) {
    return settings.organizationVisibilityPolicy
  }
  if (settings?.allowOrganizationVisibility === false) {
    return 'disabled'
  }
  return 'allow_direct'
}

export function formatHostedAppStatus(status: HostedAppStatus): { value: string; label: string } {
  switch (status) {
    case 'online':
      return { value: 'online', label: '在线' }
    case 'draft':
      return { value: 'draft', label: '草稿' }
    case 'offline':
      return { value: 'offline', label: '已下线' }
    case 'suspended_by_admin':
      return { value: 'blocked', label: '已强制封停' }
    default:
      return { value: status, label: status }
  }
}

export function formatHostedAppKind(kind: HostedAppKind): {
  label: string
  tone: MetadataTagTone
} {
  return kind === 'dynamic'
    ? { label: '动态应用', tone: 'purple' }
    : { label: '静态站点', tone: 'blue' }
}

export function formatVisibilityScope(scope?: HostedAppVisibilityScope | null): string {
  if (!scope) return '-'
  if (scope.mode === 'private') return '仅作者本人'
  if (scope.mode === 'organization') return '全公司公开'
  const teamCount = scope.teamIds?.length ?? 0
  const userCount = scope.userIds?.length ?? 0
  const segments: string[] = []
  if (teamCount > 0) segments.push(`部门 ${teamCount}`)
  if (userCount > 0) segments.push(`个人 ${userCount}`)
  return segments.length > 0 ? `指定范围（${segments.join(' / ')}）` : '指定范围（未选受众）'
}

export function formatApprovalStatus(status: HostedAppVisibilityApprovalStatus): {
  value: string
  label: string
} {
  switch (status) {
    case 'pending':
      return { value: 'pending', label: '待审批' }
    case 'approved':
      return { value: 'allow', label: '已批准' }
    case 'rejected':
      return { value: 'denied', label: '已拒绝' }
    case 'withdrawn':
      return { value: 'default', label: '已撤回' }
    case 'invalidated':
      return { value: 'expired', label: '已失效' }
    default:
      return { value: status, label: status }
  }
}

const INVALIDATION_REASON_LABELS: Record<string, string> = {
  ownership_transferred: '归属已变更',
  visibility_narrowed: '受众已收窄',
  audience_narrowed: '受众已收窄',
  visibility_changed: '可见范围已变更',
  policy_changed: '平台公开策略已变更',
  organization_policy_changed: '平台公开策略已变更',
  route_generation_changed: '应用版本或路由配置已变更',
  app_suspended: '应用已被管理员封停',
  suspended_by_admin: '应用已被管理员封停',
  app_deleted: '应用已删除',
  requester_revoked: '申请人发布权限已变更',
  permission_revoked: '申请人发布权限已变更',
  superseded: '已被新申请替代',
}

export function formatInvalidationReason(reason?: string | null): string {
  const normalized = reason?.trim()
  if (!normalized) return '条件已变更自动失效'
  return INVALIDATION_REASON_LABELS[normalized] ?? normalized
}

export function formatRevisionStatus(status: HostedAppRevisionStatus): {
  value: string
  label: string
} {
  switch (status) {
    case 'ready':
      return { value: 'ready', label: '就绪' }
    case 'uploading':
      return { value: 'syncing', label: '上传中' }
    case 'validating':
      return { value: 'checking', label: '预检中' }
    case 'failed':
      return { value: 'failed', label: '失败' }
    case 'evicted':
      return { value: 'default', label: '已回收' }
    default:
      return { value: status, label: status }
  }
}
