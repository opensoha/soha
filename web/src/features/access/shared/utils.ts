import type { AccessTeam, AccessUser } from './types'

export function parseCSV(value: unknown) {
  return String(value ?? '')
    .split(',')
    .map((item) => item.trim())
    .filter(Boolean)
}

export function toStringArray(value: unknown) {
  if (!Array.isArray(value)) return []
  return value.map((item) => String(item).trim()).filter(Boolean)
}

export function joinCSV(items?: string[]) {
  return items?.join(', ') ?? ''
}

export function getUserLabel(user?: Pick<AccessUser, 'displayName' | 'email' | 'username'> | null) {
  if (!user) return '用户'
  return user.displayName || user.username || user.email || '用户'
}

export function getUserInitial(user: Pick<AccessUser, 'displayName' | 'email' | 'username'>) {
  const first = Array.from(getUserLabel(user).trim())[0]
  return (first ?? 'U').toUpperCase()
}

export function getGroupDescription(metadata?: Record<string, unknown>) {
  return String(metadata?.description ?? '').trim()
}

export function getOrganizationLabel(item?: Pick<AccessTeam, 'name' | 'path' | 'slug'> | null) {
  if (!item) return '全部组织'
  return item.name || item.path || item.slug || '未命名组织'
}

export function getOrganizationPathLabel(item: AccessTeam) {
  return item.path || `/${item.slug || item.id}`
}

export function getOrganizationDisplayPath(items: AccessTeam[], item: AccessTeam) {
  const byID = new Map(items.map((organization) => [organization.id, organization]))
  const names: string[] = []
  const visited = new Set<string>()
  let current: AccessTeam | undefined = item
  while (current && !visited.has(current.id)) {
    visited.add(current.id)
    names.unshift(getOrganizationLabel(current))
    current = current.parentId ? byID.get(current.parentId) : undefined
  }
  return `/${names.join('/')}`
}

export function collectOrganizationDescendantIds(items: AccessTeam[], organizationId: string) {
  const childrenByParent = new Map<string, string[]>()
  items.forEach((item) => {
    if (!item.parentId) return
    const children = childrenByParent.get(item.parentId) ?? []
    children.push(item.id)
    childrenByParent.set(item.parentId, children)
  })
  const result = new Set<string>()
  const visit = (id: string) => {
    ;(childrenByParent.get(id) ?? []).forEach((childID) => {
      if (result.has(childID)) return
      result.add(childID)
      visit(childID)
    })
  }
  visit(organizationId)
  return result
}
