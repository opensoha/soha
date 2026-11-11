import { describe, expect, it } from 'vitest'
import { systemKeys, systemMutationKeys } from './keys'

describe('systemKeys', () => {
  it('keeps online-user queries and mutations under one canonical root', () => {
    expect(systemKeys.sessions.list()).toEqual(['online-users', 'list'])
    expect(systemMutationKeys.sessions('revoke')).toEqual(['online-users', 'mutation', 'revoke'])
  })

  it('normalizes audit and operation filters for stable cache identity', () => {
    expect(
      systemKeys.audit.list('identity', {
        action: ' login ',
        limit: 8,
        result: '',
        metadataValue: ' user-1 ',
      }),
    ).toEqual(['audit-logs', 'identity', { action: 'login', limit: '8', metadataValue: 'user-1' }])
    expect(systemKeys.operationLogs.list({ operationType: ' apply ', metadataKey: ' ' })).toEqual([
      'operation-logs',
      { operationType: 'apply' },
    ])
  })
})
