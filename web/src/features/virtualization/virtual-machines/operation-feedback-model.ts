import { isStaleVirtualMachine, virtualMachineDisplayStatus } from '../virtualization-model'
import type { VirtualMachine, VirtualizationOperation } from '../virtualization-types'

export function vmOperationState(status?: string) {
  switch (status) {
    case 'queued':
      return 'queued'
    case 'running':
    case 'canceling':
      return 'active'
    case 'completed':
      return 'success'
    case 'failed':
      return 'error'
    case 'callback_timeout':
      return 'uncertain'
    case 'canceled':
      return 'canceled'
    default:
      return 'unknown'
  }
}

export function vmOperationBlocksPower(operation?: VirtualizationOperation) {
  if (!operation) return false
  return ['queued', 'active', 'uncertain', 'unknown'].includes(vmOperationState(operation.status))
}

export function vmRecordMatchesOperation(operation: VirtualizationOperation, vm?: VirtualMachine) {
  return operation.status === 'completed' && vmRecordMatchesAction(operation, vm)
}

export function vmRecordMatchesAction(operation: VirtualizationOperation, vm?: VirtualMachine) {
  if (!vm || isStaleVirtualMachine(vm)) return false
  const target =
    operation.action === 'start' || operation.action === 'restart'
      ? 'running'
      : operation.action === 'stop' || operation.action === 'shutdown'
        ? 'stopped'
        : ''
  const completedAt = Date.parse(
    operation.completedAt || operation.updatedAt || operation.createdAt || '',
  )
  const updatedAt = Date.parse(vm.updatedAt || '')
  // Core may predict powerState after a command; this confirms only its record, not Provider readiness.
  return (
    Boolean(target) &&
    virtualMachineDisplayStatus(vm) === target &&
    Number.isFinite(completedAt) &&
    Number.isFinite(updatedAt) &&
    updatedAt >= completedAt
  )
}
