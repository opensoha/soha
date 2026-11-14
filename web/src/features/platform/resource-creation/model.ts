import type {
  ResourceCreateContext,
  ResourceCreateRequest,
  ResourceCreateScopeDecision,
  ResourceCreateSource,
  ResourcePreflight,
} from './types'

export function buildResourceCreateRequest(
  context: ResourceCreateContext,
  content: string,
  mode: 'form' | 'yaml',
  defaultNamespace?: string,
  target?: Partial<
    Pick<ResourceCreateContext, 'expectedApiVersion' | 'expectedKind' | 'resourceGroup'>
  > | null,
): ResourceCreateRequest {
  const source: ResourceCreateSource = mode === 'form' ? 'form' : context.source
  const resourceGroup = target?.resourceGroup ?? context.resourceGroup
  const expectedApiVersion = target?.expectedApiVersion ?? context.expectedApiVersion
  const expectedKind = target?.expectedKind ?? context.expectedKind
  return {
    source,
    ...(defaultNamespace ? { defaultNamespace } : {}),
    ...(resourceGroup ? { resourceGroup } : {}),
    ...(expectedApiVersion ? { expectedApiVersion } : {}),
    ...(expectedKind ? { expectedKind } : {}),
    content,
  }
}

export function resolveCreateEntryAvailability({
  clusterId,
  decision,
  error,
  isLoading,
  localeCode,
}: {
  clusterId: string
  decision?: ResourceCreateScopeDecision
  error?: Error | null
  isLoading: boolean
  localeCode: 'zh_CN' | 'en_US'
}) {
  const isChinese = localeCode === 'zh_CN'
  if (!clusterId) {
    return { disabled: true, reason: isChinese ? '请先选择集群。' : 'Select a cluster first.' }
  }
  if (isLoading) {
    return {
      disabled: true,
      reason: isChinese ? '正在检查创建权限。' : 'Checking create permission.',
    }
  }
  if (error) return { disabled: true, reason: error.message }
  if (decision?.capability?.status === 'unsupported') {
    return {
      disabled: true,
      reason:
        decision.capability.reason ||
        (isChinese
          ? '当前连接模式不支持创建。'
          : 'Creation is unsupported in this connection mode.'),
    }
  }
  if (!decision?.allowed) {
    return {
      disabled: true,
      reason:
        decision?.reason ||
        (isChinese ? '当前范围没有创建权限。' : 'Create permission is denied in this scope.'),
    }
  }
  return { disabled: false, reason: '' }
}

export function resourceCreateRequestFingerprint(
  clusterId: string,
  request: ResourceCreateRequest,
) {
  return JSON.stringify({
    clusterId: clusterId.trim(),
    source: request.source,
    defaultNamespace: request.defaultNamespace?.trim() || null,
    resourceGroup: request.resourceGroup?.trim() || null,
    expectedApiVersion: request.expectedApiVersion?.trim() || null,
    expectedKind: request.expectedKind?.trim() || null,
    content: request.content,
  })
}

export function resolveResourceCreateDefaultNamespace({
  contextNamespace,
  formNamespace,
  mode,
}: {
  contextNamespace?: string
  formNamespace?: string
  mode: 'form' | 'yaml'
}) {
  if (mode === 'form') {
    const normalizedFormNamespace = formNamespace?.trim()
    if (normalizedFormNamespace) return normalizedFormNamespace
  }
  return contextNamespace?.trim() || undefined
}

export function isPreflightCurrent(
  requestFingerprint: string,
  preflightRequestFingerprint: string | null,
  preflight?: ResourcePreflight,
) {
  return Boolean(preflight?.contentHash && preflightRequestFingerprint === requestFingerprint)
}
