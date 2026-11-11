import { describe, expect, it } from 'vitest'
import {
  vmOperationBlocksPower,
  vmOperationState,
  vmRecordMatchesOperation,
  vmRecordMatchesAction,
} from './operation-feedback-model'

describe('VM operation feedback', () => {
  it('can match a newer resource record without declaring a queued task complete', () => {
    const operation = {
      id: 'op',
      action: 'start',
      status: 'queued',
      createdAt: '2026-10-13T01:00:00Z',
    }
    const vm = {
      id: 'vm',
      name: 'vm',
      status: 'running',
      powerState: 'running',
      updatedAt: operation.createdAt,
    }
    expect(vmRecordMatchesAction(operation, vm)).toBe(true)
    expect(vmRecordMatchesOperation(operation, vm)).toBe(false)
  })
  it.each(['queued', 'running', 'canceling', 'callback_timeout', 'provider_unknown', undefined])(
    'blocks conflicting power requests for %s',
    (status) => {
      expect(vmOperationBlocksPower({ id: 'op', status })).toBe(true)
    },
  )
  it.each(['completed', 'failed', 'canceled'])(
    'releases terminal task controls for %s',
    (status) => {
      expect(vmOperationBlocksPower({ id: 'op', status })).toBe(false)
    },
  )
  it('keeps unknown, canceled and timed out tasks distinct from success', () => {
    expect(vmOperationState('unrecognized')).toBe('unknown')
    expect(vmOperationState('canceled')).toBe('canceled')
    expect(vmOperationState('callback_timeout')).toBe('uncertain')
  })
  it('requires a matching non-stale record at least as recent as the completed task', () => {
    const operation = {
      id: 'op',
      action: 'start',
      status: 'completed',
      completedAt: '2026-10-13T01:00:00Z',
    }
    const vm = {
      id: 'vm',
      name: 'vm',
      status: 'running',
      powerState: 'running',
      updatedAt: operation.completedAt,
    }
    expect(vmRecordMatchesOperation(operation, vm)).toBe(true)
    expect(vmRecordMatchesOperation(operation, { ...vm, status: 'stale' })).toBe(false)
    expect(vmRecordMatchesOperation(operation, { ...vm, updatedAt: '2026-10-12T01:00:00Z' })).toBe(
      false,
    )
    expect(vmRecordMatchesOperation(operation, { ...vm, updatedAt: undefined })).toBe(false)
    expect(vmRecordMatchesOperation(operation, { ...vm, powerState: 'stopped' })).toBe(false)
    expect(vmRecordMatchesOperation({ ...operation, action: 'delete' }, vm)).toBe(false)
  })
})
