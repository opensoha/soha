import { describe, expect, it } from 'vitest'
import {
  identityOutpostKeys,
  identityOutpostMutationKeys,
  normalizeIdentityOutpostFilters,
} from './keys'

describe('identity outpost query keys', () => {
  it('uses stable hierarchical list and detail prefixes', () => {
    expect(identityOutpostKeys.lists()).toEqual(['identity', 'outposts', 'list'])
    expect(identityOutpostKeys.list({ mode: 'embedded', status: 'offline' })).toEqual([
      'identity',
      'outposts',
      'list',
      { mode: 'embedded', status: 'offline' },
    ])
    expect(identityOutpostKeys.detail(' edge/id ')).toEqual([
      'identity',
      'outposts',
      'detail',
      'edge/id',
    ])
  })

  it('normalizes filters exactly as the API query serializer does', () => {
    expect(
      normalizeIdentityOutpostFilters({
        mode: '',
        status: '',
        limit: 25.9,
        offset: 0,
      }),
    ).toEqual({ limit: 25 })
    expect(identityOutpostKeys.list({ mode: '', status: '', offset: -1 })).toEqual(
      identityOutpostKeys.list(),
    )
    expect(identityOutpostKeys.list({ limit: 0.5, offset: 0.5 })).toEqual(
      identityOutpostKeys.list(),
    )
  })

  it('provides stable mutation keys', () => {
    expect(identityOutpostMutationKeys.create).toEqual([
      'identity',
      'outposts',
      'mutation',
      'create',
    ])
    expect(identityOutpostMutationKeys.update).toEqual([
      'identity',
      'outposts',
      'mutation',
      'update',
    ])
    expect(identityOutpostMutationKeys.remove).toEqual([
      'identity',
      'outposts',
      'mutation',
      'delete',
    ])
  })
})
