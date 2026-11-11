import { describe, expect, it } from 'vitest'
import { getOrganizationDisplayPath } from './utils'
import type { AccessTeam } from './types'

const organization = (id: string, name: string, parentId?: string): AccessTeam => ({
  id,
  name,
  parentId,
  slug: `directory-${id}`,
  metadata: {},
  userCount: 0,
})

describe('getOrganizationDisplayPath', () => {
  it('builds a readable breadcrumb from organization names', () => {
    const items = [
      organization('root', '客服中心'),
      organization('service', '服务部', 'root'),
      organization('complaint', '客诉组', 'service'),
    ]
    expect(getOrganizationDisplayPath(items, items[2])).toBe('/客服中心/服务部/客诉组')
  })

  it('stops safely when malformed hierarchy data contains a cycle', () => {
    const items = [organization('a', 'A', 'b'), organization('b', 'B', 'a')]
    expect(getOrganizationDisplayPath(items, items[0])).toBe('/B/A')
  })
})
