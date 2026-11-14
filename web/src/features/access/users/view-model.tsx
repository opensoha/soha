import { Space } from 'antd'
import { MetadataTag } from '@/components/status-tag'
import type { DataNode } from 'antd/es/tree'
import type { AccessTeam, AccessUser } from '../shared/types'
import { getOrganizationLabel, getOrganizationPathLabel } from '../shared/utils'

export const ORG_ALL_KEY = '__all-organizations__'

export function buildOrganizationUserCounts(items: AccessTeam[], users: AccessUser[]) {
  const teamsByID = new Map(items.map((item) => [item.id, item]))
  const userIDsByOrganization = new Map(items.map((item) => [item.id, new Set<string>()]))
  users.forEach((user) => {
    new Set(user.teams ?? []).forEach((teamID) => {
      const visited = new Set<string>()
      let currentID: string | undefined = teamID
      while (currentID && !visited.has(currentID)) {
        visited.add(currentID)
        userIDsByOrganization.get(currentID)?.add(user.id)
        currentID = teamsByID.get(currentID)?.parentId
      }
    })
  })
  const counts = new Map(
    Array.from(userIDsByOrganization, ([organizationID, userIDs]) => [
      organizationID,
      userIDs.size,
    ]),
  )
  counts.set(ORG_ALL_KEY, new Set(users.map((user) => user.id)).size)
  return counts
}

export function buildOrganizationTree(
  items: AccessTeam[],
  userCountByOrg: Map<string, number>,
): DataNode[] {
  const nodes = new Map<string, DataNode & { children?: DataNode[] }>()
  const roots: Array<DataNode & { children?: DataNode[] }> = []
  const sortedItems = [...items].sort((left, right) => {
    const pathCompare = getOrganizationPathLabel(left).localeCompare(
      getOrganizationPathLabel(right),
    )
    if (pathCompare !== 0) return pathCompare
    return getOrganizationLabel(left).localeCompare(getOrganizationLabel(right))
  })

  sortedItems.forEach((item) => {
    nodes.set(item.id, {
      key: item.id,
      title: (
        <Space size={6} className="soha-org-tree-title">
          <span>{getOrganizationLabel(item)}</span>
          <MetadataTag label={userCountByOrg.get(item.id) ?? item.userCount ?? 0} />
        </Space>
      ),
      children: [],
    })
  })
  sortedItems.forEach((item) => {
    const node = nodes.get(item.id)
    if (!node) return
    const parent = item.parentId ? nodes.get(item.parentId) : null
    if (parent) {
      parent.children = [...(parent.children ?? []), node]
      return
    }
    roots.push(node)
  })

  const trimEmptyChildren = (node: DataNode & { children?: DataNode[] }): DataNode => {
    const children = (node.children ?? []).map((child) =>
      trimEmptyChildren(child as DataNode & { children?: DataNode[] }),
    )
    return children.length ? { ...node, children } : { ...node, children: undefined }
  }

  return [
    {
      key: ORG_ALL_KEY,
      title: (
        <Space size={6} className="soha-org-tree-title">
          <span>全部组织</span>
          <MetadataTag label={userCountByOrg.get(ORG_ALL_KEY) ?? 0} />
        </Space>
      ),
      children: roots.map(trimEmptyChildren),
    },
  ]
}

export function organizationMatchesSelection(
  user: AccessUser,
  selectedOrgId: string,
  scopedOrganizationIds: Set<string>,
) {
  if (!selectedOrgId || selectedOrgId === ORG_ALL_KEY) return true
  return user.teams?.some((teamID) => scopedOrganizationIds.has(teamID)) ?? false
}
