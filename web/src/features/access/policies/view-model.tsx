import type { AccessPolicy } from '../shared/types'

export function buildPolicySubjectsSummary(
  policy: AccessPolicy,
  roleMap: Record<string, string>,
  teamMap: Record<string, string>,
) {
  const parts: string[] = []
  if (policy.subjects?.roles?.length) {
    parts.push(`角色: ${policy.subjects.roles.map((item) => roleMap[item] || item).join(', ')}`)
  }
  if (policy.subjects?.teams?.length) {
    parts.push(`组织: ${policy.subjects.teams.map((item) => teamMap[item] || item).join(', ')}`)
  }
  if (policy.subjects?.users?.length) parts.push(`用户: ${policy.subjects.users.join(', ')}`)
  if (policy.subjects?.tags?.length) parts.push(`标签: ${policy.subjects.tags.join(', ')}`)
  return parts.join(' | ') || '全部主体'
}

export function buildPolicyTargetsSummary(policy: AccessPolicy, teamMap: Record<string, string>) {
  const parts: string[] = []
  if (policy.clusters?.environments?.length)
    parts.push(`环境: ${policy.clusters.environments.join(', ')}`)
  if (policy.clusters?.regions?.length) parts.push(`地域: ${policy.clusters.regions.join(', ')}`)
  if (policy.clusters?.ids?.length) parts.push(`集群: ${policy.clusters.ids.join(', ')}`)
  if (policy.namespaces?.names?.length)
    parts.push(`命名空间: ${policy.namespaces.names.join(', ')}`)
  if (policy.namespaces?.ownerTeams?.length) {
    parts.push(
      `归属组织: ${policy.namespaces.ownerTeams.map((item) => teamMap[item] || item).join(', ')}`,
    )
  }
  if (policy.resources?.kinds?.length) parts.push(`资源: ${policy.resources.kinds.join(', ')}`)
  if (policy.resources?.names?.length) parts.push(`对象: ${policy.resources.names.join(', ')}`)
  return parts.join(' | ') || '全部资源'
}
