import type {
  IdentityApplication,
  IdentityApplicationInput,
  IdentityApplicationPolicyConditions,
  IdentityApplicationStatus,
  IdentityAssignmentEffect,
  IdentityAssignmentSubjectType,
  IdentityProviderType,
} from '../shared/types'

export interface IdentityApplicationTagOption {
  label: string
  value: string
}

export interface IdentityApplicationFormValues {
  assignments: Array<{
    effect: IdentityAssignmentEffect
    subjectIds: string[]
    subjectType: IdentityAssignmentSubjectType
  }>
  allowedCidrs: string[]
  description: string
  endTimeUtc: string
  featured: boolean
  iconUrl: string
  launchUrl: string
  name: string
  portalVisible: boolean
  providerId: string
  providerType: IdentityProviderType
  requireMfa: boolean
  slug: string
  sortOrder: number
  status: IdentityApplicationStatus
  startTimeUtc: string
  tags: string[]
}

export const IDENTITY_APPLICATION_ICON_ACCEPT = '.jpg,.jpeg,.png,.webp,.ico'
export const IDENTITY_APPLICATION_ICON_MAX_BYTES = 512 * 1024

const identityApplicationIconTypes: Record<string, readonly string[]> = {
  '.ico': ['image/ico', 'image/vnd.microsoft.icon', 'image/x-icon'],
  '.jpeg': ['image/jpeg'],
  '.jpg': ['image/jpeg'],
  '.png': ['image/png'],
  '.webp': ['image/webp'],
}

export async function readIdentityApplicationIconFile(file: File): Promise<string> {
  const extension = file.name.toLowerCase().match(/\.[^.]+$/)?.[0] ?? ''
  const allowedTypes = identityApplicationIconTypes[extension]
  if (!allowedTypes?.includes(file.type.toLowerCase())) {
    throw new Error('仅支持 JPG、PNG、WEBP 或 ICO 图片')
  }
  if (file.size === 0) {
    throw new Error('图片文件不能为空')
  }
  if (file.size > IDENTITY_APPLICATION_ICON_MAX_BYTES) {
    throw new Error('图片大小不能超过 512KB')
  }

  return new Promise((resolve, reject) => {
    const reader = new FileReader()
    reader.onload = () => {
      if (typeof reader.result === 'string') resolve(reader.result)
      else reject(new Error('读取图片失败'))
    }
    reader.onerror = () => reject(new Error('读取图片失败'))
    reader.readAsDataURL(file)
  })
}

export const identityApplicationProviderTypeOptions: Array<{
  label: string
  value: IdentityProviderType
}> = [
  { label: 'Link', value: 'link' },
  { label: 'OIDC', value: 'oidc' },
  { label: 'SAML', value: 'saml' },
  { label: 'Proxy', value: 'proxy' },
]

export const identityApplicationStatusOptions: Array<{
  label: string
  value: IdentityApplicationStatus
}> = [
  { label: 'Draft', value: 'draft' },
  { label: 'Enabled', value: 'enabled' },
  { label: 'Disabled', value: 'disabled' },
  { label: 'Maintenance', value: 'maintenance' },
]

export const identityApplicationAssignmentSubjectOptions: Array<{
  label: string
  value: IdentityAssignmentSubjectType
}> = [
  { label: 'User', value: 'user' },
  { label: 'Role', value: 'role' },
  { label: 'Team', value: 'team' },
  { label: 'Tag', value: 'tag' },
]

export const identityApplicationAssignmentEffectOptions: Array<{
  label: string
  value: IdentityAssignmentEffect
}> = [
  { label: 'Allow', value: 'allow' },
  { label: 'Deny', value: 'deny' },
]

export function identityApplicationTagOptions(
  applications: IdentityApplication[],
): IdentityApplicationTagOption[] {
  return Array.from(
    new Set(
      applications.flatMap((application) =>
        (application.tags ?? []).map((tag) => String(tag).trim()).filter(Boolean),
      ),
    ),
  )
    .sort((left, right) => left.localeCompare(right, undefined, { sensitivity: 'base' }))
    .map((tag) => ({ label: tag, value: tag }))
}

function compactStrings(values: string[] = []) {
  const seen = new Set<string>()
  const result: string[] = []
  values.forEach((value) => {
    const normalized = String(value ?? '').trim()
    if (!normalized || seen.has(normalized)) return
    seen.add(normalized)
    result.push(normalized)
  })
  return result
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return Boolean(value) && typeof value === 'object' && !Array.isArray(value)
}

