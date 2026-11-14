import { describe, expect, it } from 'vitest'
import type { AccessTeam, AccessUser } from '../shared/types'
import { buildOrganizationUserCounts, ORG_ALL_KEY } from './view-model'

const team = (id: string, parentId?: string): AccessTeam => ({
  id,
  parentId,
  name: id,
  slug: id,
  metadata: {},
  userCount: 0,
})

const user = (id: string, teams: string[]): AccessUser => ({
  id,
  username: id,
  displayName: id,
  email: `${id}@example.com`,
  status: 'active',
  tags: [],
  roles: [],
  teams,
  projects: [],
  loginSources: [],
})

describe('buildOrganizationUserCounts', () => {
  it('rolls members up through every ancestor', () => {
    const items = [
      team('information'),
      team('foundation', 'information'),
      team('ops', 'foundation'),
    ]
    const counts = buildOrganizationUserCounts(items, [user('u1', ['ops'])])
    expect(counts.get('information')).toBe(1)
    expect(counts.get('foundation')).toBe(1)
    expect(counts.get('ops')).toBe(1)
    expect(counts.get(ORG_ALL_KEY)).toBe(1)
  })

  it('counts unassigned users and users with unavailable organizations in the unfiltered scope', () => {
    const counts = buildOrganizationUserCounts(
      [team('known')],
      [user('unassigned', []), user('unknown', ['missing']), user('known', ['known'])],
    )
    expect(counts.get(ORG_ALL_KEY)).toBe(3)
    expect(counts.get('known')).toBe(1)
  })

  it('deduplicates a user assigned to multiple descendants of the same parent', () => {
    const items = [team('parent'), team('left', 'parent'), team('right', 'parent')]
    const counts = buildOrganizationUserCounts(items, [user('u1', ['left', 'right'])])
    expect(counts.get('parent')).toBe(1)
    expect(counts.get('left')).toBe(1)
    expect(counts.get('right')).toBe(1)
    expect(counts.get(ORG_ALL_KEY)).toBe(1)
  })
})
