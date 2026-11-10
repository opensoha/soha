import type { MemoryRecord } from './types'

export function memoryDeleted(record: MemoryRecord) {
  return (
    record.status === 'deleted' ||
    record.syncStatus === 'delete_pending' ||
    record.syncStatus === 'deleted'
  )
}
export function memorySyncLabel(record: MemoryRecord) {
  if (record.quarantined && !memoryDeleted(record))
    return `待核对（已隔离）${record.quarantineReason ? `：${record.quarantineReason}` : ''}`
  const labels = {
    pending: '保存中',
    synced: '已同步',
    delete_pending: '已禁用，删除传播中',
    deleted: '删除已传播',
    error: '同步失败，请刷新查看',
    disabled: '同步已关闭',
  }
  return record.syncStatus
    ? labels[record.syncStatus]
    : memoryDeleted(record)
      ? '已删除'
      : '历史记录，未提供同步状态'
}