export function identityApplicationAccessPolicyFor(
  application: Pick<IdentityApplication, 'metadata'>,
): IdentityApplicationPolicyConditions {
  const policy = application.metadata?.accessPolicy
  if (!isRecord(policy)) {
    return { allowedCidrs: [], endTimeUtc: '', requireMfa: false, startTimeUtc: '' }
  }
  return {
    allowedCidrs: Array.isArray(policy.allowedCidrs)
      ? compactStrings(
          policy.allowedCidrs.filter((value): value is string => typeof value === 'string'),
        )
      : [],
    endTimeUtc: typeof policy.endTimeUtc === 'string' ? policy.endTimeUtc.trim() : '',
    requireMfa: policy.requireMfa === true,
    startTimeUtc: typeof policy.startTimeUtc === 'string' ? policy.startTimeUtc.trim() : '',
  }
}

export function defaultIdentityApplicationFormValues(): IdentityApplicationFormValues {
  return {
    allowedCidrs: [],
    assignments: [],
    description: '',
    endTimeUtc: '',
    featured: false,
    iconUrl: '',
    launchUrl: '',
    name: '',
    portalVisible: true,
    providerId: '',
    providerType: 'link',
    requireMfa: false,
    slug: '',
    sortOrder: 1000,
    status: 'draft',
    startTimeUtc: '',
    tags: [],
  }
}

export function identityApplicationFormValuesFor(
  application: IdentityApplication,
): IdentityApplicationFormValues {
  const accessPolicy = identityApplicationAccessPolicyFor(application)
  return {
    allowedCidrs: accessPolicy.allowedCidrs,
    assignments: (application.assignments ?? []).map((assignment) => ({
      effect: assignment.effect || 'allow',
      subjectIds: [assignment.subjectId],
      subjectType: assignment.subjectType,
    })),
    description: application.description ?? '',
    endTimeUtc: accessPolicy.endTimeUtc,
    featured: application.featured,
    iconUrl: application.iconUrl ?? '',
    launchUrl: application.launchUrl ?? '',
    name: application.name,
    portalVisible: application.portalVisible,
    providerId: application.providerId ?? '',
    providerType: application.providerType,
    requireMfa: accessPolicy.requireMfa,
    slug: application.slug,
    sortOrder: application.sortOrder,
    status: application.status,
    startTimeUtc: accessPolicy.startTimeUtc,
    tags: application.tags ?? [],
  }
}

function metadataFromFormValues(
  values: IdentityApplicationFormValues,
  current?: IdentityApplication | null,
) {
  const metadata: Record<string, unknown> = { ...(current?.metadata ?? {}) }
  delete metadata.oidcClientId
  delete metadata.oidcRedirectUri
  delete metadata.oidcScopes
  delete metadata.oidc

  const accessPolicy: IdentityApplicationPolicyConditions = {
    allowedCidrs: compactStrings(values.allowedCidrs),
    endTimeUtc: String(values.endTimeUtc ?? '').trim(),
    requireMfa: Boolean(values.requireMfa),
    startTimeUtc: String(values.startTimeUtc ?? '').trim(),
  }
  if (
    accessPolicy.requireMfa ||
    accessPolicy.allowedCidrs.length ||
    accessPolicy.startTimeUtc ||
    accessPolicy.endTimeUtc
  ) {
    metadata.accessPolicy = accessPolicy
  } else {
    delete metadata.accessPolicy
  }
  return metadata
}

export function buildIdentityApplicationInput(
  values: IdentityApplicationFormValues,
  current?: IdentityApplication | null,
): IdentityApplicationInput {
  return {
    assignments: (values.assignments ?? []).flatMap((assignment) =>
      compactStrings(assignment.subjectIds).map((subjectId) => ({
        effect: assignment.effect || 'allow',
        subjectId,
        subjectType: assignment.subjectType || 'role',
      })),
    ),
    description: values.description?.trim() ?? '',
    featured: Boolean(values.featured),
    iconUrl: values.iconUrl?.trim() ?? '',
    launchUrl: values.launchUrl?.trim() ?? '',
    metadata: metadataFromFormValues(values, current),
    name: values.name.trim(),
    portalVisible: Boolean(values.portalVisible),
    providerId: current ? (values.providerId?.trim() ?? '') : '',
    providerType: values.providerType || 'link',
    slug: values.slug?.trim() ?? '',
    sortOrder: Number(values.sortOrder || 1000),
    status: values.status || 'draft',
    tags: compactStrings(values.tags),
  }
}
