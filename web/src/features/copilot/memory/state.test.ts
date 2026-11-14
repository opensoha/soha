import { describe, expect, it } from 'vitest'
import { memoryDeleted, memorySyncLabel } from './state'
import type { MemoryRecord } from './types'

describe('memory synchronization presentation', () => {
  it('keeps immediate revocation distinct from external deletion completion', () => {
    const pending = { status: 'deleted', syncStatus: 'delete_pending' } as MemoryRecord
    expect(memoryDeleted(pending)).toBe(true)
    expect(memorySyncLabel(pending)).toContain('传播中')
    expect(memorySyncLabel({ ...pending, syncStatus: 'deleted' })).toBe('删除已传播')
    expect(memorySyncLabel({ ...pending, syncStatus: 'error' })).toContain('失败')
    expect(memoryDeleted({ ...pending, syncStatus: 'error' })).toBe(true)
  })
  it('does not claim legacy records were externally synchronized', () => {
    expect(memorySyncLabel({ status: 'active' } as MemoryRecord)).toContain('未提供同步状态')
  })
})

it('shows quarantined records truthfully without classifying normal local-only memory as isolated', () => {
  const local = { status: 'active', syncStatus: 'disabled' } as MemoryRecord
  expect(memorySyncLabel(local)).toBe('同步已关闭')
  expect(memorySyncLabel({ ...local, quarantined: true, quarantineReason: '来源待核对' })).toBe(
    '待核对（已隔离）：来源待核对',
  )
  expect(memoryDeleted({ ...local, quarantined: true })).toBe(false)
})
